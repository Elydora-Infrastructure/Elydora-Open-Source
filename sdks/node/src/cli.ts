import { parseArgs } from 'node:util';
import fsp from 'node:fs/promises';
import path from 'node:path';
import os from 'node:os';
import { derivePublicKey } from './crypto.js';
import { SUPPORTED_AGENTS } from './plugins/registry.js';
import type { AgentPlugin, InstallConfig } from './plugins/base.js';
import { generateHookScript, generateGuardScript } from './plugins/hook-template.js';
import { claudecodePlugin } from './plugins/claudecode.js';
import { cursorPlugin } from './plugins/cursor.js';
import { geminiPlugin } from './plugins/gemini.js';
import { kirocliPlugin } from './plugins/kirocli.js';
import { kiroidePlugin } from './plugins/kiroide.js';
import { opencodePlugin } from './plugins/opencode.js';
import { copilotPlugin } from './plugins/copilot.js';
import { lettaPlugin } from './plugins/letta.js';

const ELYDORA_DIR = path.join(os.homedir(), '.elydora');

const PLUGINS: ReadonlyMap<string, AgentPlugin> = new Map([
  ['claudecode', claudecodePlugin],
  ['cursor', cursorPlugin],
  ['gemini', geminiPlugin],
  ['kirocli', kirocliPlugin],
  ['kiroide', kiroidePlugin],
  ['opencode', opencodePlugin],
  ['copilot', copilotPlugin],
  ['letta', lettaPlugin],
]);

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function die(message: string): never {
  console.error(`Error: ${message}`);
  process.exit(1);
}

function printUsage(): void {
  console.log(`Elydora CLI — Tamper-evident audit for AI coding agents

Usage:
  elydora install   --agent <name> --org_id <id> --agent_id <id> --private_key <key> --kid <kid> [--token <jwt>] [--base_url <url>]
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
      private_key: { type: 'string' },
      kid: { type: 'string' },
      token: { type: 'string' },
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

  const privateKey = values.private_key;
  if (!privateKey) die('--private_key is required');

  const kid = values.kid;
  if (!kid) die('--kid is required');

  const token = values.token;
  const baseUrl = values.base_url ?? 'https://api.elydora.com';

  // Validate private key by deriving public key
  let publicKey: string;
  try {
    publicKey = derivePublicKey(privateKey);
  } catch {
    die('Invalid private key — could not derive public key');
  }

  console.log(`Verifying private key... Public key: ${publicKey.slice(0, 12)}...`);

  // Create ~/.elydora/{agentId}/ directory
  const agentDir = path.join(ELYDORA_DIR, agentId);
  await fsp.mkdir(agentDir, { recursive: true });

  // Write agent config
  const agentConfigPath = path.join(agentDir, 'config.json');
  const agentConfig = {
    org_id: orgId,
    agent_id: agentId,
    kid,
    base_url: baseUrl,
    ...(token ? { token } : {}),
    agent_name: agentName,
  };
  await fsp.writeFile(agentConfigPath, JSON.stringify(agentConfig, null, 2) + '\n', 'utf-8');
  if (process.platform !== 'win32') {
    await fsp.chmod(agentConfigPath, 0o600);
  }
  console.log(`  Agent config: ${agentConfigPath}`);

  // Write private key (chmod 600)
  const keyPath = path.join(agentDir, 'private.key');
  await fsp.writeFile(keyPath, privateKey, { encoding: 'utf-8', mode: 0o600 });
  if (process.platform !== 'win32') {
    await fsp.chmod(keyPath, 0o600);
  }
  console.log(`  Private key:  ${keyPath}`);

  // Generate and write hook script (PostToolUse — audit logging)
  const hookScriptPath = path.join(agentDir, 'hook.js');
  const hookScript = generateHookScript(agentName, agentId);
  await fsp.writeFile(hookScriptPath, hookScript, { encoding: 'utf-8', mode: 0o755 });
  console.log(`  Hook script:  ${hookScriptPath}`);

  // Generate and write guard script (PreToolUse — freeze enforcement)
  const guardScriptPath = path.join(agentDir, 'guard.js');
  const guardScript = generateGuardScript(agentName, agentId);
  await fsp.writeFile(guardScriptPath, guardScript, { encoding: 'utf-8', mode: 0o755 });
  console.log(`  Guard script: ${guardScriptPath}`);

  // Install agent-specific config hook
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

  await plugin.install(installConfig);

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

  // If --agent_id not provided, scan ~/.elydora/*/config.json for matching agent_name
  if (!agentId) {
    try {
      const entries = await fsp.readdir(ELYDORA_DIR, { withFileTypes: true });
      for (const entry of entries) {
        if (!entry.isDirectory()) continue;
        const cfgPath = path.join(ELYDORA_DIR, entry.name, 'config.json');
        try {
          const raw = await fsp.readFile(cfgPath, 'utf-8');
          const cfg = JSON.parse(raw);
          if (cfg.agent_name === agentName) {
            agentId = entry.name;
            break;
          }
        } catch {
          // Skip unreadable configs
        }
      }
    } catch {
      // ELYDORA_DIR may not exist
    }
    if (!agentId) {
      die(`No installed agent found for "${agentName}". Use --agent_id to specify explicitly.`);
    }
  }

  const plugin = PLUGINS.get(agentName)!;
  const registryEntry = SUPPORTED_AGENTS.get(agentName)!;

  // Uninstall agent-specific config
  await plugin.uninstall(agentId);

  // Remove entire agent directory
  const agentDir = path.join(ELYDORA_DIR, agentId);
  try {
    await fsp.rm(agentDir, { recursive: true, force: true });
  } catch {
    // Already removed
  }

  console.log(`Elydora audit hook uninstalled for ${registryEntry.name}.`);
}

async function cmdStatus(): Promise<void> {
  console.log('Elydora Agent Status\n');

  let anyInstalled = false;

  // Scan ~/.elydora/*/config.json to discover installed agents
  const installedAgents: Array<{ agentId: string; agentName: string; configPath: string }> = [];
  try {
    const entries = await fsp.readdir(ELYDORA_DIR, { withFileTypes: true });
    for (const entry of entries) {
      if (!entry.isDirectory()) continue;
      const cfgPath = path.join(ELYDORA_DIR, entry.name, 'config.json');
      try {
        const raw = await fsp.readFile(cfgPath, 'utf-8');
        const cfg = JSON.parse(raw);
        if (cfg.agent_name && cfg.agent_id) {
          installedAgents.push({ agentId: entry.name, agentName: cfg.agent_name, configPath: cfgPath });
        }
      } catch {
        // Skip unreadable configs
      }
    }
  } catch {
    // ELYDORA_DIR may not exist
  }

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
