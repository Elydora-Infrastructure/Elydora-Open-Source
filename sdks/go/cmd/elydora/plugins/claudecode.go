package plugins

import (
	"fmt"
	"path/filepath"
)

// ClaudeCodePlugin manages Claude Code global user hooks.
type ClaudeCodePlugin struct {
	rename renameFunc
}

// ManagesGuardRuntime reports that Claude Code commits its provider guard with
// the audit runtime and user settings document.
func (p *ClaudeCodePlugin) ManagesGuardRuntime() bool {
	return true
}

func requireClaudeHooksEnabled(document *claudeDocument) error {
	if document.hooksDisabled {
		return fmt.Errorf(
			"Claude Code hooks are disabled by disableAllHooks: %s",
			document.filePath,
		)
	}
	return nil
}

// PreflightInstall validates settings and runtime identity before any write.
func (p *ClaudeCodePlugin) PreflightInstall(config InstallConfig) error {
	document, err := readClaudeDocument()
	if err != nil {
		return err
	}
	if err := requireClaudeHooksEnabled(document); err != nil {
		return err
	}
	_, _, err = preflightClaudeInstallation(config, document)
	return err
}

func (p *ClaudeCodePlugin) Install(config InstallConfig) error {
	document, err := readClaudeDocument()
	if err != nil {
		return err
	}
	if err := requireClaudeHooksEnabled(document); err != nil {
		return err
	}
	paths, nodePath, err := preflightClaudeInstallation(config, document)
	if err != nil {
		return err
	}
	hooks, err := removeManagedClaudeHooks(document.hooks, "")
	if err != nil {
		return err
	}
	for _, item := range []struct{ event, script, status string }{
		{"PreToolUse", paths.guardPath, claudeGuardStatusMessage},
		{"PostToolUse", paths.auditPath, claudeAuditStatusMessage},
		{"PostToolUseFailure", paths.auditPath, claudeAuditStatusMessage},
	} {
		hooks[item.event] = append(
			hooks[item.event],
			buildClaudeGroup(nodePath, item.script, item.status),
		)
	}
	rendered, err := renderClaudeDocument(document, hooks)
	if err != nil {
		return fmt.Errorf("render Claude Code user settings: %w", err)
	}
	changes, err := prepareClaudeInstallationChanges(config, paths, rendered)
	if err != nil {
		return err
	}
	if err := writeClaudeChanges(
		changes,
		"Install Claude Code hooks",
		p.rename,
		paths.runtimeRoot,
		paths.agentDirectory,
		filepath.Dir(document.filePath),
	); err != nil {
		return err
	}
	fmt.Printf("Claude Code hooks installed at %s.\n", document.filePath)
	fmt.Println("Claude Code verification: run /hooks and claude doctor.")
	return nil
}

func (p *ClaudeCodePlugin) Uninstall(agentID string) error {
	document, err := readClaudeDocument()
	if err != nil {
		return err
	}
	hooks, err := removeManagedClaudeHooks(document.hooks, agentID)
	if err != nil {
		return err
	}
	rendered, err := renderClaudeDocument(document, hooks)
	if err != nil {
		return fmt.Errorf("render Claude Code user settings: %w", err)
	}
	change, err := prepareRenderedClaudeChange(rendered)
	if err != nil {
		return err
	}
	return writeClaudeChanges(
		[]*fileChange{change},
		"Uninstall Claude Code hooks",
		p.rename,
		"",
		"",
		filepath.Dir(document.filePath),
	)
}

func (p *ClaudeCodePlugin) Status() (PluginStatus, error) {
	configPath, pathErr := claudeSettingsPath()
	entry := SupportedAgents[claudeAgentKey]
	status := PluginStatus{
		AgentName: claudeAgentKey, DisplayName: entry.Name, ConfigPath: configPath,
	}
	if pathErr != nil {
		return status, pathErr
	}
	document, err := readClaudeDocument()
	if err != nil {
		return status, err
	}
	if document.hooksDisabled {
		return status, nil
	}
	contracts, err := claudeRuntimeContracts(document.hooks)
	if err != nil {
		return status, err
	}
	status.HookConfigured = len(contracts) > 0
	if !status.HookConfigured {
		return status, nil
	}
	status.HookScriptExists, err = claudeRuntimeFilesExist(contracts)
	if err != nil {
		return status, err
	}
	status.Installed = status.HookScriptExists
	return status, nil
}
