export interface AgentRegistryEntry {
  readonly name: string;
  readonly configDir: string;
  readonly configFile: string;
}

export const SUPPORTED_AGENTS: ReadonlyMap<string, AgentRegistryEntry> = new Map([
  ['augment', { name: 'Augment Code CLI', configDir: '~/.augment', configFile: 'settings.json' }],
  ['claudecode', { name: 'Claude Code', configDir: '~/.claude', configFile: 'settings.json' }],
  ['cursor', { name: 'Cursor', configDir: '~/.cursor', configFile: 'hooks.json' }],
  ['gemini', { name: 'Gemini CLI', configDir: '~/.gemini', configFile: 'settings.json' }],
  ['kirocli', { name: 'Kiro CLI', configDir: '~/.kiro/hooks', configFile: 'elydora-audit.json' }],
  ['kiroide', { name: 'Kiro IDE', configDir: '.kiro/hooks', configFile: 'elydora-audit.kiro.hook' }],
  ['opencode', { name: 'OpenCode', configDir: '.config/opencode/plugins', configFile: 'elydora-audit.mjs' }],
  ['copilot', { name: 'GitHub Copilot CLI', configDir: '~/.copilot/hooks', configFile: 'elydora-audit.json' }],
  ['letta', { name: 'Letta Code', configDir: '~/.letta', configFile: 'settings.json' }],
  ['codex', { name: 'OpenAI Codex', configDir: '~/.codex', configFile: 'hooks.json' }],
  ['cline', { name: 'Cline', configDir: '~/.cline/hooks', configFile: 'PreToolUse.mjs' }],
  ['droid', { name: 'Factory Droid', configDir: '~/.factory', configFile: 'hooks.json' }],
  ['kimi', { name: 'Kimi Code', configDir: '~/.kimi-code', configFile: 'config.toml' }],
  ['grok', { name: 'Grok Build', configDir: '~/.grok/hooks', configFile: 'elydora-audit.json' }],
  ['qwen', { name: 'Qwen Code', configDir: '~/.qwen', configFile: 'settings.json' }],
]);
