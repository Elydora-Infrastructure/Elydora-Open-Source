package plugins

import "fmt"

// QwenPlugin manages Qwen Code native user hooks.
type QwenPlugin struct {
	rename renameFunc
}

// ManagesGuardRuntime reports that Qwen commits its guard, audit runtime, and
// settings in one transaction.
func (p *QwenPlugin) ManagesGuardRuntime() bool {
	return true
}

// PreflightInstall validates every effective source and runtime identity before
// the CLI writes any managed file.
func (p *QwenPlugin) PreflightInstall(config InstallConfig) error {
	sources, err := readQwenSources()
	if err != nil {
		return err
	}
	_, _, err = preflightQwenInstallation(config, sources)
	return err
}

func qwenInstalledGroups(
	nodePath, guardPath, auditPath string,
) map[string]map[string]any {
	return map[string]map[string]any{
		"PreToolUse":  buildQwenGroup(nodePath, guardPath, qwenGuardHookName),
		"PostToolUse": buildQwenGroup(nodePath, auditPath, qwenAuditHookName),
		"PostToolUseFailure": buildQwenGroup(
			nodePath,
			auditPath,
			qwenAuditHookName,
		),
	}
}

func (p *QwenPlugin) Install(config InstallConfig) error {
	sources, err := readQwenSources()
	if err != nil {
		return err
	}
	paths, nodePath, err := preflightQwenInstallation(config, sources)
	if err != nil {
		return err
	}
	rendered, err := renderQwenDocument(
		sources.user,
		"",
		paths.runtimeRoot,
		qwenInstalledGroups(nodePath, paths.guardPath, paths.auditPath),
	)
	if err != nil {
		return err
	}
	prepared, err := prepareQwenInstallation(config, sources, rendered)
	if err != nil {
		return err
	}
	if err := commitQwenInstallation(prepared, p.rename); err != nil {
		return err
	}
	fmt.Printf("Qwen Code hooks: %s\n", sources.user.filePath)
	fmt.Println("Qwen Code verification: run /hooks.")
	return nil
}

func (p *QwenPlugin) Uninstall(agentID string) error {
	sources, err := readQwenSources()
	if err != nil {
		return err
	}
	runtimeRoot, err := qwenRuntimeRoot()
	if err != nil {
		return err
	}
	rendered, err := renderQwenDocument(
		sources.user,
		agentID,
		runtimeRoot,
		map[string]map[string]any{},
	)
	if err != nil {
		return err
	}
	if !rendered.changed {
		return nil
	}
	change, preconditions, err := prepareQwenUninstall(sources, rendered)
	if err != nil {
		return err
	}
	return commitQwenUninstall(change, preconditions, p.rename)
}

func (p *QwenPlugin) Status() (PluginStatus, error) {
	entry := SupportedAgents[qwenAgentKey]
	status := PluginStatus{AgentName: qwenAgentKey, DisplayName: entry.Name}
	sources, err := readQwenSources()
	if err != nil {
		return status, err
	}
	status.ConfigPath = sources.user.filePath
	runtimeRoot, err := qwenRuntimeRoot()
	if err != nil {
		return status, err
	}
	contracts := qwenRuntimeContracts(sources.user.hooks, runtimeRoot)
	status.HookConfigured = !sources.disableControl.disabled && len(contracts) > 0
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
