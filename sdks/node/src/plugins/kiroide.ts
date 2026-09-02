import type { AgentPlugin, InstallConfig, PluginStatus } from './base.js';
import {
  AGENT_KEY,
  buildKiroIdeHook,
  kiroIdeRuntimeContracts,
  renderKiroIdeDocument,
  requireAvailableKiroIdeHooks,
  withoutManagedKiroIdeHooks,
} from './kiroide-contract.js';
import {
  commitKiroIdeInstallation,
  commitKiroIdeUninstall,
  preflightKiroIdeInstallation,
  prepareKiroIdeInstallation,
  prepareKiroIdeUninstall,
} from './kiroide-installation.js';
import { readKiroIdeSources } from './kiroide-io.js';
import { managedRuntimeFilesExist } from './managed-runtime-status.js';
import { SUPPORTED_AGENTS } from './registry.js';

const entry = SUPPORTED_AGENTS.get(AGENT_KEY)!;

export const kiroidePlugin: AgentPlugin = {
  managesRuntime: true,

  async preflightInstall(config: InstallConfig): Promise<void> {
    const sources = await readKiroIdeSources();
    requireAvailableKiroIdeHooks(sources.document.hooks);
    await preflightKiroIdeInstallation(config, sources.paths);
  },

  async install(config: InstallConfig): Promise<void> {
    const sources = await readKiroIdeSources();
    requireAvailableKiroIdeHooks(sources.document.hooks);
    const paths = await preflightKiroIdeInstallation(config, sources.paths);
    const hooks = withoutManagedKiroIdeHooks(sources.document.hooks);
    const rendered = renderKiroIdeDocument(sources.document, [
      ...hooks,
      buildKiroIdeHook('elydora-guard', paths.guardPath),
      buildKiroIdeHook('elydora-audit', paths.auditPath),
    ]);
    const prepared = await prepareKiroIdeInstallation(config, sources, rendered);
    await commitKiroIdeInstallation(prepared);
    console.log(`  Kiro IDE workspace hooks: ${sources.paths.configPath}`);
    console.log('  Kiro IDE verification: confirm Elydora hooks in the Agent Hooks panel.');
  },

  async uninstall(agentId?: string): Promise<void> {
    const sources = await readKiroIdeSources();
    const rendered = renderKiroIdeDocument(
      sources.document,
      withoutManagedKiroIdeHooks(sources.document.hooks, agentId),
    );
    const prepared = await prepareKiroIdeUninstall(sources, rendered, agentId);
    await commitKiroIdeUninstall(prepared);
  },

  async status(): Promise<PluginStatus> {
    const sources = await readKiroIdeSources();
    requireAvailableKiroIdeHooks(sources.document.hooks);
    const contracts = kiroIdeRuntimeContracts(sources.document.hooks);
    const hookConfigured = contracts.length > 0;
    const hookScriptExists = hookConfigured
      && (await Promise.all(contracts.map((contract) => (
        managedRuntimeFilesExist(contract, AGENT_KEY)
      )))).every(Boolean);
    return {
      installed: hookConfigured && hookScriptExists,
      agentName: AGENT_KEY,
      displayName: entry.name,
      hookConfigured,
      hookScriptExists,
      configPath: sources.paths.configPath,
    };
  },
};
