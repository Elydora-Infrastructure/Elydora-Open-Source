import type { AgentPlugin, InstallConfig, PluginStatus } from './base.js';
import {
  AGENT_KEY,
  AUDIT_STATUS,
  GUARD_STATUS,
  buildHandler,
  removeManagedHooks,
  renderDocument,
  runtimeContracts,
} from './codex-contract.js';
import {
  commitCodexInstallation,
  preflightCodexInstallation,
  prepareCodexInstallation,
} from './codex-installation.js';
import { readDocument, runtimeFilesExist, writeDocument } from './codex-io.js';
import { SUPPORTED_AGENTS } from './registry.js';

const entry = SUPPORTED_AGENTS.get(AGENT_KEY)!;

function matcherGroup(handler: ReturnType<typeof buildHandler>): Record<string, unknown> {
  return { matcher: '*', hooks: [handler] };
}

export const codexPlugin: AgentPlugin = {
  managesRuntime: true,

  async preflightInstall(config: InstallConfig): Promise<void> {
    const document = await readDocument();
    await preflightCodexInstallation(config, document.filePath);
  },

  async install(config: InstallConfig): Promise<void> {
    const document = await readDocument();
    const paths = await preflightCodexInstallation(config, document.filePath);
    const hooks = removeManagedHooks(document.hooks);
    hooks.PreToolUse = [
      ...(hooks.PreToolUse ?? []),
      matcherGroup(buildHandler(paths.guardPath, GUARD_STATUS)),
    ];
    hooks.PostToolUse = [
      ...(hooks.PostToolUse ?? []),
      matcherGroup(buildHandler(paths.auditPath, AUDIT_STATUS)),
    ];
    const prepared = await prepareCodexInstallation(config, renderDocument(document, hooks));
    await commitCodexInstallation(prepared);
    console.log(`  Codex hooks: ${document.filePath}`);
    console.log('  Codex trust: run /hooks and approve both Elydora command hooks.');
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
