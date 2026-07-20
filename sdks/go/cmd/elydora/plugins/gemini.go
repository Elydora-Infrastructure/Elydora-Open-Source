package plugins

import (
	"fmt"
	"path/filepath"
)

// GeminiPlugin manages Gemini CLI global user hooks.
type GeminiPlugin struct {
	rename renameFunc
}

// ManagesGuardRuntime reports that Gemini commits both generated runtimes and
// the user settings document through one transaction.
func (p *GeminiPlugin) ManagesGuardRuntime() bool {
	return true
}

func requireGeminiHooksEnabled(document *geminiDocument) error {
	if !document.hookControls.enabled {
		return fmt.Errorf(
			"Gemini CLI hooks are disabled by hooksConfig.enabled: %s",
			document.filePath,
		)
	}
	disabled, err := disabledManagedGeminiEntries(document.hookControls)
	if err != nil {
		return err
	}
	if len(disabled) > 0 {
		return fmt.Errorf(
			"Gemini CLI hooks are disabled by hooksConfig.disabled: %v",
			disabled,
		)
	}
	return nil
}

// PreflightInstall validates settings and runtime identity before any write.
func (p *GeminiPlugin) PreflightInstall(config InstallConfig) error {
	document, err := readGeminiDocument()
	if err != nil {
		return err
	}
	if err := requireGeminiHooksEnabled(document); err != nil {
		return err
	}
	_, _, err = preflightGeminiInstallation(config, document)
	return err
}

func (p *GeminiPlugin) Install(config InstallConfig) error {
	document, err := readGeminiDocument()
	if err != nil {
		return err
	}
	if err := requireGeminiHooksEnabled(document); err != nil {
		return err
	}
	paths, nodePath, err := preflightGeminiInstallation(config, document)
	if err != nil {
		return err
	}
	guardGroup, err := buildGeminiGroup(
		nodePath,
		paths.guardPath,
		geminiGuardHookName,
	)
	if err != nil {
		return err
	}
	auditGroup, err := buildGeminiGroup(
		nodePath,
		paths.auditPath,
		geminiAuditHookName,
	)
	if err != nil {
		return err
	}
	rendered, err := renderGeminiDocument(
		document,
		"",
		map[string]map[string]any{
			"BeforeTool": guardGroup,
			"AfterTool":  auditGroup,
		},
	)
	if err != nil {
		return fmt.Errorf("render Gemini CLI user settings: %w", err)
	}
	changes, err := prepareGeminiInstallationChanges(config, paths, rendered)
	if err != nil {
		return err
	}
	if err := writeGeminiChanges(
		changes,
		"Install Gemini CLI hooks",
		p.rename,
		paths.runtimeRoot,
		paths.agentDirectory,
		filepath.Dir(document.filePath),
	); err != nil {
		return err
	}
	fmt.Printf("Gemini CLI hooks installed at %s.\n", document.filePath)
	fmt.Println("Gemini CLI verification: run /hooks list.")
	return nil
}

func (p *GeminiPlugin) Uninstall(agentID string) error {
	document, err := readGeminiDocument()
	if err != nil {
		return err
	}
	rendered, err := renderGeminiDocument(document, agentID, nil)
	if err != nil {
		return fmt.Errorf("render Gemini CLI user settings: %w", err)
	}
	change, err := prepareRenderedGeminiChange(rendered)
	if err != nil {
		return err
	}
	return writeGeminiChanges(
		[]*fileChange{change},
		"Uninstall Gemini CLI hooks",
		p.rename,
		"",
		"",
		filepath.Dir(document.filePath),
	)
}

func (p *GeminiPlugin) Status() (PluginStatus, error) {
	configPath, pathErr := geminiSettingsPath()
	entry := SupportedAgents[geminiAgentKey]
	status := PluginStatus{
		AgentName: geminiAgentKey, DisplayName: entry.Name, ConfigPath: configPath,
	}
	if pathErr != nil {
		return status, pathErr
	}
	document, err := readGeminiDocument()
	if err != nil {
		return status, err
	}
	if !managedGeminiHooksEnabled(document.hookControls) {
		return status, nil
	}
	contracts, err := geminiRuntimeContracts(document.hooks)
	if err != nil {
		return status, err
	}
	status.HookConfigured = len(contracts) > 0
	if !status.HookConfigured {
		return status, nil
	}
	status.HookScriptExists, err = geminiRuntimeFilesExist(contracts)
	if err != nil {
		return status, err
	}
	status.Installed = status.HookScriptExists
	return status, nil
}
