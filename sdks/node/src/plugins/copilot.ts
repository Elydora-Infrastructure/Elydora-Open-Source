import type { AgentPlugin, InstallConfig, PluginStatus } from './base.js';
import {
  AGENT_KEY,
  AUDIT_SCRIPT,
  GUARD_SCRIPT,
  buildHandler,
  removeManagedHooks,
  renderDocument,
  runtimeContracts,
} from './copilot-contract.js';
import {
  readSources,
  requireRuntime,
  runtimeFilesExist,
  writeDocuments,
} from './copilot-io.js';
import { SUPPORTED_AGENTS } from './registry.js';

const entry = SUPPORTED_AGENTS.get(AGENT_KEY)!;

function mergedContracts(
  userHooks: Parameters<typeof runtimeContracts>[0],
  legacyHooks?: Parameters<typeof runtimeContracts>[0],
): ReturnType<typeof runtimeContracts> {
  const contracts = [
    ...runtimeContracts(userHooks),
    ...runtimeContracts(legacyHooks ?? {}),
  ];
  const unique = new Map(contracts.map((contract) => [contract.agentId, contract]));
  return [...unique.values()];
}

export const copilotPlugin: AgentPlugin = {
  async install(config: InstallConfig): Promise<void> {
    if (!config.agentId) throw new Error('agentId is required');
    const sources = await readSources();
    await requireRuntime(config.guardScriptPath, 'Elydora guard runtime');
    await requireRuntime(config.hookScriptPath, 'Elydora audit runtime');

    const userHooks = removeManagedHooks(sources.user.hooks);
    userHooks.preToolUse = [
      ...(userHooks.preToolUse ?? []),
      buildHandler(config.guardScriptPath),
    ];
    userHooks.postToolUse = [
      ...(userHooks.postToolUse ?? []),
      buildHandler(config.hookScriptPath),
    ];

    const rendered = [renderDocument(sources.user, userHooks)];
    if (sources.legacy) {
      rendered.push(renderDocument(
        sources.legacy,
        removeManagedHooks(sources.legacy.hooks),
      ));
    }
    await writeDocuments(rendered);
    console.log(`  GitHub Copilot CLI hooks: ${sources.user.filePath}`);
  },

  async uninstall(agentId?: string): Promise<void> {
    const sources = await readSources();
    const rendered = [
      renderDocument(sources.user, removeManagedHooks(sources.user.hooks, agentId)),
    ];
    if (sources.legacy) {
      rendered.push(renderDocument(
        sources.legacy,
        removeManagedHooks(sources.legacy.hooks, agentId),
      ));
    }
    await writeDocuments(rendered);
  },

  async status(): Promise<PluginStatus> {
    const sources = await readSources();
    const contracts = mergedContracts(sources.user.hooks, sources.legacy?.hooks);
    const hookConfigured = contracts.length > 0;
    const hookScriptExists = hookConfigured && await runtimeFilesExist(contracts);
    return {
      installed: hookConfigured && hookScriptExists,
      agentName: AGENT_KEY,
      displayName: entry.name,
      hookConfigured,
      hookScriptExists,
      configPath: sources.user.filePath,
    };
  },
};
