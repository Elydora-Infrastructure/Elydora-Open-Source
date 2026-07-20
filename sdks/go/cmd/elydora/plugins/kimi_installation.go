package plugins

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type kimiRuntimePaths struct {
	runtimeRoot    string
	agentDirectory string
	configPath     string
	keyPath        string
	guardPath      string
	auditPath      string
}

type kimiRenderedDocument struct {
	document kimiDocument
	next     []byte
}

func validateKimiInstallConfig(config InstallConfig) error {
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
	if config.AgentName != kimiAgentKey {
		return fmt.Errorf("kimi installation requires agent name %s", kimiAgentKey)
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

func kimiAgentPaths(config InstallConfig) (*kimiRuntimePaths, error) {
	if err := validateKimiInstallConfig(config); err != nil {
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
	paths := &kimiRuntimePaths{
		runtimeRoot: runtimeRoot, agentDirectory: agentDirectory,
		configPath: filepath.Join(agentDirectory, "config.json"),
		keyPath:    filepath.Join(agentDirectory, "private.key"),
		guardPath:  filepath.Join(agentDirectory, kimiGuardScript),
		auditPath:  filepath.Join(agentDirectory, kimiAuditScript),
	}
	if !filepath.IsAbs(config.GuardScriptPath) ||
		!sameKimiPath(config.GuardScriptPath, paths.guardPath) {
		return nil, fmt.Errorf(
			"guard runtime must use the managed Elydora agent directory: %s",
			paths.guardPath,
		)
	}
	if config.HookScript != "" && (!filepath.IsAbs(config.HookScript) ||
		!sameKimiPath(config.HookScript, paths.auditPath)) {
		return nil, fmt.Errorf(
			"audit runtime must use the managed Elydora agent directory: %s",
			paths.auditPath,
		)
	}
	return paths, nil
}

func preflightKimiInstallation(
	config InstallConfig,
	documents []kimiDocument,
) (*kimiRuntimePaths, string, error) {
	if len(documents) == 0 {
		return nil, "", fmt.Errorf("kimi installation requires at least one hook contract")
	}
	paths, err := kimiAgentPaths(config)
	if err != nil {
		return nil, "", err
	}
	if err := validateKimiRuntimeIdentity(paths.agentDirectory, config.AgentID); err != nil {
		return nil, "", err
	}
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		return nil, "", err
	}
	return paths, nodePath, nil
}

func buildKimiRuntimeConfig(config InstallConfig) ([]byte, error) {
	value := map[string]any{
		"org_id": config.OrgID, "agent_id": config.AgentID, "kid": config.KID,
		"base_url": config.BaseURL, "agent_name": kimiAgentKey,
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
			"elydora runtime config exceeds %d bytes after JSON encoding",
			maxRuntimeConfigBytes,
		)
	}
	return encoded, nil
}

func renderKimiChange(
	document kimiDocument,
	keep []int,
	additions []kimiHook,
) (kimiRenderedDocument, error) {
	next, err := renderKimiHooks(document, keep, additions)
	if err != nil {
		return kimiRenderedDocument{}, err
	}
	if strings.TrimSpace(string(next)) != "" {
		if _, err := parseKimiDocument(document.contract, next, true); err != nil {
			return kimiRenderedDocument{}, fmt.Errorf(
				"validate rendered %s: %w", document.contract.label, err,
			)
		}
	}
	return kimiRenderedDocument{document: document, next: next}, nil
}

func prepareRenderedKimiChange(rendered kimiRenderedDocument) (*fileChange, error) {
	document := rendered.document
	if document.exists && bytes.Equal(document.raw, rendered.next) {
		return nil, nil
	}
	remove := document.exists && strings.TrimSpace(string(rendered.next)) == ""
	return prepareSourceChange(
		document.contract.configPath,
		document.contract.label,
		document.raw,
		document.exists,
		rendered.next,
		0600,
		remove,
	)
}

func prepareKimiInstallationChanges(
	config InstallConfig,
	paths *kimiRuntimePaths,
	rendered []kimiRenderedDocument,
) ([]*fileChange, error) {
	runtimeConfig, err := buildKimiRuntimeConfig(config)
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
			[]byte(generateGuardScript(kimiAgentKey, config.AgentID, "", false)), 0700,
		},
		{paths.configPath, "Elydora runtime config", runtimeConfig, 0600},
		{paths.keyPath, "Elydora private key", []byte(config.PrivateKey), 0600},
		{
			paths.auditPath, "Elydora audit runtime",
			[]byte(buildHookScriptWithOutput(
				kimiAgentKey, config.AgentID, "", false, true,
			)), 0700,
		},
	}
	changes := make([]*fileChange, 0, len(items)+len(rendered))
	for _, item := range items {
		change, err := prepareFileChange(item.path, item.label, item.content, item.mode)
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}
	for _, document := range rendered {
		change, err := prepareRenderedKimiChange(document)
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}
	return changes, nil
}

func prepareKimiUninstallChanges(
	rendered []kimiRenderedDocument,
) ([]*fileChange, error) {
	changes := make([]*fileChange, 0, len(rendered))
	for _, document := range rendered {
		change, err := prepareRenderedKimiChange(document)
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}
	return changes, nil
}

func writeKimiChanges(
	changes []*fileChange,
	label string,
	rename renameFunc,
	runtimeRoot string,
	agentDirectory string,
) error {
	hasChanges := false
	for _, change := range changes {
		if change != nil {
			hasChanges = true
			break
		}
	}
	if !hasChanges {
		return nil
	}
	if agentDirectory != "" {
		if err := EnsurePrivateDirectory(runtimeRoot); err != nil {
			return err
		}
		if err := EnsurePrivateDirectory(agentDirectory); err != nil {
			return err
		}
	}
	return writeChanges(changes, label, rename)
}
