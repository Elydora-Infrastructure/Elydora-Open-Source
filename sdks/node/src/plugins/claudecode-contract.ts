import os from 'node:os';
import path from 'node:path';
import { isObject, parseStrictJsonObject, type JsonObject } from './strict-json.js';

export const AGENT_KEY = 'claudecode';
export const CONFIG_FILE = 'settings.json';
export const GUARD_SCRIPT = 'guard.js';
export const AUDIT_SCRIPT = 'hook.js';
export const HOOK_TIMEOUT_SECONDS = 10;
export const GUARD_STATUS = 'Checking Elydora agent state';
export const AUDIT_STATUS = 'Recording Elydora tool use';

const HOOK_EVENTS = new Set([
  'SessionStart', 'Setup', 'InstructionsLoaded', 'UserPromptSubmit',
  'UserPromptExpansion', 'MessageDisplay', 'PreToolUse', 'PermissionRequest',
  'PostToolUse', 'PostToolUseFailure', 'PostToolBatch', 'PermissionDenied',
  'Notification', 'SubagentStart', 'SubagentStop', 'TaskCreated', 'TaskCompleted',
  'Stop', 'StopFailure', 'TeammateIdle', 'ConfigChange', 'CwdChanged',
  'FileChanged', 'WorktreeCreate', 'WorktreeRemove', 'PreCompact', 'PostCompact',
  'SessionEnd', 'Elicitation', 'ElicitationResult',
]);

const COMMON_HANDLER_KEYS = ['if', 'once', 'statusMessage', 'timeout'];
const HANDLER_KEYS: Readonly<Record<string, readonly string[]>> = {
  command: [
    ...COMMON_HANDLER_KEYS,
    'args',
    'async',
    'asyncRewake',
    'command',
    'rewakeMessage',
    'rewakeSummary',
    'shell',
    'type',
  ],
  prompt: [...COMMON_HANDLER_KEYS, 'continueOnBlock', 'model', 'prompt', 'type'],
  agent: [...COMMON_HANDLER_KEYS, 'model', 'prompt', 'type'],
  http: [...COMMON_HANDLER_KEYS, 'allowedEnvVars', 'headers', 'type', 'url'],
  mcp_tool: [...COMMON_HANDLER_KEYS, 'input', 'server', 'tool', 'type'],
};

export interface ClaudeHandler extends JsonObject {
  readonly type: 'command' | 'prompt' | 'agent' | 'http' | 'mcp_tool';
}

export interface ClaudeGroup extends JsonObject {
  readonly matcher?: string;
  readonly hooks: ClaudeHandler[];
}

export type ClaudeHooks = Record<string, ClaudeGroup[]>;

export interface ClaudeDocument {
  readonly exists: boolean;
  readonly filePath: string;
  readonly root: JsonObject;
  readonly hooks: ClaudeHooks;
  readonly hooksDisabled: boolean;
  readonly raw?: string;
}

export interface RenderedClaudeDocument {
  readonly document: ClaudeDocument;
  readonly changed: boolean;
  readonly next?: string;
}

export interface ClaudeRuntimeContract {
  readonly agentId: string;
  readonly guardPath: string;
  readonly auditPath: string;
}

interface RuntimeReference {
  readonly agentId: string;
  readonly scriptPath: string;
}

function samePath(left: string, right: string): boolean {
  const normalizedLeft = path.resolve(left);
  const normalizedRight = path.resolve(right);
  return process.platform === 'win32'
    ? normalizedLeft.toLowerCase() === normalizedRight.toLowerCase()
    : normalizedLeft === normalizedRight;
}

function sameAgentId(left: string, right: string): boolean {
  return process.platform === 'win32'
    ? left.toLowerCase() === right.toLowerCase()
    : left === right;
}

function requireKnownKeys(value: JsonObject, allowed: readonly string[], label: string): void {
  const keys = new Set(allowed);
  const extra = Object.keys(value).find((key) => !keys.has(key));
  if (extra) throw new Error(`${label} contains unsupported field "${extra}"`);
}

function requireNonEmptyString(value: unknown, field: string, label: string): string {
  if (typeof value !== 'string' || value.length === 0) {
    throw new Error(`${label} field "${field}" must be a non-empty string`);
  }
  return value;
}

function optionalString(value: JsonObject, field: string, label: string): void {
  if (value[field] !== undefined && typeof value[field] !== 'string') {
    throw new Error(`${label} field "${field}" must be a string`);
  }
}

function optionalNonEmptyString(value: JsonObject, field: string, label: string): void {
  if (value[field] !== undefined) requireNonEmptyString(value[field], field, label);
}

function optionalBoolean(value: JsonObject, field: string, label: string): void {
  if (value[field] !== undefined && typeof value[field] !== 'boolean') {
    throw new Error(`${label} field "${field}" must be a boolean`);
  }
}

function validateCommonHandlerFields(value: JsonObject, label: string): void {
  optionalString(value, 'if', label);
  optionalString(value, 'statusMessage', label);
  optionalBoolean(value, 'once', label);
  if (value.timeout !== undefined && (
    typeof value.timeout !== 'number'
    || !Number.isFinite(value.timeout)
    || value.timeout <= 0
  )) {
    throw new Error(`${label} timeout must be a positive finite number`);
  }
}

function validateStringArray(value: unknown, field: string, label: string): string[] {
  if (!Array.isArray(value) || value.some((item) => typeof item !== 'string')) {
    throw new Error(`${label} field "${field}" must be an array of strings`);
  }
  return value as string[];
}

function validateHandler(value: unknown, event: string, group: number, index: number): ClaudeHandler {
  const label = `Claude Code settings handler hooks.${event}[${group}].hooks[${index}]`;
  if (!isObject(value)) throw new Error(`${label} must be an object`);
  if (typeof value.type !== 'string' || !HANDLER_KEYS[value.type]) {
    throw new Error(`${label} has unsupported type "${String(value.type)}"`);
  }
  requireKnownKeys(value, HANDLER_KEYS[value.type], label);
  validateCommonHandlerFields(value, label);
  if (value.type === 'command') {
    requireNonEmptyString(value.command, 'command', label);
    if (value.args !== undefined) validateStringArray(value.args, 'args', label);
    optionalBoolean(value, 'async', label);
    optionalBoolean(value, 'asyncRewake', label);
    optionalNonEmptyString(value, 'rewakeMessage', label);
    optionalNonEmptyString(value, 'rewakeSummary', label);
    if (value.shell !== undefined && value.shell !== 'bash' && value.shell !== 'powershell') {
      throw new Error(`${label} field "shell" must be "bash" or "powershell"`);
    }
  } else if (value.type === 'prompt' || value.type === 'agent') {
    requireNonEmptyString(value.prompt, 'prompt', label);
    optionalString(value, 'model', label);
    if (value.type === 'prompt') optionalBoolean(value, 'continueOnBlock', label);
  } else if (value.type === 'http') {
    const rawUrl = requireNonEmptyString(value.url, 'url', label);
    let url: URL;
    try {
      url = new URL(rawUrl);
    } catch (error) {
      throw new Error(`${label} field "url" must be a valid URL`, {
        cause: error instanceof Error ? error : new Error(String(error)),
      });
    }
    if (!['http:', 'https:'].includes(url.protocol) || !url.hostname) {
      throw new Error(`${label} field "url" must use HTTP or HTTPS`);
    }
    if (value.headers !== undefined && (
      !isObject(value.headers)
      || Object.values(value.headers).some((item) => typeof item !== 'string')
    )) {
      throw new Error(`${label} field "headers" must map names to strings`);
    }
    if (value.allowedEnvVars !== undefined) {
      const allowedEnvVars = validateStringArray(value.allowedEnvVars, 'allowedEnvVars', label);
      if (allowedEnvVars.some((item) => item.length === 0)) {
        throw new Error(`${label} field "allowedEnvVars" must contain non-empty strings`);
      }
    }
  } else {
    requireNonEmptyString(value.server, 'server', label);
    requireNonEmptyString(value.tool, 'tool', label);
    if (value.input !== undefined && !isObject(value.input)) {
      throw new Error(`${label} field "input" must be an object`);
    }
  }
  return value as ClaudeHandler;
}

function validateGroup(value: unknown, event: string, index: number): ClaudeGroup {
  const label = `Claude Code settings matcher group hooks.${event}[${index}]`;
  if (!isObject(value)) throw new Error(`${label} must be an object`);
  requireKnownKeys(value, ['hooks', 'matcher'], label);
  if (value.matcher !== undefined) {
    if (typeof value.matcher !== 'string') throw new Error(`${label} matcher must be a string`);
  }
  if (!Array.isArray(value.hooks)) throw new Error(`${label} must contain a hooks array`);
  return {
    ...value,
    hooks: value.hooks.map((handler, handlerIndex) => (
      validateHandler(handler, event, index, handlerIndex)
    )),
  } as ClaudeGroup;
}

function readHooks(value: unknown): ClaudeHooks {
  if (value === undefined) return {};
  if (!isObject(value)) throw new Error('Claude Code settings field "hooks" must be an object');
  const hooks: ClaudeHooks = {};
  for (const [event, groups] of Object.entries(value)) {
    if (!HOOK_EVENTS.has(event)) {
      throw new Error(`Claude Code settings contains unsupported hook event "${event}"`);
    }
    if (!Array.isArray(groups)) {
      throw new Error(`Claude Code settings field "hooks.${event}" must be an array`);
    }
    hooks[event] = groups.map((group, index) => validateGroup(group, event, index));
  }
  return hooks;
}

export function parseClaudeDocument(filePath: string, raw: string): ClaudeDocument {
  const root = parseStrictJsonObject(raw, `Claude Code user settings at ${filePath}`);
  if (root.disableAllHooks !== undefined && typeof root.disableAllHooks !== 'boolean') {
    throw new Error('Claude Code settings field "disableAllHooks" must be a boolean');
  }
  return {
    exists: true,
    filePath,
    root,
    hooks: readHooks(root.hooks),
    hooksDisabled: root.disableAllHooks === true,
    raw,
  };
}

export function createClaudeDocument(filePath: string): ClaudeDocument {
  return { exists: false, filePath, root: {}, hooks: {}, hooksDisabled: false };
}

export function buildClaudeGroup(scriptPath: string, statusMessage: string): ClaudeGroup {
  return {
    hooks: [{
      type: 'command',
      command: process.execPath,
      args: [scriptPath],
      timeout: HOOK_TIMEOUT_SECONDS,
      statusMessage,
    }],
  };
}

function exactManagedGroup(group: ClaudeGroup): boolean {
  return Object.keys(group).length === 1 && Object.hasOwn(group, 'hooks');
}

function runtimeReference(scriptPath: string, scriptName: string): RuntimeReference | undefined {
  if (!path.isAbsolute(scriptPath) || path.basename(scriptPath) !== scriptName) return undefined;
  const agentDirectory = path.dirname(scriptPath);
  if (!samePath(path.dirname(agentDirectory), path.join(os.homedir(), '.elydora'))) return undefined;
  const agentId = path.basename(agentDirectory);
  return agentId && agentId !== '.' && agentId !== '..' ? { agentId, scriptPath } : undefined;
}

function managedReference(
  handler: ClaudeHandler,
  scriptName: string,
  statusMessage: string,
  includeLegacy = false,
): RuntimeReference | undefined {
  const keys = Object.keys(handler).sort().join('|');
  if (keys === 'args|command|statusMessage|timeout|type'
    && handler.type === 'command'
    && handler.timeout === HOOK_TIMEOUT_SECONDS
    && handler.statusMessage === statusMessage
    && typeof handler.command === 'string'
    && samePath(handler.command, process.execPath)
    && Array.isArray(handler.args)
    && handler.args.length === 1
    && typeof handler.args[0] === 'string') {
    return runtimeReference(handler.args[0], scriptName);
  }
  if (!includeLegacy || keys !== 'command|type' || handler.type !== 'command') return undefined;
  const match = typeof handler.command === 'string'
    ? /^node "([^"\r\n]+)"$/.exec(handler.command)
    : undefined;
  return match ? runtimeReference(match[1], scriptName) : undefined;
}

function removeFromGroups(
  groups: readonly ClaudeGroup[],
  scriptName: string,
  statusMessage: string,
  agentId?: string,
): { groups: ClaudeGroup[]; removed: boolean } {
  const result: ClaudeGroup[] = [];
  let removed = false;
  for (const group of groups) {
    const handlers = group.hooks.filter((handler) => {
      const reference = managedReference(handler, scriptName, statusMessage, true);
      const owned = reference !== undefined
        && (agentId === undefined || sameAgentId(reference.agentId, agentId));
      if (owned) removed = true;
      return !owned;
    });
    if (handlers.length > 0) result.push({ ...group, hooks: handlers });
    else if (!exactManagedGroup(group)) result.push({ ...group, hooks: [] });
  }
  return { groups: result, removed };
}

export function removeManagedClaudeHooks(hooks: ClaudeHooks, agentId?: string): ClaudeHooks {
  const next: ClaudeHooks = Object.fromEntries(
    Object.entries(hooks).map(([event, groups]) => [event, [...groups]]),
  );
  for (const [event, scriptName, statusMessage] of [
    ['PreToolUse', GUARD_SCRIPT, GUARD_STATUS],
    ['PostToolUse', AUDIT_SCRIPT, AUDIT_STATUS],
    ['PostToolUseFailure', AUDIT_SCRIPT, AUDIT_STATUS],
  ] as const) {
    if (!next[event]) continue;
    const result = removeFromGroups(next[event], scriptName, statusMessage, agentId);
    if (!result.removed) continue;
    if (result.groups.length > 0) next[event] = result.groups;
    else delete next[event];
  }
  return next;
}

function managedEvent(event: string): readonly [string, string] | undefined {
  if (event === 'PreToolUse') return [GUARD_SCRIPT, GUARD_STATUS];
  if (event === 'PostToolUse' || event === 'PostToolUseFailure') {
    return [AUDIT_SCRIPT, AUDIT_STATUS];
  }
  return undefined;
}

function entirelyManaged(document: ClaudeDocument): boolean {
  if (!document.exists || !Object.keys(document.root).every((key) => key === 'hooks')) return false;
  const events = Object.entries(document.hooks);
  if (events.length === 0) return false;
  let handlers = 0;
  for (const [event, groups] of events) {
    const contract = managedEvent(event);
    if (!contract || groups.length === 0) return false;
    for (const group of groups) {
      if (!exactManagedGroup(group)
        || group.hooks.length === 0
        || group.hooks.some((handler) => (
          !managedReference(handler, contract[0], contract[1], true)
        ))) return false;
      handlers += group.hooks.length;
    }
  }
  return handlers > 0;
}

export function renderClaudeDocument(
  document: ClaudeDocument,
  hooks: ClaudeHooks,
): RenderedClaudeDocument {
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
  groups: readonly ClaudeGroup[],
  scriptName: string,
  statusMessage: string,
): Map<string, RuntimeReference[]> {
  const references = new Map<string, RuntimeReference[]>();
  for (const group of groups) {
    if (!exactManagedGroup(group)) continue;
    for (const handler of group.hooks) {
      const reference = managedReference(handler, scriptName, statusMessage);
      if (!reference) continue;
      const key = process.platform === 'win32'
        ? reference.agentId.toLowerCase()
        : reference.agentId;
      references.set(key, [...(references.get(key) ?? []), reference]);
    }
  }
  return references;
}

export function claudeRuntimeContracts(hooks: ClaudeHooks): ClaudeRuntimeContract[] {
  const guards = referencesForEvent(hooks.PreToolUse ?? [], GUARD_SCRIPT, GUARD_STATUS);
  const successes = referencesForEvent(hooks.PostToolUse ?? [], AUDIT_SCRIPT, AUDIT_STATUS);
  const failures = referencesForEvent(
    hooks.PostToolUseFailure ?? [],
    AUDIT_SCRIPT,
    AUDIT_STATUS,
  );
  const contracts: ClaudeRuntimeContract[] = [];
  for (const [key, guard] of guards) {
    const success = successes.get(key);
    const failure = failures.get(key);
    if (guard.length !== 1 || success?.length !== 1 || failure?.length !== 1) continue;
    if (!samePath(success[0].scriptPath, failure[0].scriptPath)) continue;
    contracts.push({
      agentId: guard[0].agentId,
      guardPath: guard[0].scriptPath,
      auditPath: success[0].scriptPath,
    });
  }
  return contracts;
}
