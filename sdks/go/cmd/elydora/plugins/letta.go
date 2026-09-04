package plugins

import (
	"fmt"
	"os"
	"path/filepath"
)

// LettaPlugin merges Elydora hooks into ~/.letta/settings.json.
type LettaPlugin struct{}

func lettaConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".letta", "settings.json"), nil
}

func lettaHookEntry(scriptPath string) map[string]any {
	return map[string]any{
		"matcher": "*",
		"hooks": []any{map[string]any{
			"type":    "command",
			"command": "node " + scriptPath,
		}},
	}
}

func (p *LettaPlugin) Install(config InstallConfig) error {
	scriptPath, err := hookScriptPath(config.AgentID)
	if err != nil {
		return err
	}
	if config.HookScript != "" {
		scriptPath = config.HookScript
	}
	if err := GenerateHookScript(scriptPath, config); err != nil {
		return fmt.Errorf("generate hook script: %w", err)
	}
	guardPath := config.GuardScriptPath
	if guardPath == "" {
		guardPath, err = guardScriptPath(config.AgentID)
		if err != nil {
			return err
		}
	}
	configPath, err := lettaConfigPath()
	if err != nil {
		return err
	}
	settings, err := readJSONFile(configPath)
	if err != nil {
		return err
	}
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	hooks["PreToolUse"] = append(withoutElydoraEntries(hooks["PreToolUse"]), lettaHookEntry(guardPath))
	hooks["PostToolUse"] = append(withoutElydoraEntries(hooks["PostToolUse"]), lettaHookEntry(scriptPath))
	settings["hooks"] = hooks
	if err := writeJSONFile(configPath, settings); err != nil {
		return err
	}
	fmt.Printf("Installed Elydora hook for Letta Code at %s\n", configPath)
	return nil
}

func (p *LettaPlugin) Uninstall(agentID string) error {
	configPath, err := lettaConfigPath()
	if err != nil {
		return err
	}
	settings, err := readJSONFile(configPath)
	if err != nil {
		return err
	}
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		fmt.Println("No Letta Code hooks found.")
		return nil
	}
	for _, event := range []string{"PreToolUse", "PostToolUse"} {
		kept := withoutElydoraEntries(hooks[event])
		if len(kept) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = kept
		}
	}
	if len(hooks) == 0 {
		delete(settings, "hooks")
	} else {
		settings["hooks"] = hooks
	}
	if err := writeJSONFile(configPath, settings); err != nil {
		return err
	}
	if agentID != "" {
		if err := removeAgentScripts(agentID); err != nil {
			return err
		}
	}
	fmt.Println("Uninstalled Elydora hook for Letta Code.")
	return nil
}

func (p *LettaPlugin) Status() (PluginStatus, error) {
	configPath, err := lettaConfigPath()
	if err != nil {
		return PluginStatus{}, err
	}
	status := PluginStatus{
		AgentName:   "letta",
		DisplayName: "Letta Code",
		ConfigPath:  configPath,
	}
	settings, err := readJSONFile(configPath)
	if err != nil {
		return status, err
	}
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		return status, nil
	}
	status.HookConfigured = hasElydoraEntry(hooks["PreToolUse"]) &&
		hasElydoraEntry(hooks["PostToolUse"])
	if scriptPath := extractElydoraScriptPath(hooks["PostToolUse"]); scriptPath != "" {
		if _, err := os.Stat(scriptPath); err == nil {
			status.HookScriptExists = true
		}
	}
	status.Installed = status.HookConfigured && status.HookScriptExists
	return status, nil
}
