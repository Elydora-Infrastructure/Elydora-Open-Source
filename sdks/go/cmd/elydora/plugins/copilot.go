package plugins

import (
	"fmt"
	"path/filepath"
)

// CopilotPlugin manages GitHub Copilot CLI native user hooks.
type CopilotPlugin struct {
	rename renameFunc
}

// ManagesGuardRuntime reports that Copilot commits its provider guard with the
// audit runtime and hook documents.
func (p *CopilotPlugin) ManagesGuardRuntime() bool {
	return true
}

// PreflightInstall validates hook sources, settings, matchers, credentials,
// and runtime identity before any write.
func (p *CopilotPlugin) PreflightInstall(config InstallConfig) error {
	sources, _, err := readCopilotSources()
	if err != nil {
		return err
	}
	_, _, err = preflightCopilotInstallation(config, sources)
	return err
}

func renderCopilotInstallation(
	sources *copilotSources,
	guardPath string,
	auditPath string,
	nodePath string,
) ([]*copilotRenderedDocument, error) {
	hooks, err := removeManagedCopilotHooks(sources.user.hooks, "")
	if err != nil {
		return nil, err
	}
	for _, item := range []struct {
		event      string
		scriptPath string
	}{
		{"preToolUse", guardPath},
		{"postToolUse", auditPath},
		{"postToolUseFailure", auditPath},
	} {
		hooks[item.event] = append(
			hooks[item.event],
			buildCopilotHandler(nodePath, item.scriptPath),
		)
	}
	user, err := renderCopilotDocument(sources.user, hooks)
	if err != nil {
		return nil, fmt.Errorf("render GitHub Copilot user hooks: %w", err)
	}
	rendered := []*copilotRenderedDocument{user}
	if sources.legacy != nil {
		legacyHooks, removeErr := removeManagedCopilotHooks(sources.legacy.hooks, "")
		if removeErr != nil {
			return nil, removeErr
		}
		legacy, renderErr := renderCopilotDocument(sources.legacy, legacyHooks)
		if renderErr != nil {
			return nil, fmt.Errorf("render GitHub Copilot legacy project hooks: %w", renderErr)
		}
		rendered = append(rendered, legacy)
	}
	return rendered, nil
}

func (p *CopilotPlugin) Install(config InstallConfig) error {
	sources, _, err := readCopilotSources()
	if err != nil {
		return err
	}
	paths, nodePath, err := preflightCopilotInstallation(config, sources)
	if err != nil {
		return err
	}
	rendered, err := renderCopilotInstallation(
		sources,
		paths.guardPath,
		paths.auditPath,
		nodePath,
	)
	if err != nil {
		return err
	}
	prepared, err := prepareCopilotInstallation(config, sources, rendered)
	if err != nil {
		return err
	}
	if err := commitCopilotInstallation(prepared, p.rename); err != nil {
		return err
	}
	fmt.Printf("GitHub Copilot CLI hooks: %s\n", sources.user.filePath)
	fmt.Println("GitHub Copilot CLI: restart active sessions to load updated hooks.")
	return nil
}

func renderCopilotUninstall(
	sources *copilotSources,
	agentID string,
) ([]*copilotRenderedDocument, error) {
	documents := []*copilotDocument{sources.user}
	if sources.legacy != nil {
		documents = append(documents, sources.legacy)
	}
	rendered := make([]*copilotRenderedDocument, 0, len(documents))
	for _, document := range documents {
		hooks, err := removeManagedCopilotHooks(document.hooks, agentID)
		if err != nil {
			return nil, err
		}
		item, err := renderCopilotDocument(document, hooks)
		if err != nil {
			return nil, fmt.Errorf("render GitHub Copilot hook source: %w", err)
		}
		rendered = append(rendered, item)
	}
	return rendered, nil
}

func (p *CopilotPlugin) Uninstall(agentID string) error {
	sources, _, err := readCopilotSources()
	if err != nil {
		return err
	}
	rendered, err := renderCopilotUninstall(sources, agentID)
	if err != nil {
		return err
	}
	changes := make([]*fileChange, 0, len(rendered))
	for _, document := range rendered {
		change, changeErr := prepareRenderedCopilotChange(document)
		if changeErr != nil {
			return changeErr
		}
		changes = append(changes, change)
	}
	return writeCopilotChanges(
		changes,
		"Uninstall GitHub Copilot hooks",
		p.rename,
		"",
		"",
		nil,
	)
}

func mergedCopilotContracts(
	sources *copilotSources,
) ([]copilotRuntimeContract, error) {
	contracts, err := copilotRuntimeContracts(sources.user.hooks)
	if err != nil {
		return nil, err
	}
	if sources.legacy != nil {
		legacy, legacyErr := copilotRuntimeContracts(sources.legacy.hooks)
		if legacyErr != nil {
			return nil, legacyErr
		}
		contracts = append(contracts, legacy...)
	}
	unique := map[string]copilotRuntimeContract{}
	for _, contract := range contracts {
		unique[copilotEntryKey(contract.agentID)] = contract
	}
	result := make([]copilotRuntimeContract, 0, len(unique))
	for _, contract := range unique {
		result = append(result, contract)
	}
	return result, nil
}

func configuredCopilotPath(
	sources *copilotSources,
	defaultPath string,
) (string, error) {
	userContracts, err := copilotRuntimeContracts(sources.user.hooks)
	if err != nil {
		return "", err
	}
	if len(userContracts) > 0 {
		return sources.user.filePath, nil
	}
	if sources.legacy != nil {
		legacyContracts, legacyErr := copilotRuntimeContracts(sources.legacy.hooks)
		if legacyErr != nil {
			return "", legacyErr
		}
		if len(legacyContracts) > 0 {
			return sources.legacy.filePath, nil
		}
	}
	return filepath.Clean(defaultPath), nil
}

func (p *CopilotPlugin) Status() (PluginStatus, error) {
	paths, pathErr := resolveCopilotPaths()
	entry := SupportedAgents[copilotAgentKey]
	status := PluginStatus{AgentName: copilotAgentKey, DisplayName: entry.Name}
	if pathErr != nil {
		return status, pathErr
	}
	status.ConfigPath = paths.userHookPath
	sources, _, err := readCopilotSources()
	if err != nil {
		return status, err
	}
	contracts, err := mergedCopilotContracts(sources)
	if err != nil {
		return status, err
	}
	status.ConfigPath, err = configuredCopilotPath(sources, paths.userHookPath)
	if err != nil {
		return status, err
	}
	status.HookConfigured = sources.disabledBy == "" && len(contracts) > 0
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
