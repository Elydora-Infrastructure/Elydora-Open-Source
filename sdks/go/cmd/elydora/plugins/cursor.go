package plugins

import (
	"fmt"
	"path/filepath"
)

// CursorPlugin manages Cursor's native global user hooks.
type CursorPlugin struct {
	rename renameFunc
}

// ManagesGuardRuntime reports that the guard is committed with the audit runtime.
func (p *CursorPlugin) ManagesGuardRuntime() bool {
	return true
}

func cursorAgentPaths(config InstallConfig) (*managedRuntimePaths, error) {
	return resolveManagedRuntimePaths(config, cursorGuardScript, cursorAuditScript)
}

// PreflightInstall validates every source before the CLI creates runtime state.
func (p *CursorPlugin) PreflightInstall(config InstallConfig) error {
	if _, err := readCursorDocument(); err != nil {
		return err
	}
	paths, err := cursorAgentPaths(config)
	if err != nil {
		return err
	}
	if err := validateManagedRuntimeIdentity(
		paths.agentDirectory, config.AgentID, cursorAgentKey, "Cursor",
	); err != nil {
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
	paths, err := cursorAgentPaths(config)
	if err != nil {
		return err
	}
	if err := validateManagedRuntimeIdentity(
		paths.agentDirectory, config.AgentID, cursorAgentKey, "Cursor",
	); err != nil {
		return err
	}
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		return err
	}
	hooks := removeManagedCursorHooks(document.hooks, paths.runtimeRoot, "")
	hooks["preToolUse"] = append(hooks["preToolUse"], buildCursorHandler(nodePath, paths.guardPath))
	hooks["postToolUse"] = append(hooks["postToolUse"], buildCursorHandler(nodePath, paths.auditPath))
	hooks["postToolUseFailure"] = append(
		hooks["postToolUseFailure"],
		buildCursorHandler(nodePath, paths.auditPath),
	)
	rendered, err := renderCursorDocument(document, hooks, paths.runtimeRoot)
	if err != nil {
		return fmt.Errorf("render Cursor user hooks: %w", err)
	}
	changes, err := prepareCursorInstallationChanges(config, paths, rendered)
	if err != nil {
		return err
	}
	if err := writeManagedChanges(
		changes,
		"Install Cursor hooks",
		p.rename,
		paths.runtimeRoot,
		paths.agentDirectory,
		filepath.Dir(document.filePath),
		"Cursor hooks directory",
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
	return writeManagedChanges(
		[]*fileChange{change},
		"Uninstall Cursor hooks",
		p.rename,
		"",
		"",
		filepath.Dir(document.filePath),
		"Cursor hooks directory",
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
