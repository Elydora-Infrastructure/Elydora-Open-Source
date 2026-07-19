import type { AgentPlugin, InstallConfig, PluginStatus } from './base.js';
import {
  AGENT_KEY,
  buildMetadata,
  buildWrapper,
  resolveHookFiles,
  runtimeContract,
} from './cline-contract.js';
import {
  readHookFile,
  removeOwnedHooks,
  requireAvailableHookFile,
  requireRuntime,
  runtimeFilesExist,
  writeHookPair,
} from './cline-io.js';
import { SUPPORTED_AGENTS } from './registry.js';

const entry = SUPPORTED_AGENTS.get(AGENT_KEY)!;

export const clinePlugin: AgentPlugin = {
  async install(config: InstallConfig): Promise<void> {
    if (!config.agentId) throw new Error('agentId is required');
    const paths = resolveHookFiles();
    const guardState = await readHookFile(paths.guardPath);
    const auditState = await readHookFile(paths.auditPath);
    requireAvailableHookFile(guardState);
    requireAvailableHookFile(auditState);
    await requireRuntime(config.guardScriptPath, 'Elydora guard runtime');
    await requireRuntime(config.hookScriptPath, 'Elydora audit runtime');

    const guardMetadata = buildMetadata('guard', config.agentId, config.guardScriptPath);
    const auditMetadata = buildMetadata('audit', config.agentId, config.hookScriptPath);
    const guardSource = buildWrapper(guardMetadata);
    const auditSource = buildWrapper(auditMetadata);
    runtimeContract(
      { exists: true, filePath: paths.guardPath, source: guardSource, metadata: guardMetadata },
      { exists: true, filePath: paths.auditPath, source: auditSource, metadata: auditMetadata },
    );
    await writeHookPair(
      { state: guardState, source: guardSource },
      { state: auditState, source: auditSource },
    );
    console.log('  Cline: user-level PreToolUse and PostToolUse hooks installed.');
  },

  async uninstall(agentId?: string): Promise<void> {
    const paths = resolveHookFiles();
    const guardState = await readHookFile(paths.guardPath);
    const auditState = await readHookFile(paths.auditPath);
    await removeOwnedHooks([guardState, auditState], agentId);
  },

  async status(): Promise<PluginStatus> {
    const paths = resolveHookFiles();
    const guardState = await readHookFile(paths.guardPath);
    const auditState = await readHookFile(paths.auditPath);
    const contract = runtimeContract(guardState, auditState);
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
