import os from 'node:os';
import path from 'node:path';
import {
  buildKiroIdeCommand,
  kiroIdeRuntimeReference,
  sameKiroIdeAgentId,
  sameKiroIdePath,
  type KiroIdeRuntimeReference,
} from './kiroide-command.js';
import { isObject, parseStrictJsonObject, type JsonObject } from './strict-json.js';

export const AGENT_KEY = 'kiroide';
export const AUDIT_SCRIPT = 'hook.js';
export const CONFIG_FILE = 'elydora-audit.json';
export const GUARD_SCRIPT = 'guard.js';
export const LEGACY_CONFIG_FILE = 'elydora-audit.kiro.hook';
export const HOOK_TIMEOUT_SECONDS = 10;

const GUARD_NAME = 'elydora-guard';
const AUDIT_NAME = 'elydora-audit';
const GUARD_DESCRIPTION = 'Block tool use when the Elydora agent is frozen';
const AUDIT_DESCRIPTION = 'Record tool use in the Elydora audit trail';
const KIRO_TRIGGERS = new Set([
  'SessionStart',
  'Stop',
  'PreToolUse',
  'PostToolUse',
  'PreTaskExec',
  'PostTaskExec',
  'UserPromptSubmit',
  'PostFileCreate',
  'PostFileSave',
  'PostFileDelete',
]);

export interface KiroIdeAction extends JsonObject {
  readonly type: 'command' | 'agent';
  readonly command?: string;
  readonly prompt?: string;
}

export interface KiroIdeHook extends JsonObject {
  readonly name: string;
  readonly description?: string;
  readonly trigger: string;
  readonly matcher?: string;
  readonly action: KiroIdeAction;
  readonly timeout?: number;
  readonly enabled?: boolean;
}

export interface KiroIdeDocument {
  readonly exists: boolean;
  readonly filePath: string;
  readonly root: JsonObject;
  readonly hooks: readonly KiroIdeHook[];
  readonly raw?: string;
}

export interface RenderedKiroIdeDocument {
  readonly document: KiroIdeDocument;
  readonly changed: boolean;
  readonly next?: string;
}

export interface KiroIdeRuntimeContract {
  readonly agentId: string;
  readonly guardPath: string;
  readonly auditPath: string;
}

export interface KiroIdePaths {
  readonly workspaceRoot: string;
  readonly kiroDirectory: string;
  readonly hooksDirectory: string;
  readonly configPath: string;
  readonly legacyConfigPath: string;
}

export function resolveKiroIdePaths(
  workspaceRoot = process.cwd(),
  homeDirectory = os.homedir(),
): KiroIdePaths {
  const resolvedWorkspace = path.resolve(workspaceRoot);
  const kiroDirectory = path.join(resolvedWorkspace, '.kiro');
  const hooksDirectory = path.join(kiroDirectory, 'hooks');
  return {
    workspaceRoot: resolvedWorkspace,
    kiroDirectory,
    hooksDirectory,
    configPath: path.join(hooksDirectory, CONFIG_FILE),
    legacyConfigPath: path.join(homeDirectory, '.kiro', 'hooks', LEGACY_CONFIG_FILE),
  };
}

function validateAction(value: unknown, label: string): KiroIdeAction {
  if (!isObject(value)) throw new Error(`${label} action must be an object`);
  if (value.type === 'command') {
    if (typeof value.command !== 'string' || !value.command) {
      throw new Error(`${label} command action requires a non-empty command`);
    }
    return value as KiroIdeAction;
  }
  if (value.type === 'agent') {
    if (typeof value.prompt !== 'string' || !value.prompt) {
      throw new Error(`${label} agent action requires a non-empty prompt`);
    }
    return value as KiroIdeAction;
  }
  throw new Error(`${label} action has unsupported type "${String(value.type)}"`);
}

function validateHook(value: unknown, index: number, documentLabel: string): KiroIdeHook {
  const label = `${documentLabel} hooks[${index}]`;
  if (!isObject(value)) throw new Error(`${label} must be an object`);
  if (typeof value.name !== 'string' || !value.name) throw new Error(`${label} requires a name`);
  if (typeof value.trigger !== 'string' || !KIRO_TRIGGERS.has(value.trigger)) {
    throw new Error(`${label} has unsupported trigger "${String(value.trigger)}"`);
  }
  if (value.description !== undefined && typeof value.description !== 'string') {
    throw new Error(`${label} description must be a string`);
  }
  if (value.matcher !== undefined && typeof value.matcher !== 'string') {
    throw new Error(`${label} matcher must be a string`);
  }
  if (value.timeout !== undefined
    && (!Number.isSafeInteger(value.timeout) || (value.timeout as number) < 0)) {
    throw new Error(`${label} timeout must be a non-negative integer`);
  }
  if (value.enabled !== undefined && typeof value.enabled !== 'boolean') {
    throw new Error(`${label} enabled must be a boolean`);
  }
  return { ...value, action: validateAction(value.action, label) } as KiroIdeHook;
}

export function parseKiroIdeDocument(
  filePath: string,
  raw: string,
  documentLabel = 'Kiro IDE hooks',
): KiroIdeDocument {
  const root = parseStrictJsonObject(raw, `${documentLabel} at ${filePath}`);
  if (root.version !== 'v1') throw new Error(`${documentLabel} version must be "v1": ${filePath}`);
  if (!Array.isArray(root.hooks)) throw new Error(`${documentLabel} field "hooks" must be an array: ${filePath}`);
  return {
    exists: true,
    filePath,
    root,
    hooks: root.hooks.map((hook, index) => validateHook(hook, index, documentLabel)),
    raw,
  };
}

export function createKiroIdeDocument(filePath: string): KiroIdeDocument {
  return { exists: false, filePath, root: {}, hooks: [] };
}

function exactKeys(value: JsonObject, keys: readonly string[]): boolean {
  return Object.keys(value).sort().join('|') === [...keys].sort().join('|');
}

function managedSpecification(name: string): {
  readonly description: string;
  readonly trigger: 'PreToolUse' | 'PostToolUse';
  readonly scriptName: string;
} | undefined {
  if (name === GUARD_NAME) {
    return { description: GUARD_DESCRIPTION, trigger: 'PreToolUse', scriptName: GUARD_SCRIPT };
  }
  if (name === AUDIT_NAME) {
    return { description: AUDIT_DESCRIPTION, trigger: 'PostToolUse', scriptName: AUDIT_SCRIPT };
  }
  return undefined;
}

function managedReference(hook: KiroIdeHook) {
  const specification = managedSpecification(hook.name);
  const owned = ownedReference(hook);
  if (!specification
    || !owned
    || !exactKeys(hook, ['name', 'description', 'trigger', 'matcher', 'action', 'timeout', 'enabled'])
    || hook.description !== specification.description
    || hook.trigger !== specification.trigger
    || hook.matcher !== '.*'
    || hook.timeout !== HOOK_TIMEOUT_SECONDS
    || hook.enabled !== true
    || !exactKeys(hook.action, ['type', 'command'])) return undefined;
  return owned;
}

function ownedReference(hook: KiroIdeHook) {
  const specification = managedSpecification(hook.name);
  if (!specification
    || hook.action.type !== 'command'
    || typeof hook.action.command !== 'string') return undefined;
  return kiroIdeRuntimeReference(hook.action.command, specification.scriptName);
}

export function requireAvailableKiroIdeHooks(
  hooks: readonly KiroIdeHook[],
  productLabel = 'Kiro IDE',
): void {
  for (const hook of hooks) {
    if (managedSpecification(hook.name) && !ownedReference(hook)) {
      throw new Error(`${productLabel} hook name "${hook.name}" conflicts with the Elydora contract`);
    }
  }
}

export function buildKiroIdeHook(
  name: typeof GUARD_NAME | typeof AUDIT_NAME,
  scriptPath: string,
): KiroIdeHook {
  const specification = managedSpecification(name)!;
  return {
    name,
    description: specification.description,
    trigger: specification.trigger,
    matcher: '.*',
    action: { type: 'command', command: buildKiroIdeCommand(scriptPath) },
    timeout: HOOK_TIMEOUT_SECONDS,
    enabled: true,
  };
}

export function withoutManagedKiroIdeHooks(
  hooks: readonly KiroIdeHook[],
  agentId?: string,
): KiroIdeHook[] {
  return hooks.filter((hook) => {
    const reference = ownedReference(hook);
    return !reference || (agentId !== undefined && !sameKiroIdeAgentId(reference.agentId, agentId));
  });
}

function entirelyManaged(document: KiroIdeDocument): boolean {
  return document.exists
    && exactKeys(document.root, ['version', 'hooks'])
    && document.hooks.length > 0
    && document.hooks.every((hook) => managedReference(hook) !== undefined);
}

export function renderKiroIdeDocument(
  document: KiroIdeDocument,
  hooks: readonly KiroIdeHook[],
): RenderedKiroIdeDocument {
  if (!document.exists && hooks.length === 0) return { document, changed: false };
  if (document.exists
    && hooks.length === document.hooks.length
    && hooks.every((hook, index) => hook === document.hooks[index])) {
    return { document, changed: false };
  }
  if (hooks.length === 0 && entirelyManaged(document)) return { document, changed: true };
  const next = `${JSON.stringify({ ...document.root, version: 'v1', hooks }, null, 2)}\n`;
  return { document, changed: next !== document.raw, next };
}

export function kiroIdeRuntimeContracts(
  hooks: readonly KiroIdeHook[],
): KiroIdeRuntimeContract[] {
  const guards = new Map<string, KiroIdeRuntimeReference[]>();
  const audits = new Map<string, KiroIdeRuntimeReference[]>();
  for (const hook of hooks) {
    if (!managedSpecification(hook.name)) continue;
    const reference = managedReference(hook);
    if (!reference) return [];
    const key = process.platform === 'win32' ? reference.agentId.toLowerCase() : reference.agentId;
    const target = hook.name === GUARD_NAME ? guards : audits;
    const entries = target.get(key) ?? [];
    entries.push(reference);
    target.set(key, entries);
  }
  const contracts: KiroIdeRuntimeContract[] = [];
  for (const [key, guard] of guards) {
    const audit = audits.get(key);
    if (guard.length !== 1 || audit?.length !== 1 || !guard[0] || !audit[0]) continue;
    contracts.push({
      agentId: guard[0].agentId,
      guardPath: guard[0].scriptPath,
      auditPath: audit[0].scriptPath,
    });
  }
  return contracts;
}

function legacyReference(command: unknown, scriptName: string) {
  if (typeof command !== 'string') return undefined;
  const match = /^node "([^"\r\n]+)"$/.exec(command);
  if (!match || !path.isAbsolute(match[1]) || path.basename(match[1]) !== scriptName) return undefined;
  const agentDirectory = path.dirname(match[1]);
  if (!sameKiroIdePath(path.dirname(agentDirectory), path.join(os.homedir(), '.elydora'))) {
    return undefined;
  }
  return { agentId: path.basename(agentDirectory), scriptPath: match[1] };
}

export function legacyKiroIdeRuntimeContract(raw: string, filePath: string): KiroIdeRuntimeContract | undefined {
  const root = parseStrictJsonObject(raw, `legacy Kiro IDE hook at ${filePath}`);
  if (!exactKeys(root, ['name', 'description', 'version', 'hooks'])
    || root.name !== 'Elydora Audit'
    || root.description !== 'Sends tool-use events to the Elydora tamper-evident audit platform'
    || root.version !== '1.0.0'
    || !isObject(root.hooks)
    || !exactKeys(root.hooks, ['pre_tool_use', 'post_tool_use'])) return undefined;
  const guardHook = root.hooks.pre_tool_use;
  const auditHook = root.hooks.post_tool_use;
  if (!isObject(guardHook)
    || !isObject(auditHook)
    || !exactKeys(guardHook, ['command', 'timeout_ms'])
    || !exactKeys(auditHook, ['command', 'timeout_ms'])
    || guardHook.timeout_ms !== 5000
    || auditHook.timeout_ms !== 5000) return undefined;
  const guard = legacyReference(guardHook.command, GUARD_SCRIPT);
  const audit = legacyReference(auditHook.command, AUDIT_SCRIPT);
  if (!guard || !audit || !sameKiroIdeAgentId(guard.agentId, audit.agentId)) return undefined;
  return { agentId: guard.agentId, guardPath: guard.scriptPath, auditPath: audit.scriptPath };
}
