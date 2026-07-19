package plugins

import "fmt"

// CodexPlugin manages OpenAI Codex user lifecycle hooks.
type CodexPlugin struct{}

func (p *CodexPlugin) Install(config InstallConfig) error {
	if config.AgentID == "" {
		return fmt.Errorf("agent ID is required")
	}
	configPath, err := codexConfigPath()
	if err != nil {
		return err
	}
	settings, exists, err := readHookJSONObject(configPath, "Codex hooks config")
	if err != nil {
		return err
	}
	if !exists {
		settings = map[string]any{"description": codexOwnedDescription}
	}
	hooks, err := codexHooksObject(settings)
	if err != nil {
		return err
	}
	preGroups, err := codexEventGroups(hooks, "PreToolUse")
	if err != nil {
		return err
	}
	postGroups, err := codexEventGroups(hooks, "PostToolUse")
	if err != nil {
		return err
	}
	preGroups, _, err = withoutCodexHandlers(preGroups, "")
	if err != nil {
		return err
	}
	postGroups, _, err = withoutCodexHandlers(postGroups, "")
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

	hooks["PreToolUse"] = append(preGroups, codexMatcherGroup(
		codexHandler(nodePath, config.GuardScriptPath, codexGuardStatusMessage),
	))
	hooks["PostToolUse"] = append(postGroups, codexMatcherGroup(
		codexHandler(nodePath, hookPath, codexAuditStatusMessage),
	))
	next := cloneCodexObject(settings)
	next["hooks"] = hooks

	runtimeConfig := config
	runtimeConfig.AgentName = codexAgentKey
	if err := GenerateHookScript(hookPath, runtimeConfig); err != nil {
		return fmt.Errorf("generate hook script: %w", err)
	}
	if err := writeHookJSONObjectAtomic(configPath, next); err != nil {
		return fmt.Errorf("write Codex hooks config: %w", err)
	}
	fmt.Println("Codex: run /hooks to review and trust the Elydora hooks.")
	return nil
}

func (p *CodexPlugin) Uninstall(agentID string) error {
	configPath, err := codexConfigPath()
	if err != nil {
		return err
	}
	settings, exists, err := readHookJSONObject(configPath, "Codex hooks config")
	if err != nil || !exists {
		return err
	}
	hooks, err := codexHooksObject(settings)
	if err != nil {
		return err
	}
	preGroups, err := codexEventGroups(hooks, "PreToolUse")
	if err != nil {
		return err
	}
	postGroups, err := codexEventGroups(hooks, "PostToolUse")
	if err != nil {
		return err
	}
	filteredPre, preChanged, err := withoutCodexHandlers(preGroups, agentID)
	if err != nil {
		return err
	}
	filteredPost, postChanged, err := withoutCodexHandlers(postGroups, agentID)
	if err != nil {
		return err
	}
	if !preChanged && !postChanged {
		return nil
	}
	hooks["PreToolUse"] = filteredPre
	hooks["PostToolUse"] = filteredPost
	if codexSettingsOwned(settings, hooks) {
		return removeHookFile(configPath, "Codex hooks config")
	}
	next := cloneCodexObject(settings)
	next["hooks"] = hooks
	return writeHookJSONObjectAtomic(configPath, next)
}

func (p *CodexPlugin) Status() (PluginStatus, error) {
	configPath, err := codexConfigPath()
	status := PluginStatus{
		AgentName:   codexAgentKey,
		DisplayName: "OpenAI Codex",
		ConfigPath:  configPath,
	}
	if err != nil {
		return status, err
	}
	settings, exists, err := readHookJSONObject(configPath, "Codex hooks config")
	if err != nil || !exists {
		return status, err
	}
	hooks, err := codexHooksObject(settings)
	if err != nil {
		return status, err
	}
	guard, err := findCodexHandler(hooks, "PreToolUse", codexGuardStatusMessage)
	if err != nil {
		return status, err
	}
	audit, err := findCodexHandler(hooks, "PostToolUse", codexAuditStatusMessage)
	if err != nil {
		return status, err
	}
	if guard == nil || audit == nil {
		return status, nil
	}
	status.HookConfigured = true
	status.HookScriptExists, err = codexRuntimeScriptsExist(guard, audit)
	if err != nil {
		return status, err
	}
	status.Installed = status.HookScriptExists
	return status, nil
}
