package plugins

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type geminiRuntimePaths struct {
	runtimeRoot    string
	agentDirectory string
	configPath     string
	keyPath        string
	guardPath      string
	auditPath      string
}

func validateGeminiInstallConfig(config InstallConfig) error {
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
	if config.AgentName != geminiAgentKey {
		return fmt.Errorf(
			"Gemini CLI installation requires agent name %s",
			geminiAgentKey,
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

func geminiAgentPaths(config InstallConfig) (*geminiRuntimePaths, error) {
	if err := validateGeminiInstallConfig(config); err != nil {
		return nil, err
	}
	runtimeRoot, err := geminiRuntimeRoot()
	if err != nil {
		return nil, err
	}
	agentDirectory, err := ResolveAgentRuntimeDirectory(config.AgentID)
	if err != nil {
		return nil, err
	}
	paths := &geminiRuntimePaths{
		runtimeRoot: runtimeRoot, agentDirectory: agentDirectory,
		configPath: filepath.Join(agentDirectory, "config.json"),
		keyPath:    filepath.Join(agentDirectory, "private.key"),
		guardPath:  filepath.Join(agentDirectory, geminiGuardScript),
		auditPath:  filepath.Join(agentDirectory, geminiAuditScript),
	}
	if !filepath.IsAbs(config.GuardScriptPath) ||
		!sameGeminiPath(config.GuardScriptPath, paths.guardPath) {
		return nil, fmt.Errorf(
			"guard runtime must use the managed Elydora agent directory: %s",
			paths.guardPath,
		)
	}
	if config.HookScript != "" && (!filepath.IsAbs(config.HookScript) ||
		!sameGeminiPath(config.HookScript, paths.auditPath)) {
		return nil, fmt.Errorf(
			"audit runtime must use the managed Elydora agent directory: %s",
			paths.auditPath,
		)
	}
	return paths, nil
}

func preflightGeminiInstallation(
	config InstallConfig,
	document *geminiDocument,
) (*geminiRuntimePaths, string, error) {
	if document == nil || document.filePath == "" {
		return nil, "", fmt.Errorf(
			"Gemini CLI installation requires a user settings document",
		)
	}
	paths, err := geminiAgentPaths(config)
	if err != nil {
		return nil, "", err
	}
	if err := validateGeminiRuntimeIdentity(paths.agentDirectory, config.AgentID); err != nil {
		return nil, "", err
	}
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		return nil, "", err
	}
	if !filepath.IsAbs(nodePath) || !isGeminiNodeExecutable(nodePath) {
		return nil, "", fmt.Errorf(
			"Gemini CLI hooks require an absolute Node.js executable path",
		)
	}
	return paths, nodePath, nil
}

func buildGeminiRuntimeConfig(config InstallConfig) ([]byte, error) {
	value := map[string]any{
		"org_id": config.OrgID, "agent_id": config.AgentID, "kid": config.KID,
		"base_url": config.BaseURL, "agent_name": geminiAgentKey,
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

func prepareGeminiInstallationChanges(
	config InstallConfig,
	paths *geminiRuntimePaths,
	rendered *geminiRenderedDocument,
) ([]*fileChange, error) {
	runtimeConfig, err := buildGeminiRuntimeConfig(config)
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
			[]byte(generateGuardScript(
				geminiAgentKey,
				config.AgentID,
				"{}\n",
				false,
				"",
			)),
			0700,
		},
		{paths.configPath, "Elydora runtime config", runtimeConfig, 0600},
		{paths.keyPath, "Elydora private key", []byte(config.PrivateKey), 0600},
		{
			paths.auditPath, "Elydora audit runtime",
			[]byte(buildHookScriptWithOutput(
				geminiAgentKey,
				config.AgentID,
				"{}\n",
				false,
				true,
			)),
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
	documentChange, err := prepareRenderedGeminiChange(rendered)
	if err != nil {
		return nil, err
	}
	return append(changes, documentChange), nil
}
