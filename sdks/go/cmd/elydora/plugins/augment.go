package plugins

import "fmt"

// AugmentPlugin manages Auggie native global user hooks.
type AugmentPlugin struct{}

func (p *AugmentPlugin) Install(config InstallConfig) error {
	if config.AgentID == "" {
		return fmt.Errorf("agent ID is required")
	}
	document, err := readAugmentConfig()
	if err != nil {
		return err
	}
	if config.GuardScriptPath == "" {
		return fmt.Errorf("guard script path is required")
	}
	guardExists, err := regularFileExists(
		config.GuardScriptPath, "Elydora guard runtime",
	)
	if err != nil {
		return err
	}
	if !guardExists {
		return fmt.Errorf(
			"Elydora guard runtime is missing: %s", config.GuardScriptPath,
		)
	}
	hookPath, err := hookScriptPath(config.AgentID)
	if err != nil {
		return err
	}
	if config.HookScript != "" {
		hookPath = config.HookScript
	}
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		return err
	}
	if err := validateAugmentMatchers(document.hooks, nodePath); err != nil {
		return err
	}
	runtimeRoot, err := augmentRuntimeRoot()
	if err != nil {
		return err
	}
	wrappers := resolveAugmentWrapperPaths(runtimeRoot, config.AgentID)
	hooks, _ := removeManagedAugmentHooks(document.hooks, "", runtimeRoot)
	hooks["PreToolUse"] = append(
		hooks["PreToolUse"],
		buildAugmentGroup(buildAugmentHandler(wrappers.guard)),
	)
	hooks["PostToolUse"] = append(
		hooks["PostToolUse"],
		buildAugmentGroup(buildAugmentHandler(wrappers.audit)),
	)
	next := cloneAugmentObject(document.root)
	next["hooks"] = renderAugmentHooks(hooks)

	runtimeConfig := config
	runtimeConfig.AgentName = augmentAgentKey
	if err := GenerateHookScript(hookPath, runtimeConfig); err != nil {
		return fmt.Errorf("generate hook script: %w", err)
	}
	if err := writeHookFileAtomic(
		wrappers.guard,
		buildAugmentWrapper(nodePath, config.GuardScriptPath),
		0700,
	); err != nil {
		return fmt.Errorf("write Auggie guard wrapper: %w", err)
	}
	if err := writeHookFileAtomic(
		wrappers.audit,
		buildAugmentWrapper(nodePath, hookPath),
		0700,
	); err != nil {
		return fmt.Errorf("write Auggie audit wrapper: %w", err)
	}
	if err := writeHookJSONObjectAtomic(document.configPath, next); err != nil {
		return fmt.Errorf("write Auggie settings: %w", err)
	}
	fmt.Println("Auggie: user-level PreToolUse and PostToolUse hooks installed.")
	return nil
}

func (p *AugmentPlugin) Uninstall(agentID string) error {
	document, err := readAugmentConfig()
	if err != nil || !document.exists {
		return err
	}
	runtimeRoot, err := augmentRuntimeRoot()
	if err != nil {
		return err
	}
	hooks, changed := removeManagedAugmentHooks(
		document.hooks, agentID, runtimeRoot,
	)
	if !changed {
		return nil
	}
	next := cloneAugmentObject(document.root)
	if len(hooks) == 0 {
		delete(next, "hooks")
	} else {
		next["hooks"] = renderAugmentHooks(hooks)
	}
	if len(next) == 0 {
		return removeHookFile(document.configPath, "Auggie settings")
	}
	return writeHookJSONObjectAtomic(document.configPath, next)
}

func (p *AugmentPlugin) Status() (PluginStatus, error) {
	document, err := readAugmentConfig()
	status := PluginStatus{
		AgentName:   augmentAgentKey,
		DisplayName: "Augment Code CLI",
		ConfigPath:  document.configPath,
	}
	if err != nil {
		return status, err
	}
	runtimeRoot, err := augmentRuntimeRoot()
	if err != nil {
		return status, err
	}
	contracts := augmentRuntimeContracts(document.hooks, runtimeRoot)
	if len(contracts) == 0 {
		return status, nil
	}
	status.HookConfigured = true
	status.HookScriptExists, err = augmentRuntimeFilesExist(
		contracts, runtimeRoot,
	)
	if err != nil {
		return status, err
	}
	status.Installed = status.HookScriptExists
	return status, nil
}
