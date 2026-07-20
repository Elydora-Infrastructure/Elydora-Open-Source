import type { AgentPlugin, InstallConfig, PluginStatus } from './base.js';
import {
  AGENT_KEY,
  AUDIT_HOOK_NAME,
  GUARD_HOOK_NAME,
  buildGeminiGroup,
  disabledManagedGeminiEntries,
  geminiRuntimeContracts,
  managedGeminiHooksEnabled,
  type GeminiGroup,
  type ManagedGeminiEvent,
} from './gemini-contract.js';
import { renderGeminiDocument, type GeminiDocument } from './gemini-config.js';
import {
  commitGeminiInstallation,
  preflightGeminiInstallation,
  prepareGeminiInstallation,
} from './gemini-installation.js';
import {
  geminiRuntimeFilesExist,
  readGeminiDocument,
  writeGeminiDocument,
} from './gemini-io.js';
import { SUPPORTED_AGENTS } from './registry.js';

const entry = SUPPORTED_AGENTS.get(AGENT_KEY)!;

function requireEnabledHooks(document: GeminiDocument): void {
  if (!document.hookControls.enabled) {
    throw new Error(`Gemini CLI hooks are disabled by hooksConfig.enabled: ${document.filePath}`);
  }
  const disabled = disabledManagedGeminiEntries(document.hookControls);
  if (disabled.length > 0) {
    throw new Error(
      `Gemini CLI hooks are disabled by hooksConfig.disabled: ${disabled.join(', ')}`,
    );
  }
}

function installedGroups(guardPath: string, auditPath: string): ReadonlyMap<
  ManagedGeminiEvent,
  GeminiGroup
> {
  return new Map([
    ['BeforeTool', buildGeminiGroup(guardPath, GUARD_HOOK_NAME)],
    ['AfterTool', buildGeminiGroup(auditPath, AUDIT_HOOK_NAME)],
  ]);
}

export const geminiPlugin: AgentPlugin = {
  managesRuntime: true,

  async preflightInstall(config: InstallConfig): Promise<void> {
    const document = await readGeminiDocument();
    requireEnabledHooks(document);
    await preflightGeminiInstallation(config, document.filePath);
  },

  async install(config: InstallConfig): Promise<void> {
    const document = await readGeminiDocument();
    requireEnabledHooks(document);
    const paths = await preflightGeminiInstallation(config, document.filePath);
    const rendered = renderGeminiDocument(
      document,
      undefined,
      installedGroups(paths.guardPath, paths.auditPath),
    );
    await commitGeminiInstallation(await prepareGeminiInstallation(config, rendered));
    console.log(`  Gemini CLI hooks: ${document.filePath}`);
    console.log('  Gemini CLI verification: run /hooks list.');
  },

  async uninstall(agentId?: string): Promise<void> {
    const document = await readGeminiDocument();
    await writeGeminiDocument(renderGeminiDocument(document, agentId, new Map()));
  },

  async status(): Promise<PluginStatus> {
    const document = await readGeminiDocument();
    const contracts = geminiRuntimeContracts(document.hooks);
    const hookConfigured = managedGeminiHooksEnabled(document.hookControls)
      && contracts.length > 0;
    const hookScriptExists = hookConfigured
      ? await geminiRuntimeFilesExist(contracts)
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
