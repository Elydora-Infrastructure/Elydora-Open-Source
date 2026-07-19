import { randomUUID } from 'node:crypto';
import fsp from 'node:fs/promises';
import type { FileHandle } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import type { AgentPlugin, InstallConfig, PluginStatus } from './base.js';
import { SUPPORTED_AGENTS } from './registry.js';

const AGENT_KEY = 'codex';
const OWNED_DESCRIPTION = 'Elydora audit and freeze enforcement';
const GUARD_STATUS = 'Checking Elydora agent state';
const AUDIT_STATUS = 'Recording Elydora tool use';
const GUARD_SCRIPT = 'guard.js';
const AUDIT_SCRIPT = 'hook.js';
const entry = SUPPORTED_AGENTS.get(AGENT_KEY)!;

type JsonObject = Record<string, unknown>;

function isObject(value: unknown): value is JsonObject {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function hasErrorCode(error: unknown, code: string): boolean {
  return isObject(error) && error.code === code;
}

function asError(error: unknown): Error {
  return error instanceof Error ? error : new Error(String(error));
}

function resolveConfigPath(): string {
  const configDir = entry.configDir.replace(/^~/, os.homedir());
  return path.join(configDir, entry.configFile);
}

function quotePosix(value: string): string {
  return `'${value.replaceAll("'", `'"'"'`)}'`;
}

function quoteWindows(value: string): string {
  return `"${value.replaceAll('"', '\\"')}"`;
}

function buildHandler(scriptPath: string, statusMessage: string): JsonObject {
  return {
    type: 'command',
    command: `${quotePosix(process.execPath)} ${quotePosix(scriptPath)}`,
    commandWindows: `${quoteWindows(process.execPath)} ${quoteWindows(scriptPath)}`,
    timeout: 10,
    statusMessage,
  };
}

function eventGroups(hooks: JsonObject, event: string): JsonObject[] {
  const value = hooks[event];
  if (value === undefined) return [];
  if (!Array.isArray(value) || !value.every(isObject)) {
    throw new Error(`Codex hooks config field "hooks.${event}" must be an array of objects`);
  }
  return value;
}

function isManagedCommand(command: unknown, scriptName: string, agentId?: string): boolean {
  if (typeof command !== 'string') return false;
  const normalized = command.toLowerCase();
  if (!normalized.includes('.elydora') || !normalized.includes(scriptName)) return false;
  return !agentId || command.includes(agentId);
}

function isElydoraHandler(handler: JsonObject, agentId?: string): boolean {
  const scriptName = handler.statusMessage === GUARD_STATUS
    ? GUARD_SCRIPT
    : handler.statusMessage === AUDIT_STATUS
      ? AUDIT_SCRIPT
      : undefined;
  if (!scriptName) return false;
  return [handler.command, handler.commandWindows].some(
    (command) => isManagedCommand(command, scriptName, agentId),
  );
}

function withoutElydora(groups: JsonObject[], agentId?: string): JsonObject[] {
  const result: JsonObject[] = [];
  for (const group of groups) {
    if (!Array.isArray(group.hooks) || !group.hooks.every(isObject)) {
      throw new Error('Codex hook matcher group must contain a hooks array');
    }
    const handlers = group.hooks.filter((handler) => !isElydoraHandler(handler, agentId));
    if (handlers.length > 0) result.push({ ...group, hooks: handlers });
  }
  return result;
}

function findHandler(groups: JsonObject[], statusMessage: string): JsonObject | undefined {
  for (const group of groups) {
    if (!Array.isArray(group.hooks) || !group.hooks.every(isObject)) {
      throw new Error('Codex hook matcher group must contain a hooks array');
    }
    const handler = group.hooks.find(
      (candidate) => candidate.statusMessage === statusMessage
        && isElydoraHandler(candidate),
    );
    if (isObject(handler)) return handler;
  }
  return undefined;
}

function commandReferences(handler: JsonObject, scriptPath: string): boolean {
  return [handler.command, handler.commandWindows].some(
    (command) => typeof command === 'string' && command.includes(scriptPath),
  );
}

async function readJsonObject(filePath: string, label: string): Promise<JsonObject | undefined> {
  let raw: string;
  try {
    raw = await fsp.readFile(filePath, 'utf-8');
  } catch (error) {
    if (hasErrorCode(error, 'ENOENT')) return undefined;
    throw new Error(`Read ${label} at ${filePath}: ${errorMessage(error)}`, {
      cause: asError(error),
    });
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

async function readSettings(configPath: string): Promise<JsonObject | undefined> {
  return readJsonObject(configPath, 'Codex hooks config');
}

async function failWrite(
  handle: FileHandle | undefined,
  tempPath: string,
  label: string,
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
  const message = `Write ${label}: ${errorMessage(cause)}`;
  if (errors.length > 1) throw new AggregateError(errors, message);
  throw new Error(message, { cause: errors[0] });
}

async function writeSettings(configPath: string, settings: JsonObject): Promise<void> {
  const directory = path.dirname(configPath);
  await fsp.mkdir(directory, { recursive: true, mode: 0o700 });
  const tempPath = path.join(directory, `.${path.basename(configPath)}.${randomUUID()}.tmp`);
  let handle: FileHandle | undefined;
  try {
    handle = await fsp.open(tempPath, 'wx', 0o600);
    await handle.writeFile(JSON.stringify(settings, null, 2) + '\n', 'utf-8');
    await handle.sync();
    await handle.close();
    handle = undefined;
    await fsp.rename(tempPath, configPath);
  } catch (error) {
    await failWrite(handle, tempPath, `Codex hooks config at ${configPath}`, error);
  }
}

async function removeSettings(configPath: string): Promise<void> {
  try {
    await fsp.unlink(configPath);
  } catch (error) {
    if (hasErrorCode(error, 'ENOENT')) return;
    throw new Error(`Remove Codex hooks config at ${configPath}: ${errorMessage(error)}`, {
      cause: asError(error),
    });
  }
}

function getHooks(settings: JsonObject): JsonObject {
  if (settings.hooks === undefined) return {};
  if (!isObject(settings.hooks)) throw new Error('Codex hooks config field "hooks" must be an object');
  return { ...settings.hooks };
}

function isOwnedSettings(settings: JsonObject, hooks: JsonObject): boolean {
  const settingsKeys = new Set(['description', 'hooks']);
  const hookKeys = new Set(['PreToolUse', 'PostToolUse']);
  return Object.keys(settings).every((key) => settingsKeys.has(key))
    && settings.description === OWNED_DESCRIPTION
    && Object.keys(hooks).every((key) => hookKeys.has(key))
    && Object.values(hooks).every((value) => Array.isArray(value) && value.length === 0);
}

async function regularFileExists(filePath: string, label: string): Promise<boolean> {
  try {
    const metadata = await fsp.stat(filePath);
    return metadata.isFile();
  } catch (error) {
    if (hasErrorCode(error, 'ENOENT')) return false;
    throw new Error(`Read ${label} at ${filePath}: ${errorMessage(error)}`, {
      cause: asError(error),
    });
  }
}

async function requireRuntime(filePath: string, label: string): Promise<void> {
  if (!filePath) throw new Error(`${label} path is required`);
  if (!await regularFileExists(filePath, label)) {
    throw new Error(`${label} is missing: ${filePath}`);
  }
}

async function runtimeScriptsExist(guard: JsonObject, audit: JsonObject): Promise<boolean> {
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

  for (const directory of entries) {
    if (!directory.isDirectory()) continue;
    const agentDir = path.join(root, directory.name);
    const guardPath = path.join(agentDir, GUARD_SCRIPT);
    const hookPath = path.join(agentDir, AUDIT_SCRIPT);
    if (!commandReferences(guard, guardPath) || !commandReferences(audit, hookPath)) continue;

    const configPath = path.join(agentDir, 'config.json');
    const config = await readJsonObject(configPath, 'Elydora runtime config');
    if (!config || config.agent_name !== AGENT_KEY) continue;
    const [guardExists, hookExists] = await Promise.all([
      regularFileExists(guardPath, 'Elydora guard runtime'),
      regularFileExists(hookPath, 'Elydora audit runtime'),
    ]);
    return guardExists && hookExists;
  }
  return false;
}

export const codexPlugin: AgentPlugin = {
  async install(config: InstallConfig): Promise<void> {
    if (!config.agentId) throw new Error('agentId is required');
    const configPath = resolveConfigPath();
    const existingSettings = await readSettings(configPath);
    await requireRuntime(config.guardScriptPath, 'Elydora guard runtime');
    await requireRuntime(config.hookScriptPath, 'Elydora audit runtime');
    const settings = existingSettings ?? { description: OWNED_DESCRIPTION };
    const hooks = getHooks(settings);

    hooks.PreToolUse = [
      ...withoutElydora(eventGroups(hooks, 'PreToolUse')),
      { matcher: '*', hooks: [buildHandler(config.guardScriptPath, GUARD_STATUS)] },
    ];
    hooks.PostToolUse = [
      ...withoutElydora(eventGroups(hooks, 'PostToolUse')),
      { matcher: '*', hooks: [buildHandler(config.hookScriptPath, AUDIT_STATUS)] },
    ];

    await writeSettings(configPath, { ...settings, hooks });
    console.log('  Codex: run /hooks to review and trust the Elydora hooks.');
  },

  async uninstall(agentId?: string): Promise<void> {
    const configPath = resolveConfigPath();
    const settings = await readSettings(configPath);
    if (!settings) return;
    const hooks = getHooks(settings);
    hooks.PreToolUse = withoutElydora(eventGroups(hooks, 'PreToolUse'), agentId);
    hooks.PostToolUse = withoutElydora(eventGroups(hooks, 'PostToolUse'), agentId);
    if (isOwnedSettings(settings, hooks)) {
      await removeSettings(configPath);
    } else {
      await writeSettings(configPath, { ...settings, hooks });
    }
  },

  async status(): Promise<PluginStatus> {
    const configPath = resolveConfigPath();
    const settings = await readSettings(configPath);
    let guard: JsonObject | undefined;
    let audit: JsonObject | undefined;
    if (settings) {
      const hooks = getHooks(settings);
      guard = findHandler(eventGroups(hooks, 'PreToolUse'), GUARD_STATUS);
      audit = findHandler(eventGroups(hooks, 'PostToolUse'), AUDIT_STATUS);
    }
    const hookConfigured = Boolean(guard && audit);
    const hookScriptExists = guard && audit ? await runtimeScriptsExist(guard, audit) : false;
    return {
      installed: hookConfigured && hookScriptExists,
      agentName: AGENT_KEY,
      displayName: entry.name,
      hookConfigured,
      hookScriptExists,
      configPath,
    };
  },
};
