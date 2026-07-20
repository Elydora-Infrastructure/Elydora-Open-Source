import os from 'node:os';
import path from 'node:path';
import type { JsonObject } from './strict-json.js';

export const AGENT_KEY = 'droid';
export const GUARD_SCRIPT = 'guard.js';
export const AUDIT_SCRIPT = 'hook.js';
export const HOOK_TIMEOUT_SECONDS = 10;
export const TOOL_EVENTS = ['PreToolUse', 'PostToolUse'] as const;

const HANDLER_KEYS = ['command', 'timeout', 'type'];
const GROUP_KEYS = ['hooks', 'matcher'];
const WINDOWS_EXIT_SUFFIX = '; exit $LASTEXITCODE';

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

export interface DroidHookMap extends JsonObject {
  readonly PreToolUse?: DroidGroup[];
  readonly PostToolUse?: DroidGroup[];
}

export interface RuntimeContract {
  readonly agentId: string;
  readonly guardPath: string;
  readonly auditPath: string;
}

export interface ManagedRemoval {
  readonly event: ToolEvent;
  readonly groupIndex: number;
  readonly handlerIndexes: number[];
  readonly removeGroup: boolean;
}

interface ParsedArgument {
  readonly value: string;
  readonly next: number;
}

function quotePosix(value: string): string {
  return `'${value.replaceAll("'", `'"'"'`)}'`;
}

function quotePowerShell(value: string): string {
  return `'${value.replaceAll("'", "''")}'`;
}

export function buildCommand(scriptPath: string): string {
  if (!path.isAbsolute(process.execPath) || !path.isAbsolute(scriptPath)) {
    throw new Error('Factory Droid hook commands require absolute executable and script paths');
  }
  return process.platform === 'win32'
    ? `& ${quotePowerShell(process.execPath)} ${quotePowerShell(scriptPath)}${WINDOWS_EXIT_SUFFIX}`
    : `${quotePosix(process.execPath)} ${quotePosix(scriptPath)}`;
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
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    throw new Error(`${location} must be an object`);
  }
  const handler = value as JsonObject;
  if (handler.type !== 'command') throw new Error(`${location} type must be "command"`);
  if (typeof handler.command !== 'string' || handler.command.trim().length === 0) {
    throw new Error(`${location} command must be a non-empty string`);
  }
  if (handler.timeout !== undefined
    && (typeof handler.timeout !== 'number'
      || !Number.isFinite(handler.timeout)
      || handler.timeout <= 0)) {
    throw new Error(`${location} timeout must be a positive finite number`);
  }
  return handler as DroidHandler;
}

function validateGroup(value: unknown, label: string, groupIndex: number): DroidGroup {
  const location = `${label}[${groupIndex}]`;
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    throw new Error(`${location} must be an object`);
  }
  const group = value as JsonObject;
  if (group.matcher !== undefined) {
    if (typeof group.matcher !== 'string') throw new Error(`${location} matcher must be a string`);
    validateRegex(group.matcher, `${location} matcher`, true);
  }
  if (group.commandRegex !== undefined) {
    if (typeof group.commandRegex !== 'string') {
      throw new Error(`${location} commandRegex must be a string`);
    }
    validateRegex(group.commandRegex, `${location} commandRegex`, false);
  }
  if (!Array.isArray(group.hooks)) throw new Error(`${location} must contain a hooks array`);
  return {
    ...group,
    hooks: group.hooks.map(
      (handler, handlerIndex) => validateHandler(handler, label, groupIndex, handlerIndex),
    ),
  } as DroidGroup;
}

export function readHookMap(value: unknown, label: string): DroidHookMap {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    throw new Error(`${label} must contain a JSON object`);
  }
  const hooks: DroidHookMap = {};
  for (const [event, groups] of Object.entries(value)) {
    if (!Array.isArray(groups)) throw new Error(`${label} field "${event}" must be an array`);
    hooks[event] = groups.map(
      (group, groupIndex) => validateGroup(group, `${label} field "${event}"`, groupIndex),
    );
  }
  return hooks;
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

function parseTwoArguments(
  command: string,
  start: number,
  readArgument: (command: string, start: number) => ParsedArgument | undefined,
): readonly [string, string] | undefined {
  const executable = readArgument(command, start);
  if (!executable || command[executable.next] !== ' ') return undefined;
  const script = readArgument(command, executable.next + 1);
  if (!script || script.next !== command.length || !executable.value || !script.value) return undefined;
  return [executable.value, script.value];
}

function parseLegacyWindowsCommand(command: string): readonly [string, string] | undefined {
  const match = /^"([^"\r\n]+)" "([^"\r\n]+)"$/.exec(command);
  return match ? [match[1], match[2]] : undefined;
}

function parsePowerShellCommand(command: string): readonly [string, string] | undefined {
  if (!command.startsWith('& ')) return undefined;
  const executable = readPowerShellArgument(command, 2);
  if (!executable || command[executable.next] !== ' ') return undefined;
  const script = readPowerShellArgument(command, executable.next + 1);
  if (!script
    || command.slice(script.next) !== WINDOWS_EXIT_SUFFIX
    || !executable.value
    || !script.value) return undefined;
  return [executable.value, script.value];
}

function parseGeneratedCommand(
  command: string,
  includeLegacy: boolean,
): readonly [string, string] | undefined {
  if (process.platform !== 'win32') return parseTwoArguments(command, 0, readPosixArgument);
  const current = parsePowerShellCommand(command);
  return current ?? (includeLegacy ? parseLegacyWindowsCommand(command) : undefined);
}

export function samePath(left: string, right: string): boolean {
  const normalizedLeft = path.resolve(left);
  const normalizedRight = path.resolve(right);
  return process.platform === 'win32'
    ? normalizedLeft.toLowerCase() === normalizedRight.toLowerCase()
    : normalizedLeft === normalizedRight;
}

export function sameAgentId(left: string, right: string): boolean {
  return process.platform === 'win32' ? left.toLowerCase() === right.toLowerCase() : left === right;
}

export function managedAgentId(
  handler: DroidHandler,
  scriptName: string,
  includeLegacy = false,
): string | undefined {
  if (Object.keys(handler).sort().join('\0') !== HANDLER_KEYS.join('\0')
    || handler.type !== 'command'
    || handler.timeout !== HOOK_TIMEOUT_SECONDS) return undefined;
  const parsed = parseGeneratedCommand(handler.command, includeLegacy);
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

export function managedRemovals(hooks: DroidHookMap, agentId?: string): ManagedRemoval[] {
  const removals: ManagedRemoval[] = [];
  for (const [event, scriptName] of [
    ['PreToolUse', GUARD_SCRIPT],
    ['PostToolUse', AUDIT_SCRIPT],
  ] as const) {
    const groups = hooks[event] ?? [];
    groups.forEach((group, groupIndex) => {
      const handlerIndexes = group.hooks.flatMap((handler, handlerIndex) => {
        const managedId = managedAgentId(handler, scriptName, true);
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

function configuredIds(groups: DroidGroup[], scriptName: string): Set<string> {
  const ids = new Set<string>();
  for (const group of groups) {
    if (Object.keys(group).sort().join('\0') !== GROUP_KEYS.join('\0')
      || group.matcher !== '*'
      || group.hooks.length !== 1) continue;
    const agentId = managedAgentId(group.hooks[0], scriptName);
    if (agentId) ids.add(agentId);
  }
  return ids;
}

export function runtimeContracts(hooks: DroidHookMap): RuntimeContract[] {
  const guards = configuredIds(hooks.PreToolUse ?? [], GUARD_SCRIPT);
  const audits = configuredIds(hooks.PostToolUse ?? [], AUDIT_SCRIPT);
  const root = path.join(os.homedir(), '.elydora');
  return [...guards]
    .filter((agentId) => [...audits].some((auditId) => sameAgentId(auditId, agentId)))
    .map((agentId) => ({
      agentId,
      guardPath: path.join(root, agentId, GUARD_SCRIPT),
      auditPath: path.join(root, agentId, AUDIT_SCRIPT),
    }));
}
