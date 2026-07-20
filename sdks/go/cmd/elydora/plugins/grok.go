package plugins

import (
	"fmt"
	"path/filepath"
)

// GrokPlugin manages Grok global user hooks.
type GrokPlugin struct {
	rename renameFunc
}

// ManagesGuardRuntime reports that Grok commits its provider guard with the
// audit runtime and user hook document.
func (p *GrokPlugin) ManagesGuardRuntime() bool {
	return true
}

// PreflightInstall validates the hook source and runtime identity before any
// directory or file is created.
func (p *GrokPlugin) PreflightInstall(config InstallConfig) error {
	document, err := readGrokDocument()
	if err != nil {
		return err
	}
	_, _, err = preflightGrokInstallation(config, document)
	return err
}

func (p *GrokPlugin) Install(config InstallConfig) error {
	document, err := readGrokDocument()
	if err != nil {
		return err
	}
	paths, nodePath, err := preflightGrokInstallation(config, document)
	if err != nil {
		return err
	}
	guardCommand, err := buildGrokCommand(nodePath, paths.guardPath)
	if err != nil {
		return err
	}
	auditCommand, err := buildGrokCommand(nodePath, paths.auditPath)
	if err != nil {
		return err
	}
	hooks, err := removeManagedGrokHooks(document.hooks, "")
	if err != nil {
		return err
	}
	for _, item := range []struct{ event, command string }{
		{"PreToolUse", guardCommand},
		{"PostToolUse", auditCommand},
		{"PostToolUseFailure", auditCommand},
	} {
		hooks[item.event] = append(hooks[item.event], buildGrokGroup(item.command))
	}
	rendered, err := renderGrokDocument(document, hooks)
	if err != nil {
		return fmt.Errorf("render Grok user hooks: %w", err)
	}
	changes, err := prepareGrokInstallationChanges(config, paths, rendered)
	if err != nil {
		return err
	}
	if err := writeGrokChanges(
		changes,
		"Install Grok hooks",
		p.rename,
		paths.runtimeRoot,
		paths.agentDirectory,
		filepath.Dir(document.configPath),
	); err != nil {
		return err
	}
	fmt.Println(
		"Grok Build: global PreToolUse, PostToolUse, and PostToolUseFailure hooks installed.",
	)
	return nil
}

func (p *GrokPlugin) Uninstall(agentID string) error {
	document, err := readGrokDocument()
	if err != nil {
		return err
	}
	hooks, err := removeManagedGrokHooks(document.hooks, agentID)
	if err != nil {
		return err
	}
	rendered, err := renderGrokDocument(document, hooks)
	if err != nil {
		return fmt.Errorf("render Grok user hooks: %w", err)
	}
	change, err := prepareRenderedGrokChange(rendered)
	if err != nil {
		return err
	}
	return writeGrokChanges(
		[]*fileChange{change},
		"Uninstall Grok hooks",
		p.rename,
		"",
		"",
		filepath.Dir(document.configPath),
	)
}

func (p *GrokPlugin) Status() (PluginStatus, error) {
	configPath, pathErr := grokConfigPath()
	entry := SupportedAgents[grokAgentKey]
	status := PluginStatus{
		AgentName: grokAgentKey, DisplayName: entry.Name, ConfigPath: configPath,
	}
	if pathErr != nil {
		return status, pathErr
	}
	document, err := readGrokDocument()
	if err != nil {
		return status, err
	}
	contracts, err := grokRuntimeContracts(document.hooks)
	if err != nil {
		return status, err
	}
	status.HookConfigured = len(contracts) > 0
	if !status.HookConfigured {
		return status, nil
	}
	status.HookScriptExists, err = grokRuntimeFilesExist(contracts)
	if err != nil {
		return status, err
	}
	status.Installed = status.HookScriptExists
	return status, nil
}
