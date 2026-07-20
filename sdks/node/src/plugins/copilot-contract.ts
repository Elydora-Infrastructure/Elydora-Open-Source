import os from 'node:os';
import path from 'node:path';

export const AGENT_KEY = 'copilot';
export const GUARD_SCRIPT = 'guard.js';
export const AUDIT_SCRIPT = 'hook.js';
export const HOOK_TIMEOUT_SECONDS = 10;
export const CONFIG_FILE = 'elydora-audit.json';

export type JsonObject = Record<string, unknown>;
export type CopilotHooks = Record<string, JsonObject[]>;

export interface CopilotDocument {
  readonly exists: boolean;
  readonly filePath: string;
  readonly root: JsonObject;
  readonly hooks: CopilotHooks;
  readonly raw?: string;
}

export interface CopilotSources {
  readonly user: CopilotDocument;
  readonly legacy?: CopilotDocument;
}

export interface RenderedDocument {
  readonly document: CopilotDocument;
  readonly changed: boolean;
  readonly next?: string;
}

export interface RuntimeContract {
  readonly agentId: string;
  readonly guardPath: string;
  readonly auditPath: string;
}

interface ParsedArgument {
  readonly value: string;
  readonly next: number;
}

export function isObject(value: unknown): value is JsonObject {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
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

function quotePosix(value: string): string {
  return `'${value.replaceAll("'", `'"'"'`)}'`;
}

function quotePowerShell(value: string): string {
  return `'${value.replaceAll("'", "''")}'`;
}

export function buildHandler(scriptPath: string): JsonObject {
  return {
    type: 'command',
    bash: `${quotePosix(process.execPath)} ${quotePosix(scriptPath)}`,
    powershell: `& ${quotePowerShell(process.execPath)} ${quotePowerShell(scriptPath)}; exit $LASTEXITCODE`,
    timeoutSec: HOOK_TIMEOUT_SECONDS,
  };
}

function readPosixArgument(command: string, start: number): ParsedArgument | undefined {
  if (command[start] !== "'") return undefined;
  const apostrophe = `'"'"'`;
  let value = '';
  for (let index = start + 1; index < command.length;) {
    if (command.startsWith(apostrophe, index)) {
      value += "'";
      index += apostrophe.length;
      continue;
    }
    if (command[index] === "'") return { value, next: index + 1 };
    value += command[index];
    index += 1;
  }
  return undefined;
}

function parseGeneratedBash(command: unknown): readonly [string, string] | undefined {
  if (typeof command !== 'string') return undefined;
  const executable = readPosixArgument(command, 0);
  if (!executable || command[executable.next] !== ' ') return undefined;
  const script = readPosixArgument(command, executable.next + 1);
  if (!script || script.next !== command.length) return undefined;
  return [executable.value, script.value];
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

function parseGeneratedPowerShell(command: unknown): readonly [string, string] | undefined {
  if (typeof command !== 'string' || !command.startsWith('& ')) return undefined;
  const executable = readPowerShellArgument(command, 2);
  if (!executable || command[executable.next] !== ' ') return undefined;
  const script = readPowerShellArgument(command, executable.next + 1);
  if (!script || command.slice(script.next) !== '; exit $LASTEXITCODE') return undefined;
  return [executable.value, script.value];
}

function parseLegacyCommand(command: unknown): string | undefined {
  if (typeof command !== 'string') return undefined;
  return /^node(?:\.exe)?\s+"([^"]+)"$/i.exec(command)?.[1];
}

function managedScriptPath(handler: JsonObject): string | undefined {
  if (handler.type !== 'command') return undefined;
  const bash = parseGeneratedBash(handler.bash);
  const powershell = parseGeneratedPowerShell(handler.powershell);
  if (handler.timeoutSec === HOOK_TIMEOUT_SECONDS && bash && powershell) {
    if (samePath(bash[0], process.execPath)
      && samePath(powershell[0], process.execPath)
      && samePath(bash[1], powershell[1])) return bash[1];
  }
  const legacyBash = parseLegacyCommand(handler.bash);
  const legacyPowerShell = parseLegacyCommand(handler.powershell);
  return legacyBash && legacyPowerShell && samePath(legacyBash, legacyPowerShell)
    ? legacyBash
    : undefined;
}

function managedAgentId(handler: JsonObject, scriptName: string): string | undefined {
  const scriptPath = managedScriptPath(handler);
  if (!scriptPath || path.basename(scriptPath) !== scriptName) return undefined;
  const agentDirectory = path.dirname(scriptPath);
  if (!samePath(path.dirname(agentDirectory), path.join(os.homedir(), '.elydora'))) return undefined;
  const agentId = path.basename(agentDirectory);
  return agentId && agentId !== '.' && agentId !== '..' ? agentId : undefined;
}

function validateHooks(value: unknown, label: string): CopilotHooks {
  if (value === undefined) return {};
  if (!isObject(value)) throw new Error(`${label} field "hooks" must be an object`);
  const hooks: CopilotHooks = {};
  for (const [event, handlers] of Object.entries(value)) {
    if (!Array.isArray(handlers)) throw new Error(`${label} field "hooks.${event}" must be an array`);
    hooks[event] = handlers.map((handler, index) => {
      if (!isObject(handler)) throw new Error(`${label} handler hooks.${event}[${index}] must be an object`);
      return handler;
    });
  }
  return hooks;
}

export function parseDocument(filePath: string, raw: string, label: string): CopilotDocument {
  let value: unknown;
  try {
    value = JSON.parse(raw);
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    throw new Error(`Failed to parse ${label} at ${filePath}: ${message}`, { cause: error });
  }
  if (!isObject(value)) throw new Error(`${label} at ${filePath} must contain a JSON object`);
  if (value.version !== 1) throw new Error(`${label} at ${filePath} must declare version 1`);
  return {
    exists: true,
    filePath,
    root: value,
    hooks: validateHooks(value.hooks, label),
    raw,
  };
}

export function createDocument(filePath: string): CopilotDocument {
  return { exists: false, filePath, root: {}, hooks: {} };
}

export function removeManagedHooks(hooks: CopilotHooks, agentId?: string): CopilotHooks {
  const next: CopilotHooks = { ...hooks };
  for (const [event, scriptName] of [
    ['preToolUse', GUARD_SCRIPT],
    ['postToolUse', AUDIT_SCRIPT],
  ] as const) {
    const handlers = (next[event] ?? []).filter((handler) => {
      const managedId = managedAgentId(handler, scriptName);
      return !managedId || (agentId !== undefined && !sameAgentId(managedId, agentId));
    });
    if (handlers.length > 0) next[event] = handlers;
    else delete next[event];
  }
  return next;
}

function isEmptyOwnedDocument(root: JsonObject, hooks: CopilotHooks): boolean {
  return Object.keys(hooks).length === 0
    && Object.keys(root).every((key) => key === 'version' || key === 'hooks');
}

export function renderDocument(
  document: CopilotDocument,
  hooks: CopilotHooks,
): RenderedDocument {
  if (!document.exists && Object.keys(hooks).length === 0) {
    return { document, changed: false };
  }
  if (document.exists && isEmptyOwnedDocument(document.root, hooks)) {
    return { document, changed: true };
  }
  const root: JsonObject = { ...document.root, version: 1 };
  if (Object.keys(hooks).length > 0) root.hooks = hooks;
  else delete root.hooks;
  const next = JSON.stringify(root, null, 2) + '\n';
  return { document, changed: next !== document.raw, next };
}

function managedIds(handlers: JsonObject[], scriptName: string): Set<string> {
  return new Set(handlers.flatMap((handler) => {
    const agentId = managedAgentId(handler, scriptName);
    return agentId ? [agentId] : [];
  }));
}

export function runtimeContracts(hooks: CopilotHooks): RuntimeContract[] {
  const guards = managedIds(hooks.preToolUse ?? [], GUARD_SCRIPT);
  const audits = managedIds(hooks.postToolUse ?? [], AUDIT_SCRIPT);
  const root = path.join(os.homedir(), '.elydora');
  return [...guards]
    .filter((agentId) => [...audits].some((auditId) => sameAgentId(agentId, auditId)))
    .map((agentId) => ({
      agentId,
      guardPath: path.join(root, agentId, GUARD_SCRIPT),
      auditPath: path.join(root, agentId, AUDIT_SCRIPT),
    }));
}
