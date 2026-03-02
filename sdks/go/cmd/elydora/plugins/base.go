package plugins

// InstallConfig holds the configuration needed to install an agent plugin.
type InstallConfig struct {
	AgentName       string
	OrgID           string
	AgentID         string
	PrivateKey      string
	KID             string
	Token           string
	BaseURL         string
	HookScript      string // absolute path to the generated hook script
	GuardScriptPath string // absolute path to the generated guard script (PreToolUse)
}

// PluginStatus describes the current state of a plugin installation.
type PluginStatus struct {
	Installed       bool
	AgentName       string
	DisplayName     string
	HookConfigured  bool
	HookScriptExists bool
	ConfigPath      string
}

// AgentPlugin is the interface that every agent integration must implement.
type AgentPlugin interface {
	Install(config InstallConfig) error
	Uninstall(agentID string) error
	Status() (PluginStatus, error)
}
