import type { AgentPlugin, InstallConfig, PluginStatus } from './base.js';
import {
  AGENT_KEY,
  buildGroup,
  runtimeContracts,
  type DroidGroup,
  type ToolEvent,
} from './droid-contract.js';
import {
  activeDocument,
  additionsForTarget,
  effectiveHooks,
  hookBlock,
  installationDocuments,
  renderDocument,
  sourceDocuments,
  type DroidSources,
  type RenderedDocument,
} from './droid-config.js';
import {
  commitDroidInstallation,
  commitDroidUninstall,
  preflightDroidInstallation,
  prepareDroidInstallation,
  prepareDroidUninstall,
} from './droid-installation.js';
import { displayConfigPath, readSources, runtimeFilesExist } from './droid-io.js';
import { SUPPORTED_AGENTS } from './registry.js';

const entry = SUPPORTED_AGENTS.get(AGENT_KEY)!;

function renderInstallation(
  sources: DroidSources,
  guardPath: string,
  auditPath: string,
): RenderedDocument[] {
  const target = activeDocument(sources);
  const groups = new Map<ToolEvent, DroidGroup>([
    ['PreToolUse', buildGroup(guardPath)],
    ['PostToolUse', buildGroup(auditPath)],
  ]);
  return installationDocuments(sources).map((document) => renderDocument(
    document,
    undefined,
    additionsForTarget(document, target, groups),
  ));
}

function renderUninstall(sources: DroidSources, agentId?: string): RenderedDocument[] {
  return sourceDocuments(sources).map(
    (document) => renderDocument(document, agentId, new Map()),
  );
}

export const droidPlugin: AgentPlugin = {
  managesRuntime: true,

  async preflightInstall(config: InstallConfig): Promise<void> {
    const sources = await readSources();
    await preflightDroidInstallation(config, sources);
  },

  async install(config: InstallConfig): Promise<void> {
    const sources = await readSources();
    const paths = await preflightDroidInstallation(config, sources);
    const rendered = renderInstallation(sources, paths.guardPath, paths.auditPath);
    const prepared = await prepareDroidInstallation(config, sources, rendered);
    await commitDroidInstallation(prepared);
    console.log(`  Factory Droid hooks: ${activeDocument(sources).filePath}`);
    console.log('  Factory Droid: run /hooks to review the Elydora hook changes.');
  },

  async uninstall(agentId?: string): Promise<void> {
    const sources = await readSources();
    const prepared = await prepareDroidUninstall(renderUninstall(sources, agentId));
    await commitDroidUninstall(prepared);
  },

  async status(): Promise<PluginStatus> {
    const sources = await readSources();
    const contracts = runtimeContracts(effectiveHooks(sources));
    const hookConfigured = hookBlock(sources) === undefined && contracts.length > 0;
    const hookScriptExists = hookConfigured && await runtimeFilesExist(contracts);
    return {
      installed: hookConfigured && hookScriptExists,
      agentName: AGENT_KEY,
      displayName: entry.name,
      hookConfigured,
      hookScriptExists,
      configPath: displayConfigPath(sources),
    };
  },
};
