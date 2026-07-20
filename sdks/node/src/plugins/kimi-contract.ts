import {
  buildKimiCommand,
  kimiRuntimeReference,
  sameKimiAgentId,
  type KimiRuntimeReference,
} from './kimi-command.js';

export const AGENT_KEY = 'kimi';
export const GUARD_SCRIPT = 'guard.js';
export const AUDIT_SCRIPT = 'hook.js';
export const HOOK_TIMEOUT_SECONDS = 10;

const SHARED_EVENTS = [
  'PreToolUse',
  'PostToolUse',
  'PostToolUseFailure',
  'UserPromptSubmit',
  'Stop',
  'StopFailure',
  'SessionStart',
  'SessionEnd',
  'SubagentStart',
  'SubagentStop',
  'PreCompact',
  'PostCompact',
  'Notification',
] as const;

export const STABLE_EVENTS = new Set<string>([
  ...SHARED_EVENTS,
  'PermissionRequest',
  'PermissionResult',
  'Interrupt',
]);
export const LEGACY_EVENTS = new Set<string>(SHARED_EVENTS);

export type TomlObject = Record<string, unknown>;
export type ManagedKimiEvent = 'PreToolUse' | 'PostToolUse' | 'PostToolUseFailure';

export interface KimiHook extends TomlObject {
  readonly event: string;
  readonly command: string;
  readonly matcher?: string;
  readonly timeout?: number;
}

export interface KimiContract {
  readonly generation: 'stable' | 'legacy';
  readonly runtimeName: 'Kimi Code' | 'kimi-cli';
  readonly label: string;
  readonly directoryLabel: string;
  readonly configPath: string;
  readonly events: ReadonlySet<string>;
}

export interface KimiConfigDocument {
  readonly contract: KimiContract;
  readonly exists: boolean;
  readonly root: TomlObject;
  readonly hooks: readonly KimiHook[];
  readonly usesHookTables: boolean;
  readonly raw?: string;
}

export interface RenderedKimiDocument {
  readonly document: KimiConfigDocument;
  readonly changed: boolean;
  readonly next?: string;
}

export interface KimiRuntimeContract {
  readonly agentId: string;
  readonly guardPath: string;
  readonly auditPath: string;
  readonly configPath: string;
}

function isObject(value: unknown): value is TomlObject {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function hasOwn(value: TomlObject, key: string): boolean {
  return Object.prototype.hasOwnProperty.call(value, key);
}

function exactManagedKeys(hook: KimiHook): boolean {
  return Object.keys(hook).sort().join('|') === 'command|event|timeout';
}

function managedReference(
  hook: KimiHook,
  event: ManagedKimiEvent,
  scriptName: string,
): KimiRuntimeReference | undefined {
  if (!exactManagedKeys(hook)
    || hook.event !== event
    || hook.timeout !== HOOK_TIMEOUT_SECONDS) return undefined;
  return kimiRuntimeReference(hook.command, scriptName);
}

function managedAgentId(
  hook: KimiHook,
  event: ManagedKimiEvent,
  scriptName: string,
): string | undefined {
  return managedReference(hook, event, scriptName)?.agentId;
}

export function validateKimiHook(
  value: unknown,
  contract: KimiContract,
  index: number,
): KimiHook {
  if (!isObject(value)) throw new Error(`${contract.label} hook ${index + 1} must be a table`);
  const supportedFields = new Set(['event', 'matcher', 'command', 'timeout']);
  const unsupportedField = Object.keys(value).find((key) => !supportedFields.has(key));
  if (unsupportedField) {
    throw new Error(`${contract.label} hook ${index + 1} has unsupported field "${unsupportedField}"`);
  }
  if (typeof value.event !== 'string' || !contract.events.has(value.event)) {
    throw new Error(
      `${contract.label} hook ${index + 1} has unsupported event "${String(value.event)}"`,
    );
  }
  if (typeof value.command !== 'string' || value.command.length === 0) {
    throw new Error(`${contract.label} hook ${index + 1} requires a non-empty command`);
  }
  if (value.matcher !== undefined && typeof value.matcher !== 'string') {
    throw new Error(`${contract.label} hook ${index + 1} matcher must be a string`);
  }
  const timeout = value.timeout;
  if (timeout !== undefined
    && (!Number.isInteger(timeout) || (timeout as number) < 1 || (timeout as number) > 600)) {
    throw new Error(`${contract.label} hook ${index + 1} timeout must be an integer from 1 to 600`);
  }
  return value as KimiHook;
}

function readHooks(root: TomlObject, contract: KimiContract): readonly KimiHook[] {
  if (root.hooks === undefined) return [];
  if (!Array.isArray(root.hooks)) throw new Error(`${contract.label} field "hooks" must be an array`);
  return root.hooks.map((hook, index) => validateKimiHook(hook, contract, index));
}

function documentUsesHookTables(nodes: readonly unknown[]): boolean {
  return nodes.some((node) => {
    if (!isObject(node) || node.type !== 'TableArray' || !isObject(node.key)) return false;
    if (!isObject(node.key.item) || !Array.isArray(node.key.item.value)) return false;
    return node.key.item.value.length === 1 && node.key.item.value[0] === 'hooks';
  });
}

export async function parseKimiDocument(
  contract: KimiContract,
  raw: string,
): Promise<KimiConfigDocument> {
  let parsed: { readonly toJsObject: unknown; readonly cst: readonly unknown[] };
  try {
    const { TomlDocument } = await import('@decimalturn/toml-patch');
    parsed = new TomlDocument(raw);
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    throw new Error(`Failed to parse ${contract.label} at ${contract.configPath}: ${message}`, {
      cause: error instanceof Error ? error : new Error(message),
    });
  }
  if (!isObject(parsed.toJsObject)) {
    throw new Error(`${contract.label} at ${contract.configPath} must contain a TOML table`);
  }
  return {
    contract,
    exists: true,
    root: parsed.toJsObject,
    hooks: readHooks(parsed.toJsObject, contract),
    usesHookTables: documentUsesHookTables(parsed.cst),
    raw,
  };
}

export function createKimiDocument(contract: KimiContract): KimiConfigDocument {
  return { contract, exists: false, root: {}, hooks: [], usesHookTables: false };
}

async function renderHookSource(
  document: KimiConfigDocument,
  hooks: readonly KimiHook[],
): Promise<string> {
  const {
    patch: patchToml,
    stringify: stringifyToml,
    TomlFormat,
  } = await import('@decimalturn/toml-patch');
  const nextRoot: TomlObject = { ...document.root, hooks };
  if (document.usesHookTables) return patchToml(document.raw ?? '', nextRoot);

  let base = document.raw ?? '';
  if (hasOwn(document.root, 'hooks')) {
    const rootWithoutHooks = { ...document.root };
    delete rootWithoutHooks.hooks;
    base = patchToml(base, rootWithoutHooks);
  }
  const format = base.length > 0
    ? TomlFormat.autoDetectFormat(base)
    : TomlFormat.default();
  const hookTables = hooks.length > 0 ? stringifyToml({ hooks }, format) : '';
  if (base.length === 0) return hookTables;
  if (hookTables.length === 0) return base;
  const newline = format.newLine;
  const separator = base.endsWith(newline + newline)
    ? ''
    : base.endsWith(newline) ? newline : newline + newline;
  return base + separator + hookTables;
}

export async function renderKimiDocument(
  document: KimiConfigDocument,
  hooks: readonly KimiHook[],
): Promise<RenderedKimiDocument> {
  if (!document.exists && hooks.length === 0) return { document, changed: false };
  const next = await renderHookSource(document, hooks);
  if (hooks.length === 0 && next.trim().length === 0) {
    return { document, changed: document.exists };
  }
  return { document, changed: next !== document.raw, next };
}

export function buildKimiHook(event: ManagedKimiEvent, scriptPath: string): KimiHook {
  return {
    event,
    command: buildKimiCommand(scriptPath),
    timeout: HOOK_TIMEOUT_SECONDS,
  };
}

function expectedManagedContract(event: string): readonly [ManagedKimiEvent, string] | undefined {
  if (event === 'PreToolUse') return ['PreToolUse', GUARD_SCRIPT];
  if (event === 'PostToolUse') return ['PostToolUse', AUDIT_SCRIPT];
  if (event === 'PostToolUseFailure') return ['PostToolUseFailure', AUDIT_SCRIPT];
  return undefined;
}

export function removeManagedKimiHooks(
  hooks: readonly KimiHook[],
  agentId?: string,
): KimiHook[] {
  return hooks.filter((hook) => {
    const expected = expectedManagedContract(hook.event);
    if (!expected) return true;
    const managedId = managedAgentId(hook, expected[0], expected[1]);
    if (!managedId) return true;
    return agentId !== undefined && !sameKimiAgentId(managedId, agentId);
  });
}

function referencesForEvent(
  hooks: readonly KimiHook[],
  event: ManagedKimiEvent,
  scriptName: string,
): Map<string, KimiRuntimeReference[]> {
  const result = new Map<string, KimiRuntimeReference[]>();
  for (const hook of hooks) {
    const reference = managedReference(hook, event, scriptName);
    if (!reference) continue;
    const key = process.platform === 'win32'
      ? reference.agentId.toLowerCase()
      : reference.agentId;
    const existing = result.get(key) ?? [];
    existing.push(reference);
    result.set(key, existing);
  }
  return result;
}

export function kimiRuntimeContracts(
  documents: readonly KimiConfigDocument[],
): KimiRuntimeContract[] {
  const contracts: KimiRuntimeContract[] = [];
  for (const document of documents) {
    const guards = referencesForEvent(document.hooks, 'PreToolUse', GUARD_SCRIPT);
    const successes = referencesForEvent(document.hooks, 'PostToolUse', AUDIT_SCRIPT);
    const failures = referencesForEvent(document.hooks, 'PostToolUseFailure', AUDIT_SCRIPT);
    for (const [key, guard] of guards) {
      const success = successes.get(key);
      const failure = failures.get(key);
      if (guard.length !== 1 || success?.length !== 1 || failure?.length !== 1) continue;
      contracts.push({
        agentId: guard[0].agentId,
        guardPath: guard[0].scriptPath,
        auditPath: success[0].scriptPath,
        configPath: document.contract.configPath,
      });
    }
  }
  return contracts;
}
