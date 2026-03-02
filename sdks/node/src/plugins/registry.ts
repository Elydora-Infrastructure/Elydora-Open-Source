export interface AgentRegistryEntry {
  readonly name: string;
  readonly configDir: string;
  readonly configFile: string;
}

export const SUPPORTED_AGENTS: ReadonlyMap<string, AgentRegistryEntry> = new Map([
  ['claudecode', { name: 'Claude Code', configDir: '~/.claude', configFile: 'settings.json' }],
  ['cursor', { name: 'Cursor', configDir: '.cursor', configFile: 'hooks.json' }],
  ['gemini', { name: 'Gemini CLI', configDir: '~/.gemini', configFile: 'settings.json' }],
  ['kirocli', { name: 'Kiro CLI', configDir: '~/.kiro', configFile: 'settings.json' }],
  ['kiroide', { name: 'Kiro IDE', configDir: '.kiro/hooks', configFile: 'elydora-audit.kiro.hook' }],
  ['opencode', { name: 'OpenCode', configDir: '.config/opencode/plugins', configFile: 'elydora-audit.mjs' }],
  ['copilot', { name: 'Copilot CLI', configDir: '.github/hooks', configFile: 'hooks.json' }],
  ['letta', { name: 'Letta Code', configDir: '~/.letta', configFile: 'settings.json' }],
]);
