import {
  buildQwenCommand,
  qwenRuntimeReference,
  sameQwenAgentId,
  sameQwenPath,
  type QwenRuntimeReference,
} from './qwen-command.js';
import { isObject, type JsonObject } from './strict-json.js';

export const AGENT_KEY = 'qwen';
export const GUARD_SCRIPT = 'guard.js';
export const AUDIT_SCRIPT = 'hook.js';
export const GUARD_HOOK_NAME = 'elydora-guard';
export const AUDIT_HOOK_NAME = 'elydora-audit';
export const HOOK_TIMEOUT_MS = 10_000;
export const MANAGED_EVENTS = [
  'PreToolUse',
  'PostToolUse',
  'PostToolUseFailure',
] as const;

const KNOWN_EVENTS = new Set([
  'PreToolUse',
  'PostToolUse',
  'PostToolUseFailure',
  'PostToolBatch',
  'Notification',
  'UserPromptSubmit',
  'UserPromptExpansion',
  'SessionStart',
  'Stop',
  'MessageDisplay',
  'SubagentStart',
  'SubagentStop',
  'PreCompact',
  'PostCompact',
  'SessionEnd',
  'PermissionRequest',
  'PermissionDenied',
  'StopFailure',
  'TodoCreated',
  'TodoCompleted',
  'InstructionsLoaded',
]);

const REGEX_MATCHER_EVENTS = new Set([
  'PreToolUse',
  'PostToolUse',
  'PostToolUseFailure',
  'PermissionRequest',
  'PermissionDenied',
  'SubagentStart',
  'SubagentStop',
  'PreCompact',
  'PostCompact',
  'SessionStart',
  'SessionEnd',
  'StopFailure',
  'Notification',
  'InstructionsLoaded',
  'UserPromptExpansion',
]);

export type ManagedQwenEvent = (typeof MANAGED_EVENTS)[number];

export interface QwenHandler extends JsonObject {
  readonly type: 'command' | 'http' | 'prompt';
  readonly command?: string;
  readonly name?: string;
  readonly shell?: 'bash' | 'powershell';
  readonly timeout?: number;
}

export interface QwenGroup extends JsonObject {
  readonly matcher?: string;
  readonly sequential?: boolean;
  readonly hooks: QwenHandler[];
}

export type QwenHooks = Record<string, QwenGroup[]>;

export interface QwenRuntimeContract {
  readonly agentId: string;
  readonly guardPath: string;
  readonly auditPath: string;
}

export interface ManagedRemoval {
  readonly event: ManagedQwenEvent;
  readonly groupIndex: number;
  readonly handlerIndexes: readonly number[];
  readonly removeGroup: boolean;
}

function optionalString(value: JsonObject, field: string, label: string): void {
  if (value[field] !== undefined && typeof value[field] !== 'string') {
    throw new Error(`${label} field "${field}" must be a string`);
  }
}

function optionalBoolean(value: JsonObject, field: string, label: string): void {
  if (value[field] !== undefined && typeof value[field] !== 'boolean') {
    throw new Error(`${label} field "${field}" must be a boolean`);
  }
}

function optionalStringMap(value: JsonObject, field: string, label: string): void {
  const item = value[field];
  if (item === undefined) return;
  if (!isObject(item) || Object.values(item).some((entry) => typeof entry !== 'string')) {
    throw new Error(`${label} field "${field}" must map names to strings`);
  }
}

function validateTimeout(value: unknown, label: string): void {
  if (value === undefined) return;
  if (typeof value !== 'number' || !Number.isFinite(value) || value < 0) {
    throw new Error(`${label} timeout must be a non-negative finite number`);
  }
}

function validateHandler(value: unknown, label: string): QwenHandler {
  if (!isObject(value)) throw new Error(`${label} must be an object`);
  if (!['command', 'http', 'prompt'].includes(String(value.type))) {
    throw new Error(`${label} has unsupported type "${String(value.type)}"`);
  }
  validateTimeout(value.timeout, label);
  for (const field of ['name', 'description', 'statusMessage', 'source']) {
    optionalString(value, field, label);
  }
  if (value.type === 'command') {
    if (typeof value.command !== 'string' || value.command.length === 0) {
      throw new Error(`${label} requires a non-empty command`);
    }
    optionalStringMap(value, 'env', label);
    optionalBoolean(value, 'async', label);
    if (value.shell !== undefined && !['bash', 'powershell'].includes(String(value.shell))) {
      throw new Error(`${label} shell must be "bash" or "powershell"`);
    }
  }
  if (value.type === 'http') {
    if (typeof value.url !== 'string' || value.url.length === 0) {
      throw new Error(`${label} requires a non-empty url`);
    }
    optionalStringMap(value, 'headers', label);
    optionalBoolean(value, 'once', label);
    optionalString(value, 'if', label);
    if (value.allowedEnvVars !== undefined && (
      !Array.isArray(value.allowedEnvVars)
      || value.allowedEnvVars.some((item) => typeof item !== 'string')
    )) {
      throw new Error(`${label} allowedEnvVars must be an array of strings`);
    }
  }
  if (value.type === 'prompt') {
    if (typeof value.prompt !== 'string' || value.prompt.length === 0) {
      throw new Error(`${label} requires a non-empty prompt`);
    }
    optionalString(value, 'model', label);
  }
  return value as QwenHandler;
}

function validateMatcher(matcher: string, event: string, label: string): void {
  if (!REGEX_MATCHER_EVENTS.has(event) || matcher.trim() === '' || matcher.trim() === '*') {
    return;
  }
  try {
    new RegExp(matcher);
  } catch (error) {
    throw new Error(`${label} matcher must be a valid regular expression`, {
      cause: error instanceof Error ? error : new Error(String(error)),
    });
  }
}

function validateGroup(value: unknown, event: string, index: number): QwenGroup {
  const label = `Qwen Code settings group hooks.${event}[${index}]`;
  if (!isObject(value)) throw new Error(`${label} must be an object`);
  if (value.matcher !== undefined) {
    if (typeof value.matcher !== 'string') throw new Error(`${label} matcher must be a string`);
    validateMatcher(value.matcher, event, label);
  }
  if (value.sequential !== undefined && typeof value.sequential !== 'boolean') {
    throw new Error(`${label} sequential must be a boolean`);
  }
  if (!Array.isArray(value.hooks)) throw new Error(`${label} must contain a hooks array`);
  return {
    ...value,
    hooks: value.hooks.map((handler, handlerIndex) => (
      validateHandler(handler, `${label}.hooks[${handlerIndex}]`)
    )),
  } as QwenGroup;
}

export function readQwenHooks(value: unknown): QwenHooks {
  if (value === undefined) return {};
  if (!isObject(value)) throw new Error('Qwen Code settings field "hooks" must be an object');
  const hooks: QwenHooks = {};
  for (const [event, groups] of Object.entries(value)) {
    if (!KNOWN_EVENTS.has(event)) {
      hooks[event] = groups as QwenGroup[];
      continue;
    }
    if (!Array.isArray(groups)) {
      throw new Error(`Qwen Code settings field "hooks.${event}" must be an array`);
    }
    hooks[event] = groups.map((group, index) => validateGroup(group, event, index));
  }
  return hooks;
}

export function buildQwenGroup(scriptPath: string, name: string): QwenGroup {
  return {
    hooks: [{
      type: 'command',
      name,
      command: buildQwenCommand(scriptPath),
      shell: process.platform === 'win32' ? 'powershell' : 'bash',
      timeout: HOOK_TIMEOUT_MS,
    }],
  };
}

function exactCurrentGroup(group: QwenGroup): boolean {
  return Object.keys(group).length === 1 && Object.hasOwn(group, 'hooks');
}

function exactLegacyGroup(group: QwenGroup): boolean {
  return Object.keys(group).sort().join('|') === 'hooks|matcher' && group.matcher === '*';
}

function currentReference(
  handler: QwenHandler,
  scriptName: string,
  hookName: string,
): QwenRuntimeReference | undefined {
  return Object.keys(handler).sort().join('|') === 'command|name|shell|timeout|type'
    && handler.type === 'command'
    && handler.name === hookName
    && handler.shell === (process.platform === 'win32' ? 'powershell' : 'bash')
    && handler.timeout === HOOK_TIMEOUT_MS
    && typeof handler.command === 'string'
    ? qwenRuntimeReference(handler.command, scriptName)
    : undefined;
}

function legacyReference(
  handler: QwenHandler,
  scriptName: string,
): QwenRuntimeReference | undefined {
  return Object.keys(handler).sort().join('|') === 'command|shell|timeout|type'
    && handler.type === 'command'
    && handler.shell === (process.platform === 'win32' ? 'powershell' : 'bash')
    && handler.timeout === HOOK_TIMEOUT_MS
    && typeof handler.command === 'string'
    ? qwenRuntimeReference(handler.command, scriptName)
    : undefined;
}

function managedReference(
  handler: QwenHandler,
  scriptName: string,
  hookName: string,
  includeLegacy: boolean,
): QwenRuntimeReference | undefined {
  return currentReference(handler, scriptName, hookName)
    ?? (includeLegacy ? legacyReference(handler, scriptName) : undefined);
}

const EVENT_CONTRACTS = [
  ['PreToolUse', GUARD_SCRIPT, GUARD_HOOK_NAME],
  ['PostToolUse', AUDIT_SCRIPT, AUDIT_HOOK_NAME],
  ['PostToolUseFailure', AUDIT_SCRIPT, AUDIT_HOOK_NAME],
] as const;

export function managedQwenRemovals(
  hooks: QwenHooks,
  agentId?: string,
): ManagedRemoval[] {
  const removals: ManagedRemoval[] = [];
  for (const [event, scriptName, hookName] of EVENT_CONTRACTS) {
    (hooks[event] ?? []).forEach((group, groupIndex) => {
      const handlerIndexes = group.hooks.flatMap((handler, handlerIndex) => {
        const reference = managedReference(handler, scriptName, hookName, true);
        return reference && (agentId === undefined || sameQwenAgentId(reference.agentId, agentId))
          ? [handlerIndex]
          : [];
      });
      if (handlerIndexes.length > 0) {
        removals.push({
          event,
          groupIndex,
          handlerIndexes,
          removeGroup: (exactCurrentGroup(group) || exactLegacyGroup(group))
            && handlerIndexes.length === group.hooks.length,
        });
      }
    });
  }
  return removals;
}

function referencesForEvent(
  groups: readonly QwenGroup[],
  scriptName: string,
  hookName: string,
): Map<string, QwenRuntimeReference[]> {
  const references = new Map<string, QwenRuntimeReference[]>();
  for (const group of groups) {
    if (!exactCurrentGroup(group)) continue;
    for (const handler of group.hooks) {
      const reference = currentReference(handler, scriptName, hookName);
      if (!reference) continue;
      const key = process.platform === 'win32'
        ? reference.agentId.toLowerCase()
        : reference.agentId;
      references.set(key, [...(references.get(key) ?? []), reference]);
    }
  }
  return references;
}

export function qwenRuntimeContracts(hooks: QwenHooks): QwenRuntimeContract[] {
  const guards = referencesForEvent(hooks.PreToolUse ?? [], GUARD_SCRIPT, GUARD_HOOK_NAME);
  const posts = referencesForEvent(hooks.PostToolUse ?? [], AUDIT_SCRIPT, AUDIT_HOOK_NAME);
  const failures = referencesForEvent(
    hooks.PostToolUseFailure ?? [],
    AUDIT_SCRIPT,
    AUDIT_HOOK_NAME,
  );
  const contracts: QwenRuntimeContract[] = [];
  for (const [key, guard] of guards) {
    const post = posts.get(key);
    const failure = failures.get(key);
    if (guard.length !== 1 || post?.length !== 1 || failure?.length !== 1) continue;
    if (!sameQwenPath(post[0].scriptPath, failure[0].scriptPath)) continue;
    contracts.push({
      agentId: guard[0].agentId,
      guardPath: guard[0].scriptPath,
      auditPath: post[0].scriptPath,
    });
  }
  return contracts;
}

export { isObject, type JsonObject };
