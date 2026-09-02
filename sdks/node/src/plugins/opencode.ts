import type { AgentPlugin, InstallConfig, PluginStatus } from './base.js';
import { AGENT_KEY } from './opencode-contract.js';
import {
  commitOpenCodeInstallation,
  commitOpenCodeUninstall,
  preflightOpenCodeInstallation,
  prepareOpenCodeInstallation,
  prepareOpenCodeUninstall,
} from './opencode-installation.js';
import {
  openCodeRuntimeFilesExist,
  readOpenCodeSources,
  requireAvailableOpenCodePlugin,
} from './opencode-io.js';
import { SUPPORTED_AGENTS } from './registry.js';

const entry = SUPPORTED_AGENTS.get(AGENT_KEY)!;

function validateSources(sources: Awaited<ReturnType<typeof readOpenCodeSources>>): void {
  requireAvailableOpenCodePlugin(sources.current);
}

export const opencodePlugin: AgentPlugin = {
  managesRuntime: true,

  async preflightInstall(config: InstallConfig): Promise<void> {
    const sources = await readOpenCodeSources();
    validateSources(sources);
    await preflightOpenCodeInstallation(config, sources);
  },

  async install(config: InstallConfig): Promise<void> {
    const sources = await readOpenCodeSources();
    validateSources(sources);
    const prepared = await prepareOpenCodeInstallation(config, sources);
    await commitOpenCodeInstallation(prepared);
    console.log(`  OpenCode global plugin: ${sources.paths.pluginPath}`);
    console.log('  OpenCode activation: restart active OpenCode sessions.');
  },

  async uninstall(agentId?: string): Promise<void> {
    const sources = await readOpenCodeSources();
    const prepared = await prepareOpenCodeUninstall(sources, agentId);
    await commitOpenCodeUninstall(prepared);
  },

  async status(): Promise<PluginStatus> {
    const sources = await readOpenCodeSources();
    validateSources(sources);
    const contract = sources.legacy.contract ? undefined : sources.current.contract;
    const hookConfigured = contract !== undefined;
    const hookScriptExists = contract !== undefined
      && await openCodeRuntimeFilesExist(contract);
    return {
      installed: hookConfigured && hookScriptExists,
      agentName: AGENT_KEY,
      displayName: entry.name,
      hookConfigured,
      hookScriptExists,
      configPath: sources.paths.pluginPath,
    };
  },
};
