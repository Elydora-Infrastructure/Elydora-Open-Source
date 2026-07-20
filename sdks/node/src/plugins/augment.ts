import type { AgentPlugin, InstallConfig, PluginStatus } from './base.js';
import { SUPPORTED_AGENTS } from './registry.js';
import {
  AGENT_KEY,
  buildHandler,
  removeManagedHooks,
  renderAugmentDocument,
  runtimeContracts,
  wrapperPaths,
} from './augment-contract.js';
import {
  commitAugmentInstallation,
  preflightAugmentInstallation,
  prepareAugmentInstallation,
} from './augment-installation.js';
import {
  augmentRuntimeFilesExist,
  readConfig,
  writeAugmentDocument,
} from './augment-io.js';

const entry = SUPPORTED_AGENTS.get(AGENT_KEY)!;

export const augmentPlugin: AgentPlugin = {
  managesRuntime: true,

  async preflightInstall(config: InstallConfig): Promise<void> {
    const document = await readConfig();
    await preflightAugmentInstallation(config, document.configPath);
  },

  async install(config: InstallConfig): Promise<void> {
    const document = await readConfig();
    const paths = await preflightAugmentInstallation(config, document.configPath);
    const wrappers = wrapperPaths(paths.agentDirectory);

    const cleaned = removeManagedHooks(document.hooks).hooks;
    const hooks = {
      ...cleaned,
      PreToolUse: [
        ...(cleaned.PreToolUse ?? []),
        { matcher: '.*', hooks: [buildHandler(wrappers.guardPath)] },
      ],
      PostToolUse: [
        ...(cleaned.PostToolUse ?? []),
        { matcher: '.*', hooks: [buildHandler(wrappers.auditPath)] },
      ],
    };
    const rendered = renderAugmentDocument(document, hooks);
    const prepared = await prepareAugmentInstallation(config, rendered);
    await commitAugmentInstallation(prepared);
    console.log('  Auggie: user-level PreToolUse and PostToolUse hooks installed.');
  },

  async uninstall(agentId?: string): Promise<void> {
    const document = await readConfig();
    if (!document.exists) return;
    const result = removeManagedHooks(document.hooks, agentId);
    if (!result.changed) return;
    await writeAugmentDocument(renderAugmentDocument(document, result.hooks));
  },

  async status(): Promise<PluginStatus> {
    const document = await readConfig();
    const contracts = runtimeContracts(document.hooks);
    const hookConfigured = contracts.length > 0;
    const hookScriptExists = hookConfigured
      ? await augmentRuntimeFilesExist(contracts)
      : false;
    return {
      installed: hookConfigured && hookScriptExists,
      agentName: AGENT_KEY,
      displayName: entry.name,
      hookConfigured,
      hookScriptExists,
      configPath: document.configPath,
    };
  },
};
