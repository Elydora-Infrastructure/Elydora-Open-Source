package plugins

import "fmt"

// KiroCliPlugin manages the stable v2 custom-agent and early-access v3 hook contracts.
type KiroCliPlugin struct{}

func (p *KiroCliPlugin) Install(config InstallConfig) error {
	if config.AgentID == "" {
		return fmt.Errorf("agent ID is required")
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

	v2Path, v3Path, err := kiroConfigPaths()
	if err != nil {
		return err
	}
	v2Settings, _, err := readHookJSONObject(v2Path, "Kiro CLI v2 agent config")
	if err != nil {
		return err
	}
	v3Settings, _, err := readHookJSONObject(v3Path, "Kiro CLI v3 hooks config")
	if err != nil {
		return err
	}
	v2Hooks, err := kiroHooksObject(v2Settings, "Kiro CLI v2 agent config")
	if err != nil {
		return err
	}
	currentV3Hooks, err := kiroV3Hooks(v3Settings)
	if err != nil {
		return err
	}

	scriptPath, err := hookScriptPath(config.AgentID)
	if err != nil {
		return err
	}
	if config.HookScript != "" {
		scriptPath = config.HookScript
	}
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		return err
	}

	preToolUse, err := kiroHookEntries(v2Hooks, "preToolUse", "Kiro CLI v2 agent config")
	if err != nil {
		return err
	}
	postToolUse, err := kiroHookEntries(v2Hooks, "postToolUse", "Kiro CLI v2 agent config")
	if err != nil {
		return err
	}
	v2Hooks["preToolUse"] = append(
		withoutKiroV2Hooks(preToolUse, ""),
		buildKiroV2Hook(nodePath, config.GuardScriptPath),
	)
	v2Hooks["postToolUse"] = append(
		withoutKiroV2Hooks(postToolUse, ""),
		buildKiroV2Hook(nodePath, scriptPath),
	)

	nextV2 := map[string]any{
		"name":           kiroV2AgentName,
		"description":    kiroV2Description,
		"tools":          []any{"*"},
		"includeMcpJson": true,
	}
	copyKiroObject(nextV2, v2Settings)
	nextV2["hooks"] = v2Hooks

	nextV3 := cloneKiroObject(v3Settings)
	nextV3["version"] = "v1"
	v3Hooks := make([]any, 0, len(currentV3Hooks)+2)
	for _, hook := range currentV3Hooks {
		if !isManagedKiroV3Hook(hook, "") {
			v3Hooks = append(v3Hooks, hook)
		}
	}
	v3Hooks = append(
		v3Hooks,
		buildKiroV3Hook(
			kiroV3GuardName,
			"Block tool use when the Elydora agent is frozen",
			"PreToolUse",
			nodePath,
			config.GuardScriptPath,
		),
		buildKiroV3Hook(
			kiroV3AuditName,
			"Record tool use in the Elydora audit trail",
			"PostToolUse",
			nodePath,
			scriptPath,
		),
	)
	nextV3["hooks"] = v3Hooks

	if err := GenerateHookScript(scriptPath, config); err != nil {
		return fmt.Errorf("generate hook script: %w", err)
	}
	if err := writeHookJSONObjectAtomic(v2Path, nextV2); err != nil {
		return err
	}
	if err := writeHookJSONObjectAtomic(v3Path, nextV3); err != nil {
		return err
	}

	fmt.Println(`Kiro CLI v2: start with "kiro-cli --agent elydora-audit".`)
	fmt.Println(`Kiro CLI v3: start with "kiro-cli --v3"; global hooks load automatically.`)
	return nil
}

func (p *KiroCliPlugin) Uninstall(agentID string) error {
	v2Path, v3Path, err := kiroConfigPaths()
	if err != nil {
		return err
	}
	v2Settings, v2Exists, err := readHookJSONObject(v2Path, "Kiro CLI v2 agent config")
	if err != nil {
		return err
	}
	v3Settings, v3Exists, err := readHookJSONObject(v3Path, "Kiro CLI v3 hooks config")
	if err != nil {
		return err
	}

	v2Mutation, err := prepareKiroV2Uninstall(v2Path, v2Settings, v2Exists, agentID)
	if err != nil {
		return err
	}
	v3Mutation, err := prepareKiroV3Uninstall(v3Path, v3Settings, v3Exists, agentID)
	if err != nil {
		return err
	}
	if err := applyKiroMutation(v2Mutation); err != nil {
		return err
	}
	if err := applyKiroMutation(v3Mutation); err != nil {
		return err
	}

	fmt.Println("Uninstalled Elydora hooks from Kiro CLI.")
	return nil
}

func (p *KiroCliPlugin) Status() (PluginStatus, error) {
	v2Path, v3Path, err := kiroConfigPaths()
	if err != nil {
		return PluginStatus{}, err
	}
	status := PluginStatus{
		AgentName:   kiroAgentKey,
		DisplayName: "Kiro CLI",
		ConfigPath:  v3Path,
	}
	v2Settings, v2Exists, err := readHookJSONObject(v2Path, "Kiro CLI v2 agent config")
	if err != nil {
		return status, err
	}
	v3Settings, v3Exists, err := readHookJSONObject(v3Path, "Kiro CLI v3 hooks config")
	if err != nil {
		return status, err
	}

	contracts, err := configuredKiroContracts(
		v2Path,
		v2Settings,
		v2Exists,
		v3Path,
		v3Settings,
		v3Exists,
	)
	if err != nil {
		return status, err
	}
	if len(contracts) == 0 {
		return status, nil
	}
	status.ConfigPath = contracts[len(contracts)-1].configPath
	status.HookConfigured = true
	status.HookScriptExists, err = kiroRuntimeScriptsExist(contracts)
	if err != nil {
		return status, err
	}
	status.Installed = status.HookScriptExists
	return status, nil
}
