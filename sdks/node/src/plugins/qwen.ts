import type { AgentPlugin, InstallConfig, PluginStatus } from './base.js';
import {
  AGENT_KEY,
  AUDIT_HOOK_NAME,
  GUARD_HOOK_NAME,
  buildQwenGroup,
  qwenRuntimeContracts,
  type ManagedQwenEvent,
  type QwenGroup,
} from './qwen-contract.js';
import { renderQwenDocument } from './qwen-config.js';
import {
  commitQwenInstallation,
  commitQwenUninstall,
  preflightQwenInstallation,
  prepareQwenInstallation,
  prepareQwenUninstall,
} from './qwen-installation.js';
import { qwenRuntimeFilesExist } from './qwen-io.js';
import { readQwenSources } from './qwen-sources.js';
import { SUPPORTED_AGENTS } from './registry.js';

const entry = SUPPORTED_AGENTS.get(AGENT_KEY)!;

function installedGroups(guardPath: string, auditPath: string): ReadonlyMap<
  ManagedQwenEvent,
  QwenGroup
> {
  return new Map([
    ['PreToolUse', buildQwenGroup(guardPath, GUARD_HOOK_NAME)],
    ['PostToolUse', buildQwenGroup(auditPath, AUDIT_HOOK_NAME)],
    ['PostToolUseFailure', buildQwenGroup(auditPath, AUDIT_HOOK_NAME)],
  ]);
}

export const qwenPlugin: AgentPlugin = {
  managesRuntime: true,

  async preflightInstall(config: InstallConfig): Promise<void> {
    const sources = await readQwenSources();
    await preflightQwenInstallation(config, sources);
  },

  async install(config: InstallConfig): Promise<void> {
    const sources = await readQwenSources();
    const paths = await preflightQwenInstallation(config, sources);
    const rendered = renderQwenDocument(
      sources.user,
      undefined,
      installedGroups(paths.guardPath, paths.auditPath),
    );
    await commitQwenInstallation(
      await prepareQwenInstallation(config, sources, rendered),
    );
    console.log(`  Qwen Code hooks: ${sources.user.filePath}`);
    console.log('  Qwen Code verification: run /hooks.');
  },

  async uninstall(agentId?: string): Promise<void> {
    const sources = await readQwenSources();
    const rendered = renderQwenDocument(sources.user, agentId, new Map());
    if (!rendered.changed) return;
    await commitQwenUninstall(await prepareQwenUninstall(sources, rendered));
  },

  async status(): Promise<PluginStatus> {
    const sources = await readQwenSources();
    const contracts = qwenRuntimeContracts(sources.user.hooks);
    const hookConfigured = !sources.disableControl.disabled && contracts.length > 0;
    const hookScriptExists = hookConfigured
      ? await qwenRuntimeFilesExist(contracts)
      : false;
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
