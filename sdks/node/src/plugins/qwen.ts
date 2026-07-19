import type { AgentPlugin, InstallConfig, PluginStatus } from "./base.js";
import {
  AGENT_KEY,
  buildGroup,
  runtimeContracts,
  type QwenGroup,
  type ToolEvent,
} from "./qwen-contract.js";
import { renderDocument } from "./qwen-config.js";
import {
  readDocument,
  requireRuntime,
  runtimeFilesExist,
  writeDocument,
} from "./qwen-io.js";
import { SUPPORTED_AGENTS } from "./registry.js";

const entry = SUPPORTED_AGENTS.get(AGENT_KEY)!;

export const qwenPlugin: AgentPlugin = {
  async install(config: InstallConfig): Promise<void> {
    if (!config.agentId) throw new Error("agentId is required");
    const document = await readDocument();
    await requireRuntime(config.guardScriptPath, "Elydora guard runtime");
    await requireRuntime(config.hookScriptPath, "Elydora audit runtime");
    const additions = new Map<ToolEvent, QwenGroup>([
      ["PreToolUse", buildGroup(config.guardScriptPath)],
      ["PostToolUse", buildGroup(config.hookScriptPath)],
    ]);
    await writeDocument(renderDocument(document, undefined, additions));
    console.log(`  Qwen Code: user hooks installed at ${document.filePath}`);
    console.log("  Qwen Code: run /hooks to review the Elydora hook changes.");
  },

  async uninstall(agentId?: string): Promise<void> {
    const document = await readDocument();
    if (!document.exists) return;
    await writeDocument(renderDocument(document, agentId, new Map()));
  },

  async status(): Promise<PluginStatus> {
    const document = await readDocument();
    const contracts = runtimeContracts(document.hooks);
    const hookConfigured = !document.hooksDisabled && contracts.length > 0;
    const hookScriptExists = hookConfigured
      ? await runtimeFilesExist(contracts)
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
