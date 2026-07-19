import os from 'node:os';
import path from 'node:path';

export const AGENT_KEY = 'droid';
export const GUARD_SCRIPT = 'guard.js';
export const AUDIT_SCRIPT = 'hook.js';
export const HOOK_TIMEOUT_SECONDS = 10;
export const TOOL_EVENTS = ['PreToolUse', 'PostToolUse'] as const;

const EVENT_NAMES = new Set([
  'PreToolUse',
  'PostToolUse',
  'Notification',
  'UserPromptSubmit',
  'Stop',
  'SubagentStop',
  'PreCompact',
  'SessionStart',
  'SessionEnd',
]);
const FLAG_NAMES = new Set(['hooksDisabled', 'showHookOutput']);
const HANDLER_KEYS = ['command', 'timeout', 'type'];
const GROUP_KEYS = ['hooks', 'matcher'];

export type JsonObject = Record<string, unknown>;
export type ToolEvent = typeof TOOL_EVENTS[number];

export interface DroidHandler extends JsonObject {
  readonly type: 'command';
  readonly command: string;
  readonly timeout?: number;
}

export interface DroidGroup extends JsonObject {
  readonly matcher?: string;
  readonly commandRegex?: string;
  readonly hooks: DroidHandler[];
}

export interface DroidHookSettings extends JsonObject {
  readonly PreToolUse?: DroidGroup[];
  readonly PostToolUse?: DroidGroup[];
  readonly hooksDisabled?: boolean;
  readonly showHookOutput?: boolean;
}

export interface RuntimeContract {
  readonly agentId: string;
  readonly guardPath: string;
  readonly auditPath: string;
}

export interface ManagedRemoval {
  readonly event: string;
  readonly groupIndex: number;
  readonly handlerIndexes: number[];
  readonly removeGroup: boolean;
}

interface ParsedArgument {
  readonly value: string;
  readonly next: number;
}

export function isObject(value: unknown): value is JsonObject {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

export function hasOwn(value: JsonObject, key: string): boolean {
  return Object.prototype.hasOwnProperty.call(value, key);
}

function quotePosix(value: string): string {
  return `'${value.replaceAll("'", `'"'"'`)}'`;
}

function quoteWindows(value: string): string {
  return `"${value.replaceAll('"', '\\"')}"`;
}

export function buildCommand(scriptPath: string): string {
  const quote = process.platform === 'win32' ? quoteWindows : quotePosix;
  return `${quote(process.execPath)} ${quote(scriptPath)}`;
}

export function buildGroup(scriptPath: string): DroidGroup {
  return {
    matcher: '*',
    hooks: [{
      type: 'command',
      command: buildCommand(scriptPath),
      timeout: HOOK_TIMEOUT_SECONDS,
    }],
  };
}

function validateRegex(value: string, label: string, allowWildcard: boolean): void {
  if (allowWildcard && (value === '*' || value === '')) return;
  try {
    new RegExp(value);
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    throw new Error(`${label} must be a valid regular expression: ${message}`);
  }
}

function validateHandler(
  value: unknown,
  label: string,
  groupIndex: number,
  handlerIndex: number,
): DroidHandler {
  const location = `${label}[${groupIndex}].hooks[${handlerIndex}]`;
  if (!isObject(value)) throw new Error(`${location} must be an object`);
  if (value.type !== 'command') throw new Error(`${location} type must be "command"`);
  if (typeof value.command !== 'string') throw new Error(`${location} command must be a string`);
  if (value.timeout !== undefined
    && (typeof value.timeout !== 'number'
      || !Number.isFinite(value.timeout))) {
    throw new Error(`${location} timeout must be a finite number`);
  }
  return value as DroidHandler;
}

function validateGroup(value: unknown, label: string, groupIndex: number): DroidGroup {
  const location = `${label}[${groupIndex}]`;
  if (!isObject(value)) throw new Error(`${location} must be an object`);
  if (value.matcher !== undefined) {
    if (typeof value.matcher !== 'string') throw new Error(`${location} matcher must be a string`);
    validateRegex(value.matcher, `${location} matcher`, true);
  }
  if (value.commandRegex !== undefined) {
    if (typeof value.commandRegex !== 'string') {
      throw new Error(`${location} commandRegex must be a string`);
    }
    validateRegex(value.commandRegex, `${location} commandRegex`, false);
  }
  if (!Array.isArray(value.hooks)) throw new Error(`${location} must contain a hooks array`);
  const handlers = value.hooks.map(
    (handler, handlerIndex) => validateHandler(handler, label, groupIndex, handlerIndex),
  );
  return { ...value, hooks: handlers } as DroidGroup;
}

export function readHookSettings(value: unknown, label: string): DroidHookSettings {
  if (!isObject(value)) throw new Error(`${label} must contain a JSON object`);
  const settings: DroidHookSettings = { ...value };
  for (const [key, item] of Object.entries(value)) {
    if (FLAG_NAMES.has(key)) {
      if (typeof item !== 'boolean') throw new Error(`${label} field "${key}" must be a boolean`);
      continue;
    }
    if (!EVENT_NAMES.has(key)) throw new Error(`${label} contains unsupported field "${key}"`);
    if (!Array.isArray(item)) throw new Error(`${label} field "${key}" must be an array`);
    settings[key] = item.map(
      (group, groupIndex) => validateGroup(group, `${label} field "${key}"`, groupIndex),
    );
  }
  return settings;
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

function parseGeneratedCommand(command: string): readonly [string, string] | undefined {
  const readArgument = process.platform === 'win32' ? readWindowsArgument : readPosixArgument;
  const executable = readArgument(command, 0);
  if (!executable || command[executable.next] !== ' ') return undefined;
  const script = readArgument(command, executable.next + 1);
  if (!script || script.next !== command.length || !executable.value || !script.value) return undefined;
  return [executable.value, script.value];
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

export function managedAgentId(handler: DroidHandler, scriptName: string): string | undefined {
  if (Object.keys(handler).sort().join('\0') !== HANDLER_KEYS.join('\0')
    || handler.type !== 'command'
    || handler.timeout !== HOOK_TIMEOUT_SECONDS) return undefined;
  const parsed = parseGeneratedCommand(handler.command);
  if (!parsed || !samePath(parsed[0], process.execPath)) return undefined;
  const scriptPath = parsed[1];
  if (path.basename(scriptPath) !== scriptName) return undefined;
  const agentDirectory = path.dirname(scriptPath);
  if (!samePath(path.dirname(agentDirectory), path.join(os.homedir(), '.elydora'))) return undefined;
  const agentId = path.basename(agentDirectory);
  return agentId && agentId !== '.' && agentId !== '..' ? agentId : undefined;
}

function exactOwnedGroup(group: DroidGroup, managedIndexes: number[]): boolean {
  return Object.keys(group).sort().join('\0') === GROUP_KEYS.join('\0')
    && group.matcher === '*'
    && managedIndexes.length > 0
    && managedIndexes.length === group.hooks.length;
}

export function managedRemovals(
  settings: DroidHookSettings,
  agentId?: string,
): ManagedRemoval[] {
  const removals: ManagedRemoval[] = [];
  for (const [event, scriptName] of [
    ['PreToolUse', GUARD_SCRIPT],
    ['PostToolUse', AUDIT_SCRIPT],
  ] as const) {
    const groups = settings[event] ?? [];
    groups.forEach((group, groupIndex) => {
      const handlerIndexes = group.hooks.flatMap((handler, handlerIndex) => {
        const managedId = managedAgentId(handler, scriptName);
        return managedId && (agentId === undefined || sameAgentId(managedId, agentId))
          ? [handlerIndex]
          : [];
      });
      if (handlerIndexes.length > 0) {
        removals.push({
          event,
          groupIndex,
          handlerIndexes,
          removeGroup: exactOwnedGroup(group, handlerIndexes),
        });
      }
    });
  }
  return removals;
}

function managedIds(groups: DroidGroup[], scriptName: string): Set<string> {
  const ids = new Set<string>();
  for (const group of groups) {
    for (const handler of group.hooks) {
      const agentId = managedAgentId(handler, scriptName);
      if (agentId) ids.add(agentId);
    }
  }
  return ids;
}

export function runtimeContracts(settings: DroidHookSettings): RuntimeContract[] {
  const guards = managedIds(settings.PreToolUse ?? [], GUARD_SCRIPT);
  const audits = managedIds(settings.PostToolUse ?? [], AUDIT_SCRIPT);
  const root = path.join(os.homedir(), '.elydora');
  return [...guards]
    .filter((agentId) => [...audits].some((auditId) => sameAgentId(auditId, agentId)))
    .map((agentId) => ({
      agentId,
      guardPath: path.join(root, agentId, GUARD_SCRIPT),
      auditPath: path.join(root, agentId, AUDIT_SCRIPT),
    }));
}

export function mergeHookSettings(
  primary: DroidHookSettings | undefined,
  fallback: DroidHookSettings | undefined,
): DroidHookSettings {
  return { ...(fallback ?? {}), ...(primary ?? {}) };
}
