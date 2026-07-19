package plugins

import "fmt"

// ClinePlugin manages Cline's native global file hooks.
type ClinePlugin struct{}

func (p *ClinePlugin) Install(config InstallConfig) error {
	if config.AgentID == "" {
		return fmt.Errorf("agent ID is required")
	}
	paths, err := resolveClineHookFiles()
	if err != nil {
		return err
	}
	guardState, err := readClineHookFile(paths.guardPath)
	if err != nil {
		return err
	}
	auditState, err := readClineHookFile(paths.auditPath)
	if err != nil {
		return err
	}
	if err := requireAvailableClineHook(guardState); err != nil {
		return err
	}
	if err := requireAvailableClineHook(auditState); err != nil {
		return err
	}
	if config.GuardScriptPath == "" {
		return fmt.Errorf("guard script path is required")
	}
	guardExists, err := regularFileExists(config.GuardScriptPath, "Elydora guard runtime")
	if err != nil {
		return err
	}
	if !guardExists {
		return fmt.Errorf("Elydora guard runtime is missing: %s", config.GuardScriptPath)
	}
	hookPath, err := hookScriptPath(config.AgentID)
	if err != nil {
		return err
	}
	if config.HookScript != "" {
		hookPath = config.HookScript
	}
	guardMetadata, err := buildClineMetadata("guard", config.AgentID, config.GuardScriptPath)
	if err != nil {
		return fmt.Errorf("build Cline guard metadata: %w", err)
	}
	auditMetadata, err := buildClineMetadata("audit", config.AgentID, hookPath)
	if err != nil {
		return fmt.Errorf("build Cline audit metadata: %w", err)
	}
	guardSource, err := buildClineWrapper(guardMetadata)
	if err != nil {
		return fmt.Errorf("build Cline guard wrapper: %w", err)
	}
	auditSource, err := buildClineWrapper(auditMetadata)
	if err != nil {
		return fmt.Errorf("build Cline audit wrapper: %w", err)
	}
	if _, err := clineContractForFiles(
		clineHookFile{
			exists: true, filePath: paths.guardPath,
			source: guardSource, metadata: &guardMetadata,
		},
		clineHookFile{
			exists: true, filePath: paths.auditPath,
			source: auditSource, metadata: &auditMetadata,
		},
	); err != nil {
		return err
	}
	runtimeConfig := config
	runtimeConfig.AgentName = clineAgentKey
	if err := GenerateHookScript(hookPath, runtimeConfig); err != nil {
		return fmt.Errorf("generate hook script: %w", err)
	}
	if err := writeClineHookPair(
		clinePendingWrite{state: guardState, source: guardSource},
		clinePendingWrite{state: auditState, source: auditSource},
	); err != nil {
		return err
	}
	fmt.Println("Cline: user-level PreToolUse and PostToolUse hooks installed.")
	return nil
}

func (p *ClinePlugin) Uninstall(agentID string) error {
	paths, err := resolveClineHookFiles()
	if err != nil {
		return err
	}
	guardState, err := readClineHookFile(paths.guardPath)
	if err != nil {
		return err
	}
	auditState, err := readClineHookFile(paths.auditPath)
	if err != nil {
		return err
	}
	return removeOwnedClineHooks([]clineHookFile{guardState, auditState}, agentID)
}

func (p *ClinePlugin) Status() (PluginStatus, error) {
	paths, err := resolveClineHookFiles()
	status := PluginStatus{
		AgentName:   clineAgentKey,
		DisplayName: "Cline",
		ConfigPath:  paths.hooksDirectory,
	}
	if err != nil {
		return status, err
	}
	guardState, err := readClineHookFile(paths.guardPath)
	if err != nil {
		return status, err
	}
	auditState, err := readClineHookFile(paths.auditPath)
	if err != nil {
		return status, err
	}
	contract, err := clineContractForFiles(guardState, auditState)
	if err != nil || contract == nil {
		return status, err
	}
	status.HookConfigured = true
	status.HookScriptExists, err = clineRuntimeFilesExist(contract)
	if err != nil {
		return status, err
	}
	status.Installed = status.HookScriptExists
	return status, nil
}
