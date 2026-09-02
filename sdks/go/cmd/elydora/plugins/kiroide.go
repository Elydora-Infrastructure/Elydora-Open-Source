package plugins

import "fmt"

// KiroIdePlugin manages Kiro IDE 1.0 workspace hooks.
type KiroIdePlugin struct {
	rename renameFunc
}

// ManagesGuardRuntime reports that Kiro IDE commits both generated runtimes,
// runtime metadata, credentials, workspace hooks, and legacy migration in one
// transaction.
func (p *KiroIdePlugin) ManagesGuardRuntime() bool {
	return true
}

// PreflightInstall validates every source and runtime identity before writes.
func (p *KiroIdePlugin) PreflightInstall(config InstallConfig) error {
	sources, err := readKiroIdeSources()
	if err != nil {
		return err
	}
	if err := requireAvailableKiroIdeHooks(sources.document.hooks); err != nil {
		return err
	}
	_, _, err = preflightKiroIdeInstallation(config)
	return err
}

func (p *KiroIdePlugin) Install(config InstallConfig) error {
	sources, err := readKiroIdeSources()
	if err != nil {
		return err
	}
	if err := requireAvailableKiroIdeHooks(sources.document.hooks); err != nil {
		return err
	}
	paths, nodePath, err := preflightKiroIdeInstallation(config)
	if err != nil {
		return err
	}
	hooks := withoutManagedKiroIdeHooks(sources.document.hooks, "")
	guard, err := buildKiroIdeHook(kiroIdeGuardName, nodePath, paths.guardPath)
	if err != nil {
		return err
	}
	audit, err := buildKiroIdeHook(kiroIdeAuditName, nodePath, paths.auditPath)
	if err != nil {
		return err
	}
	rendered, err := renderKiroIdeDocument(
		sources.document,
		append(hooks, guard, audit),
	)
	if err != nil {
		return err
	}
	prepared, err := prepareKiroIdeInstallation(config, sources, paths, rendered)
	if err != nil {
		return err
	}
	if err := commitKiroIdeInstallation(prepared, p.rename); err != nil {
		return err
	}
	fmt.Printf("Kiro IDE workspace hooks: %s\n", sources.paths.configPath)
	fmt.Println("Kiro IDE verification: confirm both Elydora entries in the Agent Hooks panel.")
	return nil
}

func (p *KiroIdePlugin) Uninstall(agentID string) error {
	sources, err := readKiroIdeSources()
	if err != nil {
		return err
	}
	rendered, err := renderKiroIdeDocument(
		sources.document,
		withoutManagedKiroIdeHooks(sources.document.hooks, agentID),
	)
	if err != nil {
		return err
	}
	prepared, err := prepareKiroIdeUninstall(sources, rendered, agentID)
	if err != nil {
		return err
	}
	if err := commitKiroIdeUninstall(prepared, p.rename); err != nil {
		return err
	}
	fmt.Println("Uninstalled Elydora hooks from Kiro IDE.")
	return nil
}

func (p *KiroIdePlugin) Status() (PluginStatus, error) {
	sources, err := readKiroIdeSources()
	entry := SupportedAgents[kiroIdeAgentKey]
	status := PluginStatus{
		AgentName:   kiroIdeAgentKey,
		DisplayName: entry.Name,
		ConfigPath:  sourcesPathOrEmpty(sources),
	}
	if err != nil {
		return status, err
	}
	if err := requireAvailableKiroIdeHooks(sources.document.hooks); err != nil {
		return status, err
	}
	contracts, err := kiroIdeRuntimeContracts(sources.document.hooks)
	if err != nil {
		return status, err
	}
	status.HookConfigured = len(contracts) > 0
	if !status.HookConfigured {
		return status, nil
	}
	status.HookScriptExists, err = kiroIdeRuntimeFilesExist(contracts)
	if err != nil {
		return status, err
	}
	status.Installed = status.HookScriptExists
	return status, nil
}

func sourcesPathOrEmpty(sources *kiroIdeSources) string {
	if sources == nil {
		return ""
	}
	return sources.paths.configPath
}
