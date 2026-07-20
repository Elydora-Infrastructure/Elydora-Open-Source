package plugins

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type DroidPlugin struct {
	rename renameFunc
}

type droidRuntimeConfig struct {
	OrgID     string `json:"org_id"`
	AgentID   string `json:"agent_id"`
	KID       string `json:"kid"`
	BaseURL   string `json:"base_url"`
	Token     string `json:"token"`
	AgentName string `json:"agent_name"`
}

func (p *DroidPlugin) Install(config InstallConfig) error {
	runtimeRoot, agentDirectory, err := droidAgentDirectory(config.AgentID)
	if err != nil {
		return err
	}
	sources, err := readDroidSources()
	if err != nil {
		return err
	}
	settings := []droidHookSettings{sources.settings.hooks}
	if sources.primary != nil {
		settings = append(settings, sources.primary.hooks)
	}
	if err := validateDroidRegexes(settings...); err != nil {
		return err
	}
	expectedGuard := filepath.Join(agentDirectory, droidGuardScript)
	if !sameDroidPath(config.GuardScriptPath, expectedGuard) {
		return fmt.Errorf("Elydora guard runtime must use the managed agent directory: %s", expectedGuard)
	}
	guardExists, err := regularFileExists(config.GuardScriptPath, "Elydora guard runtime")
	if err != nil {
		return err
	}
	if !guardExists {
		return fmt.Errorf("Elydora guard runtime is missing: %s", config.GuardScriptPath)
	}
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		return err
	}
	auditPath := filepath.Join(agentDirectory, droidAuditScript)
	selection, err := selectDroidInstallationTargets(sources)
	if err != nil {
		return err
	}
	groups := map[string]map[string]any{
		"PreToolUse":  buildDroidGroup(nodePath, config.GuardScriptPath),
		"PostToolUse": buildDroidGroup(nodePath, auditPath),
	}
	documents := uniqueDroidDocuments(
		sources.primary,
		droidSettingsDocument(sources.settings),
		selection.targets["PreToolUse"],
		selection.targets["PostToolUse"],
	)
	rendered := make([]*droidRenderedDocument, 0, len(documents))
	for _, document := range documents {
		result, renderErr := renderDroidDocument(
			document, "", runtimeRoot, droidAdditionsFor(document, selection.targets, groups),
		)
		if renderErr != nil {
			return renderErr
		}
		rendered = append(rendered, result)
	}
	changes, err := prepareDroidInstallationChanges(config, agentDirectory, auditPath, rendered)
	if err != nil {
		return err
	}
	if err := writeChanges(changes, "Write Factory Droid installation", p.rename); err != nil {
		return err
	}
	fmt.Printf(
		"  Factory Droid: PreToolUse: %s, PostToolUse: %s\n",
		selection.targets["PreToolUse"].filePath,
		selection.targets["PostToolUse"].filePath,
	)
	fmt.Println("  Factory Droid: run /hooks to review the Elydora hook changes.")
	return nil
}

func (p *DroidPlugin) Uninstall(agentID string) error {
	runtimeRoot, err := droidRuntimeRoot()
	if err != nil {
		return err
	}
	sources, err := readDroidSources()
	if err != nil {
		return err
	}
	documents := uniqueDroidDocuments(sources.primary, droidSettingsDocument(sources.settings))
	changes := make([]*fileChange, 0, len(documents))
	for _, document := range documents {
		rendered, renderErr := renderDroidDocument(document, agentID, runtimeRoot, nil)
		if renderErr != nil {
			return renderErr
		}
		change, changeErr := prepareRenderedDroidChange(rendered)
		if changeErr != nil {
			return changeErr
		}
		changes = append(changes, change)
	}
	return writeChanges(changes, "Write Factory Droid hook sources", p.rename)
}

func (p *DroidPlugin) Status() (PluginStatus, error) {
	entry := SupportedAgents[droidAgentKey]
	status := PluginStatus{
		AgentName: droidAgentKey, DisplayName: entry.Name,
	}
	sources, err := readDroidSources()
	if err != nil {
		return status, err
	}
	status.ConfigPath = displayDroidConfigPath(sources)
	primary := droidHookSettings{}
	if sources.primary != nil {
		primary = sources.primary.hooks
	}
	effective := mergeDroidSettings(primary, sources.settings.hooks)
	if disabled, _ := effective["hooksDisabled"].(bool); disabled {
		return status, nil
	}
	runtimeRoot, err := droidRuntimeRoot()
	if err != nil {
		return status, err
	}
	contracts := droidRuntimeContracts(effective, runtimeRoot)
	status.HookConfigured = len(contracts) > 0
	if !status.HookConfigured {
		return status, nil
	}
	status.HookScriptExists, err = droidRuntimeFilesExist(contracts)
	if err != nil {
		return status, err
	}
	status.Installed = status.HookScriptExists
	return status, nil
}

func droidAgentDirectory(agentID string) (string, string, error) {
	runtimeRoot, err := droidRuntimeRoot()
	if err != nil {
		return "", "", err
	}
	agentDirectory, err := ResolveAgentRuntimeDirectory(agentID)
	if err != nil {
		return "", "", err
	}
	return runtimeRoot, agentDirectory, nil
}

func droidSettingsDocument(settings *droidDocument) *droidDocument {
	if settings != nil && settings.hasHooksContainer {
		return settings
	}
	return nil
}

func prepareDroidInstallationChanges(
	config InstallConfig,
	agentDirectory, auditPath string,
	rendered []*droidRenderedDocument,
) ([]*fileChange, error) {
	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = "https://api.elydora.com"
	}
	runtimeConfig, err := json.MarshalIndent(droidRuntimeConfig{
		OrgID: config.OrgID, AgentID: config.AgentID, KID: config.KID,
		BaseURL: baseURL, Token: config.Token, AgentName: droidAgentKey,
	}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode Elydora runtime config: %w", err)
	}
	runtimeConfig = append(runtimeConfig, '\n')
	runtimeChanges := []struct {
		path, label string
		content     []byte
		mode        os.FileMode
	}{
		{filepath.Join(agentDirectory, "config.json"), "Elydora runtime config", runtimeConfig, 0600},
		{filepath.Join(agentDirectory, "private.key"), "Elydora private key", []byte(config.PrivateKey), 0600},
		{auditPath, "Elydora audit runtime", []byte(buildHookScript(droidAgentKey, config.AgentID)), 0700},
	}
	changes := make([]*fileChange, 0, len(runtimeChanges)+len(rendered))
	for _, item := range runtimeChanges {
		change, changeErr := prepareFileChange(item.path, item.label, item.content, item.mode)
		if changeErr != nil {
			return nil, changeErr
		}
		changes = append(changes, change)
	}
	for _, document := range rendered {
		change, changeErr := prepareRenderedDroidChange(document)
		if changeErr != nil {
			return nil, changeErr
		}
		changes = append(changes, change)
	}
	return changes, nil
}
