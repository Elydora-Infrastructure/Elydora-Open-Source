package plugins

import (
	"fmt"
	"path/filepath"
)

// CursorPlugin manages Cursor's native global user hooks.
type CursorPlugin struct {
	rename renameFunc
}

// ManagesGuardRuntime reports that Cursor commits its provider-specific guard
// in the same transaction as the rest of the installation.
func (p *CursorPlugin) ManagesGuardRuntime() bool {
	return true
}

func cursorAgentPaths(config InstallConfig) (
	runtimeRoot, agentDirectory, guardPath, auditPath string,
	err error,
) {
	if config.AgentID == "" {
		return "", "", "", "", fmt.Errorf("agent ID is required")
	}
	runtimeRoot, err = AgentRuntimeRoot()
	if err != nil {
		return "", "", "", "", err
	}
	agentDirectory, err = ResolveAgentRuntimeDirectory(config.AgentID)
	if err != nil {
		return "", "", "", "", err
	}
	guardPath = filepath.Join(agentDirectory, cursorGuardScript)
	if !sameCursorPath(config.GuardScriptPath, guardPath) {
		return "", "", "", "", fmt.Errorf(
			"Elydora guard runtime must use the managed agent directory: %s",
			guardPath,
		)
	}
	auditPath = filepath.Join(agentDirectory, cursorAuditScript)
	if config.HookScript != "" && !sameCursorPath(config.HookScript, auditPath) {
		return "", "", "", "", fmt.Errorf(
			"Elydora audit runtime must use the managed agent directory: %s",
			auditPath,
		)
	}
	return runtimeRoot, agentDirectory, guardPath, auditPath, nil
}

// PreflightInstall validates every existing source before the CLI creates a
// runtime directory or guard file.
func (p *CursorPlugin) PreflightInstall(config InstallConfig) error {
	if _, err := readCursorDocument(); err != nil {
		return err
	}
	_, agentDirectory, _, _, err := cursorAgentPaths(config)
	if err != nil {
		return err
	}
	if err := preflightCursorRuntime(agentDirectory, config.AgentID); err != nil {
		return err
	}
	_, err = resolveNodeRuntime()
	return err
}

func (p *CursorPlugin) Install(config InstallConfig) error {
	document, err := readCursorDocument()
	if err != nil {
		return err
	}
	runtimeRoot, agentDirectory, guardPath, auditPath, err := cursorAgentPaths(config)
	if err != nil {
		return err
	}
	if err := preflightCursorRuntime(agentDirectory, config.AgentID); err != nil {
		return err
	}
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		return err
	}
	hooks := removeManagedCursorHooks(document.hooks, runtimeRoot, "")
	hooks["preToolUse"] = append(
		hooks["preToolUse"],
		buildCursorHandler(nodePath, guardPath),
	)
	hooks["postToolUse"] = append(
		hooks["postToolUse"],
		buildCursorHandler(nodePath, auditPath),
	)
	hooks["postToolUseFailure"] = append(
		hooks["postToolUseFailure"],
		buildCursorHandler(nodePath, auditPath),
	)
	rendered, err := renderCursorDocument(document, hooks, runtimeRoot)
	if err != nil {
		return fmt.Errorf("render Cursor user hooks: %w", err)
	}
	changes, err := prepareCursorInstallationChanges(config, agentDirectory, rendered)
	if err != nil {
		return err
	}
	if err := writeCursorChanges(
		changes,
		"Install Cursor hooks",
		p.rename,
		runtimeRoot,
		agentDirectory,
	); err != nil {
		return err
	}
	fmt.Printf("  Cursor hooks: %s\n", document.filePath)
	return nil
}

func (p *CursorPlugin) Uninstall(agentID string) error {
	document, err := readCursorDocument()
	if err != nil {
		return err
	}
	runtimeRoot, err := AgentRuntimeRoot()
	if err != nil {
		return err
	}
	hooks := removeManagedCursorHooks(document.hooks, runtimeRoot, agentID)
	rendered, err := renderCursorDocument(document, hooks, runtimeRoot)
	if err != nil {
		return fmt.Errorf("render Cursor user hooks: %w", err)
	}
	change, err := prepareRenderedCursorChange(rendered)
	if err != nil {
		return err
	}
	return writeCursorChanges(
		[]*fileChange{change},
		"Uninstall Cursor hooks",
		p.rename,
		"",
		"",
	)
}

func (p *CursorPlugin) Status() (PluginStatus, error) {
	configPath, pathErr := cursorConfigPath()
	entry := SupportedAgents[cursorAgentKey]
	status := PluginStatus{
		AgentName: cursorAgentKey, DisplayName: entry.Name, ConfigPath: configPath,
	}
	if pathErr != nil {
		return status, pathErr
	}
	document, err := readCursorDocument()
	if err != nil {
		return status, err
	}
	runtimeRoot, err := AgentRuntimeRoot()
	if err != nil {
		return status, err
	}
	contracts := cursorRuntimeContracts(document.hooks, runtimeRoot)
	status.HookConfigured = len(contracts) > 0
	if !status.HookConfigured {
		return status, nil
	}
	status.HookScriptExists, err = cursorRuntimeFilesExist(contracts)
	if err != nil {
		return status, err
	}
	status.Installed = status.HookScriptExists
	return status, nil
}
