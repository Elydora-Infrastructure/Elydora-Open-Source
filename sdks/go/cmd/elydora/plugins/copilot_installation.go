package plugins

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type copilotRuntimePaths struct {
	runtimeRoot    string
	agentDirectory string
	configPath     string
	keyPath        string
	guardPath      string
	auditPath      string
}

type preparedCopilotInstallation struct {
	changes       []*fileChange
	preconditions []filePrecondition
	paths         *copilotRuntimePaths
}

func validateCopilotInstallConfig(config InstallConfig) error {
	for _, field := range []struct{ name, value string }{
		{"agent name", config.AgentName},
		{"organization ID", config.OrgID},
		{"agent ID", config.AgentID},
		{"key ID", config.KID},
		{"private key", config.PrivateKey},
		{"base URL", config.BaseURL},
		{"guard script path", config.GuardScriptPath},
	} {
		if field.value == "" {
			return fmt.Errorf("%s is required", field.name)
		}
	}
	if config.AgentName != copilotAgentKey {
		return fmt.Errorf(
			"GitHub Copilot CLI installation requires agent name %s",
			copilotAgentKey,
		)
	}
	if strings.TrimSpace(config.OrgID) == "" {
		return fmt.Errorf("organization ID is required")
	}
	if strings.TrimSpace(config.KID) == "" {
		return fmt.Errorf("key ID is required")
	}
	if config.Token != "" && strings.TrimSpace(config.Token) == "" {
		return fmt.Errorf("token must contain a non-whitespace value when provided")
	}
	if err := validateManagedPrivateKey(config.PrivateKey); err != nil {
		return err
	}
	return validateManagedBaseURL(config.BaseURL)
}

func copilotAgentPaths(config InstallConfig) (*copilotRuntimePaths, error) {
	if err := validateCopilotInstallConfig(config); err != nil {
		return nil, err
	}
	runtimeRoot, err := AgentRuntimeRoot()
	if err != nil {
		return nil, err
	}
	agentDirectory, err := ResolveAgentRuntimeDirectory(config.AgentID)
	if err != nil {
		return nil, err
	}
	paths := &copilotRuntimePaths{
		runtimeRoot: runtimeRoot, agentDirectory: agentDirectory,
		configPath: filepath.Join(agentDirectory, "config.json"),
		keyPath:    filepath.Join(agentDirectory, "private.key"),
		guardPath:  filepath.Join(agentDirectory, copilotGuardScript),
		auditPath:  filepath.Join(agentDirectory, copilotAuditScript),
	}
	if !filepath.IsAbs(config.GuardScriptPath) ||
		!sameCopilotPath(config.GuardScriptPath, paths.guardPath) {
		return nil, fmt.Errorf(
			"Elydora guard runtime must use the managed agent directory: %s",
			paths.guardPath,
		)
	}
	if config.HookScript != "" && (!filepath.IsAbs(config.HookScript) ||
		!sameCopilotPath(config.HookScript, paths.auditPath)) {
		return nil, fmt.Errorf(
			"Elydora audit runtime must use the managed agent directory: %s",
			paths.auditPath,
		)
	}
	return paths, nil
}

func preflightCopilotInstallation(
	config InstallConfig,
	sources *copilotSources,
) (*copilotRuntimePaths, string, error) {
	if sources == nil || sources.user == nil || sources.user.filePath == "" {
		return nil, "", fmt.Errorf(
			"GitHub Copilot CLI installation requires a user hook path",
		)
	}
	if err := requireCopilotHooksEnabled(sources); err != nil {
		return nil, "", err
	}
	paths, err := copilotAgentPaths(config)
	if err != nil {
		return nil, "", err
	}
	if err := validateCopilotRuntimeIdentity(paths.agentDirectory, config.AgentID); err != nil {
		return nil, "", err
	}
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		return nil, "", err
	}
	if !filepath.IsAbs(nodePath) || !isCopilotNodeExecutable(nodePath) {
		return nil, "", fmt.Errorf(
			"GitHub Copilot CLI hooks require an absolute Node.js executable path",
		)
	}
	hookSources := []copilotHooks{sources.user.hooks}
	if sources.legacy != nil {
		hookSources = append(hookSources, sources.legacy.hooks)
	}
	if err := validateCopilotJavaScriptMatchers(hookSources, nodePath); err != nil {
		return nil, "", err
	}
	return paths, nodePath, nil
}

func buildCopilotRuntimeConfig(config InstallConfig) ([]byte, error) {
	value := map[string]any{
		"org_id": config.OrgID, "agent_id": config.AgentID, "kid": config.KID,
		"base_url": config.BaseURL, "agent_name": copilotAgentKey,
	}
	if config.Token != "" {
		value["token"] = config.Token
	}
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode Elydora runtime config: %w", err)
	}
	encoded = append(encoded, '\n')
	if len(encoded) > maxRuntimeConfigBytes {
		return nil, fmt.Errorf(
			"Elydora runtime config exceeds %d bytes after JSON encoding",
			maxRuntimeConfigBytes,
		)
	}
	return encoded, nil
}

func validateCopilotRenderedSet(
	sources *copilotSources,
	rendered []*copilotRenderedDocument,
) error {
	expected := []*copilotDocument{sources.user}
	if sources.legacy != nil {
		expected = append(expected, sources.legacy)
	}
	if len(rendered) != len(expected) {
		return fmt.Errorf("GitHub Copilot rendered source set is incomplete")
	}
	for _, document := range expected {
		matches := 0
		for _, item := range rendered {
			if item != nil && item.document != nil &&
				sameCopilotPath(item.document.filePath, document.filePath) {
				matches++
			}
		}
		if matches != 1 {
			return fmt.Errorf(
				"GitHub Copilot rendered source set contains unexpected paths",
			)
		}
	}
	return nil
}

func copilotInstallationPreconditions(
	sources *copilotSources,
	rendered []*copilotRenderedDocument,
) []filePrecondition {
	result := make([]filePrecondition, 0, len(sources.settingsPreconditions)+len(rendered))
	for _, item := range sources.settingsPreconditions {
		result = append(result, filePrecondition{
			filePath: item.filePath, label: item.label,
			snapshot: item.snapshot, maximumSize: maxManagedSourceBytes,
		})
	}
	for _, item := range rendered {
		if item != nil && !item.changed {
			result = append(result, filePrecondition{
				filePath:    item.document.filePath,
				label:       "GitHub Copilot hook source",
				snapshot:    item.document.snapshot,
				maximumSize: maxManagedSourceBytes,
			})
		}
	}
	return result
}

func prepareCopilotInstallation(
	config InstallConfig,
	sources *copilotSources,
	rendered []*copilotRenderedDocument,
) (*preparedCopilotInstallation, error) {
	paths, _, err := preflightCopilotInstallation(config, sources)
	if err != nil {
		return nil, err
	}
	if err := validateCopilotRenderedSet(sources, rendered); err != nil {
		return nil, err
	}
	runtimeConfig, err := buildCopilotRuntimeConfig(config)
	if err != nil {
		return nil, err
	}
	items := []struct {
		path, label string
		content     []byte
		mode        os.FileMode
	}{
		{
			paths.guardPath, "Elydora guard runtime",
			[]byte(generateGuardScript(copilotAgentKey, config.AgentID, "", false, "")), 0700,
		},
		{paths.configPath, "Elydora runtime config", runtimeConfig, 0600},
		{paths.keyPath, "Elydora private key", []byte(config.PrivateKey), 0600},
		{
			paths.auditPath, "Elydora audit runtime",
			[]byte(buildHookScriptWithOutput(copilotAgentKey, config.AgentID, "", false, true)), 0700,
		},
	}
	changes := make([]*fileChange, 0, len(items)+len(rendered))
	for _, item := range items {
		change, changeErr := prepareFileChange(item.path, item.label, item.content, item.mode)
		if changeErr != nil {
			return nil, changeErr
		}
		changes = append(changes, change)
	}
	for _, document := range rendered {
		change, changeErr := prepareRenderedCopilotChange(document)
		if changeErr != nil {
			return nil, changeErr
		}
		changes = append(changes, change)
	}
	return &preparedCopilotInstallation{
		changes: changes, paths: paths,
		preconditions: copilotInstallationPreconditions(sources, rendered),
	}, nil
}

func commitCopilotInstallation(
	prepared *preparedCopilotInstallation,
	rename renameFunc,
) error {
	if prepared == nil || prepared.paths == nil {
		return fmt.Errorf("prepared GitHub Copilot installation is required")
	}
	return writeCopilotChanges(
		prepared.changes,
		"Install GitHub Copilot hooks",
		rename,
		prepared.paths.runtimeRoot,
		prepared.paths.agentDirectory,
		prepared.preconditions,
	)
}
