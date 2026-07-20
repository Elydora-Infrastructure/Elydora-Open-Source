package plugins

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type clineRuntimePaths struct {
	runtimeRoot    string
	agentDirectory string
	configPath     string
	keyPath        string
	guardPath      string
	auditPath      string
	hooks          clineHookPaths
}

func validateClineInstallConfig(config InstallConfig) error {
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
	if config.AgentName != clineAgentKey {
		return fmt.Errorf("Cline installation requires agent name %s", clineAgentKey)
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

func clineAgentPaths(
	config InstallConfig,
	hooks clineHookPaths,
) (*clineRuntimePaths, error) {
	if err := validateClineInstallConfig(config); err != nil {
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
	paths := &clineRuntimePaths{
		runtimeRoot: runtimeRoot, agentDirectory: agentDirectory, hooks: hooks,
		configPath: filepath.Join(agentDirectory, "config.json"),
		keyPath:    filepath.Join(agentDirectory, "private.key"),
		guardPath:  filepath.Join(agentDirectory, clineGuardScript),
		auditPath:  filepath.Join(agentDirectory, clineAuditScript),
	}
	if !filepath.IsAbs(config.GuardScriptPath) ||
		!sameClinePathValue(config.GuardScriptPath, paths.guardPath) {
		return nil, fmt.Errorf(
			"Elydora guard runtime must use the managed agent directory: %s",
			paths.guardPath,
		)
	}
	if config.HookScript != "" && (!filepath.IsAbs(config.HookScript) ||
		!sameClinePathValue(config.HookScript, paths.auditPath)) {
		return nil, fmt.Errorf(
			"Elydora audit runtime must use the managed agent directory: %s",
			paths.auditPath,
		)
	}
	return paths, nil
}

func sameClinePathValue(left, right string) bool {
	matches, err := sameClinePath(left, right)
	return err == nil && matches
}

func preflightClineInstallation(
	config InstallConfig,
	hooks clineHookPaths,
) (*clineRuntimePaths, error) {
	paths, err := clineAgentPaths(config, hooks)
	if err != nil {
		return nil, err
	}
	if err := validateClineRuntimeIdentity(paths.agentDirectory, config.AgentID); err != nil {
		return nil, err
	}
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(nodePath) || !isClaudeNodeExecutable(nodePath) {
		return nil, fmt.Errorf("Cline hooks require an absolute Node.js executable path")
	}
	return paths, nil
}

func buildClineRuntimeConfig(config InstallConfig) ([]byte, error) {
	value := map[string]any{
		"org_id": config.OrgID, "agent_id": config.AgentID, "kid": config.KID,
		"base_url": config.BaseURL, "agent_name": clineAgentKey,
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

func prepareClineInstallationChanges(
	config InstallConfig,
	paths *clineRuntimePaths,
	guardFile clineHookFile,
	auditFile clineHookFile,
) ([]*fileChange, error) {
	if paths == nil {
		return nil, fmt.Errorf("prepared Cline installation is required")
	}
	if err := validateClineRuntimeIdentity(paths.agentDirectory, config.AgentID); err != nil {
		return nil, err
	}
	runtimeConfig, err := buildClineRuntimeConfig(config)
	if err != nil {
		return nil, err
	}
	guardMetadata, err := buildClineMetadata("guard", config.AgentID, paths.guardPath)
	if err != nil {
		return nil, fmt.Errorf("build Cline guard metadata: %w", err)
	}
	auditMetadata, err := buildClineMetadata("audit", config.AgentID, paths.auditPath)
	if err != nil {
		return nil, fmt.Errorf("build Cline audit metadata: %w", err)
	}
	guardWrapper, err := buildClineWrapper(guardMetadata)
	if err != nil {
		return nil, fmt.Errorf("build Cline guard wrapper: %w", err)
	}
	auditWrapper, err := buildClineWrapper(auditMetadata)
	if err != nil {
		return nil, fmt.Errorf("build Cline audit wrapper: %w", err)
	}
	if _, err := clineContractForFiles(
		clineHookFile{exists: true, filePath: guardFile.filePath, source: guardWrapper, metadata: &guardMetadata},
		clineHookFile{exists: true, filePath: auditFile.filePath, source: auditWrapper, metadata: &auditMetadata},
	); err != nil {
		return nil, err
	}
	runtimeItems := []struct {
		path, label string
		content     []byte
		mode        os.FileMode
	}{
		{paths.guardPath, "Elydora guard runtime", []byte(generateGuardScript(clineAgentKey, config.AgentID, "", false, "")), 0700},
		{paths.configPath, "Elydora runtime config", runtimeConfig, 0600},
		{paths.keyPath, "Elydora private key", []byte(config.PrivateKey), 0600},
		{paths.auditPath, "Elydora audit runtime", []byte(buildHookScriptWithOutput(clineAgentKey, config.AgentID, "", false, true)), 0700},
	}
	changes := make([]*fileChange, 0, len(runtimeItems)+2)
	for _, item := range runtimeItems {
		change, err := prepareFileChange(item.path, item.label, item.content, item.mode)
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}
	for _, item := range []struct {
		file   clineHookFile
		label  string
		source string
	}{
		{guardFile, "Cline PreToolUse hook", guardWrapper},
		{auditFile, "Cline PostToolUse hook", auditWrapper},
	} {
		change, err := prepareSourceChange(
			item.file.filePath,
			item.label,
			[]byte(item.file.source),
			item.file.exists,
			[]byte(item.source),
			0700,
			false,
		)
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}
	return changes, nil
}
