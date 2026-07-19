import { randomUUID } from 'node:crypto';
import fsp from 'node:fs/promises';
import type { FileHandle } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import type { AgentPlugin, InstallConfig, PluginStatus } from './base.js';
import { SUPPORTED_AGENTS } from './registry.js';

const AGENT_KEY = 'grok';
const GUARD_SCRIPT = 'guard.js';
const AUDIT_SCRIPT = 'hook.js';
const HOOK_TIMEOUT_SECONDS = 10;
const entry = SUPPORTED_AGENTS.get(AGENT_KEY)!;

type JsonObject = Record<string, unknown>;

interface GrokHandler extends JsonObject {
  readonly type: 'command' | 'http';
  readonly command?: string;
  readonly url?: string;
  readonly timeout?: number;
}

interface GrokGroup extends JsonObject {
  readonly matcher?: string;
  readonly hooks: GrokHandler[];
}

type GrokHooks = Record<string, GrokGroup[]>;

interface GrokDocument {
  readonly exists: boolean;
  readonly configPath: string;
  readonly root: JsonObject;
  readonly hooks: GrokHooks;
}

interface ParsedArgument {
  readonly value: string;
  readonly next: number;
}

interface RuntimeContract {
  readonly agentId: string;
  readonly guardPath: string;
  readonly auditPath: string;
}

function isObject(value: unknown): value is JsonObject {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function hasOwn(value: JsonObject, key: string): boolean {
  return Object.prototype.hasOwnProperty.call(value, key);
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function asError(error: unknown): Error {
  return error instanceof Error ? error : new Error(String(error));
}

function hasErrorCode(error: unknown, code: string): boolean {
  return isObject(error) && error.code === code;
}

function resolveConfigPath(): string {
  const grokHome = process.env.GROK_HOME || path.join(os.homedir(), '.grok');
  return path.join(grokHome, 'hooks', entry.configFile);
}

function quotePosix(value: string): string {
  return `'${value.replaceAll("'", `'"'"'`)}'`;
}

function quoteWindows(value: string): string {
  return `"${value.replaceAll('"', '\\"')}"`;
}

function buildCommand(scriptPath: string): string {
  const quote = process.platform === 'win32' ? quoteWindows : quotePosix;
  return `${quote(process.execPath)} ${quote(scriptPath)}`;
}

function buildHandler(scriptPath: string): GrokHandler {
  return {
    type: 'command',
    command: buildCommand(scriptPath),
    timeout: HOOK_TIMEOUT_SECONDS,
  };
}

function readWindowsArgument(command: string, start: number): ParsedArgument | undefined {
  if (command[start] !== '"') return undefined;
  let value = '';
  for (let index = start + 1; index < command.length; index += 1) {
    if (command[index] === '\\' && command[index + 1] === '"') {
      value += '"';
      index += 1;
      continue;
    }
    if (command[index] === '"') return { value, next: index + 1 };
    value += command[index];
  }
  return undefined;
}

const POSIX_APOSTROPHE = `'"'"'`;

function readPosixArgument(command: string, start: number): ParsedArgument | undefined {
  if (command[start] !== "'") return undefined;
  let value = '';
  for (let index = start + 1; index < command.length;) {
    if (command.startsWith(POSIX_APOSTROPHE, index)) {
      value += "'";
      index += POSIX_APOSTROPHE.length;
      continue;
    }
    if (command[index] === "'") return { value, next: index + 1 };
    value += command[index];
    index += 1;
  }
  return undefined;
}

function parseGeneratedCommand(command: string): readonly [string, string] | undefined {
  const readArgument = process.platform === 'win32' ? readWindowsArgument : readPosixArgument;
  const executable = readArgument(command, 0);
  if (!executable || command[executable.next] !== ' ') return undefined;
  const script = readArgument(command, executable.next + 1);
  if (!script || script.next !== command.length || !executable.value || !script.value) return undefined;
  return [executable.value, script.value];
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

function managedAgentId(handler: GrokHandler, scriptName: string): string | undefined {
  if (handler.type !== 'command'
    || handler.timeout !== HOOK_TIMEOUT_SECONDS
    || typeof handler.command !== 'string') return undefined;
  const parsed = parseGeneratedCommand(handler.command);
  if (!parsed) return undefined;
  const scriptPath = parsed[1];
  if (path.basename(scriptPath) !== scriptName) return undefined;
  const agentDirectory = path.dirname(scriptPath);
  const runtimeRoot = path.join(os.homedir(), '.elydora');
  if (!samePath(path.dirname(agentDirectory), runtimeRoot)) return undefined;
  const agentId = path.basename(agentDirectory);
  return agentId && agentId !== '.' && agentId !== '..' ? agentId : undefined;
}

function validateHandler(value: unknown, event: string, groupIndex: number, handlerIndex: number): GrokHandler {
  const label = `Grok hooks config handler hooks.${event}[${groupIndex}].hooks[${handlerIndex}]`;
  if (!isObject(value)) throw new Error(`${label} must be an object`);
  if (value.type !== 'command' && value.type !== 'http') {
    throw new Error(`${label} has unsupported type "${String(value.type)}"`);
  }
  if (value.type === 'command' && (typeof value.command !== 'string' || value.command.length === 0)) {
    throw new Error(`${label} requires a non-empty command`);
  }
  if (value.type === 'http' && (typeof value.url !== 'string' || value.url.length === 0)) {
    throw new Error(`${label} requires a non-empty url`);
  }
  if (value.timeout !== undefined
    && (typeof value.timeout !== 'number' || !Number.isFinite(value.timeout) || value.timeout <= 0)) {
    throw new Error(`${label} timeout must be a positive finite number`);
  }
  return value as GrokHandler;
}

function validateGroup(value: unknown, event: string, groupIndex: number): GrokGroup {
  const label = `Grok hooks config group hooks.${event}[${groupIndex}]`;
  if (!isObject(value)) throw new Error(`${label} must be an object`);
  if (value.matcher !== undefined && typeof value.matcher !== 'string') {
    throw new Error(`${label} matcher must be a string`);
  }
  if (!Array.isArray(value.hooks)) throw new Error(`${label} must contain a hooks array`);
  const handlers = value.hooks.map(
    (handler, handlerIndex) => validateHandler(handler, event, groupIndex, handlerIndex),
  );
  return { ...value, hooks: handlers } as GrokGroup;
}

function readHooks(root: JsonObject): GrokHooks {
  if (root.hooks === undefined) return {};
  if (!isObject(root.hooks)) throw new Error('Grok hooks config field "hooks" must be an object');
  const hooks: GrokHooks = {};
  for (const [event, value] of Object.entries(root.hooks)) {
    if (!Array.isArray(value)) {
      throw new Error(`Grok hooks config field "hooks.${event}" must be an array`);
    }
    hooks[event] = value.map((group, index) => validateGroup(group, event, index));
  }
  return hooks;
}

async function readConfig(): Promise<GrokDocument> {
  const configPath = resolveConfigPath();
  let raw: string;
  try {
    raw = await fsp.readFile(configPath, 'utf-8');
  } catch (error) {
    if (hasErrorCode(error, 'ENOENT')) return { exists: false, configPath, root: {}, hooks: {} };
    throw new Error(`Read Grok hooks config at ${configPath}: ${errorMessage(error)}`, {
      cause: asError(error),
    });
  }
  let root: unknown;
  try {
    root = JSON.parse(raw);
  } catch (error) {
    throw new Error(`Failed to parse Grok hooks config at ${configPath}: ${errorMessage(error)}`, {
      cause: asError(error),
    });
  }
  if (!isObject(root)) throw new Error(`Grok hooks config at ${configPath} must contain a JSON object`);
  return { exists: true, configPath, root, hooks: readHooks(root) };
}

function removeManaged(
  groups: GrokGroup[],
  scriptName: string,
  agentId?: string,
): { readonly groups: GrokGroup[]; readonly changed: boolean } {
  let changed = false;
  const result: GrokGroup[] = [];
  for (const group of groups) {
    if (hasOwn(group, 'matcher')) {
      result.push(group);
      continue;
    }
    let groupChanged = false;
    const handlers = group.hooks.filter((handler) => {
      const managedId = managedAgentId(handler, scriptName);
      const remove = managedId !== undefined
        && (agentId === undefined || sameAgentId(managedId, agentId));
      if (remove) {
        changed = true;
        groupChanged = true;
      }
      return !remove;
    });
    if (!groupChanged) result.push(group);
    else if (handlers.length > 0) result.push({ ...group, hooks: handlers });
  }
  return { groups: result, changed };
}

function removeManagedHooks(hooks: GrokHooks, agentId?: string): {
  readonly hooks: GrokHooks;
  readonly changed: boolean;
} {
  const next = { ...hooks };
  let changed = false;
  for (const [event, scriptName] of [
    ['PreToolUse', GUARD_SCRIPT],
    ['PostToolUse', AUDIT_SCRIPT],
  ] as const) {
    const result = removeManaged(next[event] ?? [], scriptName, agentId);
    if (!result.changed) continue;
    changed = true;
    if (result.groups.length > 0) next[event] = result.groups;
    else delete next[event];
  }
  return { hooks: next, changed };
}

async function failWrite(
  handle: FileHandle | undefined,
  tempPath: string,
  configPath: string,
  cause: unknown,
): Promise<never> {
  const errors = [asError(cause)];
  if (handle) {
    try {
      await handle.close();
    } catch (error) {
      errors.push(asError(error));
    }
  }
  try {
    await fsp.unlink(tempPath);
  } catch (error) {
    if (!hasErrorCode(error, 'ENOENT')) errors.push(asError(error));
  }
  const message = `Write Grok hooks config at ${configPath}: ${errorMessage(cause)}`;
  if (errors.length > 1) throw new AggregateError(errors, message);
  throw new Error(message, { cause: errors[0] });
}

async function writeConfig(configPath: string, root: JsonObject): Promise<void> {
  const directory = path.dirname(configPath);
  await fsp.mkdir(directory, { recursive: true, mode: 0o700 });
  const tempPath = path.join(directory, `.${path.basename(configPath)}.${randomUUID()}.tmp`);
  let handle: FileHandle | undefined;
  try {
    handle = await fsp.open(tempPath, 'wx', 0o600);
    await handle.writeFile(JSON.stringify(root, null, 2) + '\n', 'utf-8');
    await handle.sync();
    await handle.close();
    handle = undefined;
    await fsp.rename(tempPath, configPath);
  } catch (error) {
    await failWrite(handle, tempPath, configPath, error);
  }
}

async function removeConfig(configPath: string): Promise<void> {
  try {
    await fsp.unlink(configPath);
  } catch (error) {
    if (hasErrorCode(error, 'ENOENT')) return;
    throw new Error(`Remove Grok hooks config at ${configPath}: ${errorMessage(error)}`, {
      cause: asError(error),
    });
  }
}

async function regularFileExists(filePath: string, label: string): Promise<boolean> {
  try {
    return (await fsp.stat(filePath)).isFile();
  } catch (error) {
    if (hasErrorCode(error, 'ENOENT') || hasErrorCode(error, 'ENOTDIR')) return false;
    throw new Error(`Read ${label} at ${filePath}: ${errorMessage(error)}`, { cause: asError(error) });
  }
}

async function requireRuntime(filePath: string, label: string): Promise<void> {
  if (!filePath) throw new Error(`${label} path is required`);
  if (!await regularFileExists(filePath, label)) throw new Error(`${label} is missing: ${filePath}`);
}

async function readJsonObject(filePath: string, label: string): Promise<JsonObject | undefined> {
  let raw: string;
  try {
    raw = await fsp.readFile(filePath, 'utf-8');
  } catch (error) {
    if (hasErrorCode(error, 'ENOENT')) return undefined;
    throw new Error(`Read ${label} at ${filePath}: ${errorMessage(error)}`, { cause: asError(error) });
  }
  let value: unknown;
  try {
    value = JSON.parse(raw);
  } catch (error) {
    throw new Error(`Failed to parse ${label} at ${filePath}: ${errorMessage(error)}`, {
      cause: asError(error),
    });
  }
  if (!isObject(value)) throw new Error(`${label} at ${filePath} must contain a JSON object`);
  return value;
}

function managedIds(groups: GrokGroup[], scriptName: string): Set<string> {
  const ids = new Set<string>();
  for (const group of groups) {
    if (hasOwn(group, 'matcher')) continue;
    for (const handler of group.hooks) {
      const agentId = managedAgentId(handler, scriptName);
      if (agentId) ids.add(agentId);
    }
  }
  return ids;
}

function runtimeContracts(hooks: GrokHooks): RuntimeContract[] {
  const guards = managedIds(hooks.PreToolUse ?? [], GUARD_SCRIPT);
  const audits = managedIds(hooks.PostToolUse ?? [], AUDIT_SCRIPT);
  const root = path.join(os.homedir(), '.elydora');
  return [...guards]
    .filter((agentId) => audits.has(agentId))
    .map((agentId) => ({
      agentId,
      guardPath: path.join(root, agentId, GUARD_SCRIPT),
      auditPath: path.join(root, agentId, AUDIT_SCRIPT),
    }));
}

async function runtimeScriptsExist(contracts: RuntimeContract[]): Promise<boolean> {
  const root = path.join(os.homedir(), '.elydora');
  let entries: Array<{ isDirectory(): boolean; name: string }>;
  try {
    entries = await fsp.readdir(root, { withFileTypes: true });
  } catch (error) {
    if (hasErrorCode(error, 'ENOENT')) return false;
    throw new Error(`Read Elydora runtime directory at ${root}: ${errorMessage(error)}`, {
      cause: asError(error),
    });
  }
  for (const contract of contracts) {
    if (!entries.some((item) => item.isDirectory() && sameAgentId(item.name, contract.agentId))) continue;
    const runtimeConfigPath = path.join(root, contract.agentId, 'config.json');
    const runtimeConfig = await readJsonObject(runtimeConfigPath, 'Elydora runtime config');
    if (!runtimeConfig || runtimeConfig.agent_name !== AGENT_KEY) continue;
    const [guardExists, auditExists] = await Promise.all([
      regularFileExists(contract.guardPath, 'Elydora guard runtime'),
      regularFileExists(contract.auditPath, 'Elydora audit runtime'),
    ]);
    if (guardExists && auditExists) return true;
  }
  return false;
}

export const grokPlugin: AgentPlugin = {
  async install(config: InstallConfig): Promise<void> {
    if (!config.agentId) throw new Error('agentId is required');
    const document = await readConfig();
    await requireRuntime(config.guardScriptPath, 'Elydora guard runtime');
    await requireRuntime(config.hookScriptPath, 'Elydora audit runtime');
    const cleaned = removeManagedHooks(document.hooks).hooks;
    const hooks: GrokHooks = {
      ...cleaned,
      PreToolUse: [...(cleaned.PreToolUse ?? []), { hooks: [buildHandler(config.guardScriptPath)] }],
      PostToolUse: [...(cleaned.PostToolUse ?? []), { hooks: [buildHandler(config.hookScriptPath)] }],
    };
    await writeConfig(document.configPath, { ...document.root, hooks });
    console.log('  Grok Build: global PreToolUse and PostToolUse hooks installed.');
  },

  async uninstall(agentId?: string): Promise<void> {
    const document = await readConfig();
    if (!document.exists) return;
    const result = removeManagedHooks(document.hooks, agentId);
    if (!result.changed) return;
    const root = { ...document.root };
    if (Object.keys(result.hooks).length > 0) root.hooks = result.hooks;
    else delete root.hooks;
    if (Object.keys(root).length === 0) await removeConfig(document.configPath);
    else await writeConfig(document.configPath, root);
  },

  async status(): Promise<PluginStatus> {
    const document = await readConfig();
    const contracts = runtimeContracts(document.hooks);
    const hookConfigured = contracts.length > 0;
    const hookScriptExists = hookConfigured ? await runtimeScriptsExist(contracts) : false;
    return {
      installed: hookConfigured && hookScriptExists,
      agentName: AGENT_KEY,
      displayName: entry.name,
      hookConfigured,
      hookScriptExists,
      configPath: document.configPath,
    };
  },
};
