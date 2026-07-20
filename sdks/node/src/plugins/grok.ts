import type { AgentPlugin, InstallConfig, PluginStatus } from './base.js';
import {
  AGENT_KEY,
  buildGrokGroup,
  grokRuntimeContracts,
  removeManagedGrokHooks,
  renderGrokDocument,
  type GrokHooks,
} from './grok-contract.js';
import {
  commitGrokInstallation,
  commitGrokUninstall,
  preflightGrokInstallation,
  prepareGrokInstallation,
  prepareGrokUninstall,
} from './grok-installation.js';
import { grokRuntimeFilesExist, readGrokDocument } from './grok-io.js';
import { SUPPORTED_AGENTS } from './registry.js';

const entry = SUPPORTED_AGENTS.get(AGENT_KEY)!;

function installedHooks(
  hooks: GrokHooks,
  guardPath: string,
  auditPath: string,
): GrokHooks {
  const cleaned = removeManagedGrokHooks(hooks);
  return {
    ...cleaned,
    PreToolUse: [...(cleaned.PreToolUse ?? []), buildGrokGroup(guardPath)],
    PostToolUse: [...(cleaned.PostToolUse ?? []), buildGrokGroup(auditPath)],
    PostToolUseFailure: [
      ...(cleaned.PostToolUseFailure ?? []),
      buildGrokGroup(auditPath),
    ],
  };
}

export const grokPlugin: AgentPlugin = {
  managesRuntime: true,

  async preflightInstall(config: InstallConfig): Promise<void> {
    const document = await readGrokDocument();
    await preflightGrokInstallation(config, document.filePath);
  },

  async install(config: InstallConfig): Promise<void> {
    const document = await readGrokDocument();
    const paths = await preflightGrokInstallation(config, document.filePath);
    const rendered = renderGrokDocument(
      document,
      installedHooks(document.hooks, paths.guardPath, paths.auditPath),
    );
    await commitGrokInstallation(await prepareGrokInstallation(config, rendered));
    console.log(
      '  Grok Build: global PreToolUse, PostToolUse, and PostToolUseFailure hooks installed.',
    );
  },

  async uninstall(agentId?: string): Promise<void> {
    const document = await readGrokDocument();
    const rendered = renderGrokDocument(
      document,
      removeManagedGrokHooks(document.hooks, agentId),
    );
    await commitGrokUninstall(await prepareGrokUninstall(rendered));
  },

  async status(): Promise<PluginStatus> {
    const document = await readGrokDocument();
    const contracts = grokRuntimeContracts(document.hooks);
    const hookConfigured = contracts.length > 0;
    const hookScriptExists = hookConfigured && await grokRuntimeFilesExist(contracts);
    return {
      installed: hookConfigured && hookScriptExists,
      agentName: AGENT_KEY,
      displayName: entry.name,
      hookConfigured,
      hookScriptExists,
      configPath: document.filePath,
    };
  },
};
