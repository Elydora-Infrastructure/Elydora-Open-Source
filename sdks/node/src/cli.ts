import { parseArgs } from 'node:util';
import fsp from 'node:fs/promises';
import path from 'node:path';
import os from 'node:os';
import { resolveInstallSecrets } from './cli-secrets.js';
import { derivePublicKey } from './crypto.js';
import {
  ensurePrivateDirectory,
  requirePhysicalDirectory,
  requirePhysicalFile,
  resolvePrivateChildDirectory,
} from './runtime-paths.js';
import { writePrivateFile } from './secure-files.js';
import { SUPPORTED_AGENTS } from './plugins/registry.js';
import type { AgentPlugin, InstallConfig } from './plugins/base.js';
import { generateHookScript, generateGuardScript } from './plugins/hook-template.js';
import { augmentPlugin } from './plugins/augment.js';
import { claudecodePlugin } from './plugins/claudecode.js';
import { cursorPlugin } from './plugins/cursor.js';
import { geminiPlugin } from './plugins/gemini.js';
import { kirocliPlugin } from './plugins/kirocli.js';
import { kiroidePlugin } from './plugins/kiroide.js';
import { opencodePlugin } from './plugins/opencode.js';
import { copilotPlugin } from './plugins/copilot.js';
import { lettaPlugin } from './plugins/letta.js';
import { codexPlugin } from './plugins/codex.js';
import { clinePlugin } from './plugins/cline.js';
import { droidPlugin } from './plugins/droid.js';
import { kimiPlugin } from './plugins/kimi.js';
import { grokPlugin } from './plugins/grok.js';
import { qwenPlugin } from './plugins/qwen.js';

const ELYDORA_DIR = path.join(os.homedir(), '.elydora');

const PLUGINS: ReadonlyMap<string, AgentPlugin> = new Map([
  ['augment', augmentPlugin],
  ['claudecode', claudecodePlugin],
  ['cursor', cursorPlugin],
  ['gemini', geminiPlugin],
  ['kirocli', kirocliPlugin],
  ['kiroide', kiroidePlugin],
  ['opencode', opencodePlugin],
  ['copilot', copilotPlugin],
  ['letta', lettaPlugin],
  ['codex', codexPlugin],
  ['cline', clinePlugin],
  ['droid', droidPlugin],
  ['kimi', kimiPlugin],
  ['grok', grokPlugin],
  ['qwen', qwenPlugin],
]);

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function die(message: string): never {
  console.error(`Error: ${message}`);
  process.exit(1);
}

interface InstalledAgent {
  agentId: string;
  agentName: string;
  configPath: string;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

async function readInstalledAgent(agentId: string): Promise<InstalledAgent | undefined> {
  const agentDirectory = resolvePrivateChildDirectory(ELYDORA_DIR, agentId);
  if (!(await requirePhysicalDirectory(agentDirectory))) return undefined;

  const configPath = path.join(agentDirectory, 'config.json');
  if (!(await requirePhysicalFile(configPath))) return undefined;
  let raw: string;
  try {
    raw = await fsp.readFile(configPath, 'utf-8');
  } catch (error) {
    throw new Error(`Could not read agent config: ${configPath}`, { cause: error });
  }

  let config: unknown;
  try {
    config = JSON.parse(raw);
  } catch (error) {
    throw new Error(`Could not parse agent config: ${configPath}`, { cause: error });
  }
  if (
    !isRecord(config)
    || typeof config.agent_id !== 'string'
    || typeof config.agent_name !== 'string'
  ) {
    throw new Error(`Agent config has an invalid runtime identity: ${configPath}`);
  }
  if (config.agent_id !== agentId) {
    throw new Error(`Agent config crosses its runtime directory: ${configPath}`);
  }
  resolvePrivateChildDirectory(ELYDORA_DIR, config.agent_id);
  return { agentId, agentName: config.agent_name, configPath };
}

async function discoverInstalledAgents(): Promise<InstalledAgent[]> {
  if (!(await requirePhysicalDirectory(ELYDORA_DIR))) return [];

  const entries = await fsp.readdir(ELYDORA_DIR, { withFileTypes: true });
  const installedAgents: InstalledAgent[] = [];
  for (const entry of entries) {
    if (entry.isSymbolicLink()) {
      throw new Error(`Agent runtime path is not a physical directory: ${path.join(ELYDORA_DIR, entry.name)}`);
    }
    if (!entry.isDirectory()) continue;
    const installedAgent = await readInstalledAgent(entry.name);
    if (installedAgent) installedAgents.push(installedAgent);
  }
  return installedAgents;
}

function printUsage(): void {
  console.log(`Elydora CLI — Tamper-evident audit for AI coding agents

Usage:
  elydora install   --agent <name> --org_id <id> --agent_id <id> --kid <kid> [--private_key_file <path>] [--token_file <path>] [--base_url <url>]
  elydora uninstall --agent <name> [--agent_id <id>]
  elydora status
  elydora agents

Commands:
  install     Install Elydora audit hook for a coding agent
  uninstall   Remove Elydora audit hook for a coding agent
  status      Show installation status for all agents
  agents      List supported coding agents

Supported agents: ${Array.from(SUPPORTED_AGENTS.keys()).join(', ')}
`);
}

// ---------------------------------------------------------------------------
// Commands
// ---------------------------------------------------------------------------

async function cmdInstall(args: string[]): Promise<void> {
  const { values } = parseArgs({
    args,
    options: {
      agent: { type: 'string' },
      org_id: { type: 'string' },
      agent_id: { type: 'string' },
      private_key_file: { type: 'string' },
      kid: { type: 'string' },
      token_file: { type: 'string' },
      base_url: { type: 'string' },
    },
    strict: true,
  });

  const agentName = values.agent;
  if (!agentName) die('--agent is required');
  if (!SUPPORTED_AGENTS.has(agentName)) {
    die(`Unknown agent "${agentName}". Supported: ${Array.from(SUPPORTED_AGENTS.keys()).join(', ')}`);
  }

  const orgId = values.org_id;
  if (!orgId) die('--org_id is required');

  const agentId = values.agent_id;
  if (!agentId) die('--agent_id is required');
  const agentDir = resolvePrivateChildDirectory(ELYDORA_DIR, agentId);

  const kid = values.kid;
  if (!kid) die('--kid is required');

  const { privateKey, token } = await resolveInstallSecrets({
    privateKeyFile: values.private_key_file,
    tokenFile: values.token_file,
  });
  const baseUrl = values.base_url ?? 'https://api.elydora.com';

  // Validate private key by deriving public key
  let publicKey: string;
  try {
    publicKey = derivePublicKey(privateKey);
  } catch {
    die('Invalid private key — could not derive public key');
  }

  console.log(`Verifying private key... Public key: ${publicKey.slice(0, 12)}...`);

  const agentConfigPath = path.join(agentDir, 'config.json');
  const keyPath = path.join(agentDir, 'private.key');
  const hookScriptPath = path.join(agentDir, 'hook.js');
  const guardScriptPath = path.join(agentDir, 'guard.js');
  const plugin = PLUGINS.get(agentName)!;
  const installConfig: InstallConfig = {
    agentName,
    orgId,
    agentId,
    privateKey,
    kid,
    token,
    baseUrl,
    hookScriptPath,
    guardScriptPath,
  };
  await plugin.preflightInstall?.(installConfig);

  if (!plugin.managesRuntime) {
    await ensurePrivateDirectory(ELYDORA_DIR);
    await ensurePrivateDirectory(agentDir);
    const agentConfig = {
      org_id: orgId,
      agent_id: agentId,
      kid,
      base_url: baseUrl,
      ...(token ? { token } : {}),
      agent_name: agentName,
    };
    await writePrivateFile(agentConfigPath, JSON.stringify(agentConfig, null, 2) + '\n');
    await writePrivateFile(keyPath, privateKey);
    const hookScript = generateHookScript(agentName, agentId);
    await fsp.writeFile(hookScriptPath, hookScript, { encoding: 'utf-8', mode: 0o755 });
    const guardScript = generateGuardScript(agentName, agentId);
    await fsp.writeFile(guardScriptPath, guardScript, { encoding: 'utf-8', mode: 0o755 });
  }

  await plugin.install(installConfig);
  console.log(`  Agent config: ${agentConfigPath}`);
  console.log(`  Private key:  ${keyPath}`);
  console.log(`  Hook script:  ${hookScriptPath}`);
  console.log(`  Guard script: ${guardScriptPath}`);

  const entry = SUPPORTED_AGENTS.get(agentName)!;
  console.log(`\nElydora audit hook installed for ${entry.name}.`);
}

async function cmdUninstall(args: string[]): Promise<void> {
  const { values } = parseArgs({
    args,
    options: {
      agent: { type: 'string' },
      agent_id: { type: 'string' },
    },
    strict: true,
  });

  const agentName = values.agent;
  if (!agentName) die('--agent is required');
  if (!SUPPORTED_AGENTS.has(agentName)) {
    die(`Unknown agent "${agentName}". Supported: ${Array.from(SUPPORTED_AGENTS.keys()).join(', ')}`);
  }

  let agentId = values.agent_id;
  let agentDir: string;
  let agentDirectoryExists: boolean;

  // If --agent_id not provided, scan ~/.elydora/*/config.json for matching agent_name
  if (agentId) {
    agentDir = resolvePrivateChildDirectory(ELYDORA_DIR, agentId);
    const runtimeRootExists = await requirePhysicalDirectory(ELYDORA_DIR);
    agentDirectoryExists = runtimeRootExists
      ? await requirePhysicalDirectory(agentDir)
      : false;
    if (agentDirectoryExists) {
      const installedAgent = await readInstalledAgent(agentId);
      if (installedAgent && installedAgent.agentName !== agentName) {
        throw new Error(
          `Agent runtime ${agentId} belongs to ${installedAgent.agentName}, not ${agentName}`,
        );
      }
    }
  } else {
    const matches = (await discoverInstalledAgents())
      .filter((installedAgent) => installedAgent.agentName === agentName);
    if (matches.length === 0) {
      die(`No installed agent found for "${agentName}". Use --agent_id to specify explicitly.`);
    }
    if (matches.length > 1) {
      die(`Multiple installed agents found for "${agentName}". Use --agent_id to specify explicitly.`);
    }
    agentId = matches[0].agentId;
    agentDir = resolvePrivateChildDirectory(ELYDORA_DIR, agentId);
    agentDirectoryExists = true;
  }

  const plugin = PLUGINS.get(agentName)!;
  const registryEntry = SUPPORTED_AGENTS.get(agentName)!;

  // Uninstall agent-specific config
  await plugin.uninstall(agentId);

  // Remove entire agent directory
  if (agentDirectoryExists) {
    await fsp.rm(agentDir, { recursive: true });
  }

  console.log(`Elydora audit hook uninstalled for ${registryEntry.name}.`);
}

async function cmdStatus(): Promise<void> {
  console.log('Elydora Agent Status\n');

  let anyInstalled = false;

  // Scan ~/.elydora/*/config.json to discover installed agents
  const installedAgents = await discoverInstalledAgents();

  for (const [name, plugin] of PLUGINS) {
    const st = await plugin.status();
    const statusIcon = st.installed ? '[installed]' : '[not installed]';

    // Find matching installed agent(s) for this plugin
    const matching = installedAgents.filter((a) => a.agentName === name);

    console.log(`  ${st.displayName} (${name}) ${statusIcon}`);
    if (st.installed || st.hookConfigured || st.hookScriptExists) {
      console.log(`    Hook config: ${st.hookConfigured ? 'yes' : 'no'}`);
      console.log(`    Hook script: ${st.hookScriptExists ? 'yes' : 'no'}`);
      console.log(`    Config path: ${st.configPath}`);
      for (const m of matching) {
        console.log(`    Agent ID:    ${m.agentId}`);
      }
    }

    if (st.installed) anyInstalled = true;
  }

  if (!anyInstalled) {
    console.log('\nNo agents installed. Run "elydora install --agent <name>" to get started.');
  }
}

function cmdAgents(): void {
  console.log('Supported Coding Agents:\n');
  for (const [key, entry] of SUPPORTED_AGENTS) {
    console.log(`  ${key.padEnd(12)} ${entry.name}`);
  }
  console.log('\nUse "elydora install --agent <name>" to install an audit hook.');
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

async function main(): Promise<void> {
  const args = process.argv.slice(2);

  if (args.length === 0 || args[0] === '--help' || args[0] === '-h') {
    printUsage();
    process.exit(0);
  }

  const command = args[0];
  const commandArgs = args.slice(1);

  switch (command) {
    case 'install':
      await cmdInstall(commandArgs);
      break;
    case 'uninstall':
      await cmdUninstall(commandArgs);
      break;
    case 'status':
      await cmdStatus();
      break;
    case 'agents':
      cmdAgents();
      break;
    default:
      die(`Unknown command "${command}". Run "elydora --help" for usage.`);
  }
}

main().catch((err) => {
  console.error(`Error: ${err instanceof Error ? err.message : String(err)}`);
  process.exit(1);
});
