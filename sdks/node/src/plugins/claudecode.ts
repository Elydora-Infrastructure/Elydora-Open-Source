import type { AgentPlugin, InstallConfig, PluginStatus } from './base.js';
import {
  AGENT_KEY,
  AUDIT_STATUS,
  GUARD_STATUS,
  buildClaudeGroup,
  claudeRuntimeContracts,
  removeManagedClaudeHooks,
  renderClaudeDocument,
} from './claudecode-contract.js';
import {
  commitClaudeInstallation,
  preflightClaudeInstallation,
  prepareClaudeInstallation,
} from './claudecode-installation.js';
import {
  claudeRuntimeFilesExist,
  readClaudeDocument,
  writeClaudeDocument,
} from './claudecode-io.js';
import { SUPPORTED_AGENTS } from './registry.js';

const entry = SUPPORTED_AGENTS.get(AGENT_KEY)!;

function requireEnabledHooks(disabled: boolean, filePath: string): void {
  if (disabled) {
    throw new Error(`Claude Code hooks are disabled by disableAllHooks: ${filePath}`);
  }
}

export const claudecodePlugin: AgentPlugin = {
  managesRuntime: true,

  async preflightInstall(config: InstallConfig): Promise<void> {
    const document = await readClaudeDocument();
    requireEnabledHooks(document.hooksDisabled, document.filePath);
    await preflightClaudeInstallation(config, document.filePath);
  },

  async install(config: InstallConfig): Promise<void> {
    const document = await readClaudeDocument();
    requireEnabledHooks(document.hooksDisabled, document.filePath);
    const paths = await preflightClaudeInstallation(config, document.filePath);
    const hooks = removeManagedClaudeHooks(document.hooks);
    hooks.PreToolUse = [
      ...(hooks.PreToolUse ?? []),
      buildClaudeGroup(paths.guardPath, GUARD_STATUS),
    ];
    hooks.PostToolUse = [
      ...(hooks.PostToolUse ?? []),
      buildClaudeGroup(paths.auditPath, AUDIT_STATUS),
    ];
    hooks.PostToolUseFailure = [
      ...(hooks.PostToolUseFailure ?? []),
      buildClaudeGroup(paths.auditPath, AUDIT_STATUS),
    ];
    const rendered = renderClaudeDocument(document, hooks);
    const prepared = await prepareClaudeInstallation(config, rendered);
    await commitClaudeInstallation(prepared);
    console.log(`  Claude Code hooks: ${document.filePath}`);
    console.log('  Claude Code verification: run /hooks and claude doctor.');
  },

  async uninstall(agentId?: string): Promise<void> {
    const document = await readClaudeDocument();
    const hooks = removeManagedClaudeHooks(document.hooks, agentId);
    await writeClaudeDocument(renderClaudeDocument(document, hooks));
  },

  async status(): Promise<PluginStatus> {
    const document = await readClaudeDocument();
    const contracts = claudeRuntimeContracts(document.hooks);
    const hookConfigured = !document.hooksDisabled && contracts.length > 0;
    const hookScriptExists = hookConfigured
      ? await claudeRuntimeFilesExist(contracts)
      : false;
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
