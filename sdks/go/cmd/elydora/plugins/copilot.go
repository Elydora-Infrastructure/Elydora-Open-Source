package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CopilotPlugin manages the Elydora audit hook for GitHub Copilot CLI.
// It writes/merges into .github/hooks/hooks.json (project-relative) using
// hooks.preToolUse[]/postToolUse[] with bash/powershell command fields.
type CopilotPlugin struct{}

func (p *CopilotPlugin) configPath() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	return filepath.Join(cwd, ".github", "hooks", "hooks.json"), nil
}

func (p *CopilotPlugin) Install(config InstallConfig) error {
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

	// Ensure version field is set
	settings["version"] = float64(1)

	// Ensure hooks object exists
	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		hooks = make(map[string]interface{})
	}

	// --- preToolUse (guard — freeze enforcement) ---
	preToolUse, _ := hooks["preToolUse"].([]interface{})
	var preFiltered []interface{}
	for _, entry := range preToolUse {
		if m, ok := entry.(map[string]interface{}); ok {
			if isCopilotElydoraEntry(m) {
				continue
			}
		}
		preFiltered = append(preFiltered, entry)
	}
	guardEntry := map[string]interface{}{
		"type":       "command",
		"bash":       "node " + guardPath,
		"powershell": "node " + guardPath,
		"timeoutSec": float64(5),
	}
	preFiltered = append(preFiltered, guardEntry)
	hooks["preToolUse"] = preFiltered

	// --- postToolUse (audit logging) ---
	postToolUse, _ := hooks["postToolUse"].([]interface{})
	var postFiltered []interface{}
	for _, entry := range postToolUse {
		if m, ok := entry.(map[string]interface{}); ok {
			if isCopilotElydoraEntry(m) {
				continue
			}
		}
		postFiltered = append(postFiltered, entry)
	}
	hookEntry := map[string]interface{}{
		"type":       "command",
		"bash":       "node " + scriptPath,
		"powershell": "node " + scriptPath,
		"timeoutSec": float64(5),
	}
	postFiltered = append(postFiltered, hookEntry)
	hooks["postToolUse"] = postFiltered

	settings["hooks"] = hooks

	if err := writeJSONFile(configPath, settings); err != nil {
		return err
	}
	fmt.Printf("Installed Elydora hook for Copilot CLI at %s\n", configPath)
	return nil
}

func (p *CopilotPlugin) Uninstall(agentID string) error {
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
		fmt.Println("No Copilot CLI hooks found.")
		return nil
	}

	// Remove preToolUse Elydora entries
	preToolUse, _ := hooks["preToolUse"].([]interface{})
	var preFiltered []interface{}
	for _, entry := range preToolUse {
		if m, ok := entry.(map[string]interface{}); ok {
			if isCopilotElydoraEntry(m) {
				continue
			}
		}
		preFiltered = append(preFiltered, entry)
	}
	if len(preFiltered) == 0 {
		delete(hooks, "preToolUse")
	} else {
		hooks["preToolUse"] = preFiltered
	}

	// Remove postToolUse Elydora entries
	postToolUse, _ := hooks["postToolUse"].([]interface{})
	var postFiltered []interface{}
	for _, entry := range postToolUse {
		if m, ok := entry.(map[string]interface{}); ok {
			if isCopilotElydoraEntry(m) {
				continue
			}
		}
		postFiltered = append(postFiltered, entry)
	}
	if len(postFiltered) == 0 {
		delete(hooks, "postToolUse")
	} else {
		hooks["postToolUse"] = postFiltered
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
	fmt.Println("Uninstalled Elydora hook for Copilot CLI.")
	return nil
}

func (p *CopilotPlugin) Status() (PluginStatus, error) {
	configPath, err := p.configPath()
	if err != nil {
		return PluginStatus{}, err
	}

	status := PluginStatus{
		AgentName:   "copilot",
		DisplayName: "Copilot CLI",
		ConfigPath:  configPath,
	}

	settings, err := readJSONFile(configPath)
	if err != nil {
		return status, nil
	}

	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks != nil {
		preConfigured := hasCopilotElydoraEntry(hooks["preToolUse"])
		postConfigured := hasCopilotElydoraEntry(hooks["postToolUse"])
		status.HookConfigured = preConfigured && postConfigured

		// Extract hook script path from the configured command
		scriptPath := extractCopilotElydoraScriptPath(hooks["postToolUse"])
		if scriptPath != "" {
			if _, err := os.Stat(scriptPath); err == nil {
				status.HookScriptExists = true
			}
		}
	}

	status.Installed = status.HookConfigured && status.HookScriptExists
	return status, nil
}

// isCopilotElydoraEntry checks if a Copilot hook entry (bash/powershell fields) is an Elydora hook.
func isCopilotElydoraEntry(m map[string]interface{}) bool {
	if bash, _ := m["bash"].(string); strings.Contains(bash, "elydora") {
		return true
	}
	if ps, _ := m["powershell"].(string); strings.Contains(ps, "elydora") {
		return true
	}
	return false
}

// hasCopilotElydoraEntry checks if a Copilot hook array contains an Elydora entry.
func hasCopilotElydoraEntry(hookArray interface{}) bool {
	arr, _ := hookArray.([]interface{})
	for _, entry := range arr {
		if m, ok := entry.(map[string]interface{}); ok {
			if isCopilotElydoraEntry(m) {
				return true
			}
		}
	}
	return false
}

// extractCopilotElydoraScriptPath extracts the script path from a Copilot hook array's Elydora command.
func extractCopilotElydoraScriptPath(hookArray interface{}) string {
	arr, _ := hookArray.([]interface{})
	for _, entry := range arr {
		if m, ok := entry.(map[string]interface{}); ok {
			if bash, _ := m["bash"].(string); strings.Contains(bash, "elydora") {
				return extractPathFromNodeCommand(bash)
			}
			if ps, _ := m["powershell"].(string); strings.Contains(ps, "elydora") {
				return extractPathFromNodeCommand(ps)
			}
		}
	}
	return ""
}
