import fsp from 'node:fs/promises';
import path from 'node:path';
import os from 'node:os';
import type { AgentPlugin, InstallConfig, PluginStatus } from './base.js';
import { SUPPORTED_AGENTS } from './registry.js';

const AGENT_KEY = 'kiroide';
const entry = SUPPORTED_AGENTS.get(AGENT_KEY)!;

function resolveConfigDir(): string {
  // .kiro/hooks is project-relative, but for global install use home dir
  return path.join(os.homedir(), entry.configDir);
}

function resolveConfigPath(): string {
  return path.join(resolveConfigDir(), entry.configFile);
}

export const kiroidePlugin: AgentPlugin = {
  async install(config: InstallConfig): Promise<void> {
    const configDir = resolveConfigDir();
    await fsp.mkdir(configDir, { recursive: true });

    const configPath = resolveConfigPath();

    const hookConfig = {
      name: 'Elydora Audit',
      description: 'Sends tool-use events to the Elydora tamper-evident audit platform',
      version: '1.0.0',
      hooks: {
        pre_tool_use: {
          command: `node "${config.guardScriptPath}"`,
          timeout_ms: 5000,
        },
        post_tool_use: {
          command: `node "${config.hookScriptPath}"`,
          timeout_ms: 5000,
        },
      },
    };

    await fsp.writeFile(configPath, JSON.stringify(hookConfig, null, 2) + '\n', 'utf-8');
  },

  async uninstall(_agentId?: string): Promise<void> {
    const configPath = resolveConfigPath();
    try {
      await fsp.unlink(configPath);
    } catch {
      // Already removed
    }
  },

  async status(): Promise<PluginStatus> {
    const configPath = resolveConfigPath();

    let hookConfigured = false;
    let hookScriptPath = '';
    try {
      const raw = await fsp.readFile(configPath, 'utf-8');
      const config = JSON.parse(raw);
      hookConfigured = !!(config.hooks && config.hooks.pre_tool_use && config.hooks.post_tool_use);

      // Extract hook script path from post_tool_use command
      if (hookConfigured && config.hooks.post_tool_use) {
        const cmd = config.hooks.post_tool_use.command as string;
        if (cmd && cmd.includes('elydora')) {
          hookScriptPath = cmd.replace(/^node\s+"?/, '').replace(/"?\s*$/, '');
        }
      }
    } catch {
      // Config not readable
    }

    let hookScriptExists = false;
    if (hookScriptPath) {
      try {
        await fsp.access(hookScriptPath);
        hookScriptExists = true;
      } catch {
        // File doesn't exist
      }
    }

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
