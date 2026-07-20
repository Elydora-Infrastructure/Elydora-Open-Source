package plugins

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type qwenRuntimePaths struct {
	runtimeRoot    string
	agentDirectory string
	configPath     string
	keyPath        string
	guardPath      string
	auditPath      string
}

type preparedQwenInstallation struct {
	changes       []*fileChange
	preconditions []filePrecondition
	paths         *qwenRuntimePaths
}

type qwenRuntimeConfig struct {
	OrgID     string `json:"org_id"`
	AgentID   string `json:"agent_id"`
	KID       string `json:"kid"`
	BaseURL   string `json:"base_url"`
	Token     string `json:"token,omitempty"`
	AgentName string `json:"agent_name"`
}

func validateQwenInstallConfig(config InstallConfig) error {
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
	if config.AgentName != qwenAgentKey {
		return fmt.Errorf(
			"Qwen Code installation requires agent name %s",
			qwenAgentKey,
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

func qwenAgentPaths(config InstallConfig) (*qwenRuntimePaths, error) {
	if err := validateQwenInstallConfig(config); err != nil {
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
	paths := &qwenRuntimePaths{
		runtimeRoot:    runtimeRoot,
		agentDirectory: agentDirectory,
		configPath:     filepath.Join(agentDirectory, "config.json"),
		keyPath:        filepath.Join(agentDirectory, "private.key"),
		guardPath:      filepath.Join(agentDirectory, qwenGuardScript),
		auditPath:      filepath.Join(agentDirectory, qwenAuditScript),
	}
	if !filepath.IsAbs(config.GuardScriptPath) ||
		!sameQwenPath(config.GuardScriptPath, paths.guardPath) {
		return nil, fmt.Errorf(
			"Elydora guard runtime must use the managed agent directory: %s",
			paths.guardPath,
		)
	}
	if config.HookScript != "" && (!filepath.IsAbs(config.HookScript) ||
		!sameQwenPath(config.HookScript, paths.auditPath)) {
		return nil, fmt.Errorf(
			"Elydora audit runtime must use the managed agent directory: %s",
			paths.auditPath,
		)
	}
	return paths, nil
}

func preflightQwenInstallation(
	config InstallConfig,
	sources *qwenSources,
) (*qwenRuntimePaths, string, error) {
	if sources == nil || sources.user == nil || sources.user.filePath == "" {
		return nil, "", fmt.Errorf("Qwen Code installation requires user settings")
	}
	if err := requireQwenHooksEnabled(sources); err != nil {
		return nil, "", err
	}
	paths, err := qwenAgentPaths(config)
	if err != nil {
		return nil, "", err
	}
	if err := validateQwenRuntimeIdentity(paths.agentDirectory, config.AgentID); err != nil {
		return nil, "", err
	}
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		return nil, "", err
	}
	if !filepath.IsAbs(nodePath) || !isQwenNodeExecutable(nodePath) {
		return nil, "", fmt.Errorf(
			"Qwen Code hooks require an absolute Node.js executable path",
		)
	}
	return paths, nodePath, nil
}

func buildQwenRuntimeConfig(config InstallConfig) ([]byte, error) {
	encoded, err := json.MarshalIndent(qwenRuntimeConfig{
		OrgID:     config.OrgID,
		AgentID:   config.AgentID,
		KID:       config.KID,
		BaseURL:   config.BaseURL,
		Token:     config.Token,
		AgentName: qwenAgentKey,
	}, "", "  ")
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

func prepareRenderedQwenChange(
	rendered *qwenRenderedDocument,
) (*fileChange, error) {
	if rendered == nil || !rendered.changed {
		return nil, nil
	}
	return prepareSnapshotSourceChange(
		rendered.document.filePath,
		qwenDocumentLabel(rendered.document),
		rendered.document.snapshot,
		rendered.next,
		0600,
		rendered.remove,
	)
}

func qwenReadOnlyPreconditions(
	sources *qwenSources,
	changedPath string,
) []filePrecondition {
	if sources == nil {
		return nil
	}
	result := make([]filePrecondition, 0, len(sources.preconditions))
	for _, condition := range sources.preconditions {
		if changedPath != "" && sameQwenPath(condition.filePath, changedPath) {
			continue
		}
		result = append(result, condition)
	}
	return result
}

func prepareQwenInstallation(
	config InstallConfig,
	sources *qwenSources,
	rendered *qwenRenderedDocument,
) (*preparedQwenInstallation, error) {
	paths, _, err := preflightQwenInstallation(config, sources)
	if err != nil {
		return nil, err
	}
	if rendered == nil || rendered.document == nil ||
		!sameQwenPath(rendered.document.filePath, sources.user.filePath) {
		return nil, fmt.Errorf("Qwen Code rendered user settings are required")
	}
	runtimeConfig, err := buildQwenRuntimeConfig(config)
	if err != nil {
		return nil, err
	}
	items := []struct {
		path, label string
		content     []byte
		mode        os.FileMode
	}{
		{
			paths.guardPath,
			"Elydora guard runtime",
			[]byte(generateGuardScript(qwenAgentKey, config.AgentID, "", false, "")),
			0700,
		},
		{paths.configPath, "Elydora runtime config", runtimeConfig, 0600},
		{paths.keyPath, "Elydora private key", []byte(config.PrivateKey), 0600},
		{
			paths.auditPath,
			"Elydora audit runtime",
			[]byte(buildHookScriptWithOutput(
				qwenAgentKey,
				config.AgentID,
				"",
				false,
				true,
			)),
			0700,
		},
	}
	changes := make([]*fileChange, 0, len(items)+1)
	for _, item := range items {
		change, changeErr := prepareFileChange(item.path, item.label, item.content, item.mode)
		if changeErr != nil {
			return nil, changeErr
		}
		changes = append(changes, change)
	}
	settingsChange, err := prepareRenderedQwenChange(rendered)
	if err != nil {
		return nil, err
	}
	changes = append(changes, settingsChange)
	changedPath := ""
	if rendered.changed {
		changedPath = rendered.document.filePath
	}
	return &preparedQwenInstallation{
		changes:       changes,
		paths:         paths,
		preconditions: qwenReadOnlyPreconditions(sources, changedPath),
	}, nil
}

func commitQwenInstallation(
	prepared *preparedQwenInstallation,
	rename renameFunc,
) error {
	if prepared == nil || prepared.paths == nil {
		return fmt.Errorf("prepared Qwen Code installation is required")
	}
	if err := EnsurePrivateDirectory(prepared.paths.runtimeRoot); err != nil {
		return err
	}
	if err := EnsurePrivateDirectory(prepared.paths.agentDirectory); err != nil {
		return err
	}
	return writeChanges(
		prepared.changes,
		"Install Qwen Code hooks",
		rename,
		prepared.preconditions...,
	)
}

func prepareQwenUninstall(
	sources *qwenSources,
	rendered *qwenRenderedDocument,
) (*fileChange, []filePrecondition, error) {
	change, err := prepareRenderedQwenChange(rendered)
	if err != nil {
		return nil, nil, err
	}
	changedPath := ""
	if rendered != nil && rendered.changed {
		changedPath = rendered.document.filePath
	}
	return change, qwenReadOnlyPreconditions(sources, changedPath), nil
}

func commitQwenUninstall(
	change *fileChange,
	preconditions []filePrecondition,
	rename renameFunc,
) error {
	return writeChanges(
		[]*fileChange{change},
		"Uninstall Qwen Code hooks",
		rename,
		preconditions...,
	)
}
