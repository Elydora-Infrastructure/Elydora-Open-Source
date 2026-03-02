package plugins

import (
	"fmt"
	"os"
	"path/filepath"
)

// GeminiPlugin manages the Elydora audit hook for Gemini CLI.
// It merges an AfterTool hook into ~/.gemini/settings.json.
type GeminiPlugin struct{}

func (p *GeminiPlugin) Install(config InstallConfig) error {
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

	configDir, err := expandHome("~/.gemini")
	if err != nil {
		return err
	}
	configPath := filepath.Join(configDir, "settings.json")

	settings, err := readJSONFile(configPath)
	if err != nil {
		return err
	}

	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		hooks = make(map[string]interface{})
	}

	// --- BeforeTool (guard — freeze enforcement) ---
	beforeTool, _ := hooks["BeforeTool"].([]interface{})
	var preFiltered []interface{}
	for _, entry := range beforeTool {
		if m, ok := entry.(map[string]interface{}); ok {
			if isElydoraHookEntry(m) {
				continue
			}
		}
		preFiltered = append(preFiltered, entry)
	}
	guardEntry := map[string]interface{}{
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": "node " + guardPath,
			},
		},
	}
	preFiltered = append(preFiltered, guardEntry)
	hooks["BeforeTool"] = preFiltered

	// --- AfterTool (audit logging) ---
	afterTool, _ := hooks["AfterTool"].([]interface{})
	var postFiltered []interface{}
	for _, entry := range afterTool {
		if m, ok := entry.(map[string]interface{}); ok {
			if isElydoraHookEntry(m) {
				continue
			}
		}
		postFiltered = append(postFiltered, entry)
	}
	hookEntry := map[string]interface{}{
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": "node " + scriptPath,
			},
		},
	}
	postFiltered = append(postFiltered, hookEntry)
	hooks["AfterTool"] = postFiltered

	settings["hooks"] = hooks

	if err := writeJSONFile(configPath, settings); err != nil {
		return err
	}
	fmt.Printf("Installed Elydora hook for Gemini CLI at %s\n", configPath)
	return nil
}

func (p *GeminiPlugin) Uninstall(agentID string) error {
	configDir, err := expandHome("~/.gemini")
	if err != nil {
		return err
	}
	configPath := filepath.Join(configDir, "settings.json")

	settings, err := readJSONFile(configPath)
	if err != nil {
		return err
	}

	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		fmt.Println("No Gemini CLI hooks found.")
		return nil
	}

	// Remove BeforeTool Elydora entries
	beforeTool, _ := hooks["BeforeTool"].([]interface{})
	var preFiltered []interface{}
	for _, entry := range beforeTool {
		if m, ok := entry.(map[string]interface{}); ok {
			if isElydoraHookEntry(m) {
				continue
			}
		}
		preFiltered = append(preFiltered, entry)
	}
	if len(preFiltered) == 0 {
		delete(hooks, "BeforeTool")
	} else {
		hooks["BeforeTool"] = preFiltered
	}

	// Remove AfterTool Elydora entries
	afterTool, _ := hooks["AfterTool"].([]interface{})
	var postFiltered []interface{}
	for _, entry := range afterTool {
		if m, ok := entry.(map[string]interface{}); ok {
			if isElydoraHookEntry(m) {
				continue
			}
		}
		postFiltered = append(postFiltered, entry)
	}
	if len(postFiltered) == 0 {
		delete(hooks, "AfterTool")
	} else {
		hooks["AfterTool"] = postFiltered
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
	fmt.Println("Uninstalled Elydora hook for Gemini CLI.")
	return nil
}

func (p *GeminiPlugin) Status() (PluginStatus, error) {
	configDir, err := expandHome("~/.gemini")
	if err != nil {
		return PluginStatus{}, err
	}
	configPath := filepath.Join(configDir, "settings.json")

	status := PluginStatus{
		AgentName:   "gemini",
		DisplayName: "Gemini CLI",
		ConfigPath:  configPath,
	}

	settings, err := readJSONFile(configPath)
	if err != nil {
		return status, nil
	}

	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks != nil {
		preConfigured := hasElydoraEntry(hooks["BeforeTool"])
		postConfigured := hasElydoraEntry(hooks["AfterTool"])
		status.HookConfigured = preConfigured && postConfigured

		// Extract hook script path from the configured command
		scriptPath := extractElydoraScriptPath(hooks["AfterTool"])
		if scriptPath != "" {
			if _, err := os.Stat(scriptPath); err == nil {
				status.HookScriptExists = true
			}
		}
	}

	status.Installed = status.HookConfigured && status.HookScriptExists
	return status, nil
}
