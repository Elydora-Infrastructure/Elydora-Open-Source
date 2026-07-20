import {
  buildGeminiCommand,
  geminiRuntimeReference,
  sameGeminiAgentId,
  type GeminiRuntimeReference,
} from './gemini-command.js';
import { isObject, type JsonObject } from './strict-json.js';

export const AGENT_KEY = 'gemini';
export const CONFIG_FILE = 'settings.json';
export const GUARD_SCRIPT = 'guard.js';
export const AUDIT_SCRIPT = 'hook.js';
export const GUARD_HOOK_NAME = 'elydora-guard';
export const AUDIT_HOOK_NAME = 'elydora-audit';
export const HOOK_TIMEOUT_MS = 10_000;
export const MANAGED_EVENTS = ['BeforeTool', 'AfterTool'] as const;

const KNOWN_EVENTS = new Set([
  'BeforeTool',
  'AfterTool',
  'BeforeAgent',
  'Notification',
  'AfterAgent',
  'SessionStart',
  'SessionEnd',
  'PreCompress',
  'BeforeModel',
  'AfterModel',
  'BeforeToolSelection',
]);

export type ManagedGeminiEvent = (typeof MANAGED_EVENTS)[number];

export interface GeminiHandler extends JsonObject {
  readonly type: 'command';
  readonly command?: string;
  readonly name?: string;
  readonly timeout?: number;
}

export interface GeminiGroup extends JsonObject {
  readonly matcher?: string;
  readonly sequential?: boolean;
  readonly hooks: GeminiHandler[];
}

export type GeminiHooks = Record<string, GeminiGroup[]>;

export interface GeminiHookControls {
  readonly enabled: boolean;
  readonly disabled: readonly string[];
}

export interface GeminiRuntimeContract {
  readonly agentId: string;
  readonly guardPath: string;
  readonly auditPath: string;
}

export interface ManagedRemoval {
  readonly event: ManagedGeminiEvent;
  readonly groupIndex: number;
  readonly handlerIndexes: readonly number[];
  readonly removeGroup: boolean;
}

function optionalString(value: JsonObject, field: string, label: string): void {
  if (value[field] !== undefined && typeof value[field] !== 'string') {
    throw new Error(`${label} field "${field}" must be a string`);
  }
}

function validateTimeout(value: unknown, label: string): void {
  if (value === undefined) return;
  if (typeof value !== 'number' || !Number.isFinite(value) || value < 0) {
    throw new Error(`${label} timeout must be a non-negative finite number`);
  }
}

function validateEnvironment(value: unknown, label: string): void {
  if (value === undefined) return;
  if (!isObject(value)
    || Object.values(value).some((entry) => typeof entry !== 'string')) {
    throw new Error(`${label} env must map names to strings`);
  }
}

function validateHandler(
  value: unknown,
  event: string,
  groupIndex: number,
  handlerIndex: number,
): GeminiHandler {
  const label = `Gemini CLI settings handler hooks.${event}[${groupIndex}].hooks[${handlerIndex}]`;
  if (!isObject(value)) throw new Error(`${label} must be an object`);
  if (value.type !== 'command') {
    throw new Error(`${label} has unsupported type "${String(value.type)}"`);
  }
  optionalString(value, 'name', label);
  optionalString(value, 'description', label);
  optionalString(value, 'source', label);
  validateTimeout(value.timeout, label);
  validateEnvironment(value.env, label);
  if (typeof value.command !== 'string' || value.command.length === 0) {
    throw new Error(`${label} requires a non-empty command`);
  }
  return value as GeminiHandler;
}

function validateGroup(value: unknown, event: string, groupIndex: number): GeminiGroup {
  const label = `Gemini CLI settings group hooks.${event}[${groupIndex}]`;
  if (!isObject(value)) throw new Error(`${label} must be an object`);
  if (value.matcher !== undefined && typeof value.matcher !== 'string') {
    throw new Error(`${label} matcher must be a string`);
  }
  if (value.sequential !== undefined && typeof value.sequential !== 'boolean') {
    throw new Error(`${label} sequential must be a boolean`);
  }
  if (!Array.isArray(value.hooks)) throw new Error(`${label} must contain a hooks array`);
  return {
    ...value,
    hooks: value.hooks.map(
      (handler, handlerIndex) => validateHandler(handler, event, groupIndex, handlerIndex),
    ),
  } as GeminiGroup;
}

export function readGeminiHooks(value: unknown): GeminiHooks {
  if (value === undefined) return {};
  if (!isObject(value)) throw new Error('Gemini CLI settings field "hooks" must be an object');
  const hooks: GeminiHooks = {};
  for (const [event, groups] of Object.entries(value)) {
    if (!Array.isArray(groups)) {
      throw new Error(`Gemini CLI settings field "hooks.${event}" must be an array`);
    }
    hooks[event] = KNOWN_EVENTS.has(event)
      ? groups.map((group, index) => validateGroup(group, event, index))
      : groups as GeminiGroup[];
  }
  return hooks;
}

export function readGeminiHookControls(value: unknown): GeminiHookControls {
  if (value === undefined) return { enabled: true, disabled: [] };
  if (!isObject(value)) {
    throw new Error('Gemini CLI settings field "hooksConfig" must be an object');
  }
  const supported = new Set(['enabled', 'disabled', 'notifications']);
  const extra = Object.keys(value).find((field) => !supported.has(field));
  if (extra) {
    throw new Error(`Gemini CLI settings field "hooksConfig" contains unsupported field "${extra}"`);
  }
  if (value.enabled !== undefined && typeof value.enabled !== 'boolean') {
    throw new Error('Gemini CLI settings field "hooksConfig.enabled" must be a boolean');
  }
  if (value.notifications !== undefined && typeof value.notifications !== 'boolean') {
    throw new Error('Gemini CLI settings field "hooksConfig.notifications" must be a boolean');
  }
  if (value.disabled !== undefined && (
    !Array.isArray(value.disabled)
    || value.disabled.some((item) => typeof item !== 'string')
  )) {
    throw new Error('Gemini CLI settings field "hooksConfig.disabled" must be an array of strings');
  }
  return {
    enabled: value.enabled !== false,
    disabled: value.disabled === undefined ? [] : value.disabled as string[],
  };
}

export function managedGeminiHooksEnabled(controls: GeminiHookControls): boolean {
  return controls.enabled
    && !controls.disabled.includes(GUARD_HOOK_NAME)
    && !controls.disabled.includes(AUDIT_HOOK_NAME);
}

export function disabledManagedGeminiEntries(
  controls: GeminiHookControls,
): readonly string[] {
  return controls.disabled.filter((entry) => (
    entry === GUARD_HOOK_NAME
    || entry === AUDIT_HOOK_NAME
    || geminiRuntimeReference(entry, GUARD_SCRIPT, true) !== undefined
    || geminiRuntimeReference(entry, AUDIT_SCRIPT, true) !== undefined
  ));
}

export function buildGeminiGroup(scriptPath: string, name: string): GeminiGroup {
  return {
    hooks: [{
      type: 'command',
      name,
      command: buildGeminiCommand(scriptPath),
      timeout: HOOK_TIMEOUT_MS,
    }],
  };
}

function exactManagedGroup(group: GeminiGroup): boolean {
  return Object.keys(group).length === 1 && Object.hasOwn(group, 'hooks');
}

function currentManagedReference(
  handler: GeminiHandler,
  scriptName: string,
  hookName: string,
): GeminiRuntimeReference | undefined {
  return Object.keys(handler).sort().join('|') === 'command|name|timeout|type'
    && handler.type === 'command'
    && handler.name === hookName
    && handler.timeout === HOOK_TIMEOUT_MS
    && typeof handler.command === 'string'
    ? geminiRuntimeReference(handler.command, scriptName)
    : undefined;
}

function legacyManagedReference(
  handler: GeminiHandler,
  scriptName: string,
): GeminiRuntimeReference | undefined {
  return Object.keys(handler).sort().join('|') === 'command|type'
    && handler.type === 'command'
    && typeof handler.command === 'string'
    ? geminiRuntimeReference(handler.command, scriptName, true)
    : undefined;
}

function managedReference(
  handler: GeminiHandler,
  scriptName: string,
  hookName: string,
  includeLegacy: boolean,
): GeminiRuntimeReference | undefined {
  return currentManagedReference(handler, scriptName, hookName)
    ?? (includeLegacy ? legacyManagedReference(handler, scriptName) : undefined);
}

export function managedGeminiRemovals(
  hooks: GeminiHooks,
  agentId?: string,
): ManagedRemoval[] {
  const removals: ManagedRemoval[] = [];
  for (const [event, scriptName, hookName] of [
    ['BeforeTool', GUARD_SCRIPT, GUARD_HOOK_NAME],
    ['AfterTool', AUDIT_SCRIPT, AUDIT_HOOK_NAME],
  ] as const) {
    (hooks[event] ?? []).forEach((group, groupIndex) => {
      const handlerIndexes = group.hooks.flatMap((handler, handlerIndex) => {
        const reference = managedReference(handler, scriptName, hookName, true);
        return reference
          && (agentId === undefined || sameGeminiAgentId(reference.agentId, agentId))
          ? [handlerIndex]
          : [];
      });
      if (handlerIndexes.length > 0) {
        removals.push({
          event,
          groupIndex,
          handlerIndexes,
          removeGroup: exactManagedGroup(group) && handlerIndexes.length === group.hooks.length,
        });
      }
    });
  }
  return removals;
}

function referencesForEvent(
  groups: readonly GeminiGroup[],
  scriptName: string,
  hookName: string,
): Map<string, GeminiRuntimeReference[]> {
  const references = new Map<string, GeminiRuntimeReference[]>();
  for (const group of groups) {
    if (!exactManagedGroup(group)) continue;
    for (const handler of group.hooks) {
      const reference = currentManagedReference(handler, scriptName, hookName);
      if (!reference) continue;
      const key = process.platform === 'win32'
        ? reference.agentId.toLowerCase()
        : reference.agentId;
      references.set(key, [...(references.get(key) ?? []), reference]);
    }
  }
  return references;
}

export function geminiRuntimeContracts(hooks: GeminiHooks): GeminiRuntimeContract[] {
  const guards = referencesForEvent(hooks.BeforeTool ?? [], GUARD_SCRIPT, GUARD_HOOK_NAME);
  const audits = referencesForEvent(hooks.AfterTool ?? [], AUDIT_SCRIPT, AUDIT_HOOK_NAME);
  const contracts: GeminiRuntimeContract[] = [];
  for (const [key, guard] of guards) {
    const audit = audits.get(key);
    if (guard.length !== 1 || audit?.length !== 1) continue;
    contracts.push({
      agentId: guard[0].agentId,
      guardPath: guard[0].scriptPath,
      auditPath: audit[0].scriptPath,
    });
  }
  return contracts;
}
