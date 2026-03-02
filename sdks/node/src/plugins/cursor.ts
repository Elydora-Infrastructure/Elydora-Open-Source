import fsp from 'node:fs/promises';
import path from 'node:path';
import os from 'node:os';
import type { AgentPlugin, InstallConfig, PluginStatus } from './base.js';
import { SUPPORTED_AGENTS } from './registry.js';

const AGENT_KEY = 'cursor';
const entry = SUPPORTED_AGENTS.get(AGENT_KEY)!;

function resolveConfigDir(): string {
  // .cursor is project-relative, but for global install use home dir
  return path.join(os.homedir(), entry.configDir);
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
  return arr.filter(
    (h) => typeof h.command === 'string' && !isElydoraCommand(h.command, agentId),
  );
}

export const cursorPlugin: AgentPlugin = {
  async install(config: InstallConfig): Promise<void> {
    const configDir = resolveConfigDir();
    await fsp.mkdir(configDir, { recursive: true });

    const configPath = resolveConfigPath();
    let settings: Record<string, unknown> = {};

    try {
      const raw = await fsp.readFile(configPath, 'utf-8');
      settings = JSON.parse(raw);
    } catch {
      // Start fresh
    }

    if (!settings.hooks || typeof settings.hooks !== 'object') {
      settings.hooks = {};
    }
    const hooks = settings.hooks as Record<string, unknown>;

    // --- preToolUse (guard — freeze enforcement) ---
    if (!Array.isArray(hooks.preToolUse)) {
      hooks.preToolUse = [];
    }
    const preFiltered = filterElydoraEntries(hooks.preToolUse as Array<Record<string, unknown>>);
    preFiltered.push({ command: buildHookCommand(config.guardScriptPath) });
    hooks.preToolUse = preFiltered;

    // --- postToolUse (audit logging) ---
    if (!Array.isArray(hooks.postToolUse)) {
      hooks.postToolUse = [];
    }
    const postFiltered = filterElydoraEntries(hooks.postToolUse as Array<Record<string, unknown>>);
    postFiltered.push({ command: buildHookCommand(config.hookScriptPath) });
    hooks.postToolUse = postFiltered;

    settings.hooks = hooks;
    settings.version = 1;
    await fsp.writeFile(configPath, JSON.stringify(settings, null, 2) + '\n', 'utf-8');
  },

  async uninstall(agentId?: string): Promise<void> {
    const configPath = resolveConfigPath();

    let settings: Record<string, unknown>;
    try {
      const raw = await fsp.readFile(configPath, 'utf-8');
      settings = JSON.parse(raw);
    } catch {
      return;
    }

    const hooks = settings.hooks as Record<string, unknown> | undefined;
    if (!hooks) return;

    if (Array.isArray(hooks.preToolUse)) {
      hooks.preToolUse = filterElydoraEntries(hooks.preToolUse as Array<Record<string, unknown>>, agentId);
    }
    if (Array.isArray(hooks.postToolUse)) {
      hooks.postToolUse = filterElydoraEntries(hooks.postToolUse as Array<Record<string, unknown>>, agentId);
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
        const check = (arr: unknown) =>
          Array.isArray(arr) && (arr as Array<Record<string, unknown>>).some(
            (h) => typeof h.command === 'string' && h.command.includes('elydora'),
          );
        hookConfigured = check(hooks.preToolUse) && check(hooks.postToolUse);

        // Extract hook script path from postToolUse command
        if (hookConfigured && Array.isArray(hooks.postToolUse)) {
          for (const h of hooks.postToolUse as Array<Record<string, unknown>>) {
            const cmd = h.command as string;
            if (cmd && cmd.includes('elydora')) {
              hookScriptPath = cmd.replace(/^node\s+"?/, '').replace(/"?\s*$/, '');
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
