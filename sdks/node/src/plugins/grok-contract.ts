import {
  buildGrokCommand,
  grokRuntimeReference,
  sameGrokAgentId,
  type GrokRuntimeReference,
} from './grok-command.js';
import { isObject, parseStrictJsonObject, type JsonObject } from './strict-json.js';

export const AGENT_KEY = 'grok';
export const GUARD_SCRIPT = 'guard.js';
export const AUDIT_SCRIPT = 'hook.js';
export const HOOK_TIMEOUT_SECONDS = 10;

const MATCHER_REJECTING_EVENTS = new Set([
  'SessionStart',
  'SessionEnd',
  'Stop',
  'UserPromptSubmit',
]);

export type ManagedGrokEvent = 'PreToolUse' | 'PostToolUse' | 'PostToolUseFailure';

export interface GrokHandler extends JsonObject {
  readonly type: 'command' | 'http';
  readonly command?: string;
  readonly url?: string;
  readonly timeout?: number;
}

export interface GrokGroup extends JsonObject {
  readonly matcher?: string;
  readonly hooks: GrokHandler[];
}

export type GrokHooks = Record<string, GrokGroup[]>;

export interface GrokDocument {
  readonly exists: boolean;
  readonly filePath: string;
  readonly root: JsonObject;
  readonly hooks: GrokHooks;
  readonly raw?: string;
}

export interface RenderedGrokDocument {
  readonly document: GrokDocument;
  readonly changed: boolean;
  readonly next?: string;
}

export interface GrokRuntimeContract {
  readonly agentId: string;
  readonly guardPath: string;
  readonly auditPath: string;
}

function validateTimeout(value: unknown, label: string): void {
  if (value === undefined) return;
  if (!Number.isSafeInteger(value) || (value as number) < 0) {
    throw new Error(`${label} timeout must be a non-negative integer`);
  }
}

function validateHandler(
  value: unknown,
  event: string,
  groupIndex: number,
  handlerIndex: number,
): GrokHandler {
  const label = `Grok user hooks handler hooks.${event}[${groupIndex}].hooks[${handlerIndex}]`;
  if (!isObject(value)) throw new Error(`${label} must be an object`);
  if (value.type !== 'command' && value.type !== 'http') {
    throw new Error(`${label} has unsupported type "${String(value.type)}"`);
  }
  if (value.type === 'command' && (typeof value.command !== 'string' || !value.command)) {
    throw new Error(`${label} requires a non-empty command`);
  }
  if (value.type === 'http' && (typeof value.url !== 'string' || !value.url)) {
    throw new Error(`${label} requires a non-empty url`);
  }
  validateTimeout(value.timeout, label);
  if (value.env !== undefined) {
    if (!isObject(value.env)
      || Object.values(value.env).some((item) => typeof item !== 'string')) {
      throw new Error(`${label} env must map names to strings`);
    }
  }
  return value as GrokHandler;
}

function validateGroup(value: unknown, event: string, groupIndex: number): GrokGroup {
  const label = `Grok user hooks group hooks.${event}[${groupIndex}]`;
  if (!isObject(value)) throw new Error(`${label} must be an object`);
  if (value.matcher !== undefined && typeof value.matcher !== 'string') {
    throw new Error(`${label} matcher must be a string`);
  }
  if (value.matcher !== undefined && MATCHER_REJECTING_EVENTS.has(event)) {
    throw new Error(`${label} cannot declare a matcher for ${event}`);
  }
  if (!Array.isArray(value.hooks)) throw new Error(`${label} must contain a hooks array`);
  return {
    ...value,
    hooks: value.hooks.map(
      (handler, handlerIndex) => validateHandler(handler, event, groupIndex, handlerIndex),
    ),
  } as GrokGroup;
}

function readHooks(value: unknown): GrokHooks {
  if (value === undefined) return {};
  if (!isObject(value)) throw new Error('Grok user hooks field "hooks" must be an object');
  const hooks: GrokHooks = {};
  for (const [event, candidate] of Object.entries(value)) {
    if (!Array.isArray(candidate)) {
      throw new Error(`Grok user hooks field "hooks.${event}" must be an array`);
    }
    hooks[event] = candidate.map((group, index) => validateGroup(group, event, index));
  }
  return hooks;
}

export function parseGrokDocument(filePath: string, raw: string): GrokDocument {
  const root = parseStrictJsonObject(raw, `Grok user hooks at ${filePath}`);
  return { exists: true, filePath, root, hooks: readHooks(root.hooks), raw };
}

export function createGrokDocument(filePath: string): GrokDocument {
  return { exists: false, filePath, root: {}, hooks: {} };
}

export function buildGrokGroup(scriptPath: string): GrokGroup {
  return {
    hooks: [{
      type: 'command',
      command: buildGrokCommand(scriptPath),
      timeout: HOOK_TIMEOUT_SECONDS,
    }],
  };
}

function exactManagedGroup(group: GrokGroup): boolean {
  return Object.keys(group).length === 1 && Object.hasOwn(group, 'hooks');
}

function exactManagedHandler(handler: GrokHandler): boolean {
  return Object.keys(handler).sort().join('|') === 'command|timeout|type'
    && handler.type === 'command'
    && handler.timeout === HOOK_TIMEOUT_SECONDS
    && typeof handler.command === 'string';
}

function managedReference(
  handler: GrokHandler,
  scriptName: string,
): GrokRuntimeReference | undefined {
  if (!exactManagedHandler(handler)) return undefined;
  return grokRuntimeReference(handler.command as string, scriptName);
}

function removeFromGroups(
  groups: readonly GrokGroup[],
  scriptName: string,
  agentId?: string,
): GrokGroup[] {
  const result: GrokGroup[] = [];
  for (const group of groups) {
    if (!exactManagedGroup(group)) {
      result.push(group);
      continue;
    }
    const handlers = group.hooks.filter((handler) => {
      const reference = managedReference(handler, scriptName);
      return !reference
        || (agentId !== undefined && !sameGrokAgentId(reference.agentId, agentId));
    });
    if (handlers.length > 0) result.push({ hooks: handlers });
  }
  return result;
}

function managedEvent(event: string): readonly [ManagedGrokEvent, string] | undefined {
  if (event === 'PreToolUse') return ['PreToolUse', GUARD_SCRIPT];
  if (event === 'PostToolUse') return ['PostToolUse', AUDIT_SCRIPT];
  if (event === 'PostToolUseFailure') return ['PostToolUseFailure', AUDIT_SCRIPT];
  return undefined;
}

export function removeManagedGrokHooks(hooks: GrokHooks, agentId?: string): GrokHooks {
  const next: GrokHooks = Object.fromEntries(
    Object.entries(hooks).map(([event, groups]) => [event, [...groups]]),
  );
  for (const [event, scriptName] of [
    ['PreToolUse', GUARD_SCRIPT],
    ['PostToolUse', AUDIT_SCRIPT],
    ['PostToolUseFailure', AUDIT_SCRIPT],
  ] as const) {
    const groups = removeFromGroups(next[event] ?? [], scriptName, agentId);
    if (groups.length > 0) next[event] = groups;
    else delete next[event];
  }
  return next;
}

function entirelyManaged(document: GrokDocument): boolean {
  if (!document.exists || !Object.keys(document.root).every((key) => key === 'hooks')) return false;
  const events = Object.entries(document.hooks);
  if (events.length === 0) return false;
  let handlerCount = 0;
  for (const [event, groups] of events) {
    const contract = managedEvent(event);
    if (!contract || groups.length === 0) return false;
    for (const group of groups) {
      if (!exactManagedGroup(group)
        || group.hooks.length === 0
        || group.hooks.some((handler) => !managedReference(handler, contract[1]))) return false;
      handlerCount += group.hooks.length;
    }
  }
  return handlerCount > 0;
}

export function renderGrokDocument(
  document: GrokDocument,
  hooks: GrokHooks,
): RenderedGrokDocument {
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

function referencesForEvent(
  groups: readonly GrokGroup[],
  scriptName: string,
): Map<string, GrokRuntimeReference[]> {
  const result = new Map<string, GrokRuntimeReference[]>();
  for (const group of groups) {
    if (!exactManagedGroup(group)) continue;
    for (const handler of group.hooks) {
      const reference = managedReference(handler, scriptName);
      if (!reference) continue;
      const key = process.platform === 'win32'
        ? reference.agentId.toLowerCase()
        : reference.agentId;
      const entries = result.get(key) ?? [];
      entries.push(reference);
      result.set(key, entries);
    }
  }
  return result;
}

export function grokRuntimeContracts(hooks: GrokHooks): GrokRuntimeContract[] {
  const guards = referencesForEvent(hooks.PreToolUse ?? [], GUARD_SCRIPT);
  const successes = referencesForEvent(hooks.PostToolUse ?? [], AUDIT_SCRIPT);
  const failures = referencesForEvent(hooks.PostToolUseFailure ?? [], AUDIT_SCRIPT);
  const contracts: GrokRuntimeContract[] = [];
  for (const [key, guard] of guards) {
    const success = successes.get(key);
    const failure = failures.get(key);
    if (guard.length !== 1 || success?.length !== 1 || failure?.length !== 1) continue;
    contracts.push({
      agentId: guard[0].agentId,
      guardPath: guard[0].scriptPath,
      auditPath: success[0].scriptPath,
    });
  }
  return contracts;
}
