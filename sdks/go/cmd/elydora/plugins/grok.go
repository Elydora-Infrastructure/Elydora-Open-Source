package plugins

import "fmt"

// GrokPlugin manages Grok Build native global user hooks.
type GrokPlugin struct{}

func (p *GrokPlugin) Install(config InstallConfig) error {
	if config.AgentID == "" {
		return fmt.Errorf("agent ID is required")
	}
	document, err := readGrokConfig()
	if err != nil {
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
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		return err
	}
	runtimeRoot, err := grokRuntimeRoot()
	if err != nil {
		return err
	}
	hooks, _ := removeManagedGrokHooks(document.hooks, "", runtimeRoot)
	hooks["PreToolUse"] = append(hooks["PreToolUse"], buildGrokGroup(
		buildGrokHandler(buildGrokCommand(nodePath, config.GuardScriptPath)),
	))
	hooks["PostToolUse"] = append(hooks["PostToolUse"], buildGrokGroup(
		buildGrokHandler(buildGrokCommand(nodePath, hookPath)),
	))
	next := cloneGrokObject(document.root)
	next["hooks"] = renderGrokHooks(hooks)

	runtimeConfig := config
	runtimeConfig.AgentName = grokAgentKey
	if err := GenerateHookScript(hookPath, runtimeConfig); err != nil {
		return fmt.Errorf("generate hook script: %w", err)
	}
	if err := writeHookJSONObjectAtomic(document.configPath, next); err != nil {
		return fmt.Errorf("write Grok hooks config: %w", err)
	}
	fmt.Println("Grok Build: global PreToolUse and PostToolUse hooks installed.")
	return nil
}

func (p *GrokPlugin) Uninstall(agentID string) error {
	document, err := readGrokConfig()
	if err != nil || !document.exists {
		return err
	}
	runtimeRoot, err := grokRuntimeRoot()
	if err != nil {
		return err
	}
	hooks, changed := removeManagedGrokHooks(document.hooks, agentID, runtimeRoot)
	if !changed {
		return nil
	}
	next := cloneGrokObject(document.root)
	if len(hooks) == 0 {
		delete(next, "hooks")
	} else {
		next["hooks"] = renderGrokHooks(hooks)
	}
	if len(next) == 0 {
		return removeHookFile(document.configPath, "Grok hooks config")
	}
	return writeHookJSONObjectAtomic(document.configPath, next)
}

func (p *GrokPlugin) Status() (PluginStatus, error) {
	document, err := readGrokConfig()
	status := PluginStatus{
		AgentName:   grokAgentKey,
		DisplayName: "Grok Build",
		ConfigPath:  document.configPath,
	}
	if err != nil {
		return status, err
	}
	runtimeRoot, err := grokRuntimeRoot()
	if err != nil {
		return status, err
	}
	contracts := grokRuntimeContracts(document.hooks, runtimeRoot)
	if len(contracts) == 0 {
		return status, nil
	}
	status.HookConfigured = true
	status.HookScriptExists, err = grokRuntimeScriptsExist(contracts, runtimeRoot)
	if err != nil {
		return status, err
	}
	status.Installed = status.HookScriptExists
	return status, nil
}
