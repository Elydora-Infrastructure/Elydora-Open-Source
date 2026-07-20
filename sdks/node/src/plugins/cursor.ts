import type { AgentPlugin, InstallConfig, PluginStatus } from './base.js';
import {
  AGENT_KEY,
  buildHandler,
  removeManagedHooks,
  renderDocument,
  runtimeContracts,
} from './cursor-contract.js';
import {
  commitCursorInstallation,
  preflightCursorInstallation,
  prepareCursorInstallation,
} from './cursor-installation.js';
import {
  readDocument,
  runtimeFilesExist,
  writeDocument,
} from './cursor-io.js';
import { SUPPORTED_AGENTS } from './registry.js';

const entry = SUPPORTED_AGENTS.get(AGENT_KEY)!;

export const cursorPlugin: AgentPlugin = {
  managesRuntime: true,

  async preflightInstall(config: InstallConfig): Promise<void> {
    await readDocument();
    await preflightCursorInstallation(config);
  },

  async install(config: InstallConfig): Promise<void> {
    const document = await readDocument();
    const paths = await preflightCursorInstallation(config);

    const hooks = removeManagedHooks(document.hooks);
    hooks.preToolUse = [...(hooks.preToolUse ?? []), buildHandler(paths.guardPath)];
    hooks.postToolUse = [...(hooks.postToolUse ?? []), buildHandler(paths.auditPath)];
    hooks.postToolUseFailure = [
      ...(hooks.postToolUseFailure ?? []),
      buildHandler(paths.auditPath),
    ];
    const prepared = await prepareCursorInstallation(
      config,
      renderDocument(document, hooks),
    );
    await commitCursorInstallation(prepared);
    console.log(`  Cursor hooks: ${document.filePath}`);
  },

  async uninstall(agentId?: string): Promise<void> {
    const document = await readDocument();
    const hooks = removeManagedHooks(document.hooks, agentId);
    await writeDocument(renderDocument(document, hooks));
  },

  async status(): Promise<PluginStatus> {
    const document = await readDocument();
    const contracts = runtimeContracts(document.hooks);
    const hookConfigured = contracts.length > 0;
    const hookScriptExists = hookConfigured && await runtimeFilesExist(contracts);
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
