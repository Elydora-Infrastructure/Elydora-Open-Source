import type { AgentPlugin, InstallConfig, PluginStatus } from './base.js';
import {
  AGENT_KEY,
  TOOL_EVENTS,
  buildGroup,
  mergeHookSettings,
  runtimeContracts,
  type DroidGroup,
  type ToolEvent,
} from './droid-contract.js';
import {
  additionsFor,
  installationTargets,
  renderDocument,
  type DroidDocument,
} from './droid-config.js';
import {
  displayConfigPath,
  readSources,
  requireRuntime,
  runtimeFilesExist,
  writeDocuments,
} from './droid-io.js';
import { SUPPORTED_AGENTS } from './registry.js';

const entry = SUPPORTED_AGENTS.get(AGENT_KEY)!;

function uniqueDocuments(documents: Array<DroidDocument | undefined>): DroidDocument[] {
  const unique = new Map<string, DroidDocument>();
  for (const document of documents) {
    if (document) unique.set(document.filePath, document);
  }
  return [...unique.values()];
}

export const droidPlugin: AgentPlugin = {
  async install(config: InstallConfig): Promise<void> {
    if (!config.agentId) throw new Error('agentId is required');
    const sources = await readSources();
    await requireRuntime(config.guardScriptPath, 'Elydora guard runtime');
    await requireRuntime(config.hookScriptPath, 'Elydora audit runtime');

    const selected = installationTargets(sources);
    const groups = new Map<ToolEvent, DroidGroup>([
      ['PreToolUse', buildGroup(config.guardScriptPath)],
      ['PostToolUse', buildGroup(config.hookScriptPath)],
    ]);
    const documents = uniqueDocuments([
      sources.primary,
      sources.settings.hasHooksContainer ? sources.settings : undefined,
      ...TOOL_EVENTS.map((event) => selected.targets.get(event)),
    ]);
    const rendered = documents.map((document) => renderDocument(
      document,
      undefined,
      additionsFor(document, selected.targets, groups),
    ));
    await writeDocuments(rendered);
    const locations = TOOL_EVENTS
      .map((event) => `${event}: ${selected.targets.get(event)!.filePath}`)
      .join(', ');
    console.log(`  Factory Droid: ${locations}`);
    console.log('  Factory Droid: run /hooks to review the Elydora hook changes.');
  },

  async uninstall(agentId?: string): Promise<void> {
    const sources = await readSources();
    const documents = uniqueDocuments([
      sources.primary,
      sources.settings.hasHooksContainer ? sources.settings : undefined,
    ]);
    const rendered = documents.map((document) => renderDocument(
      document,
      agentId,
      new Map(),
    ));
    await writeDocuments(rendered);
  },

  async status(): Promise<PluginStatus> {
    const sources = await readSources();
    const effective = mergeHookSettings(sources.primary?.hooks, sources.settings.hooks);
    const contracts = runtimeContracts(effective);
    const hookConfigured = effective.hooksDisabled !== true && contracts.length > 0;
    const hookScriptExists = hookConfigured ? await runtimeFilesExist(contracts) : false;
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
