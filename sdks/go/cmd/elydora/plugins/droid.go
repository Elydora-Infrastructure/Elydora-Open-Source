package plugins

import "fmt"

// DroidPlugin manages Factory Droid native user hooks.
type DroidPlugin struct {
	rename renameFunc
}

// ManagesGuardRuntime reports that Droid commits its guard, audit runtime,
// credentials, and hook documents in one transaction.
func (p *DroidPlugin) ManagesGuardRuntime() bool {
	return true
}

// PreflightInstall validates every hook source, policy layer, credential,
// matcher, and runtime identity before any write.
func (p *DroidPlugin) PreflightInstall(config InstallConfig) error {
	sources, err := readDroidSources()
	if err != nil {
		return err
	}
	_, _, err = preflightDroidInstallation(config, sources)
	return err
}

func renderDroidInstallation(
	sources *droidSources,
	guardPath, auditPath, nodePath, runtimeRoot string,
) ([]*droidRenderedDocument, error) {
	target := activeDroidDocument(sources)
	groups := map[string]map[string]any{
		"PreToolUse":  buildDroidGroup(nodePath, guardPath),
		"PostToolUse": buildDroidGroup(nodePath, auditPath),
	}
	documents := droidInstallationDocuments(sources)
	rendered := make([]*droidRenderedDocument, 0, len(documents))
	for _, document := range documents {
		item, err := renderDroidDocument(
			document,
			"",
			runtimeRoot,
			droidAdditionsFor(document, target, groups),
		)
		if err != nil {
			return nil, err
		}
		rendered = append(rendered, item)
	}
	return rendered, nil
}

func (p *DroidPlugin) Install(config InstallConfig) error {
	sources, err := readDroidSources()
	if err != nil {
		return err
	}
	paths, nodePath, err := preflightDroidInstallation(config, sources)
	if err != nil {
		return err
	}
	rendered, err := renderDroidInstallation(
		sources,
		paths.guardPath,
		paths.auditPath,
		nodePath,
		paths.runtimeRoot,
	)
	if err != nil {
		return err
	}
	prepared, err := prepareDroidInstallation(config, sources, rendered)
	if err != nil {
		return err
	}
	if err := commitDroidInstallation(prepared, p.rename); err != nil {
		return err
	}
	fmt.Printf("Factory Droid hooks: %s\n", activeDroidDocument(sources).filePath)
	fmt.Println("Factory Droid: run /hooks to review the Elydora hook changes.")
	return nil
}

func renderDroidUninstall(
	sources *droidSources,
	agentID, runtimeRoot string,
) ([]*droidRenderedDocument, error) {
	documents := droidSourceDocuments(sources)
	rendered := make([]*droidRenderedDocument, 0, len(documents))
	for _, document := range documents {
		item, err := renderDroidDocument(
			document,
			agentID,
			runtimeRoot,
			map[string]map[string]any{},
		)
		if err != nil {
			return nil, err
		}
		rendered = append(rendered, item)
	}
	return rendered, nil
}

func (p *DroidPlugin) Uninstall(agentID string) error {
	sources, err := readDroidSources()
	if err != nil {
		return err
	}
	runtimeRoot, err := droidRuntimeRoot()
	if err != nil {
		return err
	}
	rendered, err := renderDroidUninstall(sources, agentID, runtimeRoot)
	if err != nil {
		return err
	}
	prepared, err := prepareDroidUninstall(rendered)
	if err != nil {
		return err
	}
	return commitDroidUninstall(prepared, p.rename)
}

func (p *DroidPlugin) Status() (PluginStatus, error) {
	entry := SupportedAgents[droidAgentKey]
	status := PluginStatus{AgentName: droidAgentKey, DisplayName: entry.Name}
	sources, err := readDroidSources()
	if err != nil {
		return status, err
	}
	status.ConfigPath = displayDroidConfigPath(sources)
	if droidHookBlocked(sources) != nil {
		return status, nil
	}
	runtimeRoot, err := droidRuntimeRoot()
	if err != nil {
		return status, err
	}
	contracts := droidRuntimeContracts(effectiveDroidHooks(sources), runtimeRoot)
	status.HookConfigured = len(contracts) > 0
	if !status.HookConfigured {
		return status, nil
	}
	status.HookScriptExists, err = droidRuntimeFilesExist(contracts)
	if err != nil {
		return status, err
	}
	status.Installed = status.HookScriptExists
	return status, nil
}
