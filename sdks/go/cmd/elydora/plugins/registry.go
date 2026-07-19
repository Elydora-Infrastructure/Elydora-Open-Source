package plugins

// AgentRegistryEntry describes a supported agent integration.
type AgentRegistryEntry struct {
	Name       string
	ConfigDir  string // ~ means home dir; no ~ means project-relative
	ConfigFile string
}

// SupportedAgents is the canonical registry of all supported agent integrations.
var SupportedAgents = map[string]AgentRegistryEntry{
	"augment":    {Name: "Augment Code CLI", ConfigDir: "~/.augment", ConfigFile: "settings.json"},
	"claudecode": {Name: "Claude Code", ConfigDir: "~/.claude", ConfigFile: "settings.json"},
	"codex":      {Name: "OpenAI Codex", ConfigDir: "~/.codex", ConfigFile: "hooks.json"},
	"cursor":     {Name: "Cursor", ConfigDir: "~/.cursor", ConfigFile: "hooks.json"},
	"gemini":     {Name: "Gemini CLI", ConfigDir: "~/.gemini", ConfigFile: "settings.json"},
	"grok":       {Name: "Grok Build", ConfigDir: "~/.grok/hooks", ConfigFile: "elydora-audit.json"},
	"kirocli":    {Name: "Kiro CLI", ConfigDir: "~/.kiro/hooks", ConfigFile: "elydora-audit.json"},
	"kiroide":    {Name: "Kiro IDE", ConfigDir: "~/.kiro/hooks", ConfigFile: "elydora-audit.kiro.hook"},
	"kimi":       {Name: "Kimi Code", ConfigDir: "~/.kimi-code", ConfigFile: "config.toml"},
	"opencode":   {Name: "OpenCode", ConfigDir: "~/.config/opencode/plugins", ConfigFile: "elydora-audit.mjs"},
	"copilot":    {Name: "Copilot CLI", ConfigDir: ".github/hooks", ConfigFile: "hooks.json"},
	"letta":      {Name: "Letta Code", ConfigDir: "~/.letta", ConfigFile: "settings.json"},
}

// NewPlugin creates a plugin instance for the given agent name.
// Returns nil if the agent is not supported.
func NewPlugin(agentName string) AgentPlugin {
	switch agentName {
	case "augment":
		return &AugmentPlugin{}
	case "claudecode":
		return &ClaudeCodePlugin{}
	case "codex":
		return &CodexPlugin{}
	case "cursor":
		return &CursorPlugin{}
	case "gemini":
		return &GeminiPlugin{}
	case "grok":
		return &GrokPlugin{}
	case "kirocli":
		return &KiroCliPlugin{}
	case "kiroide":
		return &KiroIdePlugin{}
	case "kimi":
		return &KimiPlugin{}
	case "opencode":
		return &OpenCodePlugin{}
	case "copilot":
		return &CopilotPlugin{}
	case "letta":
		return &LettaPlugin{}
	default:
		return nil
	}
}
