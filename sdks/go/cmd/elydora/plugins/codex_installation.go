package plugins

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

type codexRuntimePaths struct {
	runtimeRoot    string
	agentDirectory string
	configPath     string
	keyPath        string
	guardPath      string
	auditPath      string
}

func validateCodexPrivateKey(value string) error {
	seed, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(seed) != 32 || base64.RawURLEncoding.EncodeToString(seed) != value {
		return fmt.Errorf("private key must be a canonical 32-byte base64url value")
	}
	return nil
}

func validateCodexBaseURL(value string) error {
	if strings.ContainsRune(value, '\\') {
		return fmt.Errorf("base URL must be an absolute HTTP or HTTPS URL")
	}
	for _, character := range value {
		if character < 32 || unicode.IsSpace(character) {
			return fmt.Errorf("base URL must be an absolute HTTP or HTTPS URL")
		}
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("base URL must be an absolute HTTP or HTTPS URL")
	}
	validScheme := strings.EqualFold(parsed.Scheme, "http") ||
		strings.EqualFold(parsed.Scheme, "https")
	if !validScheme ||
		parsed.Host == "" || parsed.Hostname() == "" || parsed.Opaque != "" {
		return fmt.Errorf("base URL must be an absolute HTTP or HTTPS URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf(
			"base URL must exclude credentials, query parameters, and fragments",
		)
	}
	port := parsed.Port()
	if strings.HasSuffix(parsed.Host, ":") {
		return fmt.Errorf("base URL must contain a valid port")
	}
	if port != "" {
		value, convertErr := strconv.Atoi(port)
		if convertErr != nil || value < 1 || value > 65535 {
			return fmt.Errorf("base URL must contain a valid port")
		}
	}
	return nil
}

func validateCodexInstallConfig(config InstallConfig) error {
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
	if config.AgentName != codexAgentKey {
		return fmt.Errorf("codex installation requires agent name %s", codexAgentKey)
	}
	if err := validateCodexPrivateKey(config.PrivateKey); err != nil {
		return err
	}
	return validateCodexBaseURL(config.BaseURL)
}

func codexAgentPaths(config InstallConfig) (*codexRuntimePaths, error) {
	if err := validateCodexInstallConfig(config); err != nil {
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
	paths := &codexRuntimePaths{
		runtimeRoot:    runtimeRoot,
		agentDirectory: agentDirectory,
		configPath:     filepath.Join(agentDirectory, "config.json"),
		keyPath:        filepath.Join(agentDirectory, "private.key"),
		guardPath:      filepath.Join(agentDirectory, codexGuardScript),
		auditPath:      filepath.Join(agentDirectory, codexAuditScript),
	}
	if !sameCodexPath(config.GuardScriptPath, paths.guardPath) {
		return nil, fmt.Errorf(
			"guard runtime must use the managed Elydora agent directory: %s",
			paths.guardPath,
		)
	}
	if config.HookScript != "" && !sameCodexPath(config.HookScript, paths.auditPath) {
		return nil, fmt.Errorf(
			"audit runtime must use the managed Elydora agent directory: %s",
			paths.auditPath,
		)
	}
	return paths, nil
}

func preflightCodexInstallation(
	config InstallConfig,
	hooksPath string,
) (*codexRuntimePaths, string, error) {
	paths, err := codexAgentPaths(config)
	if err != nil {
		return nil, "", err
	}
	if _, err := managedPhysicalDirectoryExists(
		filepath.Dir(hooksPath),
		"Codex hooks directory",
	); err != nil {
		return nil, "", err
	}
	if err := validateCodexRuntimeIdentity(paths.agentDirectory, config.AgentID); err != nil {
		return nil, "", err
	}
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		return nil, "", err
	}
	return paths, nodePath, nil
}

func buildCodexRuntimeConfig(config InstallConfig) ([]byte, error) {
	encoded, err := json.MarshalIndent(agentRuntimeConfig{
		OrgID: config.OrgID, AgentID: config.AgentID, KID: config.KID,
		BaseURL: config.BaseURL, Token: config.Token, AgentName: codexAgentKey,
	}, "", "  ")
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

func prepareCodexInstallationChanges(
	config InstallConfig,
	paths *codexRuntimePaths,
	rendered *codexRenderedDocument,
) ([]*fileChange, error) {
	runtimeConfig, err := buildCodexRuntimeConfig(config)
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
			[]byte(generateGuardScript(codexAgentKey, config.AgentID, "", false)), 0700,
		},
		{paths.configPath, "Elydora runtime config", runtimeConfig, 0600},
		{paths.keyPath, "Elydora private key", []byte(config.PrivateKey), 0600},
		{
			paths.auditPath, "Elydora audit runtime",
			[]byte(buildHookScriptWithOutput(
				codexAgentKey, config.AgentID, "", false, true,
			)), 0700,
		},
	}
	changes := make([]*fileChange, 0, len(items)+1)
	for _, item := range items {
		change, err := prepareFileChange(
			item.path,
			item.label,
			item.content,
			item.mode,
		)
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}
	documentChange, err := prepareRenderedCodexChange(rendered)
	if err != nil {
		return nil, err
	}
	return append(changes, documentChange), nil
}
