import type { AgentPlugin, InstallConfig, PluginStatus } from './base.js';
import { AGENT_KEY, resolveHookFiles, runtimeContract } from './cline-contract.js';
import {
  commitClineInstallation,
  commitClineUninstall,
  preflightClineInstallation,
  prepareClineInstallation,
  prepareClineUninstall,
} from './cline-installation.js';
import { readHookFile, requireAvailableHookFile, runtimeFilesExist } from './cline-io.js';
import { SUPPORTED_AGENTS } from './registry.js';

const entry = SUPPORTED_AGENTS.get(AGENT_KEY)!;

async function readHookPair() {
  const paths = resolveHookFiles();
  const [guardFile, auditFile] = await Promise.all([
    readHookFile(paths.guardPath),
    readHookFile(paths.auditPath),
  ]);
  return { paths, guardFile, auditFile };
}

function requireAvailablePair(
  guardFile: Awaited<ReturnType<typeof readHookFile>>,
  auditFile: Awaited<ReturnType<typeof readHookFile>>,
): void {
  requireAvailableHookFile(guardFile);
  requireAvailableHookFile(auditFile);
}

export const clinePlugin: AgentPlugin = {
  managesRuntime: true,

  async preflightInstall(config: InstallConfig): Promise<void> {
    const { guardFile, auditFile } = await readHookPair();
    requireAvailablePair(guardFile, auditFile);
    await preflightClineInstallation(config, [guardFile, auditFile]);
  },

  async install(config: InstallConfig): Promise<void> {
    const { guardFile, auditFile } = await readHookPair();
    requireAvailablePair(guardFile, auditFile);
    const prepared = await prepareClineInstallation(config, guardFile, auditFile);
    await commitClineInstallation(prepared);
    console.log('  Cline: user-level PreToolUse and PostToolUse hooks installed.');
  },

  async uninstall(agentId?: string): Promise<void> {
    const { guardFile, auditFile } = await readHookPair();
    const prepared = await prepareClineUninstall([guardFile, auditFile], agentId);
    await commitClineUninstall(prepared);
  },

  async status(): Promise<PluginStatus> {
    const { paths, guardFile, auditFile } = await readHookPair();
    const contract = runtimeContract(guardFile, auditFile);
    const hookConfigured = contract !== undefined;
    const hookScriptExists = contract ? await runtimeFilesExist(contract) : false;
    return {
      installed: hookConfigured && hookScriptExists,
      agentName: AGENT_KEY,
      displayName: entry.name,
      hookConfigured,
      hookScriptExists,
      configPath: paths.hooksDirectory,
    };
  },
};
