import fsp from 'node:fs/promises';
import path from 'node:path';
import os from 'node:os';
import type { AgentPlugin, InstallConfig, PluginStatus } from './base.js';
import { SUPPORTED_AGENTS } from './registry.js';

const AGENT_KEY = 'claudecode';
const entry = SUPPORTED_AGENTS.get(AGENT_KEY)!;

function resolveConfigDir(): string {
  return entry.configDir.replace(/^~/, os.homedir());
}

function resolveConfigPath(): string {
  return path.join(resolveConfigDir(), entry.configFile);
}

function buildHookCommand(scriptPath: string): string {
  return `node "${scriptPath}"`;
}

function isElydoraCommand(cmd: string, agentId?: string): boolean {
  if (!cmd.includes('elydora')) return false;
  if (agentId) {
    return cmd.includes(agentId);
  }
  return true;
}

function filterElydoraEntries(arr: Array<Record<string, unknown>>, agentId?: string): Array<Record<string, unknown>> {
  return arr.filter((entry) => {
    if (Array.isArray(entry.hooks)) {
      const cmds = entry.hooks as Array<Record<string, unknown>>;
      return !cmds.some((h) => typeof h.command === 'string' && isElydoraCommand(h.command, agentId));
    }
    if (typeof entry.command === 'string') {
      return !isElydoraCommand(entry.command, agentId);
    }
    return true;
  });
}

export const claudecodePlugin: AgentPlugin = {
  async install(config: InstallConfig): Promise<void> {
    const configDir = resolveConfigDir();
    await fsp.mkdir(configDir, { recursive: true });

    const configPath = resolveConfigPath();
    let settings: Record<string, unknown> = {};

    try {
      const raw = await fsp.readFile(configPath, 'utf-8');
      settings = JSON.parse(raw);
    } catch {
      // File doesn't exist or isn't valid JSON — start fresh
    }

    if (!settings.hooks || typeof settings.hooks !== 'object') {
      settings.hooks = {};
    }
    const hooks = settings.hooks as Record<string, unknown>;

    // --- PreToolUse (guard — freeze enforcement) ---
    if (!Array.isArray(hooks.PreToolUse)) {
      hooks.PreToolUse = [];
    }
    const preFiltered = filterElydoraEntries(hooks.PreToolUse as Array<Record<string, unknown>>);
    preFiltered.push({
      hooks: [{ type: 'command', command: buildHookCommand(config.guardScriptPath) }],
    });
    hooks.PreToolUse = preFiltered;

    // --- PostToolUse (audit logging) ---
    if (!Array.isArray(hooks.PostToolUse)) {
      hooks.PostToolUse = [];
    }
    const postFiltered = filterElydoraEntries(hooks.PostToolUse as Array<Record<string, unknown>>);
    postFiltered.push({
      hooks: [{ type: 'command', command: buildHookCommand(config.hookScriptPath) }],
    });
    hooks.PostToolUse = postFiltered;

    settings.hooks = hooks;
    await fsp.writeFile(configPath, JSON.stringify(settings, null, 2) + '\n', 'utf-8');
  },

  async uninstall(agentId?: string): Promise<void> {
    const configPath = resolveConfigPath();

    let settings: Record<string, unknown>;
    try {
      const raw = await fsp.readFile(configPath, 'utf-8');
      settings = JSON.parse(raw);
    } catch {
      return; // Nothing to uninstall
    }

    const hooks = settings.hooks as Record<string, unknown> | undefined;
    if (!hooks) return;

    if (Array.isArray(hooks.PreToolUse)) {
      hooks.PreToolUse = filterElydoraEntries(hooks.PreToolUse as Array<Record<string, unknown>>, agentId);
    }
    if (Array.isArray(hooks.PostToolUse)) {
      hooks.PostToolUse = filterElydoraEntries(hooks.PostToolUse as Array<Record<string, unknown>>, agentId);
    }

    await fsp.writeFile(configPath, JSON.stringify(settings, null, 2) + '\n', 'utf-8');
  },

  async status(): Promise<PluginStatus> {
    const configPath = resolveConfigPath();

    let hookConfigured = false;
    let hookScriptPath = '';
    try {
      const raw = await fsp.readFile(configPath, 'utf-8');
      const settings = JSON.parse(raw);
      const hooks = settings.hooks as Record<string, unknown> | undefined;
      if (hooks) {
        const checkArr = (arr: unknown) => {
          if (!Array.isArray(arr)) return false;
          return (arr as Array<Record<string, unknown>>).some((entry) => {
            if (Array.isArray(entry.hooks)) {
              return (entry.hooks as Array<Record<string, unknown>>).some(
                (h) => typeof h.command === 'string' && h.command.includes('elydora'),
              );
            }
            return typeof entry.command === 'string' && entry.command.includes('elydora');
          });
        };
        hookConfigured = checkArr(hooks.PreToolUse) && checkArr(hooks.PostToolUse);

        // Extract hook script path from PostToolUse command
        if (hookConfigured && Array.isArray(hooks.PostToolUse)) {
          for (const e of hooks.PostToolUse as Array<Record<string, unknown>>) {
            if (Array.isArray(e.hooks)) {
              for (const h of e.hooks as Array<Record<string, unknown>>) {
                const cmd = h.command as string;
                if (cmd && cmd.includes('elydora')) {
                  hookScriptPath = cmd.replace(/^node\s+"?/, '').replace(/"?\s*$/, '');
                }
              }
            }
          }
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
