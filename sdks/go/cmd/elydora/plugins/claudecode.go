package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ClaudeCodePlugin manages the Elydora audit hook for Claude Code.
// It merges a PostToolUse hook into ~/.claude/settings.json.
type ClaudeCodePlugin struct{}

func (p *ClaudeCodePlugin) Install(config InstallConfig) error {
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

	configDir, err := expandHome("~/.claude")
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
	fmt.Printf("Installed Elydora hook for Claude Code at %s\n", configPath)
	return nil
}

func (p *ClaudeCodePlugin) Uninstall(agentID string) error {
	configDir, err := expandHome("~/.claude")
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
		fmt.Println("No Claude Code hooks found.")
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
	fmt.Println("Uninstalled Elydora hook for Claude Code.")
	return nil
}

func (p *ClaudeCodePlugin) Status() (PluginStatus, error) {
	configDir, err := expandHome("~/.claude")
	if err != nil {
		return PluginStatus{}, err
	}
	configPath := filepath.Join(configDir, "settings.json")

	status := PluginStatus{
		AgentName:   "claudecode",
		DisplayName: "Claude Code",
		ConfigPath:  configPath,
	}

	// Check if hook is configured in settings and extract hook script path
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

// extractElydoraScriptPath extracts the script path from a hook array's Elydora command entry.
func extractElydoraScriptPath(hookArray interface{}) string {
	arr, _ := hookArray.([]interface{})
	for _, entry := range arr {
		if m, ok := entry.(map[string]interface{}); ok {
			// New format: { "hooks": [{ "type": "command", "command": "node /path/to/hook.js" }] }
			if innerHooks, ok := m["hooks"].([]interface{}); ok {
				for _, h := range innerHooks {
					if hm, ok := h.(map[string]interface{}); ok {
						if cmd, _ := hm["command"].(string); strings.Contains(cmd, "elydora") {
							return extractPathFromNodeCommand(cmd)
						}
					}
				}
			}
			// Old format: { "command": "node /path/to/hook.js" }
			if cmd, _ := m["command"].(string); strings.Contains(cmd, "elydora") {
				return extractPathFromNodeCommand(cmd)
			}
		}
	}
	return ""
}

// extractPathFromNodeCommand extracts the file path from a "node /path/to/script.js" command.
func extractPathFromNodeCommand(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if strings.HasPrefix(cmd, "node ") {
		return strings.TrimSpace(cmd[5:])
	}
	return ""
}

// hasElydoraEntry checks if a hook array (interface{}) contains an Elydora entry.
func hasElydoraEntry(hookArray interface{}) bool {
	arr, _ := hookArray.([]interface{})
	for _, entry := range arr {
		if m, ok := entry.(map[string]interface{}); ok {
			if isElydoraHookEntry(m) {
				return true
			}
		}
	}
	return false
}

// isElydoraHookEntry checks if a hook entry (old or new format) is an Elydora hook.
func isElydoraHookEntry(m map[string]interface{}) bool {
	// New format: { "matcher": {}, "hooks": [{ "type": "command", "command": "..." }] }
	if innerHooks, ok := m["hooks"].([]interface{}); ok {
		for _, h := range innerHooks {
			if hm, ok := h.(map[string]interface{}); ok {
				if cmd, _ := hm["command"].(string); strings.Contains(cmd, "elydora") {
					return true
				}
			}
		}
	}
	// Old format: { "command": "..." }
	if cmd, _ := m["command"].(string); strings.Contains(cmd, "elydora") {
		return true
	}
	return false
}
