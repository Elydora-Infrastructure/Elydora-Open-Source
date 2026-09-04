import os from 'node:os';
import path from 'node:path';
import { sameAgentId, samePath } from './common.js';
import {
  encodedWindowsCommand,
  isNodeExecutable,
  parseEncodedWindowsCommand,
  parseLegacyWindowsCommand,
  parsePosixCommand,
  posixSource,
} from './shell-command.js';
import { isObject, parseStrictJsonObject, type JsonObject } from './strict-json.js';

export const AGENT_KEY = 'codex';
export const CONFIG_FILE = 'hooks.json';
export const GUARD_SCRIPT = 'guard.js';
export const AUDIT_SCRIPT = 'hook.js';
export const HOOK_TIMEOUT_SECONDS = 10;
export const OWNED_DESCRIPTION = 'Elydora audit and freeze enforcement';
export const GUARD_STATUS = 'Checking Elydora agent state';
export const AUDIT_STATUS = 'Recording Elydora tool use';

export type CodexHooks = Record<string, JsonObject[]>;

export interface CodexDocument {
  readonly exists: boolean;
  readonly filePath: string;
  readonly root: JsonObject;
  readonly hooks: CodexHooks;
  readonly raw?: string;
}

export interface RenderedDocument {
  readonly document: CodexDocument;
  readonly changed: boolean;
  readonly next?: string;
}

export interface RuntimeContract {
  readonly agentId: string;
  readonly guardPath: string;
  readonly auditPath: string;
}

export function buildHandler(scriptPath: string, statusMessage: string): JsonObject {
  return {
    type: 'command',
    command: posixSource(scriptPath),
    commandWindows: encodedWindowsCommand(scriptPath),
    timeout: HOOK_TIMEOUT_SECONDS,
    statusMessage,
  };
}

function exactHandlerKeys(handler: JsonObject): boolean {
  return Object.keys(handler).sort().join('|')
    === 'command|commandWindows|statusMessage|timeout|type';
}

function managedScriptPath(handler: JsonObject, statusMessage: string): string | undefined {
  if (!exactHandlerKeys(handler)
    || handler.type !== 'command'
    || handler.timeout !== HOOK_TIMEOUT_SECONDS
    || handler.statusMessage !== statusMessage) return undefined;
  if (typeof handler.command !== 'string' || typeof handler.commandWindows !== 'string') {
    return undefined;
  }
  const posix = parsePosixCommand(handler.command);
  const windows = parseEncodedWindowsCommand(handler.commandWindows)
    ?? parseLegacyWindowsCommand(handler.commandWindows);
  if (!posix || !windows
    || !path.isAbsolute(posix[0])
    || !path.isAbsolute(posix[1])
    || !isNodeExecutable(posix[0])
    || !isNodeExecutable(windows[0])
    || !samePath(posix[0], windows[0])
    || !samePath(posix[1], windows[1])) return undefined;
  return posix[1];
}

function managedAgentId(
  handler: JsonObject,
  scriptName: string,
  statusMessage: string,
): string | undefined {
  const scriptPath = managedScriptPath(handler, statusMessage);
  if (!scriptPath || path.basename(scriptPath) !== scriptName) return undefined;
  const agentDirectory = path.dirname(scriptPath);
  if (!samePath(path.dirname(agentDirectory), path.join(os.homedir(), '.elydora'))) {
    return undefined;
  }
  const agentId = path.basename(agentDirectory);
  return agentId && agentId !== '.' && agentId !== '..' ? agentId : undefined;
}

function readHooks(value: unknown, label: string): CodexHooks {
  if (value === undefined) return {};
  if (!isObject(value)) throw new Error(`${label} field "hooks" must be an object`);
  const hooks: CodexHooks = {};
  for (const [event, candidate] of Object.entries(value)) {
    if (!Array.isArray(candidate)) {
      throw new Error(`${label} field "hooks.${event}" must be an array`);
    }
    hooks[event] = candidate.map((group, groupIndex) => {
      if (!isObject(group)) {
        throw new Error(`${label} matcher group hooks.${event}[${groupIndex}] must be an object`);
      }
      if (!Array.isArray(group.hooks)) {
        throw new Error(
          `${label} matcher group hooks.${event}[${groupIndex}] must contain a hooks array`,
        );
      }
      for (const [handlerIndex, handler] of group.hooks.entries()) {
        if (!isObject(handler)) {
          throw new Error(
            `${label} handler hooks.${event}[${groupIndex}].hooks[${handlerIndex}] must be an object`,
          );
        }
      }
      return group;
    });
  }
  return hooks;
}

export function parseDocument(filePath: string, raw: string): CodexDocument {
  const label = `Codex user hooks at ${filePath}`;
  const root = parseStrictJsonObject(raw, label);
  return {
    exists: true,
    filePath,
    root,
    hooks: readHooks(root.hooks, label),
    raw,
  };
}

export function createDocument(filePath: string): CodexDocument {
  return {
    exists: false,
    filePath,
    root: { description: OWNED_DESCRIPTION },
    hooks: {},
  };
}

function removeFromGroups(
  groups: JsonObject[],
  scriptName: string,
  statusMessage: string,
  agentId?: string,
): JsonObject[] {
  const result: JsonObject[] = [];
  for (const group of groups) {
    const handlers = (group.hooks as JsonObject[]).filter((handler) => {
      const managedId = managedAgentId(handler, scriptName, statusMessage);
      return !managedId || (agentId !== undefined && !sameAgentId(managedId, agentId));
    });
    if (handlers.length > 0) result.push({ ...group, hooks: handlers });
    else if (!exactMatcherGroup(group)) result.push({ ...group, hooks: [] });
  }
  return result;
}

export function removeManagedHooks(hooks: CodexHooks, agentId?: string): CodexHooks {
  const next: CodexHooks = Object.fromEntries(
    Object.entries(hooks).map(([event, groups]) => [event, [...groups]]),
  );
  for (const [event, scriptName, statusMessage] of [
    ['PreToolUse', GUARD_SCRIPT, GUARD_STATUS],
    ['PostToolUse', AUDIT_SCRIPT, AUDIT_STATUS],
  ] as const) {
    const groups = removeFromGroups(next[event] ?? [], scriptName, statusMessage, agentId);
    if (groups.length > 0) next[event] = groups;
    else delete next[event];
  }
  return next;
}

function entirelyManaged(document: CodexDocument): boolean {
  if (!document.exists
    || document.root.description !== OWNED_DESCRIPTION
    || !Object.keys(document.root).every((key) => key === 'description' || key === 'hooks')) {
    return false;
  }
  const events = Object.entries(document.hooks);
  if (events.length === 0) return false;
  let handlerCount = 0;
  for (const [event, groups] of events) {
    const contract = event === 'PreToolUse'
      ? [GUARD_SCRIPT, GUARD_STATUS] as const
      : event === 'PostToolUse'
        ? [AUDIT_SCRIPT, AUDIT_STATUS] as const
        : undefined;
    if (!contract || groups.length === 0) return false;
    for (const group of groups) {
      const handlers = group.hooks as JsonObject[];
      if (!exactMatcherGroup(group)
        || handlers.length === 0
        || handlers.some((handler) => !managedAgentId(handler, contract[0], contract[1]))) {
        return false;
      }
      handlerCount += handlers.length;
    }
  }
  return handlerCount > 0;
}

export function renderDocument(
  document: CodexDocument,
  hooks: CodexHooks,
): RenderedDocument {
  if (!document.exists && Object.keys(hooks).length === 0) {
    return { document, changed: false };
  }
  if (Object.keys(hooks).length === 0 && entirelyManaged(document)) {
    return { document, changed: true };
  }
  const root: JsonObject = { ...document.root };
  if (Object.keys(hooks).length > 0) root.hooks = hooks;
  else delete root.hooks;
  const next = `${JSON.stringify(root, null, 2)}\n`;
  return { document, changed: next !== document.raw, next };
}

function exactMatcherGroup(group: JsonObject): boolean {
  return Object.keys(group).sort().join('|') === 'hooks|matcher' && group.matcher === '*';
}

function managedIds(
  groups: JsonObject[],
  scriptName: string,
  statusMessage: string,
): Map<string, string> {
  const result = new Map<string, string>();
  for (const group of groups) {
    if (!exactMatcherGroup(group)) continue;
    for (const handler of group.hooks as JsonObject[]) {
      const agentId = managedAgentId(handler, scriptName, statusMessage);
      if (!agentId) continue;
      const key = process.platform === 'win32' ? agentId.toLowerCase() : agentId;
      result.set(key, agentId);
    }
  }
  return result;
}

export function runtimeContracts(hooks: CodexHooks): RuntimeContract[] {
  const guards = managedIds(hooks.PreToolUse ?? [], GUARD_SCRIPT, GUARD_STATUS);
  const audits = managedIds(hooks.PostToolUse ?? [], AUDIT_SCRIPT, AUDIT_STATUS);
  const root = path.join(os.homedir(), '.elydora');
  return [...guards]
    .filter(([key]) => audits.has(key))
    .map(([, agentId]) => ({
      agentId,
      guardPath: path.join(root, agentId, GUARD_SCRIPT),
      auditPath: path.join(root, agentId, AUDIT_SCRIPT),
    }));
}
