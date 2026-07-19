package plugins

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type QwenPlugin struct {
	rename renameFunc
}

type qwenRuntimeConfig struct {
	OrgID     string `json:"org_id"`
	AgentID   string `json:"agent_id"`
	KID       string `json:"kid"`
	BaseURL   string `json:"base_url"`
	Token     string `json:"token"`
	AgentName string `json:"agent_name"`
}

func (p *QwenPlugin) Install(config InstallConfig) error {
	runtimeRoot, agentDirectory, err := qwenAgentDirectory(config.AgentID)
	if err != nil {
		return err
	}
	document, err := readQwenDocument()
	if err != nil {
		return err
	}
	expectedGuard := filepath.Join(agentDirectory, qwenGuardScript)
	if !sameQwenPath(config.GuardScriptPath, expectedGuard) {
		return fmt.Errorf("Elydora guard runtime must use the managed agent directory: %s", expectedGuard)
	}
	guardExists, err := regularFileExists(config.GuardScriptPath, "Elydora guard runtime")
	if err != nil {
		return err
	}
	if !guardExists {
		return fmt.Errorf("Elydora guard runtime is missing: %s", config.GuardScriptPath)
	}
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		return err
	}
	if err := validateQwenRegexes(nodePath, document.hooks); err != nil {
		return err
	}
	auditPath := filepath.Join(agentDirectory, qwenAuditScript)
	rendered, err := renderQwenDocument(document, "", runtimeRoot, map[string]map[string]any{
		"PreToolUse":  buildQwenGroup(nodePath, config.GuardScriptPath),
		"PostToolUse": buildQwenGroup(nodePath, auditPath),
	})
	if err != nil {
		return err
	}
	changes, err := prepareQwenInstallationChanges(config, agentDirectory, auditPath, rendered)
	if err != nil {
		return err
	}
	if err := writeChanges(changes, "Write Qwen Code installation", p.rename); err != nil {
		return err
	}
	fmt.Printf("  Qwen Code: user hooks installed at %s\n", document.filePath)
	fmt.Println("  Qwen Code: run /hooks to review the Elydora hook changes.")
	return nil
}

func (p *QwenPlugin) Uninstall(agentID string) error {
	runtimeRoot, err := qwenRuntimeRoot()
	if err != nil {
		return err
	}
	document, err := readQwenDocument()
	if err != nil {
		return err
	}
	if !document.exists {
		return nil
	}
	rendered, err := renderQwenDocument(document, agentID, runtimeRoot, nil)
	if err != nil {
		return err
	}
	change, err := prepareRenderedQwenChange(rendered)
	if err != nil {
		return err
	}
	return writeChanges([]*fileChange{change}, "Write Qwen Code settings", p.rename)
}

func (p *QwenPlugin) Status() (PluginStatus, error) {
	entry := SupportedAgents[qwenAgentKey]
	status := PluginStatus{AgentName: qwenAgentKey, DisplayName: entry.Name}
	document, err := readQwenDocument()
	if err != nil {
		return status, err
	}
	status.ConfigPath = document.filePath
	if document.hooksDisabled {
		return status, nil
	}
	runtimeRoot, err := qwenRuntimeRoot()
	if err != nil {
		return status, err
	}
	contracts := qwenRuntimeContracts(document.hooks, runtimeRoot)
	status.HookConfigured = len(contracts) > 0
	if !status.HookConfigured {
		return status, nil
	}
	status.HookScriptExists, err = qwenRuntimeFilesExist(contracts)
	if err != nil {
		return status, err
	}
	status.Installed = status.HookScriptExists
	return status, nil
}

func qwenAgentDirectory(agentID string) (string, string, error) {
	if agentID == "" || agentID == "." || agentID == ".." || filepath.IsAbs(agentID) || filepath.Base(agentID) != agentID {
		return "", "", fmt.Errorf("agent_id must be a single non-empty path segment")
	}
	runtimeRoot, err := qwenRuntimeRoot()
	if err != nil {
		return "", "", err
	}
	return runtimeRoot, filepath.Join(runtimeRoot, agentID), nil
}

func prepareQwenInstallationChanges(
	config InstallConfig,
	agentDirectory, auditPath string,
	rendered *qwenRenderedDocument,
) ([]*fileChange, error) {
	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = "https://api.elydora.com"
	}
	runtimeConfig, err := json.MarshalIndent(qwenRuntimeConfig{
		OrgID: config.OrgID, AgentID: config.AgentID, KID: config.KID,
		BaseURL: baseURL, Token: config.Token, AgentName: qwenAgentKey,
	}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode Elydora runtime config: %w", err)
	}
	runtimeConfig = append(runtimeConfig, '\n')
	items := []struct {
		path, label string
		content     []byte
		mode        os.FileMode
	}{
		{filepath.Join(agentDirectory, "config.json"), "Elydora runtime config", runtimeConfig, 0600},
		{filepath.Join(agentDirectory, "private.key"), "Elydora private key", []byte(config.PrivateKey + "\n"), 0600},
		{auditPath, "Elydora audit runtime", []byte(buildHookScript(qwenAgentKey, config.AgentID)), 0700},
	}
	changes := make([]*fileChange, 0, len(items)+1)
	for _, item := range items {
		change, changeErr := prepareFileChange(item.path, item.label, item.content, item.mode)
		if changeErr != nil {
			return nil, changeErr
		}
		changes = append(changes, change)
	}
	settingsChange, err := prepareRenderedQwenChange(rendered)
	if err != nil {
		return nil, err
	}
	changes = append(changes, settingsChange)
	return changes, nil
}
