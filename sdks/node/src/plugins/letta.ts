import type { AgentPlugin, InstallConfig, PluginStatus } from './base.js';
import {
  AGENT_KEY,
  LETTA_AUDIT_OPTIONS,
  buildLettaGroup,
  lettaRuntimeContracts,
  type LettaGroup,
  type ManagedLettaEvent,
} from './letta-contract.js';
import { renderLettaDocument } from './letta-config.js';
import {
  commitLettaInstallation,
  commitLettaUninstall,
  preflightLettaInstallation,
  prepareLettaInstallation,
  prepareLettaUninstall,
} from './letta-installation.js';
import { managedRuntimeFilesExist } from './managed-runtime-status.js';
import { readLettaSources } from './letta-sources.js';
import { SUPPORTED_AGENTS } from './registry.js';

const entry = SUPPORTED_AGENTS.get(AGENT_KEY)!;

function installedGroups(guardPath: string, auditPath: string): ReadonlyMap<
  ManagedLettaEvent,
  LettaGroup
> {
  return new Map([
    ['PreToolUse', buildLettaGroup(guardPath)],
    ['PostToolUse', buildLettaGroup(auditPath)],
    ['PostToolUseFailure', buildLettaGroup(auditPath)],
  ]);
}

async function runtimeExists(
  contracts: ReturnType<typeof lettaRuntimeContracts>,
): Promise<boolean> {
  for (const contract of contracts) {
    if (await managedRuntimeFilesExist(contract, AGENT_KEY, {
      auditOptions: LETTA_AUDIT_OPTIONS,
    })) return true;
  }
  return false;
}

export const lettaPlugin: AgentPlugin = {
  managesRuntime: true,

  async preflightInstall(config: InstallConfig): Promise<void> {
    const sources = await readLettaSources();
    await preflightLettaInstallation(config, sources);
  },

  async install(config: InstallConfig): Promise<void> {
    const sources = await readLettaSources();
    const paths = await preflightLettaInstallation(config, sources);
    const rendered = renderLettaDocument(
      sources.global,
      undefined,
      installedGroups(paths.guardPath, paths.auditPath),
    );
    await commitLettaInstallation(
      await prepareLettaInstallation(config, sources, rendered),
    );
    console.log(`  Letta Code hooks: ${sources.global.filePath}`);
    console.log('  Letta Code verification: run /hooks and restart active sessions.');
  },

  async uninstall(agentId?: string): Promise<void> {
    const sources = await readLettaSources();
    const rendered = renderLettaDocument(sources.global, agentId, new Map());
    if (!rendered.changed) return;
    await commitLettaUninstall(await prepareLettaUninstall(sources, rendered));
  },

  async status(): Promise<PluginStatus> {
    const sources = await readLettaSources();
    const contracts = lettaRuntimeContracts(sources.global.hooks);
    const hookConfigured = !sources.disableControl.disabled && contracts.length > 0;
    const hookScriptExists = hookConfigured ? await runtimeExists(contracts) : false;
    return {
      installed: hookConfigured && hookScriptExists,
      agentName: AGENT_KEY,
      displayName: entry.name,
      hookConfigured,
      hookScriptExists,
      configPath: sources.global.filePath,
    };
  },
};
