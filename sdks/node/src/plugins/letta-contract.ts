import {
  buildLettaCommand,
  lettaLegacyRuntimeReference,
  lettaRuntimeReference,
  sameLettaAgentId,
  sameLettaPath,
  type LettaRuntimeReference,
} from './letta-command.js';
import { isObject, type JsonObject } from './strict-json.js';

export const AGENT_KEY = 'letta';
export const GUARD_SCRIPT = 'guard.js';
export const AUDIT_SCRIPT = 'hook.js';
export const HOOK_TIMEOUT_MS = 10_000;
export const LETTA_AUDIT_OPTIONS = {
  nativePayload: true,
  failClosed: true,
} as const;
export const MANAGED_EVENTS = [
  'PreToolUse',
  'PostToolUse',
  'PostToolUseFailure',
] as const;

const TOOL_EVENTS = new Set([
  'PreToolUse',
  'PostToolUse',
  'PostToolUseFailure',
  'PermissionRequest',
]);

const SIMPLE_EVENTS = new Set([
  'UserPromptSubmit',
  'Notification',
  'Stop',
  'SubagentStop',
  'PreCompact',
  'SessionStart',
  'SessionEnd',
]);

const KNOWN_EVENTS = new Set([...TOOL_EVENTS, ...SIMPLE_EVENTS]);

export type ManagedLettaEvent = (typeof MANAGED_EVENTS)[number];

export interface LettaHandler extends JsonObject {
  readonly type: 'command' | 'prompt';
  readonly command?: string;
  readonly prompt?: string;
  readonly model?: string;
  readonly timeout?: number;
  readonly quiet?: boolean;
}

export interface LettaGroup extends JsonObject {
  readonly matcher?: string;
  readonly hooks: LettaHandler[];
}

export interface LettaHooks extends JsonObject {
  readonly disabled?: boolean;
  readonly [event: string]: LettaGroup[] | boolean | undefined;
}

export interface LettaRuntimeContract {
  readonly agentId: string;
  readonly guardPath: string;
  readonly auditPath: string;
}

export interface ManagedLettaRemoval {
  readonly event: ManagedLettaEvent;
  readonly groupIndex: number;
  readonly handlerIndexes: readonly number[];
  readonly removeGroup: boolean;
}

function validateTimeout(value: unknown, label: string): void {
  if (value === undefined) return;
  if (typeof value !== 'number' || !Number.isFinite(value) || value < 0) {
    throw new Error(`${label} timeout must be a non-negative finite number`);
  }
}

function validateHandler(value: unknown, label: string): LettaHandler {
  if (!isObject(value)) throw new Error(`${label} must be an object`);
  if (value.type !== 'command' && value.type !== 'prompt') {
    throw new Error(`${label} has unsupported type "${String(value.type)}"`);
  }
  validateTimeout(value.timeout, label);
  if (value.quiet !== undefined && typeof value.quiet !== 'boolean') {
    throw new Error(`${label} quiet must be a boolean`);
  }
  if (value.type === 'command') {
    if (typeof value.command !== 'string' || value.command.length === 0) {
      throw new Error(`${label} requires a non-empty command`);
    }
  } else {
    if (typeof value.prompt !== 'string' || value.prompt.length === 0) {
      throw new Error(`${label} requires a non-empty prompt`);
    }
    if (value.model !== undefined && typeof value.model !== 'string') {
      throw new Error(`${label} model must be a string`);
    }
  }
  return value as LettaHandler;
}

function validateGroup(
  value: unknown,
  event: string,
  index: number,
): LettaGroup {
  const label = `Letta Code settings group hooks.${event}[${index}]`;
  if (!isObject(value)) throw new Error(`${label} must be an object`);
  if (TOOL_EVENTS.has(event)) {
    if (typeof value.matcher !== 'string') {
      throw new Error(`${label} matcher must be a string`);
    }
  } else if (value.matcher !== undefined) {
    throw new Error(`${label} matcher is unsupported for ${event}`);
  }
  if (!Array.isArray(value.hooks)) throw new Error(`${label} must contain a hooks array`);
  return {
    ...value,
    hooks: value.hooks.map((handler, handlerIndex) => (
      validateHandler(handler, `${label}.hooks[${handlerIndex}]`)
    )),
  } as LettaGroup;
}

export function readLettaHooks(value: unknown): LettaHooks {
  if (value === undefined) return {};
  if (!isObject(value)) throw new Error('Letta Code settings field "hooks" must be an object');
  if (value.disabled !== undefined && typeof value.disabled !== 'boolean') {
    throw new Error('Letta Code settings field "hooks.disabled" must be a boolean');
  }
  const hooks: JsonObject = {};
  for (const [event, groups] of Object.entries(value)) {
    if (event === 'disabled') {
      hooks.disabled = groups;
      continue;
    }
    if (!KNOWN_EVENTS.has(event)) {
      hooks[event] = groups;
      continue;
    }
    if (!Array.isArray(groups)) {
      throw new Error(`Letta Code settings field "hooks.${event}" must be an array`);
    }
    hooks[event] = groups.map((group, index) => validateGroup(group, event, index));
  }
  return hooks as LettaHooks;
}

export function buildLettaGroup(scriptPath: string): LettaGroup {
  return {
    matcher: '*',
    hooks: [{
      type: 'command',
      command: buildLettaCommand(scriptPath),
      timeout: HOOK_TIMEOUT_MS,
    }],
  };
}

function exactGroup(group: LettaGroup): boolean {
  return Object.keys(group).sort().join('|') === 'hooks|matcher'
    && group.matcher === '*';
}

function exactCurrentReference(
  handler: LettaHandler,
  scriptName: string,
): LettaRuntimeReference | undefined {
  return Object.keys(handler).sort().join('|') === 'command|timeout|type'
    && handler.type === 'command'
    && handler.timeout === HOOK_TIMEOUT_MS
    && typeof handler.command === 'string'
    ? lettaRuntimeReference(handler.command, scriptName)
    : undefined;
}

function exactLegacyReference(
  handler: LettaHandler,
  scriptName: string,
): LettaRuntimeReference | undefined {
  return Object.keys(handler).sort().join('|') === 'command|type'
    && handler.type === 'command'
    && typeof handler.command === 'string'
    ? lettaLegacyRuntimeReference(handler.command, scriptName)
    : undefined;
}

function managedReference(
  handler: LettaHandler,
  scriptName: string,
): LettaRuntimeReference | undefined {
  return exactCurrentReference(handler, scriptName)
    ?? exactLegacyReference(handler, scriptName);
}

const EVENT_CONTRACTS = [
  ['PreToolUse', GUARD_SCRIPT],
  ['PostToolUse', AUDIT_SCRIPT],
  ['PostToolUseFailure', AUDIT_SCRIPT],
] as const;

function eventGroups(hooks: LettaHooks, event: ManagedLettaEvent): LettaGroup[] {
  const value = hooks[event];
  return Array.isArray(value) ? value : [];
}

export function managedLettaRemovals(
  hooks: LettaHooks,
  agentId?: string,
): ManagedLettaRemoval[] {
  const removals: ManagedLettaRemoval[] = [];
  for (const [event, scriptName] of EVENT_CONTRACTS) {
    eventGroups(hooks, event).forEach((group, groupIndex) => {
      const handlerIndexes = group.hooks.flatMap((handler, handlerIndex) => {
        const reference = managedReference(handler, scriptName);
        return reference && (agentId === undefined || sameLettaAgentId(reference.agentId, agentId))
          ? [handlerIndex]
          : [];
      });
      if (handlerIndexes.length > 0) {
        removals.push({
          event,
          groupIndex,
          handlerIndexes,
          removeGroup: exactGroup(group) && handlerIndexes.length === group.hooks.length,
        });
      }
    });
  }
  return removals;
}

function referencesForEvent(
  groups: readonly LettaGroup[],
  scriptName: string,
): Map<string, LettaRuntimeReference[]> {
  const references = new Map<string, LettaRuntimeReference[]>();
  for (const group of groups) {
    if (!exactGroup(group)) continue;
    for (const handler of group.hooks) {
      const reference = exactCurrentReference(handler, scriptName);
      if (!reference
        || !reference.executablePath
        || !sameLettaPath(reference.executablePath, process.execPath)) continue;
      const key = process.platform === 'win32'
        ? reference.agentId.toLowerCase()
        : reference.agentId;
      references.set(key, [...(references.get(key) ?? []), reference]);
    }
  }
  return references;
}

export function lettaRuntimeContracts(hooks: LettaHooks): LettaRuntimeContract[] {
  const guards = referencesForEvent(eventGroups(hooks, 'PreToolUse'), GUARD_SCRIPT);
  const posts = referencesForEvent(eventGroups(hooks, 'PostToolUse'), AUDIT_SCRIPT);
  const failures = referencesForEvent(
    eventGroups(hooks, 'PostToolUseFailure'),
    AUDIT_SCRIPT,
  );
  const contracts: LettaRuntimeContract[] = [];
  for (const [key, guard] of guards) {
    const post = posts.get(key);
    const failure = failures.get(key);
    if (guard.length !== 1 || post?.length !== 1 || failure?.length !== 1) continue;
    if (!sameLettaPath(post[0].scriptPath, failure[0].scriptPath)) continue;
    contracts.push({
      agentId: guard[0].agentId,
      guardPath: guard[0].scriptPath,
      auditPath: post[0].scriptPath,
    });
  }
  return contracts;
}
