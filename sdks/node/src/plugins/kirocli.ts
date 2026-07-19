import fsp from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import type { AgentPlugin, InstallConfig, PluginStatus } from './base.js';
import { SUPPORTED_AGENTS } from './registry.js';

const AGENT_KEY = 'kirocli';
const V2_AGENT_NAME = 'elydora-audit';
const V2_DESCRIPTION = 'Kiro CLI with Elydora audit and freeze enforcement';
const V3_GUARD_NAME = 'elydora-guard';
const V3_AUDIT_NAME = 'elydora-audit';
const entry = SUPPORTED_AGENTS.get(AGENT_KEY)!;

type JsonObject = Record<string, unknown>;

interface HookContract {
  readonly guard: string;
  readonly audit: string;
  readonly configPath: string;
}

function isObject(value: unknown): value is JsonObject {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function resolveV2Path(): string {
  return path.join(os.homedir(), '.kiro', 'agents', `${V2_AGENT_NAME}.json`);
}

function resolveV3Path(): string {
  const configDir = entry.configDir.replace(/^~/, os.homedir());
  return path.join(configDir, entry.configFile);
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

async function readJsonFile(configPath: string, label: string): Promise<JsonObject | undefined> {
  let raw: string;
  try {
    raw = await fsp.readFile(configPath, 'utf-8');
  } catch (error) {
    if (isObject(error) && error.code === 'ENOENT') return undefined;
    throw new Error(`Read ${label}: ${error instanceof Error ? error.message : String(error)}`);
  }

  let value: unknown;
  try {
    value = JSON.parse(raw);
  } catch (error) {
    throw new Error(`Failed to parse ${label}: ${error instanceof Error ? error.message : String(error)}`);
  }
  if (!isObject(value)) throw new Error(`${label} must contain a JSON object`);
  return value;
}

async function writeJsonFile(configPath: string, value: JsonObject, label: string): Promise<void> {
  await fsp.mkdir(path.dirname(configPath), { recursive: true });
  const tempPath = `${configPath}.${process.pid}.${Date.now()}.tmp`;
  try {
    await fsp.writeFile(tempPath, JSON.stringify(value, null, 2) + '\n', {
      encoding: 'utf-8',
      mode: 0o600,
    });
    await fsp.rename(tempPath, configPath);
  } catch (error) {
    await fsp.unlink(tempPath).catch(() => undefined);
    throw new Error(`Write ${label}: ${error instanceof Error ? error.message : String(error)}`);
  }
}

async function removeFile(configPath: string, label: string): Promise<void> {
  try {
    await fsp.unlink(configPath);
  } catch (error) {
    if (isObject(error) && error.code === 'ENOENT') return;
    throw new Error(`Remove ${label}: ${error instanceof Error ? error.message : String(error)}`);
  }
}

function hooksObject(settings: JsonObject, label: string): JsonObject {
  if (settings.hooks === undefined) return {};
  if (!isObject(settings.hooks)) throw new Error(`${label} field "hooks" must be an object`);
  return { ...settings.hooks };
}

function hookEntries(hooks: JsonObject, event: string, label: string): JsonObject[] {
  const value = hooks[event];
  if (value === undefined) return [];
  if (!Array.isArray(value) || !value.every(isObject)) {
    throw new Error(`${label} field "hooks.${event}" must be an array of objects`);
  }
  return value;
}

function isManagedCommand(command: unknown, scriptName: string, agentId?: string): command is string {
  if (typeof command !== 'string') return false;
  const normalized = command.toLowerCase();
  if (!normalized.includes('.elydora') || !normalized.includes(scriptName)) return false;
  return !agentId || command.includes(agentId);
}

function withoutV2Hooks(entries: JsonObject[], agentId?: string): JsonObject[] {
  return entries.filter(
    (hook) => !isManagedCommand(hook.command, 'guard.js', agentId)
      && !isManagedCommand(hook.command, 'hook.js', agentId),
  );
}

function buildV2Hook(scriptPath: string): JsonObject {
  return {
    matcher: '*',
    command: buildCommand(scriptPath),
    timeout_ms: 5000,
  };
}

function isOwnedV2Config(settings: JsonObject, hooks: JsonObject): boolean {
  const configKeys = new Set(['name', 'description', 'tools', 'includeMcpJson', 'hooks']);
  const hookKeys = new Set(['preToolUse', 'postToolUse']);
  return Object.keys(settings).every((key) => configKeys.has(key))
    && settings.name === V2_AGENT_NAME
    && settings.description === V2_DESCRIPTION
    && Array.isArray(settings.tools)
    && settings.tools.length === 1
    && settings.tools[0] === '*'
    && settings.includeMcpJson === true
    && Object.keys(hooks).every((key) => hookKeys.has(key))
    && Object.values(hooks).every((value) => Array.isArray(value) && value.length === 0);
}

function v3Hooks(settings: JsonObject): JsonObject[] {
  if (settings.version !== undefined && settings.version !== 'v1') {
    throw new Error('Kiro CLI v3 hooks config field "version" must be "v1"');
  }
  if (settings.hooks === undefined) return [];
  if (!Array.isArray(settings.hooks) || !settings.hooks.every(isObject)) {
    throw new Error('Kiro CLI v3 hooks config field "hooks" must be an array of objects');
  }
  return settings.hooks;
}

function v3ActionCommand(hook: JsonObject): unknown {
  return isObject(hook.action) ? hook.action.command : undefined;
}

function isManagedV3Hook(hook: JsonObject, agentId?: string): boolean {
  if (hook.name === V3_GUARD_NAME) {
    return isManagedCommand(v3ActionCommand(hook), 'guard.js', agentId);
  }
  if (hook.name === V3_AUDIT_NAME) {
    return isManagedCommand(v3ActionCommand(hook), 'hook.js', agentId);
  }
  return false;
}

function buildV3Hook(
  name: string,
  description: string,
  trigger: 'PreToolUse' | 'PostToolUse',
  scriptPath: string,
): JsonObject {
  return {
    name,
    description,
    trigger,
    matcher: '.*',
    action: { type: 'command', command: buildCommand(scriptPath) },
    timeout: 5,
    enabled: true,
  };
}

function findV2Command(entries: JsonObject[], scriptName: string): string | undefined {
  return entries.find((hook) => isManagedCommand(hook.command, scriptName))?.command as string | undefined;
}

function findV3Command(entries: JsonObject[], name: string, scriptName: string): string | undefined {
  const hook = entries.find(
    (candidate) => candidate.name === name && isManagedCommand(v3ActionCommand(candidate), scriptName),
  );
  return hook ? v3ActionCommand(hook) as string : undefined;
}

async function runtimeScriptsExist(contracts: HookContract[]): Promise<boolean> {
  const root = path.join(os.homedir(), '.elydora');
  let entries: Array<{ isDirectory(): boolean; name: string }>;
  try {
    entries = await fsp.readdir(root, { withFileTypes: true });
  } catch (error) {
    if (isObject(error) && error.code === 'ENOENT') return false;
    throw new Error(`Read Elydora runtime directory: ${error instanceof Error ? error.message : String(error)}`);
  }

  for (const directory of entries) {
    if (!directory.isDirectory()) continue;
    const agentDir = path.join(root, directory.name);
    const guardPath = path.join(agentDir, 'guard.js');
    const hookPath = path.join(agentDir, 'hook.js');
    const referencesScripts = contracts.some(
      ({ guard, audit }) => guard.includes(guardPath) && audit.includes(hookPath),
    );
    if (!referencesScripts) continue;

    const configPath = path.join(agentDir, 'config.json');
    const config = await readJsonFile(configPath, `Elydora runtime config at ${configPath}`);
    if (!config || config.agent_name !== AGENT_KEY) continue;

    for (const [scriptPath, label] of [
      [guardPath, 'Elydora guard runtime'],
      [hookPath, 'Elydora audit runtime'],
    ] as const) {
      try {
        await fsp.access(scriptPath);
      } catch (error) {
        if (isObject(error) && error.code === 'ENOENT') return false;
        throw new Error(`${label} at ${scriptPath}: ${error instanceof Error ? error.message : String(error)}`);
      }
    }
    return true;
  }
  return false;
}

export const kirocliPlugin: AgentPlugin = {
  async install(config: InstallConfig): Promise<void> {
    const v2Path = resolveV2Path();
    const v3Path = resolveV3Path();
    const v2Settings = (await readJsonFile(v2Path, 'Kiro CLI v2 agent config')) ?? {};
    const v3Settings = (await readJsonFile(v3Path, 'Kiro CLI v3 hooks config')) ?? {};
    const v2Hooks = hooksObject(v2Settings, 'Kiro CLI v2 agent config');
    const currentV3Hooks = v3Hooks(v3Settings);

    v2Hooks.preToolUse = [
      ...withoutV2Hooks(hookEntries(v2Hooks, 'preToolUse', 'Kiro CLI v2 agent config')),
      buildV2Hook(config.guardScriptPath),
    ];
    v2Hooks.postToolUse = [
      ...withoutV2Hooks(hookEntries(v2Hooks, 'postToolUse', 'Kiro CLI v2 agent config')),
      buildV2Hook(config.hookScriptPath),
    ];

    const nextV2Settings: JsonObject = {
      name: V2_AGENT_NAME,
      description: V2_DESCRIPTION,
      tools: ['*'],
      includeMcpJson: true,
      ...v2Settings,
      hooks: v2Hooks,
    };
    const nextV3Settings: JsonObject = {
      ...v3Settings,
      version: 'v1',
      hooks: [
        ...currentV3Hooks.filter((hook) => !isManagedV3Hook(hook)),
        buildV3Hook(
          V3_GUARD_NAME,
          'Block tool use when the Elydora agent is frozen',
          'PreToolUse',
          config.guardScriptPath,
        ),
        buildV3Hook(
          V3_AUDIT_NAME,
          'Record tool use in the Elydora audit trail',
          'PostToolUse',
          config.hookScriptPath,
        ),
      ],
    };

    await writeJsonFile(v2Path, nextV2Settings, 'Kiro CLI v2 agent config');
    await writeJsonFile(v3Path, nextV3Settings, 'Kiro CLI v3 hooks config');
    console.log('  Kiro CLI v2: start with "kiro-cli --agent elydora-audit".');
    console.log('  Kiro CLI v3: start with "kiro-cli --v3"; global hooks load automatically.');
  },

  async uninstall(agentId?: string): Promise<void> {
    const v2Path = resolveV2Path();
    const v3Path = resolveV3Path();
    const v2Settings = await readJsonFile(v2Path, 'Kiro CLI v2 agent config');
    const v3Settings = await readJsonFile(v3Path, 'Kiro CLI v3 hooks config');

    if (v2Settings) {
      const hooks = hooksObject(v2Settings, 'Kiro CLI v2 agent config');
      hooks.preToolUse = withoutV2Hooks(
        hookEntries(hooks, 'preToolUse', 'Kiro CLI v2 agent config'),
        agentId,
      );
      hooks.postToolUse = withoutV2Hooks(
        hookEntries(hooks, 'postToolUse', 'Kiro CLI v2 agent config'),
        agentId,
      );
      if (isOwnedV2Config(v2Settings, hooks)) {
        await removeFile(v2Path, 'Kiro CLI v2 agent config');
      } else {
        await writeJsonFile(v2Path, { ...v2Settings, hooks }, 'Kiro CLI v2 agent config');
      }
    }

    if (v3Settings) {
      const hooks = v3Hooks(v3Settings).filter((hook) => !isManagedV3Hook(hook, agentId));
      const owned = Object.keys(v3Settings).every((key) => key === 'version' || key === 'hooks');
      if (owned && hooks.length === 0) {
        await removeFile(v3Path, 'Kiro CLI v3 hooks config');
      } else {
        await writeJsonFile(v3Path, { ...v3Settings, hooks }, 'Kiro CLI v3 hooks config');
      }
    }
  },

  async status(): Promise<PluginStatus> {
    const v2Path = resolveV2Path();
    const v3Path = resolveV3Path();
    const v2Settings = await readJsonFile(v2Path, 'Kiro CLI v2 agent config');
    const v3Settings = await readJsonFile(v3Path, 'Kiro CLI v3 hooks config');
    const contracts: HookContract[] = [];

    if (v2Settings) {
      const hooks = hooksObject(v2Settings, 'Kiro CLI v2 agent config');
      const guard = findV2Command(
        hookEntries(hooks, 'preToolUse', 'Kiro CLI v2 agent config'),
        'guard.js',
      );
      const audit = findV2Command(
        hookEntries(hooks, 'postToolUse', 'Kiro CLI v2 agent config'),
        'hook.js',
      );
      if (guard && audit) contracts.push({ guard, audit, configPath: v2Path });
    }

    if (v3Settings) {
      const hooks = v3Hooks(v3Settings);
      const guard = findV3Command(hooks, V3_GUARD_NAME, 'guard.js');
      const audit = findV3Command(hooks, V3_AUDIT_NAME, 'hook.js');
      if (guard && audit) contracts.push({ guard, audit, configPath: v3Path });
    }

    const hookConfigured = contracts.length > 0;
    const hookScriptExists = hookConfigured ? await runtimeScriptsExist(contracts) : false;
    return {
      installed: hookConfigured && hookScriptExists,
      agentName: AGENT_KEY,
      displayName: entry.name,
      hookConfigured,
      hookScriptExists,
      configPath: contracts.at(-1)?.configPath ?? v3Path,
    };
  },
};
