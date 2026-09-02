import os from 'node:os';
import path from 'node:path';
import {
  buildKiroIdeCommand,
  kiroIdeRuntimeReference,
  sameKiroIdeAgentId,
  sameKiroIdePath,
  type KiroIdeRuntimeReference,
} from './kiroide-command.js';
import {
  buildKiroIdeHook,
  kiroIdeRuntimeContracts,
  renderKiroIdeDocument,
  type KiroIdeDocument,
  type KiroIdeHook,
  type RenderedKiroIdeDocument,
  type KiroIdeRuntimeContract,
} from './kiroide-contract.js';
import { isObject, parseStrictJsonObject, type JsonObject } from './strict-json.js';

export const AGENT_KEY = 'kirocli';
export const AUDIT_SCRIPT = 'hook.js';
export const GUARD_SCRIPT = 'guard.js';
export const V2_AGENT_NAME = 'elydora-audit';
export const V3_CONFIG_FILE = 'elydora-audit.json';
export const HOOK_TIMEOUT_MILLISECONDS = 10_000;

const V2_DESCRIPTION = 'Kiro CLI with Elydora audit and freeze enforcement';
const V3_GUARD_NAME = 'elydora-guard';
const V3_AUDIT_NAME = 'elydora-audit';
const V2_EVENTS = new Set([
  'agentSpawn',
  'userPromptSubmit',
  'preToolUse',
  'postToolUse',
  'stop',
]);

export interface KiroCliPaths {
  readonly homeDirectory: string;
  readonly kiroDirectory: string;
  readonly agentsDirectory: string;
  readonly hooksDirectory: string;
  readonly v2Path: string;
  readonly v3Path: string;
}

export interface KiroCliV2Hook extends JsonObject {
  readonly command: string;
  readonly matcher?: string;
  readonly timeout_ms?: number;
  readonly cache_ttl_seconds?: number;
}

export interface KiroCliV2Document {
  readonly exists: boolean;
  readonly filePath: string;
  readonly root: JsonObject;
  readonly hooks: JsonObject;
  readonly raw?: string;
}

export interface RenderedKiroCliV2Document {
  readonly document: KiroCliV2Document;
  readonly changed: boolean;
  readonly next?: string;
}

export function resolveKiroCliPaths(homeDirectory = os.homedir()): KiroCliPaths {
  const resolvedHome = path.resolve(homeDirectory);
  const kiroDirectory = path.join(resolvedHome, '.kiro');
  const agentsDirectory = path.join(kiroDirectory, 'agents');
  const hooksDirectory = path.join(kiroDirectory, 'hooks');
  return {
    homeDirectory: resolvedHome,
    kiroDirectory,
    agentsDirectory,
    hooksDirectory,
    v2Path: path.join(agentsDirectory, `${V2_AGENT_NAME}.json`),
    v3Path: path.join(hooksDirectory, V3_CONFIG_FILE),
  };
}

function validateOptionalInteger(value: unknown, field: string, label: string): void {
  if (value !== undefined && (!Number.isSafeInteger(value) || (value as number) < 0)) {
    throw new Error(`${label} ${field} must be a non-negative integer`);
  }
}

function validateV2Hook(value: unknown, event: string, index: number): KiroCliV2Hook {
  const label = `Kiro CLI v2 agent hooks.${event}[${index}]`;
  if (!isObject(value)) throw new Error(`${label} must be an object`);
  if (typeof value.command !== 'string' || !value.command) {
    throw new Error(`${label} command must be a non-empty string`);
  }
  if (value.matcher !== undefined) {
    if (!['preToolUse', 'postToolUse'].includes(event)) {
      throw new Error(`${label} matcher is valid only for tool events`);
    }
    if (typeof value.matcher !== 'string' || !value.matcher) {
      throw new Error(`${label} matcher must be a non-empty string`);
    }
  }
  validateOptionalInteger(value.timeout_ms, 'timeout_ms', label);
  validateOptionalInteger(value.cache_ttl_seconds, 'cache_ttl_seconds', label);
  return value as KiroCliV2Hook;
}

function validateV2Hooks(value: unknown, filePath: string): JsonObject {
  if (value === undefined) return {};
  if (!isObject(value)) {
    throw new Error(`Kiro CLI v2 agent field "hooks" must be an object: ${filePath}`);
  }
  const hooks: JsonObject = { ...value };
  for (const [event, entries] of Object.entries(value)) {
    if (!V2_EVENTS.has(event)) {
      throw new Error(`Kiro CLI v2 agent has unsupported hook event "${event}": ${filePath}`);
    }
    if (!Array.isArray(entries)) {
      throw new Error(`Kiro CLI v2 agent hooks.${event} must be an array: ${filePath}`);
    }
    hooks[event] = entries.map((entry, index) => validateV2Hook(entry, event, index));
  }
  return hooks;
}

function validateOptionalV2Fields(root: JsonObject, filePath: string): void {
  for (const field of ['name', 'description', 'prompt', 'model', 'keyboardShortcut', 'welcomeMessage']) {
    if (root[field] !== undefined && typeof root[field] !== 'string') {
      throw new Error(`Kiro CLI v2 agent field "${field}" must be a string: ${filePath}`);
    }
  }
  for (const field of ['tools', 'allowedTools', 'resources']) {
    const value = root[field];
    if (value !== undefined
      && (!Array.isArray(value) || !value.every((entry) => typeof entry === 'string'))) {
      throw new Error(`Kiro CLI v2 agent field "${field}" must be an array of strings: ${filePath}`);
    }
  }
  if (root.includeMcpJson !== undefined && typeof root.includeMcpJson !== 'boolean') {
    throw new Error(`Kiro CLI v2 agent field "includeMcpJson" must be a boolean: ${filePath}`);
  }
}

export function parseKiroCliV2Document(filePath: string, raw: string): KiroCliV2Document {
  const root = parseStrictJsonObject(raw, `Kiro CLI v2 agent at ${filePath}`);
  validateOptionalV2Fields(root, filePath);
  return {
    exists: true,
    filePath,
    root,
    hooks: validateV2Hooks(root.hooks, filePath),
    raw,
  };
}

export function createKiroCliV2Document(filePath: string): KiroCliV2Document {
  return { exists: false, filePath, root: {}, hooks: {} };
}

function eventHooks(document: KiroCliV2Document, event: string): readonly KiroCliV2Hook[] {
  return (document.hooks[event] as readonly KiroCliV2Hook[] | undefined) ?? [];
}

function exactKeys(value: JsonObject, keys: readonly string[]): boolean {
  return Object.keys(value).sort().join('|') === [...keys].sort().join('|');
}

function legacyWindowsReference(
  command: unknown,
  scriptName: string,
): KiroIdeRuntimeReference | undefined {
  if (typeof command !== 'string') return undefined;
  const match = /^"([^"\r\n]+)" "([^"\r\n]+)"$/.exec(command);
  if (!match
    || !path.isAbsolute(match[1])
    || !['node', 'node.exe'].includes(path.basename(match[1]).toLowerCase())
    || !path.isAbsolute(match[2])
    || path.basename(match[2]) !== scriptName) return undefined;
  const agentDirectory = path.dirname(match[2]);
  if (!sameKiroIdePath(path.dirname(agentDirectory), path.join(os.homedir(), '.elydora'))) {
    return undefined;
  }
  const agentId = path.basename(agentDirectory);
  return agentId ? { agentId, scriptPath: match[2] } : undefined;
}

function ownedReference(command: unknown, scriptName: string): KiroIdeRuntimeReference | undefined {
  return typeof command === 'string'
    ? kiroIdeRuntimeReference(command, scriptName) ?? legacyWindowsReference(command, scriptName)
    : undefined;
}

function v2OwnedReference(hook: KiroCliV2Hook, event: string) {
  if (event === 'preToolUse') return ownedReference(hook.command, GUARD_SCRIPT);
  if (event === 'postToolUse') return ownedReference(hook.command, AUDIT_SCRIPT);
  return undefined;
}

function v2ManagedReference(hook: KiroCliV2Hook, event: string) {
  const reference = v2OwnedReference(hook, event);
  if (!reference
    || !exactKeys(hook, ['matcher', 'command', 'timeout_ms'])
    || hook.matcher !== '*'
    || hook.timeout_ms !== HOOK_TIMEOUT_MILLISECONDS) return undefined;
  return reference;
}

function managedMetadata(root: JsonObject): boolean {
  return root.name === V2_AGENT_NAME
    && root.description === V2_DESCRIPTION
    && Array.isArray(root.tools)
    && root.tools.length === 1
    && root.tools[0] === '*'
    && root.includeMcpJson === true;
}

function hasOwnedV2Hooks(document: KiroCliV2Document, agentId?: string): boolean {
  return ['preToolUse', 'postToolUse'].some((event) => eventHooks(document, event).some((hook) => {
    const reference = v2OwnedReference(hook, event);
    return reference !== undefined
      && (agentId === undefined || sameKiroIdeAgentId(reference.agentId, agentId));
  }));
}

export function requireAvailableKiroCliV2Document(document: KiroCliV2Document): void {
  if (document.exists && !managedMetadata(document.root) && !hasOwnedV2Hooks(document)) {
    throw new Error(`Kiro CLI v2 agent path conflicts with the Elydora contract: ${document.filePath}`);
  }
}

export function withoutManagedKiroCliV2Hooks(
  document: KiroCliV2Document,
  agentId?: string,
): JsonObject {
  const next = { ...document.hooks };
  for (const event of ['preToolUse', 'postToolUse']) {
    const remaining = eventHooks(document, event).filter((hook) => {
      const reference = v2OwnedReference(hook, event);
      return !reference || (agentId !== undefined && !sameKiroIdeAgentId(reference.agentId, agentId));
    });
    if (remaining.length > 0) next[event] = remaining;
    else delete next[event];
  }
  return next;
}

function buildV2Hook(scriptPath: string): KiroCliV2Hook {
  return {
    matcher: '*',
    command: buildKiroIdeCommand(scriptPath),
    timeout_ms: HOOK_TIMEOUT_MILLISECONDS,
  };
}

export function buildKiroCliV2Hooks(
  document: KiroCliV2Document,
  guardPath: string,
  auditPath: string,
): JsonObject {
  const hooks = withoutManagedKiroCliV2Hooks(document);
  hooks.preToolUse = [...eventHooks({ ...document, hooks }, 'preToolUse'), buildV2Hook(guardPath)];
  hooks.postToolUse = [...eventHooks({ ...document, hooks }, 'postToolUse'), buildV2Hook(auditPath)];
  return hooks;
}

export function renderKiroCliV2Installation(
  document: KiroCliV2Document,
  hooks: JsonObject,
): RenderedKiroCliV2Document {
  const next = `${JSON.stringify({
    ...document.root,
    name: V2_AGENT_NAME,
    description: V2_DESCRIPTION,
    tools: ['*'],
    includeMcpJson: true,
    hooks,
  }, null, 2)}\n`;
  return { document, changed: next !== document.raw, next };
}

function entirelyManagedV2Document(document: KiroCliV2Document, agentId?: string): boolean {
  if (!document.exists
    || !managedMetadata(document.root)
    || !exactKeys(document.root, ['name', 'description', 'tools', 'includeMcpJson', 'hooks'])
    || !exactKeys(document.hooks, ['preToolUse', 'postToolUse'])) return false;
  for (const event of ['preToolUse', 'postToolUse']) {
    const hooks = eventHooks(document, event);
    if (hooks.length !== 1) return false;
    const reference = v2OwnedReference(hooks[0], event);
    if (!reference || (agentId !== undefined && !sameKiroIdeAgentId(reference.agentId, agentId))) {
      return false;
    }
  }
  return true;
}

export function renderKiroCliV2Uninstall(
  document: KiroCliV2Document,
  hooks: JsonObject,
  agentId?: string,
): RenderedKiroCliV2Document {
  if (!hasOwnedV2Hooks(document, agentId)) return { document, changed: false };
  if (entirelyManagedV2Document(document, agentId) && Object.keys(hooks).length === 0) {
    return { document, changed: true };
  }
  const next = `${JSON.stringify({ ...document.root, hooks }, null, 2)}\n`;
  return { document, changed: next !== document.raw, next };
}

export function kiroCliV2RuntimeContracts(
  document: KiroCliV2Document,
): KiroIdeRuntimeContract[] {
  for (const event of ['preToolUse', 'postToolUse']) {
    if (eventHooks(document, event).some((hook) => (
      v2OwnedReference(hook, event) !== undefined
      && v2ManagedReference(hook, event) === undefined
    ))) return [];
  }
  const guards = eventHooks(document, 'preToolUse')
    .map((hook) => v2ManagedReference(hook, 'preToolUse'))
    .filter((value): value is KiroIdeRuntimeReference => value !== undefined);
  const audits = eventHooks(document, 'postToolUse')
    .map((hook) => v2ManagedReference(hook, 'postToolUse'))
    .filter((value): value is KiroIdeRuntimeReference => value !== undefined);
  if (guards.length !== 1 || audits.length !== 1
    || !sameKiroIdeAgentId(guards[0].agentId, audits[0].agentId)) return [];
  return [{ agentId: guards[0].agentId, guardPath: guards[0].scriptPath, auditPath: audits[0].scriptPath }];
}

function v3Specification(name: string) {
  if (name === V3_GUARD_NAME) return { scriptName: GUARD_SCRIPT };
  if (name === V3_AUDIT_NAME) return { scriptName: AUDIT_SCRIPT };
  return undefined;
}

function v3OwnedReference(hook: KiroIdeHook) {
  const specification = v3Specification(hook.name);
  return specification && hook.action.type === 'command'
    ? ownedReference(hook.action.command, specification.scriptName)
    : undefined;
}

export function requireAvailableKiroCliV3Hooks(hooks: readonly KiroIdeHook[]): void {
  for (const hook of hooks) {
    if (v3Specification(hook.name) && !v3OwnedReference(hook)) {
      throw new Error(`Kiro CLI v3 hook name "${hook.name}" conflicts with the Elydora contract`);
    }
  }
}

export function withoutManagedKiroCliV3Hooks(
  hooks: readonly KiroIdeHook[],
  agentId?: string,
): KiroIdeHook[] {
  return hooks.filter((hook) => {
    const reference = v3OwnedReference(hook);
    return !reference || (agentId !== undefined && !sameKiroIdeAgentId(reference.agentId, agentId));
  });
}

function hasOwnedV3Hooks(hooks: readonly KiroIdeHook[], agentId?: string): boolean {
  return hooks.some((hook) => {
    const reference = v3OwnedReference(hook);
    return reference !== undefined
      && (agentId === undefined || sameKiroIdeAgentId(reference.agentId, agentId));
  });
}

function entirelyManagedV3Document(document: KiroIdeDocument, agentId?: string): boolean {
  return document.exists
    && exactKeys(document.root, ['version', 'hooks'])
    && document.hooks.length > 0
    && document.hooks.every((hook) => {
      const reference = v3OwnedReference(hook);
      return reference !== undefined
        && (agentId === undefined || sameKiroIdeAgentId(reference.agentId, agentId));
    });
}

export function renderKiroCliV3Uninstall(
  document: KiroIdeDocument,
  hooks: readonly KiroIdeHook[],
  agentId?: string,
): RenderedKiroIdeDocument {
  if (!hasOwnedV3Hooks(document.hooks, agentId)) return { document, changed: false };
  if (hooks.length === 0 && entirelyManagedV3Document(document, agentId)) {
    return { document, changed: true };
  }
  return renderKiroIdeDocument(document, hooks);
}

export function buildKiroCliV3Hooks(
  hooks: readonly KiroIdeHook[],
  guardPath: string,
  auditPath: string,
): KiroIdeHook[] {
  return [
    ...withoutManagedKiroCliV3Hooks(hooks),
    buildKiroIdeHook(V3_GUARD_NAME, guardPath),
    buildKiroIdeHook(V3_AUDIT_NAME, auditPath),
  ];
}

export function commonKiroCliRuntimeContract(
  v2Document: KiroCliV2Document,
  v3Hooks: readonly KiroIdeHook[],
): KiroIdeRuntimeContract | undefined {
  const v2 = kiroCliV2RuntimeContracts(v2Document);
  const v3 = kiroIdeRuntimeContracts(v3Hooks);
  if (v2.length !== 1 || v3.length !== 1
    || !sameKiroIdeAgentId(v2[0].agentId, v3[0].agentId)
    || !sameKiroIdePath(v2[0].guardPath, v3[0].guardPath)
    || !sameKiroIdePath(v2[0].auditPath, v3[0].auditPath)) return undefined;
  return v2[0];
}
