import type { AgentPlugin, InstallConfig, PluginStatus } from './base.js';
import {
  AGENT_KEY,
  buildHandler,
  removeManagedHooks,
  renderDocument,
  runtimeContracts,
  type CopilotSources,
  type RenderedDocument,
} from './copilot-contract.js';
import {
  commitCopilotInstallation,
  commitCopilotUninstall,
  preflightCopilotInstallation,
  prepareCopilotInstallation,
  prepareCopilotUninstall,
} from './copilot-installation.js';
import { readSources, runtimeFilesExist } from './copilot-io.js';
import { SUPPORTED_AGENTS } from './registry.js';

const entry = SUPPORTED_AGENTS.get(AGENT_KEY)!;

function renderInstallation(
  sources: CopilotSources,
  guardPath: string,
  auditPath: string,
): RenderedDocument[] {
  const userHooks = removeManagedHooks(sources.user.hooks);
  userHooks.preToolUse = [...(userHooks.preToolUse ?? []), buildHandler(guardPath)];
  userHooks.postToolUse = [...(userHooks.postToolUse ?? []), buildHandler(auditPath)];
  userHooks.postToolUseFailure = [
    ...(userHooks.postToolUseFailure ?? []),
    buildHandler(auditPath),
  ];
  const rendered = [renderDocument(sources.user, userHooks)];
  if (sources.legacy) {
    rendered.push(renderDocument(
      sources.legacy,
      removeManagedHooks(sources.legacy.hooks),
    ));
  }
  return rendered;
}

function renderUninstall(sources: CopilotSources, agentId?: string): RenderedDocument[] {
  const rendered = [
    renderDocument(sources.user, removeManagedHooks(sources.user.hooks, agentId)),
  ];
  if (sources.legacy) {
    rendered.push(renderDocument(
      sources.legacy,
      removeManagedHooks(sources.legacy.hooks, agentId),
    ));
  }
  return rendered;
}

function mergedContracts(sources: CopilotSources) {
  const contracts = [
    ...runtimeContracts(sources.user.hooks),
    ...runtimeContracts(sources.legacy?.hooks ?? {}),
  ];
  const unique = new Map(contracts.map((contract) => [
    process.platform === 'win32' ? contract.agentId.toLowerCase() : contract.agentId,
    contract,
  ]));
  return [...unique.values()];
}

export const copilotPlugin: AgentPlugin = {
  managesRuntime: true,

  async preflightInstall(config: InstallConfig): Promise<void> {
    const sources = await readSources();
    await preflightCopilotInstallation(config, sources);
  },

  async install(config: InstallConfig): Promise<void> {
    const sources = await readSources();
    const paths = await preflightCopilotInstallation(config, sources);
    const rendered = renderInstallation(sources, paths.guardPath, paths.auditPath);
    const prepared = await prepareCopilotInstallation(config, sources, rendered);
    await commitCopilotInstallation(prepared);
    console.log(`  GitHub Copilot CLI hooks: ${sources.user.filePath}`);
    console.log('  GitHub Copilot CLI: restart active sessions to load the updated hooks.');
  },

  async uninstall(agentId?: string): Promise<void> {
    const sources = await readSources();
    const prepared = await prepareCopilotUninstall(renderUninstall(sources, agentId));
    await commitCopilotUninstall(prepared);
  },

  async status(): Promise<PluginStatus> {
    const sources = await readSources();
    const contracts = mergedContracts(sources);
    const hookConfigured = sources.disabledBy === undefined && contracts.length > 0;
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
