import os from 'node:os';
import path from 'node:path';
import { sameAgentId, samePath } from './common.js';
import type { GuardScriptOptions } from './guard-template.js';
import type { HookScriptOptions } from './hook-template.js';
import {
  isNodeExecutable,
  parsePosixCommand,
  parsePowerShellSource,
  posixSource,
  powerShellSource,
} from './shell-command.js';
import { isObject, parseStrictJsonObject, type JsonObject } from './strict-json.js';

export const AGENT_KEY = 'cursor';
export const CONFIG_FILE = 'hooks.json';
export const GUARD_SCRIPT = 'guard.js';
export const AUDIT_SCRIPT = 'hook.js';
export const HOOK_TIMEOUT_SECONDS = 10;
export const GUARD_OPTIONS: GuardScriptOptions = {
  failClosed: true,
  successOutput: '{"permission":"allow"}\n',
  denyProtocol: 'cursor',
};
export const AUDIT_OPTIONS: HookScriptOptions = {
  failClosed: true,
  nativePayload: true,
  successOutput: '{}\n',
};

export type CursorHooks = Record<string, JsonObject[]>;

export interface CursorDocument {
  readonly exists: boolean;
  readonly filePath: string;
  readonly root: JsonObject;
  readonly hooks: CursorHooks;
  readonly raw?: string;
}

export interface RenderedDocument {
  readonly document: CursorDocument;
  readonly changed: boolean;
  readonly next?: string;
}

export interface RuntimeContract {
  readonly agentId: string;
  readonly guardPath: string;
  readonly auditPath: string;
}

export function buildHandler(scriptPath: string): JsonObject {
  const command = process.platform === 'win32'
    ? powerShellSource(scriptPath)
    : posixSource(scriptPath);
  return {
    command,
    timeout: HOOK_TIMEOUT_SECONDS,
    failClosed: true,
  };
}

function parseLegacyCommand(command: string): readonly [string, string] | undefined {
  const match = /^(node(?:\.exe)?)\s+"([^"\r\n]+)"$/i.exec(command);
  return match ? [match[1], match[2]] : undefined;
}

function managedScriptPath(handler: JsonObject): string | undefined {
  const keys = Object.keys(handler);
  if (keys.length === 3
    && typeof handler.command === 'string'
    && handler.timeout === HOOK_TIMEOUT_SECONDS
    && handler.failClosed === true) {
    const parsed = process.platform === 'win32'
      ? parsePowerShellSource(handler.command)
      : parsePosixCommand(handler.command);
    if (parsed
      && path.isAbsolute(parsed[0])
      && path.isAbsolute(parsed[1])
      && isNodeExecutable(parsed[0])) return parsed[1];
  }
  if (keys.length !== 1 || typeof handler.command !== 'string') return undefined;
  const legacy = parseLegacyCommand(handler.command);
  return legacy && isNodeExecutable(legacy[0]) && path.isAbsolute(legacy[1])
    ? legacy[1]
    : undefined;
}

function managedAgentId(handler: JsonObject, scriptName: string): string | undefined {
  const scriptPath = managedScriptPath(handler);
  if (!scriptPath || path.basename(scriptPath) !== scriptName) return undefined;
  const agentDirectory = path.dirname(scriptPath);
  if (!samePath(path.dirname(agentDirectory), path.join(os.homedir(), '.elydora'))) {
    return undefined;
  }
  const agentId = path.basename(agentDirectory);
  return agentId && agentId !== '.' && agentId !== '..' ? agentId : undefined;
}

function readHooks(value: unknown, label: string): CursorHooks {
  if (value === undefined) return {};
  if (!isObject(value)) throw new Error(`${label} field "hooks" must be an object`);
  const hooks: CursorHooks = {};
  for (const [event, candidate] of Object.entries(value)) {
    if (!Array.isArray(candidate)) {
      throw new Error(`${label} field "hooks.${event}" must be an array`);
    }
    hooks[event] = candidate.map((handler, index) => {
      if (!isObject(handler)) {
        throw new Error(`${label} handler hooks.${event}[${index}] must be an object`);
      }
      return handler;
    });
  }
  return hooks;
}

export function parseDocument(filePath: string, raw: string): CursorDocument {
  const label = `Cursor user hooks at ${filePath}`;
  const root = parseStrictJsonObject(raw, label);
  const hooks = readHooks(root.hooks, label);
  const hasVersion = Object.prototype.hasOwnProperty.call(root, 'version');
  if (root.version !== 1 && (hasVersion || !containsManagedHook(hooks))) {
    throw new Error(`${label} must declare version 1`);
  }
  return {
    exists: true,
    filePath,
    root,
    hooks,
    raw,
  };
}

export function createDocument(filePath: string): CursorDocument {
  return { exists: false, filePath, root: {}, hooks: {} };
}

export function removeManagedHooks(hooks: CursorHooks, agentId?: string): CursorHooks {
  const next: CursorHooks = Object.fromEntries(
    Object.entries(hooks).map(([event, handlers]) => [event, [...handlers]]),
  );
  for (const [event, scriptName] of [
    ['preToolUse', GUARD_SCRIPT],
    ['postToolUse', AUDIT_SCRIPT],
    ['postToolUseFailure', AUDIT_SCRIPT],
  ] as const) {
    const handlers = (next[event] ?? []).filter((handler) => {
      const managedId = managedAgentId(handler, scriptName);
      return !managedId || (agentId !== undefined && !sameAgentId(managedId, agentId));
    });
    if (handlers.length > 0) next[event] = handlers;
    else delete next[event];
  }
  return next;
}

function entirelyManaged(document: CursorDocument): boolean {
  if (!document.exists
    || !Object.keys(document.root).every((key) => key === 'version' || key === 'hooks')) {
    return false;
  }
  const events = Object.entries(document.hooks);
  if (events.length === 0) return false;
  let handlerCount = 0;
  for (const [event, handlers] of events) {
    const scriptName = event === 'preToolUse'
      ? GUARD_SCRIPT
      : event === 'postToolUse' || event === 'postToolUseFailure'
        ? AUDIT_SCRIPT
        : undefined;
    if (!scriptName || handlers.length === 0) return false;
    handlerCount += handlers.length;
    if (handlers.some((handler) => !managedAgentId(handler, scriptName))) return false;
  }
  return handlerCount > 0;
}

export function renderDocument(
  document: CursorDocument,
  hooks: CursorHooks,
): RenderedDocument {
  if (!document.exists && Object.keys(hooks).length === 0) {
    return { document, changed: false };
  }
  if (Object.keys(hooks).length === 0 && entirelyManaged(document)) {
    return { document, changed: true };
  }
  const root: JsonObject = { ...document.root, version: 1 };
  if (Object.keys(hooks).length > 0) root.hooks = hooks;
  else delete root.hooks;
  const next = `${JSON.stringify(root, null, 2)}\n`;
  return { document, changed: next !== document.raw, next };
}

function managedIds(handlers: JsonObject[], scriptName: string): Map<string, string> {
  const result = new Map<string, string>();
  for (const handler of handlers) {
    const agentId = managedAgentId(handler, scriptName);
    if (!agentId) continue;
    const key = process.platform === 'win32' ? agentId.toLowerCase() : agentId;
    result.set(key, agentId);
  }
  return result;
}

function containsManagedHook(hooks: CursorHooks): boolean {
  return [
    ...(hooks.preToolUse ?? []).map((handler) => managedAgentId(handler, GUARD_SCRIPT)),
    ...(hooks.postToolUse ?? []).map((handler) => managedAgentId(handler, AUDIT_SCRIPT)),
    ...(hooks.postToolUseFailure ?? []).map((handler) => managedAgentId(handler, AUDIT_SCRIPT)),
  ].some((agentId) => agentId !== undefined);
}

export function runtimeContracts(hooks: CursorHooks): RuntimeContract[] {
  const guards = managedIds(hooks.preToolUse ?? [], GUARD_SCRIPT);
  const audits = managedIds(hooks.postToolUse ?? [], AUDIT_SCRIPT);
  const failures = managedIds(hooks.postToolUseFailure ?? [], AUDIT_SCRIPT);
  const root = path.join(os.homedir(), '.elydora');
  return [...guards]
    .filter(([key]) => audits.has(key) && failures.has(key))
    .map(([, agentId]) => ({
      agentId,
      guardPath: path.join(root, agentId, GUARD_SCRIPT),
      auditPath: path.join(root, agentId, AUDIT_SCRIPT),
    }));
}
