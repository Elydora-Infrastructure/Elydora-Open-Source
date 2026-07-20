package plugins

import (
	"fmt"
	"path/filepath"
)

// AugmentPlugin manages Auggie native global user hooks.
type AugmentPlugin struct {
	rename renameFunc
}

// ManagesGuardRuntime reports that Auggie commits its provider guard together
// with both wrappers, the audit runtime, and user settings.
func (p *AugmentPlugin) ManagesGuardRuntime() bool {
	return true
}

// PreflightInstall validates settings and runtime identity before any write.
func (p *AugmentPlugin) PreflightInstall(config InstallConfig) error {
	document, err := readAugmentDocument()
	if err != nil {
		return err
	}
	_, _, err = preflightAugmentInstallation(config, document)
	return err
}

func (p *AugmentPlugin) Install(config InstallConfig) error {
	document, err := readAugmentDocument()
	if err != nil {
		return err
	}
	paths, nodePath, err := preflightAugmentInstallation(config, document)
	if err != nil {
		return err
	}
	hooks, _ := removeManagedAugmentHooks(document.hooks, "", paths.runtimeRoot)
	hooks["PreToolUse"] = append(
		hooks["PreToolUse"],
		buildAugmentGroup(buildAugmentHandler(paths.guardWrapperPath)),
	)
	hooks["PostToolUse"] = append(
		hooks["PostToolUse"],
		buildAugmentGroup(buildAugmentHandler(paths.auditWrapperPath)),
	)
	rendered, err := renderAugmentDocument(document, hooks)
	if err != nil {
		return fmt.Errorf("render Auggie user settings: %w", err)
	}
	changes, err := prepareAugmentInstallationChanges(
		config,
		paths,
		nodePath,
		rendered,
	)
	if err != nil {
		return err
	}
	if err := writeAugmentChanges(
		changes,
		"Install Augment Code CLI hooks",
		p.rename,
		paths.runtimeRoot,
		paths.agentDirectory,
		filepath.Dir(document.configPath),
	); err != nil {
		return err
	}
	fmt.Println("Auggie: user-level PreToolUse and PostToolUse hooks installed.")
	return nil
}

func (p *AugmentPlugin) Uninstall(agentID string) error {
	document, err := readAugmentDocument()
	if err != nil {
		return err
	}
	if !document.exists {
		return nil
	}
	runtimeRoot, err := AgentRuntimeRoot()
	if err != nil {
		return err
	}
	hooks, changed := removeManagedAugmentHooks(
		document.hooks,
		agentID,
		runtimeRoot,
	)
	if !changed {
		return nil
	}
	rendered, err := renderAugmentDocument(document, hooks)
	if err != nil {
		return fmt.Errorf("render Auggie user settings: %w", err)
	}
	change, err := prepareRenderedAugmentChange(rendered)
	if err != nil {
		return err
	}
	return writeAugmentChanges(
		[]*fileChange{change},
		"Uninstall Augment Code CLI hooks",
		p.rename,
		"",
		"",
		filepath.Dir(document.configPath),
	)
}

func (p *AugmentPlugin) Status() (PluginStatus, error) {
	configPath, pathErr := augmentConfigPath()
	entry := SupportedAgents[augmentAgentKey]
	status := PluginStatus{
		AgentName: augmentAgentKey, DisplayName: entry.Name, ConfigPath: configPath,
	}
	if pathErr != nil {
		return status, pathErr
	}
	document, err := readAugmentDocument()
	if err != nil {
		return status, err
	}
	runtimeRoot, err := AgentRuntimeRoot()
	if err != nil {
		return status, err
	}
	contracts := augmentRuntimeContracts(document.hooks, runtimeRoot)
	status.HookConfigured = len(contracts) > 0
	if !status.HookConfigured {
		return status, nil
	}
	status.HookScriptExists, err = augmentRuntimeFilesExist(
		contracts,
		runtimeRoot,
	)
	if err != nil {
		return status, err
	}
	status.Installed = status.HookScriptExists
	return status, nil
}
