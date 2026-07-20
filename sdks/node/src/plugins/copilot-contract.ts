import os from 'node:os';
import path from 'node:path';
import type { FileSnapshot } from './managed-files.js';
import { validateHooks, type CopilotHooks } from './copilot-schema.js';
import { parseStrictJsonObject, type JsonObject } from './strict-json.js';

export const AGENT_KEY = 'copilot';
export const GUARD_SCRIPT = 'guard.js';
export const AUDIT_SCRIPT = 'hook.js';
export const HOOK_TIMEOUT_SECONDS = 10;
export const CONFIG_FILE = 'elydora-audit.json';

const MANAGED_EVENTS = [
  ['preToolUse', GUARD_SCRIPT],
  ['postToolUse', AUDIT_SCRIPT],
  ['postToolUseFailure', AUDIT_SCRIPT],
] as const;

export interface CopilotDocument {
  readonly exists: boolean;
  readonly filePath: string;
  readonly root: JsonObject;
  readonly hooks: CopilotHooks;
  readonly hooksDisabled: boolean;
  readonly raw?: string;
  readonly snapshot?: FileSnapshot;
}

export interface CopilotSources {
  readonly user: CopilotDocument;
  readonly legacy?: CopilotDocument;
  readonly disabledBy?: string;
  readonly settingsPreconditions: readonly CopilotSourcePrecondition[];
}

export interface CopilotSourcePrecondition {
  readonly filePath: string;
  readonly label: string;
  readonly snapshot?: FileSnapshot;
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

interface ManagedEntry {
  readonly agentId: string;
  readonly scriptPath: string;
}

export function samePath(left: string, right: string): boolean {
  const normalizedLeft = path.resolve(left);
  const normalizedRight = path.resolve(right);
  return process.platform === 'win32'
    ? normalizedLeft.toLowerCase() === normalizedRight.toLowerCase()
    : normalizedLeft === normalizedRight;
}

export function sameAgentId(left: string, right: string): boolean {
  return process.platform === 'win32'
    ? left.toLowerCase() === right.toLowerCase()
    : left === right;
}

function agentKey(agentId: string): string {
  return process.platform === 'win32' ? agentId.toLowerCase() : agentId;
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

function exactHandlerKeys(handler: JsonObject): boolean {
  return Object.keys(handler).sort().join(',') === 'bash,powershell,timeoutSec,type';
}

function nodeExecutable(filePath: string): boolean {
  if (!path.isAbsolute(filePath)) return false;
  const name = path.basename(filePath).toLowerCase();
  return name === 'node' || name === 'node.exe';
}

function currentManagedScriptPath(handler: JsonObject): string | undefined {
  if (!exactHandlerKeys(handler)
    || handler.type !== 'command'
    || handler.timeoutSec !== HOOK_TIMEOUT_SECONDS) return undefined;
  const bash = parseGeneratedBash(handler.bash);
  const powershell = parseGeneratedPowerShell(handler.powershell);
  if (!bash || !powershell
    || !nodeExecutable(bash[0])
    || !samePath(bash[0], powershell[0])
    || !samePath(bash[1], powershell[1])) return undefined;
  return bash[1];
}

function parseLegacyCommand(command: unknown): string | undefined {
  if (typeof command !== 'string') return undefined;
  return /^node(?:\.exe)?\s+"([^"]+)"$/i.exec(command)?.[1];
}

function legacyManagedScriptPath(handler: JsonObject): string | undefined {
  if (!exactHandlerKeys(handler)
    || handler.type !== 'command'
    || handler.timeoutSec !== 5) return undefined;
  const bash = parseLegacyCommand(handler.bash);
  const powershell = parseLegacyCommand(handler.powershell);
  return bash && powershell && samePath(bash, powershell) ? bash : undefined;
}

function managedEntry(handler: JsonObject, scriptName: string): ManagedEntry | undefined {
  const scriptPath = currentManagedScriptPath(handler) ?? legacyManagedScriptPath(handler);
  if (!scriptPath || path.basename(scriptPath).toLowerCase() !== scriptName.toLowerCase()) {
    return undefined;
  }
  const agentDirectory = path.dirname(scriptPath);
  if (!samePath(path.dirname(agentDirectory), path.join(os.homedir(), '.elydora'))) {
    return undefined;
  }
  const agentId = path.basename(agentDirectory);
  if (!agentId || agentId === '.' || agentId === '..') return undefined;
  return { agentId, scriptPath };
}

export function parseDocument(
  filePath: string,
  snapshot: FileSnapshot,
  label: string,
): CopilotDocument {
  const root = parseStrictJsonObject(snapshot.contents, `${label} at ${filePath}`);
  if (root.version !== 1) throw new Error(`${label} at ${filePath} must declare version 1`);
  if (root.disableAllHooks !== undefined && typeof root.disableAllHooks !== 'boolean') {
    throw new Error(`${label} at ${filePath} field "disableAllHooks" must be a boolean`);
  }
  return {
    exists: true,
    filePath,
    root,
    hooks: validateHooks(root.hooks, `${label} at ${filePath}`),
    hooksDisabled: root.disableAllHooks === true,
    raw: snapshot.contents,
    snapshot,
  };
}

export function createDocument(filePath: string): CopilotDocument {
  return { exists: false, filePath, root: {}, hooks: {}, hooksDisabled: false };
}

export function removeManagedHooks(hooks: CopilotHooks, agentId?: string): CopilotHooks {
  const next: CopilotHooks = { ...hooks };
  for (const [event, scriptName] of MANAGED_EVENTS) {
    const handlers = (next[event] ?? []).filter((handler) => {
      const owned = managedEntry(handler, scriptName);
      return !owned || (agentId !== undefined && !sameAgentId(owned.agentId, agentId));
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
  const next = `${JSON.stringify(root, null, 2)}\n`;
  return { document, changed: next !== document.raw, next };
}

function managedEntries(handlers: JsonObject[], scriptName: string): Map<string, ManagedEntry> {
  const entries = new Map<string, ManagedEntry>();
  for (const handler of handlers) {
    const entry = managedEntry(handler, scriptName);
    if (entry) entries.set(agentKey(entry.agentId), entry);
  }
  return entries;
}

export function runtimeContracts(hooks: CopilotHooks): RuntimeContract[] {
  const guards = managedEntries(hooks.preToolUse ?? [], GUARD_SCRIPT);
  const successes = managedEntries(hooks.postToolUse ?? [], AUDIT_SCRIPT);
  const failures = managedEntries(hooks.postToolUseFailure ?? [], AUDIT_SCRIPT);
  const contracts: RuntimeContract[] = [];
  for (const [key, guard] of guards) {
    const success = successes.get(key);
    const failure = failures.get(key);
    if (!success || !failure || !samePath(success.scriptPath, failure.scriptPath)) continue;
    contracts.push({
      agentId: guard.agentId,
      guardPath: guard.scriptPath,
      auditPath: success.scriptPath,
    });
  }
  return contracts;
}
