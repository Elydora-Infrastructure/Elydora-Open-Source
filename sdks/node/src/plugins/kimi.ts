import { randomUUID } from 'node:crypto';
import fsp from 'node:fs/promises';
import type { FileHandle } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import type { AgentPlugin, InstallConfig, PluginStatus } from './base.js';
import { SUPPORTED_AGENTS } from './registry.js';

const AGENT_KEY = 'kimi';
const GUARD_SCRIPT = 'guard.js';
const AUDIT_SCRIPT = 'hook.js';
const HOOK_TIMEOUT_SECONDS = 10;
const entry = SUPPORTED_AGENTS.get(AGENT_KEY)!;

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

const MODERN_EVENTS = new Set<string>([
  ...SHARED_EVENTS,
  'PermissionRequest',
  'PermissionResult',
  'Interrupt',
]);
const LEGACY_EVENTS = new Set<string>(SHARED_EVENTS);

type TomlObject = Record<string, unknown>;

interface KimiHook extends TomlObject {
  readonly event: string;
  readonly command: string;
  readonly matcher?: string;
  readonly timeout?: number;
}

interface KimiContract {
  readonly runtimeName: 'Kimi Code' | 'kimi-cli';
  readonly label: string;
  readonly configPath: string;
  readonly events: ReadonlySet<string>;
}

interface KimiConfigDocument {
  readonly contract: KimiContract;
  readonly exists: boolean;
  readonly raw: string;
  readonly root: TomlObject;
  readonly hooks: KimiHook[];
  readonly usesHookTables: boolean;
}

interface RuntimeContract {
  readonly guard: string;
  readonly audit: string;
  readonly configPath: string;
}

type ConfigMutation =
  | { readonly kind: 'none' }
  | { readonly kind: 'remove'; readonly document: KimiConfigDocument }
  | { readonly kind: 'write'; readonly document: KimiConfigDocument; readonly raw: string };

function isObject(value: unknown): value is TomlObject {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function hasOwn(value: TomlObject, key: string): boolean {
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

async function pathExists(filePath: string, label: string): Promise<boolean> {
  try {
    await fsp.stat(filePath);
    return true;
  } catch (error) {
    if (hasErrorCode(error, 'ENOENT') || hasErrorCode(error, 'ENOTDIR')) return false;
    throw new Error(`Inspect ${label} at ${filePath}: ${errorMessage(error)}`, { cause: asError(error) });
  }
}

async function legacyCliOnPath(): Promise<boolean> {
  const pathValue = process.env.PATH;
  if (!pathValue) return false;
  const names = process.platform === 'win32'
    ? ['kimi-cli.exe', 'kimi-cli.cmd', 'kimi-cli.bat', 'kimi-cli.com', 'kimi-cli']
    : ['kimi-cli'];
  for (const value of pathValue.split(path.delimiter)) {
    const directory = value.replace(/^"(.*)"$/, '$1');
    if (!directory) continue;
    for (const name of names) {
      const candidate = path.join(directory, name);
      try {
        const stats = await fsp.stat(candidate);
        if (stats.isFile() && (process.platform === 'win32' || (stats.mode & 0o111) !== 0)) return true;
      } catch (error) {
        if (hasErrorCode(error, 'ENOENT') || hasErrorCode(error, 'ENOTDIR')) continue;
        throw new Error(`Inspect kimi-cli executable at ${candidate}: ${errorMessage(error)}`, {
          cause: asError(error),
        });
      }
    }
  }
  return false;
}

async function resolveContracts(): Promise<KimiContract[]> {
  const home = os.homedir();
  const explicitKimiHome = process.env.KIMI_CODE_HOME || undefined;
  const kimiHome = explicitKimiHome ?? path.join(home, '.kimi-code');
  const modern: KimiContract = {
    runtimeName: 'Kimi Code',
    label: 'Kimi Code hooks config',
    configPath: path.join(kimiHome, 'config.toml'),
    events: MODERN_EVENTS,
  };
  const legacy: KimiContract = {
    runtimeName: 'kimi-cli',
    label: 'kimi-cli legacy hooks config',
    configPath: path.join(home, '.kimi', 'config.toml'),
    events: LEGACY_EVENTS,
  };
  const modernDetected = explicitKimiHome !== undefined || await pathExists(kimiHome, 'Kimi Code home');
  const legacyDetected = await pathExists(legacy.configPath, legacy.label) || await legacyCliOnPath();
  if (legacyDetected && !modernDetected) return [legacy];
  return legacyDetected ? [modern, legacy] : [modern];
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

function buildHook(event: 'PreToolUse' | 'PostToolUse', scriptPath: string): KimiHook {
  return { event, command: buildCommand(scriptPath), timeout: HOOK_TIMEOUT_SECONDS };
}

function validateHook(value: unknown, contract: KimiContract, index: number): KimiHook {
  if (!isObject(value)) {
    throw new Error(`${contract.label} hook ${index + 1} must be a table`);
  }
  const supportedFields = new Set(['event', 'matcher', 'command', 'timeout']);
  const unsupportedField = Object.keys(value).find((key) => !supportedFields.has(key));
  if (unsupportedField) {
    throw new Error(`${contract.label} hook ${index + 1} has unsupported field "${unsupportedField}"`);
  }
  if (typeof value.event !== 'string' || !contract.events.has(value.event)) {
    throw new Error(`${contract.label} hook ${index + 1} has unsupported event "${String(value.event)}"`);
  }
  if (typeof value.command !== 'string' || value.command.length === 0) {
    throw new Error(`${contract.label} hook ${index + 1} requires a non-empty command`);
  }
  if (value.matcher !== undefined && typeof value.matcher !== 'string') {
    throw new Error(`${contract.label} hook ${index + 1} matcher must be a string`);
  }
  if (
    value.timeout !== undefined
    && (!Number.isInteger(value.timeout) || (value.timeout as number) < 1 || (value.timeout as number) > 600)
  ) {
    throw new Error(`${contract.label} hook ${index + 1} timeout must be an integer from 1 to 600`);
  }
  return value as KimiHook;
}

function readHooks(root: TomlObject, contract: KimiContract): KimiHook[] {
  if (root.hooks === undefined) return [];
  if (!Array.isArray(root.hooks)) throw new Error(`${contract.label} field "hooks" must be an array`);
  return root.hooks.map((hook, index) => validateHook(hook, contract, index));
}

function documentUsesHookTables(nodes: readonly unknown[]): boolean {
  return nodes.some((node) => {
    if (!isObject(node) || node.type !== 'TableArray' || !isObject(node.key)) return false;
    if (!isObject(node.key.item) || !Array.isArray(node.key.item.value)) return false;
    return node.key.item.value.length === 1 && node.key.item.value[0] === 'hooks';
  });
}

async function readConfig(contract: KimiContract): Promise<KimiConfigDocument> {
  let raw: string;
  try {
    raw = await fsp.readFile(contract.configPath, 'utf-8');
  } catch (error) {
    if (hasErrorCode(error, 'ENOENT')) {
      return { contract, exists: false, raw: '', root: {}, hooks: [], usesHookTables: false };
    }
    throw new Error(`Read ${contract.label} at ${contract.configPath}: ${errorMessage(error)}`, {
      cause: asError(error),
    });
  }

  let document: { readonly toJsObject: unknown; readonly cst: readonly unknown[] };
  try {
    const { TomlDocument } = await import('@decimalturn/toml-patch');
    document = new TomlDocument(raw);
  } catch (error) {
    throw new Error(`Failed to parse ${contract.label} at ${contract.configPath}: ${errorMessage(error)}`, {
      cause: asError(error),
    });
  }
  const root: unknown = document.toJsObject;
  if (!isObject(root)) throw new Error(`${contract.label} at ${contract.configPath} must contain a TOML table`);
  return {
    contract,
    exists: true,
    raw,
    root,
    hooks: readHooks(root, contract),
    usesHookTables: documentUsesHookTables(document.cst),
  };
}

async function readAllConfigs(): Promise<KimiConfigDocument[]> {
  const documents: KimiConfigDocument[] = [];
  for (const contract of await resolveContracts()) documents.push(await readConfig(contract));
  return documents;
}

async function renderHooks(document: KimiConfigDocument, hooks: KimiHook[]): Promise<string> {
  const {
    patch: patchToml,
    stringify: stringifyToml,
    TomlFormat,
  } = await import('@decimalturn/toml-patch');
  const nextRoot: TomlObject = { ...document.root, hooks };
  if (document.usesHookTables) return patchToml(document.raw, nextRoot);

  let base = document.raw;
  if (hasOwn(document.root, 'hooks')) {
    const rootWithoutHooks = { ...document.root };
    delete rootWithoutHooks.hooks;
    base = patchToml(document.raw, rootWithoutHooks);
  }
  const format = document.raw.length > 0
    ? TomlFormat.autoDetectFormat(document.raw)
    : TomlFormat.default();
  const hookTables = stringifyToml({ hooks }, format);
  if (base.length === 0) return hookTables;
  const newline = format.newLine;
  const separator = base.endsWith(newline + newline) ? '' : base.endsWith(newline) ? newline : newline + newline;
  return base + separator + hookTables;
}

function normalizeForComparison(value: string): string {
  return process.platform === 'win32' ? value.toLowerCase() : value;
}

function commandReferencesPath(command: string, filePath: string): boolean {
  return normalizeForComparison(command).includes(normalizeForComparison(filePath));
}

function isManagedHook(
  hook: KimiHook,
  event: 'PreToolUse' | 'PostToolUse',
  scriptName: string,
  agentId?: string,
): boolean {
  if (hook.event !== event) return false;
  if (agentId) {
    const scriptPath = path.join(os.homedir(), '.elydora', agentId, scriptName);
    return commandReferencesPath(hook.command, scriptPath);
  }
  const command = normalizeForComparison(hook.command);
  const runtimeMarker = normalizeForComparison(`${path.sep}.elydora${path.sep}`);
  const scriptMarker = normalizeForComparison(`${path.sep}${scriptName}`);
  return command.includes(runtimeMarker) && command.includes(scriptMarker);
}

function withoutManagedHooks(hooks: KimiHook[], agentId?: string): KimiHook[] {
  return hooks.filter((hook) => !isManagedHook(hook, 'PreToolUse', GUARD_SCRIPT, agentId)
    && !isManagedHook(hook, 'PostToolUse', AUDIT_SCRIPT, agentId));
}

async function failWrite(
  handle: FileHandle | undefined,
  tempPath: string,
  document: KimiConfigDocument,
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
  const message = `Write ${document.contract.label} at ${document.contract.configPath}: ${errorMessage(cause)}`;
  if (errors.length > 1) throw new AggregateError(errors, message);
  throw new Error(message, { cause: errors[0] });
}

async function writeConfig(document: KimiConfigDocument, raw: string): Promise<void> {
  const configPath = document.contract.configPath;
  const directory = path.dirname(configPath);
  await fsp.mkdir(directory, { recursive: true, mode: 0o700 });
  const tempPath = path.join(directory, `.${path.basename(configPath)}.${randomUUID()}.tmp`);
  let handle: FileHandle | undefined;
  try {
    handle = await fsp.open(tempPath, 'wx', 0o600);
    await handle.writeFile(raw, 'utf-8');
    await handle.sync();
    await handle.close();
    handle = undefined;
    await fsp.rename(tempPath, configPath);
  } catch (error) {
    await failWrite(handle, tempPath, document, error);
  }
}

async function removeConfig(document: KimiConfigDocument): Promise<void> {
  try {
    await fsp.unlink(document.contract.configPath);
  } catch (error) {
    if (hasErrorCode(error, 'ENOENT')) return;
    throw new Error(
      `Remove ${document.contract.label} at ${document.contract.configPath}: ${errorMessage(error)}`,
      { cause: asError(error) },
    );
  }
}

async function regularFileExists(filePath: string, label: string): Promise<boolean> {
  try {
    return (await fsp.stat(filePath)).isFile();
  } catch (error) {
    if (hasErrorCode(error, 'ENOENT')) return false;
    throw new Error(`Read ${label} at ${filePath}: ${errorMessage(error)}`, { cause: asError(error) });
  }
}

async function requireRuntime(filePath: string, label: string): Promise<void> {
  if (!filePath) throw new Error(`${label} path is required`);
  if (!await regularFileExists(filePath, label)) throw new Error(`${label} is missing: ${filePath}`);
}

async function readJsonObject(filePath: string, label: string): Promise<TomlObject | undefined> {
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

function findRuntimeContract(document: KimiConfigDocument): RuntimeContract | undefined {
  const guard = document.hooks.find((hook) => isManagedHook(hook, 'PreToolUse', GUARD_SCRIPT));
  const audit = document.hooks.find((hook) => isManagedHook(hook, 'PostToolUse', AUDIT_SCRIPT));
  return guard && audit
    ? { guard: guard.command, audit: audit.command, configPath: document.contract.configPath }
    : undefined;
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

  for (const directory of entries) {
    if (!directory.isDirectory()) continue;
    const agentDir = path.join(root, directory.name);
    const guardPath = path.join(agentDir, GUARD_SCRIPT);
    const auditPath = path.join(agentDir, AUDIT_SCRIPT);
    if (!contracts.some(({ guard, audit }) => commandReferencesPath(guard, guardPath)
      && commandReferencesPath(audit, auditPath))) continue;

    const runtimeConfigPath = path.join(agentDir, 'config.json');
    const runtimeConfig = await readJsonObject(runtimeConfigPath, 'Elydora runtime config');
    if (!runtimeConfig || runtimeConfig.agent_name !== AGENT_KEY) continue;
    const [guardExists, auditExists] = await Promise.all([
      regularFileExists(guardPath, 'Elydora guard runtime'),
      regularFileExists(auditPath, 'Elydora audit runtime'),
    ]);
    return guardExists && auditExists;
  }
  return false;
}

async function applyMutation(mutation: ConfigMutation): Promise<void> {
  if (mutation.kind === 'write') await writeConfig(mutation.document, mutation.raw);
  if (mutation.kind === 'remove') await removeConfig(mutation.document);
}

export const kimiPlugin: AgentPlugin = {
  async install(config: InstallConfig): Promise<void> {
    if (!config.agentId) throw new Error('agentId is required');
    const documents = await readAllConfigs();
    await requireRuntime(config.guardScriptPath, 'Elydora guard runtime');
    await requireRuntime(config.hookScriptPath, 'Elydora audit runtime');
    const mutations: ConfigMutation[] = [];
    for (const document of documents) {
      const hooks = [
        ...withoutManagedHooks(document.hooks),
        buildHook('PreToolUse', config.guardScriptPath),
        buildHook('PostToolUse', config.hookScriptPath),
      ];
      mutations.push({ kind: 'write', document, raw: await renderHooks(document, hooks) });
    }
    for (const mutation of mutations) await applyMutation(mutation);
    const runtimes = documents.map(({ contract }) => contract.runtimeName).join(' and ');
    console.log(`  ${runtimes}: global PreToolUse and PostToolUse hooks installed.`);
  },

  async uninstall(agentId?: string): Promise<void> {
    const documents = await readAllConfigs();
    const mutations: ConfigMutation[] = [];
    for (const document of documents) {
      const hooks = withoutManagedHooks(document.hooks, agentId);
      if (hooks.length === document.hooks.length) {
        mutations.push({ kind: 'none' });
        continue;
      }
      const raw = await renderHooks(document, hooks);
      mutations.push(raw.trim().length === 0
        ? { kind: 'remove', document }
        : { kind: 'write', document, raw });
    }
    for (const mutation of mutations) await applyMutation(mutation);
  },

  async status(): Promise<PluginStatus> {
    const documents = await readAllConfigs();
    const contracts = documents
      .map(findRuntimeContract)
      .filter((contract): contract is RuntimeContract => contract !== undefined);
    const hookConfigured = contracts.length > 0;
    const hookScriptExists = hookConfigured ? await runtimeScriptsExist(contracts) : false;
    return {
      installed: hookConfigured && hookScriptExists,
      agentName: AGENT_KEY,
      displayName: entry.name,
      hookConfigured,
      hookScriptExists,
      configPath: contracts.at(-1)?.configPath ?? documents[0]!.contract.configPath,
    };
  },
};
