package plugins

import (
	"fmt"
	"path/filepath"
)

// CodexPlugin manages OpenAI Codex global user hooks.
type CodexPlugin struct {
	rename renameFunc
}

// ManagesGuardRuntime reports that Codex commits its provider guard with the
// audit runtime and user hook document.
func (p *CodexPlugin) ManagesGuardRuntime() bool {
	return true
}

// PreflightInstall validates every existing source before the CLI creates
// runtime state.
func (p *CodexPlugin) PreflightInstall(config InstallConfig) error {
	document, err := readCodexDocument()
	if err != nil {
		return err
	}
	_, _, err = preflightCodexInstallation(config, document.filePath)
	return err
}

func (p *CodexPlugin) Install(config InstallConfig) error {
	document, err := readCodexDocument()
	if err != nil {
		return err
	}
	paths, nodePath, err := preflightCodexInstallation(config, document.filePath)
	if err != nil {
		return err
	}
	hooks := removeManagedCodexHooks(document.hooks, "")
	hooks["PreToolUse"] = append(
		hooks["PreToolUse"],
		codexMatcherGroup(codexHandler(
			nodePath,
			paths.guardPath,
			codexGuardStatusMessage,
		)),
	)
	hooks["PostToolUse"] = append(
		hooks["PostToolUse"],
		codexMatcherGroup(codexHandler(
			nodePath,
			paths.auditPath,
			codexAuditStatusMessage,
		)),
	)
	rendered, err := renderCodexDocument(document, hooks)
	if err != nil {
		return fmt.Errorf("render Codex user hooks: %w", err)
	}
	changes, err := prepareCodexInstallationChanges(config, paths, rendered)
	if err != nil {
		return err
	}
	if err := writeCodexChanges(
		changes,
		"Install Codex hooks",
		p.rename,
		paths.runtimeRoot,
		paths.agentDirectory,
		filepath.Dir(document.filePath),
	); err != nil {
		return err
	}
	fmt.Printf("  Codex hooks: %s\n", document.filePath)
	fmt.Println("  Codex trust: run /hooks and approve both Elydora command hooks.")
	return nil
}

func (p *CodexPlugin) Uninstall(agentID string) error {
	document, err := readCodexDocument()
	if err != nil {
		return err
	}
	hooks := removeManagedCodexHooks(document.hooks, agentID)
	rendered, err := renderCodexDocument(document, hooks)
	if err != nil {
		return fmt.Errorf("render Codex user hooks: %w", err)
	}
	change, err := prepareRenderedCodexChange(rendered)
	if err != nil {
		return err
	}
	return writeCodexChanges(
		[]*fileChange{change},
		"Uninstall Codex hooks",
		p.rename,
		"",
		"",
		filepath.Dir(document.filePath),
	)
}

func (p *CodexPlugin) Status() (PluginStatus, error) {
	configPath, pathErr := codexConfigPath()
	entry := SupportedAgents[codexAgentKey]
	status := PluginStatus{
		AgentName: codexAgentKey, DisplayName: entry.Name, ConfigPath: configPath,
	}
	if pathErr != nil {
		return status, pathErr
	}
	document, err := readCodexDocument()
	if err != nil {
		return status, err
	}
	contracts, err := codexRuntimeContracts(document.hooks)
	if err != nil {
		return status, err
	}
	status.HookConfigured = len(contracts) > 0
	if !status.HookConfigured {
		return status, nil
	}
	status.HookScriptExists, err = codexRuntimeFilesExist(contracts)
	if err != nil {
		return status, err
	}
	status.Installed = status.HookScriptExists
	return status, nil
}
