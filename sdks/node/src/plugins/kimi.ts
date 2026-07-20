import type { AgentPlugin, InstallConfig, PluginStatus } from './base.js';
import {
  AGENT_KEY,
  buildKimiHook,
  kimiRuntimeContracts,
  removeManagedKimiHooks,
  renderKimiDocument,
  type KimiConfigDocument,
  type KimiHook,
} from './kimi-contract.js';
import {
  commitKimiInstallation,
  commitKimiUninstall,
  preflightKimiInstallation,
  prepareKimiInstallation,
  prepareKimiUninstall,
} from './kimi-installation.js';
import { kimiRuntimeFilesExist, readKimiDocuments } from './kimi-io.js';
import { SUPPORTED_AGENTS } from './registry.js';

const entry = SUPPORTED_AGENTS.get(AGENT_KEY)!;

function installedHooks(
  document: KimiConfigDocument,
  guardPath: string,
  auditPath: string,
): KimiHook[] {
  return [
    ...removeManagedKimiHooks(document.hooks),
    buildKimiHook('PreToolUse', guardPath),
    buildKimiHook('PostToolUse', auditPath),
    buildKimiHook('PostToolUseFailure', auditPath),
  ];
}

export const kimiPlugin: AgentPlugin = {
  managesRuntime: true,

  async preflightInstall(config: InstallConfig): Promise<void> {
    const documents = await readKimiDocuments();
    await preflightKimiInstallation(config, documents);
  },

  async install(config: InstallConfig): Promise<void> {
    const documents = await readKimiDocuments();
    const paths = await preflightKimiInstallation(config, documents);
    const rendered = await Promise.all(documents.map((document) => renderKimiDocument(
      document,
      installedHooks(document, paths.guardPath, paths.auditPath),
    )));
    const prepared = await prepareKimiInstallation(config, rendered);
    await commitKimiInstallation(prepared);
    const runtimes = documents.map(({ contract }) => contract.runtimeName).join(' and ');
    console.log(
      `  ${runtimes}: global PreToolUse, PostToolUse, and PostToolUseFailure hooks installed.`,
    );
  },

  async uninstall(agentId?: string): Promise<void> {
    const documents = await readKimiDocuments();
    const rendered = await Promise.all(documents.map((document) => renderKimiDocument(
      document,
      removeManagedKimiHooks(document.hooks, agentId),
    )));
    await commitKimiUninstall(await prepareKimiUninstall(rendered));
  },

  async status(): Promise<PluginStatus> {
    const documents = await readKimiDocuments();
    const contracts = kimiRuntimeContracts(documents);
    const hookConfigured = contracts.length > 0;
    const hookScriptExists = hookConfigured && await kimiRuntimeFilesExist(contracts);
    return {
      installed: hookConfigured && hookScriptExists,
      agentName: AGENT_KEY,
      displayName: entry.name,
      hookConfigured,
      hookScriptExists,
      configPath: contracts.at(-1)?.configPath ?? documents[0].contract.configPath,
    };
  },
};
