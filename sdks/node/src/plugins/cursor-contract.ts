import os from 'node:os';
import path from 'node:path';
import { isObject, parseStrictJsonObject, type JsonObject } from './strict-json.js';

export { isObject, parseStrictJsonObject };
export type { JsonObject };

export const AGENT_KEY = 'cursor';
export const CONFIG_FILE = 'hooks.json';
export const GUARD_SCRIPT = 'guard.js';
export const AUDIT_SCRIPT = 'hook.js';
export const HOOK_TIMEOUT_SECONDS = 10;

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

interface ParsedArgument {
  readonly value: string;
  readonly next: number;
}

export function samePath(left: string, right: string): boolean {
  const normalizedLeft = path.resolve(left);
  const normalizedRight = path.resolve(right);
  return process.platform === 'win32'
    ? normalizedLeft.toLowerCase() === normalizedRight.toLowerCase()
    : normalizedLeft === normalizedRight;
}

function sameAgentId(left: string, right: string): boolean {
  return process.platform === 'win32'
    ? left.toLowerCase() === right.toLowerCase()
    : left === right;
}

function quotePosix(value: string): string {
  return `'${value.replaceAll("'", `'"'"'`)}'`;
}

function quotePowerShell(value: string): string {
  return `'${value.replaceAll("'", "''")}'`;
}

export function buildHandler(scriptPath: string): JsonObject {
  const command = process.platform === 'win32'
    ? `& ${quotePowerShell(process.execPath)} ${quotePowerShell(scriptPath)}; exit $LASTEXITCODE`
    : `${quotePosix(process.execPath)} ${quotePosix(scriptPath)}`;
  return {
    command,
    timeout: HOOK_TIMEOUT_SECONDS,
    failClosed: true,
  };
}

function readPosixArgument(command: string, start: number): ParsedArgument | undefined {
  if (command[start] !== "'") return undefined;
  const apostrophe = `'"'"'`;
  let value = '';
  for (let index = start + 1; index < command.length;) {
    if (command.startsWith(apostrophe, index)) {
      value += "'";
      index += apostrophe.length;
      continue;
    }
    if (command[index] === "'") return { value, next: index + 1 };
    value += command[index];
    index += 1;
  }
  return undefined;
}

function parsePosixCommand(command: string): readonly [string, string] | undefined {
  const executable = readPosixArgument(command, 0);
  if (!executable || command[executable.next] !== ' ') return undefined;
  const script = readPosixArgument(command, executable.next + 1);
  if (!script || script.next !== command.length) return undefined;
  return [executable.value, script.value];
}

function readPowerShellArgument(command: string, start: number): ParsedArgument | undefined {
  if (command[start] !== "'") return undefined;
  let value = '';
  for (let index = start + 1; index < command.length; index += 1) {
    if (command[index] !== "'") {
      value += command[index];
      continue;
    }
    if (command[index + 1] === "'") {
      value += "'";
      index += 1;
      continue;
    }
    return { value, next: index + 1 };
  }
  return undefined;
}

function parsePowerShellCommand(command: string): readonly [string, string] | undefined {
  if (!command.startsWith('& ')) return undefined;
  const executable = readPowerShellArgument(command, 2);
  if (!executable || command[executable.next] !== ' ') return undefined;
  const script = readPowerShellArgument(command, executable.next + 1);
  if (!script || command.slice(script.next) !== '; exit $LASTEXITCODE') return undefined;
  return [executable.value, script.value];
}

function parseLegacyCommand(command: string): readonly [string, string] | undefined {
  const match = /^(node(?:\.exe)?)\s+"([^"\r\n]+)"$/i.exec(command);
  return match ? [match[1], match[2]] : undefined;
}

function isNodeExecutable(filePath: string): boolean {
  const basename = path.basename(filePath);
  return basename === 'node' || basename.toLowerCase() === 'node.exe';
}

function managedScriptPath(handler: JsonObject): string | undefined {
  const keys = Object.keys(handler);
  if (keys.length === 3
    && typeof handler.command === 'string'
    && handler.timeout === HOOK_TIMEOUT_SECONDS
    && handler.failClosed === true) {
    const parsed = process.platform === 'win32'
      ? parsePowerShellCommand(handler.command)
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
