import type { AgentPlugin, InstallConfig, PluginStatus } from './base.js';
import { SUPPORTED_AGENTS } from './registry.js';
import {
  AGENT_KEY,
  buildHandler,
  buildWrapper,
  removeManagedHooks,
  runtimeContracts,
  wrapperPaths,
} from './augment-contract.js';
import {
  readConfig,
  removeConfig,
  requireRuntime,
  runtimeFilesExist,
  writeConfig,
  writeWrapper,
} from './augment-io.js';

const entry = SUPPORTED_AGENTS.get(AGENT_KEY)!;

export const augmentPlugin: AgentPlugin = {
  async install(config: InstallConfig): Promise<void> {
    if (!config.agentId) throw new Error('agentId is required');
    const document = await readConfig();
    await requireRuntime(config.guardScriptPath, 'Elydora guard runtime');
    await requireRuntime(config.hookScriptPath, 'Elydora audit runtime');

    const wrappers = wrapperPaths(config.agentId);
    await writeWrapper(wrappers.guardPath, buildWrapper(config.guardScriptPath));
    await writeWrapper(wrappers.auditPath, buildWrapper(config.hookScriptPath));

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
    await writeConfig(document.configPath, { ...document.root, hooks });
    console.log('  Auggie: user-level PreToolUse and PostToolUse hooks installed.');
  },

  async uninstall(agentId?: string): Promise<void> {
    const document = await readConfig();
    if (!document.exists) return;
    const result = removeManagedHooks(document.hooks, agentId);
    if (!result.changed) return;
    const root = { ...document.root };
    if (Object.keys(result.hooks).length > 0) root.hooks = result.hooks;
    else delete root.hooks;
    if (Object.keys(root).length === 0) await removeConfig(document.configPath);
    else await writeConfig(document.configPath, root);
  },

  async status(): Promise<PluginStatus> {
    const document = await readConfig();
    const contracts = runtimeContracts(document.hooks);
    const hookConfigured = contracts.length > 0;
    const hookScriptExists = hookConfigured ? await runtimeFilesExist(contracts) : false;
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
