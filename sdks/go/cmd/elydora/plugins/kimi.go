package plugins

import (
	"fmt"
)

// KimiPlugin manages Kimi Code and legacy kimi-cli global lifecycle hooks.
type KimiPlugin struct {
	rename renameFunc
}

// ManagesGuardRuntime reports that Kimi commits its provider guard with the
// audit runtime and every detected user hook document.
func (p *KimiPlugin) ManagesGuardRuntime() bool {
	return true
}

// PreflightInstall validates every detected hook source and runtime identity.
func (p *KimiPlugin) PreflightInstall(config InstallConfig) error {
	documents, err := readAllKimiConfigs()
	if err != nil {
		return err
	}
	_, _, err = preflightKimiInstallation(config, documents)
	return err
}

func (p *KimiPlugin) Install(config InstallConfig) error {
	documents, err := readAllKimiConfigs()
	if err != nil {
		return err
	}
	paths, nodePath, err := preflightKimiInstallation(config, documents)
	if err != nil {
		return err
	}
	guardCommand, err := buildKimiCommand(nodePath, paths.guardPath)
	if err != nil {
		return err
	}
	auditCommand, err := buildKimiCommand(nodePath, paths.auditPath)
	if err != nil {
		return err
	}
	additions := make([]kimiHook, 0, 3)
	for _, item := range []struct{ event, command string }{
		{"PreToolUse", guardCommand},
		{"PostToolUse", auditCommand},
		{"PostToolUseFailure", auditCommand},
	} {
		hook, err := buildKimiHook(item.event, item.command)
		if err != nil {
			return err
		}
		additions = append(additions, hook)
	}
	rendered := make([]kimiRenderedDocument, 0, len(documents))
	for _, document := range documents {
		keep, err := keptKimiHookIndices(document.hooks, "")
		if err != nil {
			return err
		}
		change, err := renderKimiChange(document, keep, additions)
		if err != nil {
			return err
		}
		rendered = append(rendered, change)
	}
	changes, err := prepareKimiInstallationChanges(config, paths, rendered)
	if err != nil {
		return err
	}
	if err := writeKimiChanges(
		changes,
		"Install Kimi hooks",
		p.rename,
		paths.runtimeRoot,
		paths.agentDirectory,
	); err != nil {
		return err
	}
	fmt.Printf(
		"%s: global PreToolUse, PostToolUse, and PostToolUseFailure hooks installed.\n",
		kimiRuntimeNames(documents),
	)
	return nil
}

func (p *KimiPlugin) Uninstall(agentID string) error {
	documents, err := readAllKimiConfigs()
	if err != nil {
		return err
	}
	rendered := make([]kimiRenderedDocument, 0, len(documents))
	for _, document := range documents {
		keep, err := keptKimiHookIndices(document.hooks, agentID)
		if err != nil {
			return err
		}
		change, err := renderKimiChange(document, keep, nil)
		if err != nil {
			return err
		}
		rendered = append(rendered, change)
	}
	changes, err := prepareKimiUninstallChanges(rendered)
	if err != nil {
		return err
	}
	return writeKimiChanges(
		changes,
		"Uninstall Kimi hooks",
		p.rename,
		"",
		"",
	)
}

func (p *KimiPlugin) Status() (PluginStatus, error) {
	documents, err := readAllKimiConfigs()
	entry := SupportedAgents[kimiAgentKey]
	status := PluginStatus{
		AgentName: kimiAgentKey, DisplayName: entry.Name,
	}
	if err != nil {
		return status, err
	}
	status.ConfigPath = documents[0].contract.configPath
	contracts, err := kimiRuntimeContracts(documents)
	if err != nil {
		return status, err
	}
	if len(contracts) == 0 {
		return status, nil
	}
	status.ConfigPath = contracts[len(contracts)-1].configPath
	status.HookConfigured = true
	status.HookScriptExists, err = kimiRuntimeFilesExist(contracts)
	if err != nil {
		return status, err
	}
	status.Installed = status.HookScriptExists
	return status, nil
}
