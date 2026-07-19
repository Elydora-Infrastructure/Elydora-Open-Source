import os from 'node:os';
import path from 'node:path';

export const AGENT_KEY = 'augment';
export const GUARD_SCRIPT = 'guard.js';
export const AUDIT_SCRIPT = 'hook.js';
export const HOOK_TIMEOUT_MILLISECONDS = 10_000;

const WRAPPER_EXTENSION = process.platform === 'win32' ? '.cmd' : '.sh';
export const GUARD_WRAPPER = `augment-guard${WRAPPER_EXTENSION}`;
export const AUDIT_WRAPPER = `augment-hook${WRAPPER_EXTENSION}`;

const TOOL_EVENTS = new Set(['PreToolUse', 'PostToolUse']);
const SESSION_EVENTS = new Set([
  'Stop',
  'SessionStart',
  'SessionEnd',
  'Notification',
  'PromptSubmit',
]);

export type JsonObject = Record<string, unknown>;

export interface AugmentHandler extends JsonObject {
  readonly type: 'command';
  readonly command: string;
  readonly args?: string[];
  readonly timeout?: number;
}

export interface AugmentGroup extends JsonObject {
  readonly matcher?: string;
  readonly hooks: AugmentHandler[];
}

export type AugmentHooks = Record<string, AugmentGroup[]>;

export interface AugmentDocument {
  readonly exists: boolean;
  readonly configPath: string;
  readonly root: JsonObject;
  readonly hooks: AugmentHooks;
}

export interface RuntimeContract {
  readonly agentId: string;
  readonly guardPath: string;
  readonly auditPath: string;
  readonly guardWrapperPath: string;
  readonly auditWrapperPath: string;
}

interface ParsedArgument {
  readonly value: string;
  readonly next: number;
}

export function isObject(value: unknown): value is JsonObject {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function hasOwn(value: JsonObject, key: string): boolean {
  return Object.prototype.hasOwnProperty.call(value, key);
}

function quotePosix(value: string): string {
  return `'${value.replaceAll("'", `'"'"'`)}'`;
}

function quoteWindows(value: string): string {
  return `"${value.replaceAll('"', '\\"')}"`;
}

function quoteBatch(value: string): string {
  return `"${value.replaceAll('%', '%%')}"`;
}

function readWindowsArgument(command: string, start: number): ParsedArgument | undefined {
  if (command[start] !== '"') return undefined;
  let value = '';
  for (let index = start + 1; index < command.length; index += 1) {
    if (command[index] === '\\' && command[index + 1] === '"') {
      value += '"';
      index += 1;
      continue;
    }
    if (command[index] === '"') return { value, next: index + 1 };
    value += command[index];
  }
  return undefined;
}

const POSIX_APOSTROPHE = `'"'"'`;

function readPosixArgument(command: string, start: number): ParsedArgument | undefined {
  if (command[start] !== "'") return undefined;
  let value = '';
  for (let index = start + 1; index < command.length;) {
    if (command.startsWith(POSIX_APOSTROPHE, index)) {
      value += "'";
      index += POSIX_APOSTROPHE.length;
      continue;
    }
    if (command[index] === "'") return { value, next: index + 1 };
    value += command[index];
    index += 1;
  }
  return undefined;
}

function parseWrapperCommand(command: string): string | undefined {
  const readArgument = process.platform === 'win32' ? readWindowsArgument : readPosixArgument;
  const parsed = readArgument(command, 0);
  return parsed?.next === command.length && parsed.value ? parsed.value : undefined;
}

function samePath(left: string, right: string): boolean {
  const normalizedLeft = path.resolve(left);
  const normalizedRight = path.resolve(right);
  return process.platform === 'win32'
    ? normalizedLeft.toLowerCase() === normalizedRight.toLowerCase()
    : normalizedLeft === normalizedRight;
}

function sameAgentId(left: string, right: string): boolean {
  return process.platform === 'win32' ? left.toLowerCase() === right.toLowerCase() : left === right;
}

export function wrapperPaths(agentId: string): {
  readonly guardPath: string;
  readonly auditPath: string;
} {
  const agentDirectory = path.join(os.homedir(), '.elydora', agentId);
  return {
    guardPath: path.join(agentDirectory, GUARD_WRAPPER),
    auditPath: path.join(agentDirectory, AUDIT_WRAPPER),
  };
}

export function buildHandler(wrapperPath: string): AugmentHandler {
  const quote = process.platform === 'win32' ? quoteWindows : quotePosix;
  return {
    type: 'command',
    command: quote(wrapperPath),
    timeout: HOOK_TIMEOUT_MILLISECONDS,
  };
}

export function buildWrapper(runtimePath: string): string {
  if (process.platform === 'win32') {
    return `@echo off\r\n${quoteBatch(process.execPath)} ${quoteBatch(runtimePath)}\r\nexit /b %errorlevel%\r\n`;
  }
  return `#!/bin/sh\nexec ${quotePosix(process.execPath)} ${quotePosix(runtimePath)}\n`;
}

function managedAgentId(handler: AugmentHandler, wrapperName: string): string | undefined {
  if (handler.type !== 'command'
    || handler.timeout !== HOOK_TIMEOUT_MILLISECONDS
    || typeof handler.command !== 'string'
    || hasOwn(handler, 'args')) return undefined;
  const wrapperPath = parseWrapperCommand(handler.command);
  if (!wrapperPath) return undefined;
  const actualName = path.basename(wrapperPath);
  const namesMatch = process.platform === 'win32'
    ? actualName.toLowerCase() === wrapperName.toLowerCase()
    : actualName === wrapperName;
  if (!namesMatch) return undefined;
  const agentDirectory = path.dirname(wrapperPath);
  if (!samePath(path.dirname(agentDirectory), path.join(os.homedir(), '.elydora'))) return undefined;
  const agentId = path.basename(agentDirectory);
  return agentId && agentId !== '.' && agentId !== '..' ? agentId : undefined;
}

function validateHandler(
  value: unknown,
  event: string,
  groupIndex: number,
  handlerIndex: number,
): AugmentHandler {
  const label = `Auggie settings handler hooks.${event}[${groupIndex}].hooks[${handlerIndex}]`;
  if (!isObject(value)) throw new Error(`${label} must be an object`);
  if (value.type !== 'command') throw new Error(`${label} type must be "command"`);
  if (typeof value.command !== 'string' || value.command.length === 0) {
    throw new Error(`${label} requires a non-empty command`);
  }
  if (value.args !== undefined
    && (!Array.isArray(value.args) || !value.args.every((argument) => typeof argument === 'string'))) {
    throw new Error(`${label} args must be an array of strings`);
  }
  if (value.timeout !== undefined
    && (typeof value.timeout !== 'number' || !Number.isFinite(value.timeout) || value.timeout <= 0)) {
    throw new Error(`${label} timeout must be a positive finite number`);
  }
  return value as AugmentHandler;
}

function validateMetadata(value: unknown, label: string): void {
  if (!isObject(value)) throw new Error(`${label} metadata must be an object`);
  for (const key of ['includeConversationData', 'includeMCPMetadata', 'includeUserContext']) {
    if (value[key] !== undefined && typeof value[key] !== 'boolean') {
      throw new Error(`${label} metadata.${key} must be a boolean`);
    }
  }
}

function validateGroup(value: unknown, event: string, groupIndex: number): AugmentGroup {
  const label = `Auggie settings group hooks.${event}[${groupIndex}]`;
  if (!isObject(value)) throw new Error(`${label} must be an object`);
  if (SESSION_EVENTS.has(event) && hasOwn(value, 'matcher')) {
    throw new Error(`${label} matcher is only supported for tool events`);
  }
  if (value.matcher !== undefined) {
    if (typeof value.matcher !== 'string') throw new Error(`${label} matcher must be a string`);
    try {
      new RegExp(value.matcher);
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      throw new Error(`${label} matcher must be a valid regular expression: ${message}`);
    }
  }
  if (value.metadata !== undefined) validateMetadata(value.metadata, label);
  if (!Array.isArray(value.hooks)) throw new Error(`${label} must contain a hooks array`);
  const handlers = value.hooks.map(
    (handler, handlerIndex) => validateHandler(handler, event, groupIndex, handlerIndex),
  );
  return { ...value, hooks: handlers } as AugmentGroup;
}

export function readHooks(root: JsonObject): AugmentHooks {
  if (root.hooks === undefined) return {};
  if (!isObject(root.hooks)) throw new Error('Auggie settings field "hooks" must be an object');
  const hooks: AugmentHooks = {};
  for (const [event, value] of Object.entries(root.hooks)) {
    if (!TOOL_EVENTS.has(event) && !SESSION_EVENTS.has(event)) {
      throw new Error(`Auggie settings field "hooks.${event}" uses an unsupported event`);
    }
    if (!Array.isArray(value)) {
      throw new Error(`Auggie settings field "hooks.${event}" must be an array`);
    }
    hooks[event] = value.map((group, index) => validateGroup(group, event, index));
  }
  return hooks;
}

function removeManaged(
  groups: AugmentGroup[],
  wrapperName: string,
  agentId?: string,
): { readonly groups: AugmentGroup[]; readonly changed: boolean } {
  let changed = false;
  const result: AugmentGroup[] = [];
  for (const group of groups) {
    let groupChanged = false;
    const handlers = group.hooks.filter((handler) => {
      const managedId = managedAgentId(handler, wrapperName);
      const remove = managedId !== undefined
        && (agentId === undefined || sameAgentId(managedId, agentId));
      if (remove) {
        changed = true;
        groupChanged = true;
      }
      return !remove;
    });
    if (!groupChanged) result.push(group);
    else if (handlers.length > 0) result.push({ ...group, hooks: handlers });
  }
  return { groups: result, changed };
}

export function removeManagedHooks(hooks: AugmentHooks, agentId?: string): {
  readonly hooks: AugmentHooks;
  readonly changed: boolean;
} {
  const next = { ...hooks };
  let changed = false;
  for (const [event, wrapperName] of [
    ['PreToolUse', GUARD_WRAPPER],
    ['PostToolUse', AUDIT_WRAPPER],
  ] as const) {
    const result = removeManaged(next[event] ?? [], wrapperName, agentId);
    if (!result.changed) continue;
    changed = true;
    if (result.groups.length > 0) next[event] = result.groups;
    else delete next[event];
  }
  return { hooks: next, changed };
}

function managedIds(groups: AugmentGroup[], wrapperName: string): Set<string> {
  const ids = new Set<string>();
  for (const group of groups) {
    for (const handler of group.hooks) {
      const agentId = managedAgentId(handler, wrapperName);
      if (agentId) ids.add(agentId);
    }
  }
  return ids;
}

export function runtimeContracts(hooks: AugmentHooks): RuntimeContract[] {
  const guards = managedIds(hooks.PreToolUse ?? [], GUARD_WRAPPER);
  const audits = managedIds(hooks.PostToolUse ?? [], AUDIT_WRAPPER);
  const root = path.join(os.homedir(), '.elydora');
  return [...guards]
    .filter((agentId) => [...audits].some((auditId) => sameAgentId(auditId, agentId)))
    .map((agentId) => {
      const agentDirectory = path.join(root, agentId);
      return {
        agentId,
        guardPath: path.join(agentDirectory, GUARD_SCRIPT),
        auditPath: path.join(agentDirectory, AUDIT_SCRIPT),
        guardWrapperPath: path.join(agentDirectory, GUARD_WRAPPER),
        auditWrapperPath: path.join(agentDirectory, AUDIT_WRAPPER),
      };
    });
}
