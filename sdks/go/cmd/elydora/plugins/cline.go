package plugins

import "fmt"

// ClinePlugin manages Cline's native global file hooks.
type ClinePlugin struct {
	rename renameFunc
}

// ManagesGuardRuntime reports that Cline commits both generated runtimes and
// both native hook files in one transaction.
func (p *ClinePlugin) ManagesGuardRuntime() bool {
	return true
}

// PreflightInstall validates hook ownership and runtime identity before writes.
func (p *ClinePlugin) PreflightInstall(config InstallConfig) error {
	paths, guard, audit, err := readClineHookPair()
	if err != nil {
		return err
	}
	if err := requireAvailableClineHook(guard); err != nil {
		return err
	}
	if err := requireAvailableClineHook(audit); err != nil {
		return err
	}
	_, err = preflightClineInstallation(config, paths)
	return err
}

func (p *ClinePlugin) Install(config InstallConfig) error {
	paths, guard, audit, err := readClineHookPair()
	if err != nil {
		return err
	}
	if err := requireAvailableClineHook(guard); err != nil {
		return err
	}
	if err := requireAvailableClineHook(audit); err != nil {
		return err
	}
	runtimePaths, err := preflightClineInstallation(config, paths)
	if err != nil {
		return err
	}
	changes, err := prepareClineInstallationChanges(
		config,
		runtimePaths,
		guard,
		audit,
	)
	if err != nil {
		return err
	}
	if err := writeClineChanges(
		changes,
		"Install Cline hooks",
		p.rename,
		runtimePaths,
	); err != nil {
		return err
	}
	fmt.Println("Cline: user-level PreToolUse and PostToolUse hooks installed.")
	return nil
}

func (p *ClinePlugin) Uninstall(agentID string) error {
	paths, guard, audit, err := readClineHookPair()
	if err != nil {
		return err
	}
	changes, err := prepareClineUninstallChanges(
		[]clineHookFile{guard, audit},
		agentID,
	)
	if err != nil {
		return err
	}
	return writeClineHookChanges(
		changes,
		"Uninstall Cline hooks",
		p.rename,
		paths,
	)
}

func (p *ClinePlugin) Status() (PluginStatus, error) {
	paths, guard, audit, err := readClineHookPair()
	status := PluginStatus{
		AgentName:   clineAgentKey,
		DisplayName: "Cline",
		ConfigPath:  paths.hooksDirectory,
	}
	if err != nil {
		return status, err
	}
	contract, err := clineContractForFiles(guard, audit)
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
