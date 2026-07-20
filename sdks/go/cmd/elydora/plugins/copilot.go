package plugins

import (
	"fmt"
	"os"
	"path/filepath"
)

// CopilotPlugin manages GitHub Copilot CLI native user hooks.
type CopilotPlugin struct {
	rename renameFunc
}

func (p *CopilotPlugin) Install(config InstallConfig) error {
	if config.AgentID == "" {
		return fmt.Errorf("agent ID is required")
	}
	sources, err := readCopilotSources()
	if err != nil {
		return err
	}
	if sources.user.disabled {
		return fmt.Errorf(
			"GitHub Copilot user hooks are disabled by disableAllHooks at %s",
			sources.user.filePath,
		)
	}
	runtimeRoot, err := AgentRuntimeRoot()
	if err != nil {
		return err
	}
	agentDirectory, err := ResolveAgentRuntimeDirectory(config.AgentID)
	if err != nil {
		return err
	}
	expectedGuard := filepath.Join(agentDirectory, copilotGuardScript)
	if !sameCopilotPath(config.GuardScriptPath, expectedGuard) {
		return fmt.Errorf("Elydora guard runtime must use the managed agent directory: %s", expectedGuard)
	}
	if err := requireCopilotRuntime(expectedGuard, "Elydora guard runtime"); err != nil {
		return err
	}
	auditPath := filepath.Join(agentDirectory, copilotAuditScript)
	if config.HookScript != "" && !sameCopilotPath(config.HookScript, auditPath) {
		return fmt.Errorf("Elydora audit runtime must use the managed agent directory: %s", auditPath)
	}
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		return err
	}

	userHooks := removeManagedCopilotHooks(sources.user.hooks, runtimeRoot, "")
	userHooks["preToolUse"] = append(
		userHooks["preToolUse"],
		buildCopilotHandler(nodePath, expectedGuard),
	)
	userHooks["postToolUse"] = append(
		userHooks["postToolUse"],
		buildCopilotHandler(nodePath, auditPath),
	)
	userRendered, err := renderCopilotDocument(sources.user, userHooks)
	if err != nil {
		return fmt.Errorf("render GitHub Copilot user hooks: %w", err)
	}
	rendered := []*copilotRenderedDocument{userRendered}
	if sources.legacy != nil {
		legacyHooks := removeManagedCopilotHooks(sources.legacy.hooks, runtimeRoot, "")
		legacyRendered, renderErr := renderCopilotDocument(sources.legacy, legacyHooks)
		if renderErr != nil {
			return fmt.Errorf("render GitHub Copilot project hooks: %w", renderErr)
		}
		rendered = append(rendered, legacyRendered)
	}
	changes, err := prepareCopilotInstallationChanges(
		config,
		agentDirectory,
		auditPath,
		rendered,
	)
	if err != nil {
		return err
	}
	if err := writeChanges(changes, "Install GitHub Copilot hooks", p.rename); err != nil {
		return err
	}
	fmt.Printf("  GitHub Copilot CLI hooks: %s\n", sources.user.filePath)
	return nil
}

func (p *CopilotPlugin) Uninstall(agentID string) error {
	sources, err := readCopilotSources()
	if err != nil {
		return err
	}
	runtimeRoot, err := AgentRuntimeRoot()
	if err != nil {
		return err
	}
	documents := []*copilotDocument{sources.user}
	if sources.legacy != nil {
		documents = append(documents, sources.legacy)
	}
	changes := make([]*fileChange, 0, len(documents))
	for _, document := range documents {
		hooks := removeManagedCopilotHooks(document.hooks, runtimeRoot, agentID)
		rendered, renderErr := renderCopilotDocument(document, hooks)
		if renderErr != nil {
			return fmt.Errorf("render GitHub Copilot hook source: %w", renderErr)
		}
		change, changeErr := prepareRenderedCopilotChange(rendered)
		if changeErr != nil {
			return changeErr
		}
		changes = append(changes, change)
	}
	return writeChanges(changes, "Uninstall GitHub Copilot hooks", p.rename)
}

func (p *CopilotPlugin) Status() (PluginStatus, error) {
	userPath, _, pathErr := copilotConfigPaths()
	entry := SupportedAgents[copilotAgentKey]
	status := PluginStatus{
		AgentName: copilotAgentKey, DisplayName: entry.Name, ConfigPath: userPath,
	}
	if pathErr != nil {
		return status, pathErr
	}
	sources, err := readCopilotSources()
	if err != nil {
		return status, err
	}
	runtimeRoot, err := AgentRuntimeRoot()
	if err != nil {
		return status, err
	}
	contracts := activeCopilotContracts(sources, runtimeRoot)
	status.ConfigPath = configuredCopilotPath(sources, runtimeRoot)
	status.HookConfigured = len(contracts) > 0
	if !status.HookConfigured {
		return status, nil
	}
	status.HookScriptExists, err = copilotRuntimeFilesExist(contracts)
	if err != nil {
		return status, err
	}
	status.Installed = status.HookScriptExists
	return status, nil
}

func requireCopilotRuntime(path, label string) error {
	if path == "" {
		return fmt.Errorf("%s path is required", label)
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return fmt.Errorf("%s is missing: %s", label, path)
	}
	if err != nil {
		return fmt.Errorf("read %s at %s: %w", label, path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("%s path is not a physical file: %s", label, path)
	}
	return nil
}
