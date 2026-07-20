package plugins

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type augmentRuntimePaths struct {
	runtimeRoot      string
	agentDirectory   string
	configPath       string
	keyPath          string
	guardPath        string
	auditPath        string
	guardWrapperPath string
	auditWrapperPath string
}

func validateAugmentInstallConfig(config InstallConfig) error {
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
	if config.AgentName != augmentAgentKey {
		return fmt.Errorf(
			"Augment Code CLI installation requires agent name %s",
			augmentAgentKey,
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

func augmentAgentPaths(config InstallConfig) (*augmentRuntimePaths, error) {
	if err := validateAugmentInstallConfig(config); err != nil {
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
	wrappers := resolveAugmentWrapperPaths(agentDirectory)
	paths := &augmentRuntimePaths{
		runtimeRoot: runtimeRoot, agentDirectory: agentDirectory,
		configPath:       filepath.Join(agentDirectory, "config.json"),
		keyPath:          filepath.Join(agentDirectory, "private.key"),
		guardPath:        filepath.Join(agentDirectory, augmentGuardScript),
		auditPath:        filepath.Join(agentDirectory, augmentAuditScript),
		guardWrapperPath: wrappers.guard,
		auditWrapperPath: wrappers.audit,
	}
	if !filepath.IsAbs(config.GuardScriptPath) ||
		!sameAugmentPath(config.GuardScriptPath, paths.guardPath) {
		return nil, fmt.Errorf(
			"Elydora guard runtime must use the managed agent directory: %s",
			paths.guardPath,
		)
	}
	if config.HookScript != "" && (!filepath.IsAbs(config.HookScript) ||
		!sameAugmentPath(config.HookScript, paths.auditPath)) {
		return nil, fmt.Errorf(
			"Elydora audit runtime must use the managed agent directory: %s",
			paths.auditPath,
		)
	}
	return paths, nil
}

func preflightAugmentInstallation(
	config InstallConfig,
	document *augmentDocument,
) (*augmentRuntimePaths, string, error) {
	if document == nil || document.configPath == "" {
		return nil, "", fmt.Errorf(
			"Augment Code CLI installation requires a user settings document",
		)
	}
	paths, err := augmentAgentPaths(config)
	if err != nil {
		return nil, "", err
	}
	if err := validateAugmentRuntimeIdentity(
		paths.agentDirectory,
		config.AgentID,
	); err != nil {
		return nil, "", err
	}
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		return nil, "", err
	}
	if err := requireAugmentAbsoluteNode(nodePath); err != nil {
		return nil, "", err
	}
	if err := validateAugmentMatchers(document.hooks, nodePath); err != nil {
		return nil, "", err
	}
	return paths, nodePath, nil
}

func buildAugmentRuntimeConfig(config InstallConfig) ([]byte, error) {
	value := map[string]any{
		"org_id": config.OrgID, "agent_id": config.AgentID, "kid": config.KID,
		"base_url": config.BaseURL, "agent_name": augmentAgentKey,
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

func prepareAugmentInstallationChanges(
	config InstallConfig,
	paths *augmentRuntimePaths,
	nodePath string,
	rendered *augmentRenderedDocument,
) ([]*fileChange, error) {
	if paths == nil || rendered == nil {
		return nil, fmt.Errorf("prepared Auggie installation is required")
	}
	if err := validateAugmentRuntimeIdentity(
		paths.agentDirectory,
		config.AgentID,
	); err != nil {
		return nil, err
	}
	runtimeConfig, err := buildAugmentRuntimeConfig(config)
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
			[]byte(generateGuardScript(
				augmentAgentKey,
				config.AgentID,
				"",
				false,
				"",
			)),
			0700,
		},
		{paths.configPath, "Elydora runtime config", runtimeConfig, 0600},
		{paths.keyPath, "Elydora private key", []byte(config.PrivateKey), 0600},
		{
			paths.auditPath,
			"Elydora audit runtime",
			[]byte(buildHookScriptWithOutput(
				augmentAgentKey,
				config.AgentID,
				"",
				false,
				true,
			)),
			0700,
		},
		{
			paths.guardWrapperPath,
			"Auggie guard wrapper",
			buildAugmentWrapper(nodePath, paths.guardPath),
			0700,
		},
		{
			paths.auditWrapperPath,
			"Auggie audit wrapper",
			buildAugmentWrapper(nodePath, paths.auditPath),
			0700,
		},
	}
	changes := make([]*fileChange, 0, len(items)+1)
	for _, item := range items {
		change, err := prepareFileChange(item.path, item.label, item.content, item.mode)
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}
	documentChange, err := prepareRenderedAugmentChange(rendered)
	if err != nil {
		return nil, err
	}
	return append(changes, documentChange), nil
}
