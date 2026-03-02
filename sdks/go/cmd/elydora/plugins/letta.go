package plugins

import (
	"fmt"
	"os"
	"path/filepath"
)

// LettaPlugin manages the Elydora audit hook for Letta Code.
// It merges PreToolUse/PostToolUse hooks into ~/.letta/settings.json.
type LettaPlugin struct{}

func (p *LettaPlugin) configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".letta", "settings.json"), nil
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

	configPath, err := p.configPath()
	if err != nil {
		return err
	}

	settings, err := readJSONFile(configPath)
	if err != nil {
		return err
	}

	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		hooks = make(map[string]interface{})
	}

	// --- PreToolUse (guard — freeze enforcement) ---
	preToolUse, _ := hooks["PreToolUse"].([]interface{})
	var preFiltered []interface{}
	for _, entry := range preToolUse {
		if m, ok := entry.(map[string]interface{}); ok {
			if isElydoraHookEntry(m) {
				continue
			}
		}
		preFiltered = append(preFiltered, entry)
	}
	guardEntry := map[string]interface{}{
		"matcher": "*",
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": "node " + guardPath,
			},
		},
	}
	preFiltered = append(preFiltered, guardEntry)
	hooks["PreToolUse"] = preFiltered

	// --- PostToolUse (audit logging) ---
	postToolUse, _ := hooks["PostToolUse"].([]interface{})
	var postFiltered []interface{}
	for _, entry := range postToolUse {
		if m, ok := entry.(map[string]interface{}); ok {
			if isElydoraHookEntry(m) {
				continue
			}
		}
		postFiltered = append(postFiltered, entry)
	}
	hookEntry := map[string]interface{}{
		"matcher": "*",
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": "node " + scriptPath,
			},
		},
	}
	postFiltered = append(postFiltered, hookEntry)
	hooks["PostToolUse"] = postFiltered

	settings["hooks"] = hooks

	if err := writeJSONFile(configPath, settings); err != nil {
		return err
	}
	fmt.Printf("Installed Elydora hook for Letta Code at %s\n", configPath)
	return nil
}

func (p *LettaPlugin) Uninstall(agentID string) error {
	configPath, err := p.configPath()
	if err != nil {
		return err
	}

	settings, err := readJSONFile(configPath)
	if err != nil {
		return err
	}

	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		fmt.Println("No Letta Code hooks found.")
		return nil
	}

	// Remove PreToolUse Elydora entries
	preToolUse, _ := hooks["PreToolUse"].([]interface{})
	var preFiltered []interface{}
	for _, entry := range preToolUse {
		if m, ok := entry.(map[string]interface{}); ok {
			if isElydoraHookEntry(m) {
				continue
			}
		}
		preFiltered = append(preFiltered, entry)
	}
	if len(preFiltered) == 0 {
		delete(hooks, "PreToolUse")
	} else {
		hooks["PreToolUse"] = preFiltered
	}

	// Remove PostToolUse Elydora entries
	postToolUse, _ := hooks["PostToolUse"].([]interface{})
	var postFiltered []interface{}
	for _, entry := range postToolUse {
		if m, ok := entry.(map[string]interface{}); ok {
			if isElydoraHookEntry(m) {
				continue
			}
		}
		postFiltered = append(postFiltered, entry)
	}
	if len(postFiltered) == 0 {
		delete(hooks, "PostToolUse")
	} else {
		hooks["PostToolUse"] = postFiltered
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
		scriptPath, _ := hookScriptPath(agentID)
		if scriptPath != "" {
			os.Remove(scriptPath)
		}
		gPath, _ := guardScriptPath(agentID)
		if gPath != "" {
			os.Remove(gPath)
		}
	}
	fmt.Println("Uninstalled Elydora hook for Letta Code.")
	return nil
}

func (p *LettaPlugin) Status() (PluginStatus, error) {
	configPath, err := p.configPath()
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
		return status, nil
	}

	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks != nil {
		preConfigured := hasElydoraEntry(hooks["PreToolUse"])
		postConfigured := hasElydoraEntry(hooks["PostToolUse"])
		status.HookConfigured = preConfigured && postConfigured

		// Extract hook script path from the configured command
		scriptPath := extractElydoraScriptPath(hooks["PostToolUse"])
		if scriptPath != "" {
			if _, err := os.Stat(scriptPath); err == nil {
				status.HookScriptExists = true
			}
		}
	}

	status.Installed = status.HookConfigured && status.HookScriptExists
	return status, nil
}

