import type { AgentPlugin, InstallConfig, PluginStatus } from './base.js';
import {
  AGENT_KEY,
  buildKiroCliV2Hooks,
  buildKiroCliV3Hooks,
  commonKiroCliRuntimeContract,
  renderKiroCliV2Installation,
  renderKiroCliV2Uninstall,
  renderKiroCliV3Uninstall,
  requireAvailableKiroCliV2Document,
  requireAvailableKiroCliV3Hooks,
  withoutManagedKiroCliV2Hooks,
  withoutManagedKiroCliV3Hooks,
} from './kirocli-contract.js';
import {
  commitKiroCliInstallation,
  commitKiroCliUninstall,
  preflightKiroCliInstallation,
  prepareKiroCliInstallation,
  prepareKiroCliUninstall,
} from './kirocli-installation.js';
import { kiroCliRuntimeFilesExist, readKiroCliSources } from './kirocli-io.js';
import { renderKiroIdeDocument } from './kiroide-contract.js';
import { SUPPORTED_AGENTS } from './registry.js';

const entry = SUPPORTED_AGENTS.get(AGENT_KEY)!;

function validateSources(
  sources: Awaited<ReturnType<typeof readKiroCliSources>>,
): void {
  requireAvailableKiroCliV2Document(sources.v2);
  requireAvailableKiroCliV3Hooks(sources.v3.hooks);
}

export const kirocliPlugin: AgentPlugin = {
  managesRuntime: true,

  async preflightInstall(config: InstallConfig): Promise<void> {
    const sources = await readKiroCliSources();
    validateSources(sources);
    await preflightKiroCliInstallation(config, sources);
  },

  async install(config: InstallConfig): Promise<void> {
    const sources = await readKiroCliSources();
    validateSources(sources);
    const paths = await preflightKiroCliInstallation(config, sources);
    const v2 = renderKiroCliV2Installation(
      sources.v2,
      buildKiroCliV2Hooks(sources.v2, paths.guardPath, paths.auditPath),
    );
    const v3 = renderKiroIdeDocument(
      sources.v3,
      buildKiroCliV3Hooks(sources.v3.hooks, paths.guardPath, paths.auditPath),
    );
    const prepared = await prepareKiroCliInstallation(config, sources, v2, v3);
    await commitKiroCliInstallation(prepared);
    console.log(`  Kiro CLI v2 agent: ${sources.paths.v2Path}`);
    console.log('  Kiro CLI v2 verification: run "kiro-cli agent validate --path <agent-file>".');
    console.log('  Kiro CLI v2 activation: start with "kiro-cli --agent elydora-audit".');
    console.log(`  Kiro CLI v3 global hooks (2.13.0+): ${sources.paths.v3Path}`);
    console.log('  Kiro CLI v3 activation: start the TUI with "kiro-cli --v3".');
  },

  async uninstall(agentId?: string): Promise<void> {
    const sources = await readKiroCliSources();
    const v2 = renderKiroCliV2Uninstall(
      sources.v2,
      withoutManagedKiroCliV2Hooks(sources.v2, agentId),
      agentId,
    );
    const v3 = renderKiroCliV3Uninstall(
      sources.v3,
      withoutManagedKiroCliV3Hooks(sources.v3.hooks, agentId),
      agentId,
    );
    const prepared = await prepareKiroCliUninstall(sources, v2, v3);
    await commitKiroCliUninstall(prepared);
  },

  async status(): Promise<PluginStatus> {
    const sources = await readKiroCliSources();
    validateSources(sources);
    const contract = commonKiroCliRuntimeContract(sources.v2, sources.v3.hooks);
    const hookConfigured = contract !== undefined;
    const hookScriptExists = contract !== undefined
      && await kiroCliRuntimeFilesExist(contract);
    return {
      installed: hookConfigured && hookScriptExists,
      agentName: AGENT_KEY,
      displayName: entry.name,
      hookConfigured,
      hookScriptExists,
      configPath: sources.paths.v3Path,
    };
  },
};
