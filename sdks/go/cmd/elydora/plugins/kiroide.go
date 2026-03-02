package plugins

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// KiroIdePlugin manages the Elydora audit hook for Kiro IDE.
// It writes a .kiro/hooks/elydora-audit.kiro.hook JSON file in the home directory.
type KiroIdePlugin struct{}

func (p *KiroIdePlugin) configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".kiro", "hooks", "elydora-audit.kiro.hook"), nil
}

func (p *KiroIdePlugin) Install(config InstallConfig) error {
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

	hookFile, err := p.configPath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(hookFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}

	content := buildKiroIdeHookFile(scriptPath, guardPath)
	if err := os.WriteFile(hookFile, []byte(content), 0644); err != nil {
		return fmt.Errorf("write %s: %w", hookFile, err)
	}

	fmt.Printf("Installed Elydora hook for Kiro IDE at %s\n", hookFile)
	return nil
}

func buildKiroIdeHookFile(scriptPath, guardPath string) string {
	hookConfig := map[string]interface{}{
		"name": "Elydora Audit",
		"hooks": map[string]interface{}{
			"pre_tool_use": map[string]interface{}{
				"command":    "node " + guardPath,
				"timeout_ms": 5000,
			},
			"post_tool_use": map[string]interface{}{
				"command":    "node " + scriptPath,
				"timeout_ms": 5000,
			},
		},
	}
	encoded, _ := json.MarshalIndent(hookConfig, "", "  ")
	return string(encoded) + "\n"
}

func (p *KiroIdePlugin) Uninstall(agentID string) error {
	hookFile, err := p.configPath()
	if err != nil {
		return err
	}
	if err := os.Remove(hookFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", hookFile, err)
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

	fmt.Println("Uninstalled Elydora hook for Kiro IDE.")
	return nil
}

func (p *KiroIdePlugin) Status() (PluginStatus, error) {
	hookFile, err := p.configPath()
	if err != nil {
		return PluginStatus{}, err
	}

	status := PluginStatus{
		AgentName:   "kiroide",
		DisplayName: "Kiro IDE",
		ConfigPath:  hookFile,
	}

	// Check if hook file exists and contains both pre_tool_use and post_tool_use
	data, err := os.ReadFile(hookFile)
	if err == nil {
		var config map[string]interface{}
		if json.Unmarshal(data, &config) == nil {
			hooks, _ := config["hooks"].(map[string]interface{})
			if hooks != nil {
				_, hasPre := hooks["pre_tool_use"]
				_, hasPost := hooks["post_tool_use"]
				status.HookConfigured = hasPre && hasPost

				// Extract hook script path from the post_tool_use command
				if postHook, ok := hooks["post_tool_use"].(map[string]interface{}); ok {
					if cmd, _ := postHook["command"].(string); cmd != "" {
						scriptPath := extractPathFromNodeCommand(cmd)
						if scriptPath != "" {
							if _, err := os.Stat(scriptPath); err == nil {
								status.HookScriptExists = true
							}
						}
					}
				}
			}
		}
	}

	status.Installed = status.HookConfigured && status.HookScriptExists
	return status, nil
}
