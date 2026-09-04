package plugins

import (
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type managedRuntimeContract struct {
	agentID   string
	guardPath string
	auditPath string
}

type managedRuntimePaths struct {
	runtimeRoot    string
	agentDirectory string
	configPath     string
	keyPath        string
	guardPath      string
	auditPath      string
}

type managedScriptReference struct {
	agentID    string
	scriptPath string
}

func stringSet(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

var managedRuntimeConfigFields = stringSet(
	"org_id", "agent_id", "kid", "base_url", "token", "agent_name",
)

func sameManagedPath(left, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if absolute, err := filepath.Abs(left); err == nil {
		left = absolute
	}
	if absolute, err := filepath.Abs(right); err == nil {
		right = absolute
	}
	return sameManagedName(left, right)
}

// sameManagedName compares identifiers case-insensitively on Windows.
func sameManagedName(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func sameManagedAgentID(left, right string) bool {
	return sameManagedName(left, right)
}

func managedReferenceKey(agentID string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(agentID)
	}
	return agentID
}

// managedScriptWithin accepts absolute <runtimeRoot>/<agent>/<scriptName> paths.
func managedScriptWithin(scriptPath, scriptName, runtimeRoot string) *managedScriptReference {
	if !filepath.IsAbs(scriptPath) || !sameManagedName(filepath.Base(scriptPath), scriptName) {
		return nil
	}
	agentDirectory := filepath.Dir(scriptPath)
	if !sameManagedPath(filepath.Dir(agentDirectory), runtimeRoot) {
		return nil
	}
	agentID := filepath.Base(agentDirectory)
	if agentID == "" || agentID == "." || agentID == ".." {
		return nil
	}
	return &managedScriptReference{agentID: agentID, scriptPath: scriptPath}
}

func resolveManagedScript(scriptPath, scriptName string) (*managedScriptReference, error) {
	runtimeRoot, err := AgentRuntimeRoot()
	if err != nil {
		return nil, err
	}
	return managedScriptWithin(scriptPath, scriptName, runtimeRoot), nil
}

func validateManagedInstallConfig(config InstallConfig, agentKey, product string) error {
	for _, field := range []struct{ name, value string }{
		{"agent name", config.AgentName},
		{"organization ID", config.OrgID},
		{"agent ID", config.AgentID},
		{"key ID", config.KID},
		{"private key", config.PrivateKey},
		{"base URL", config.BaseURL},
		{"guard script path", config.GuardScriptPath},
	} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%s is required", field.name)
		}
	}
	if config.AgentName != agentKey {
		return fmt.Errorf("%s installation requires agent name %s", product, agentKey)
	}
	if config.Token != "" && strings.TrimSpace(config.Token) == "" {
		return fmt.Errorf("token must contain a non-whitespace value when provided")
	}
	if err := validateManagedPrivateKey(config.PrivateKey); err != nil {
		return err
	}
	return validateManagedBaseURL(config.BaseURL)
}

func resolveManagedRuntimePaths(
	config InstallConfig,
	guardScript, auditScript string,
) (*managedRuntimePaths, error) {
	if config.AgentID == "" {
		return nil, fmt.Errorf("agent ID is required")
	}
	runtimeRoot, err := AgentRuntimeRoot()
	if err != nil {
		return nil, err
	}
	agentDirectory, err := ResolveAgentRuntimeDirectory(config.AgentID)
	if err != nil {
		return nil, err
	}
	paths := &managedRuntimePaths{
		runtimeRoot: runtimeRoot, agentDirectory: agentDirectory,
		configPath: filepath.Join(agentDirectory, "config.json"),
		keyPath:    filepath.Join(agentDirectory, "private.key"),
		guardPath:  filepath.Join(agentDirectory, guardScript),
		auditPath:  filepath.Join(agentDirectory, auditScript),
	}
	if !filepath.IsAbs(config.GuardScriptPath) ||
		!sameManagedPath(config.GuardScriptPath, paths.guardPath) {
		return nil, fmt.Errorf(
			"Elydora guard runtime must use the managed agent directory: %s",
			paths.guardPath,
		)
	}
	if config.HookScript != "" && (!filepath.IsAbs(config.HookScript) ||
		!sameManagedPath(config.HookScript, paths.auditPath)) {
		return nil, fmt.Errorf(
			"Elydora audit runtime must use the managed agent directory: %s",
			paths.auditPath,
		)
	}
	return paths, nil
}

func readManagedRuntimeConfig(path string) (map[string]any, bool, error) {
	snapshot, err := readManagedFile(path, "Elydora runtime config", maxRuntimeConfigBytes)
	if err != nil || snapshot == nil {
		return nil, snapshot != nil, err
	}
	label := fmt.Sprintf("Elydora runtime config at %s", path)
	config, err := decodeStrictJSONObject(snapshot.contents, label)
	return config, true, err
}

func requireManagedRuntimeString(config map[string]any, field, configPath string) (string, error) {
	value, ok := config[field].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("elydora runtime config %s is invalid: %s", field, configPath)
	}
	return value, nil
}

func validateManagedRuntimeConfig(
	config map[string]any,
	expectedAgentID, configPath, agentKey, product string,
) error {
	fields := make([]string, 0, len(config))
	for field := range config {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	for _, field := range fields {
		if _, ok := managedRuntimeConfigFields[field]; !ok {
			return fmt.Errorf(
				`elydora runtime config has unsupported field %q: %s`,
				field,
				configPath,
			)
		}
	}
	for _, field := range []string{"org_id", "kid"} {
		if _, err := requireManagedRuntimeString(config, field, configPath); err != nil {
			return err
		}
	}
	agentID, err := requireManagedRuntimeString(config, "agent_id", configPath)
	if err != nil {
		return err
	}
	if !sameManagedAgentID(agentID, expectedAgentID) || config["agent_name"] != agentKey {
		return fmt.Errorf(
			"elydora runtime identity does not match %s hooks: %s",
			product,
			configPath,
		)
	}
	if _, exists := config["token"]; exists {
		if _, err := requireManagedRuntimeString(config, "token", configPath); err != nil {
			return err
		}
	}
	baseURL, err := requireManagedRuntimeString(config, "base_url", configPath)
	if err != nil {
		return err
	}
	if err := validateManagedBaseURL(baseURL); err != nil {
		return fmt.Errorf("elydora runtime config base URL is invalid at %s: %w", configPath, err)
	}
	return nil
}

type managedArtifact struct {
	path  string
	label string
	limit int64
}

func managedRuntimeArtifacts(agentDirectory, guardScript, auditScript string) []managedArtifact {
	return []managedArtifact{
		{filepath.Join(agentDirectory, "private.key"), "Elydora private key", maxProtectedSecretBytes},
		{filepath.Join(agentDirectory, guardScript), "Elydora guard runtime", maxManagedSourceBytes},
		{filepath.Join(agentDirectory, auditScript), "Elydora audit runtime", maxManagedSourceBytes},
		{filepath.Join(agentDirectory, "chain-state.json"), "Elydora chain state", maxRuntimeConfigBytes},
		{filepath.Join(agentDirectory, "status-cache.json"), "Elydora status cache", maxRuntimeConfigBytes},
		{filepath.Join(agentDirectory, "error.log"), "Elydora error log", maxManagedSourceBytes},
	}
}

// validateManagedRuntimeIdentity rejects an agent directory owned by another identity.
func validateManagedRuntimeIdentity(
	agentDirectory, agentID, agentKey, product string,
	extraArtifacts ...managedArtifact,
) error {
	rootExists, err := managedPhysicalDirectoryExists(
		filepath.Dir(agentDirectory), "Elydora runtime directory",
	)
	if err != nil || !rootExists {
		return err
	}
	directoryExists, err := managedPhysicalDirectoryExists(
		agentDirectory, "Elydora agent runtime directory",
	)
	if err != nil || !directoryExists {
		return err
	}
	configPath := filepath.Join(agentDirectory, "config.json")
	config, configExists, err := readManagedRuntimeConfig(configPath)
	if err != nil {
		return err
	}
	artifactExists := false
	artifacts := append(managedRuntimeArtifacts(agentDirectory, "guard.js", "hook.js"), extraArtifacts...)
	for _, item := range artifacts {
		exists, inspectErr := managedPhysicalFileExists(item.path, item.label, item.limit)
		if inspectErr != nil {
			return inspectErr
		}
		artifactExists = artifactExists || exists
	}
	if !configExists {
		if artifactExists {
			return fmt.Errorf(
				"elydora runtime identity cannot be verified without config.json: %s",
				agentDirectory,
			)
		}
		return nil
	}
	configuredID, ok := config["agent_id"].(string)
	if !ok || config["agent_name"] != agentKey || !sameManagedAgentID(configuredID, agentID) {
		return fmt.Errorf(
			"elydora runtime config identity does not match %s agent %s: %s",
			product,
			agentID,
			configPath,
		)
	}
	return nil
}

type expectedRuntimeScripts func(agentID string, config map[string]any) (guard, audit []byte)

// managedRuntimeContractExists is the strict status check; nil expected accepts non-empty scripts.
func managedRuntimeContractExists(
	contract managedRuntimeContract,
	agentKey, product string,
	expected expectedRuntimeScripts,
) (bool, error) {
	runtimeRoot, err := AgentRuntimeRoot()
	if err != nil {
		return false, err
	}
	agentDirectory := filepath.Dir(contract.guardPath)
	if !sameManagedPath(filepath.Dir(agentDirectory), runtimeRoot) ||
		!sameManagedPath(contract.auditPath, filepath.Join(agentDirectory, "hook.js")) {
		return false, nil
	}
	for _, directory := range []struct{ path, label string }{
		{runtimeRoot, "Elydora runtime directory"},
		{agentDirectory, "Elydora agent runtime directory"},
	} {
		exists, err := managedPhysicalDirectoryExists(directory.path, directory.label)
		if err != nil || !exists {
			return false, err
		}
	}
	configPath := filepath.Join(agentDirectory, "config.json")
	keyPath := filepath.Join(agentDirectory, "private.key")
	config, configExists, err := readManagedRuntimeConfig(configPath)
	if err != nil {
		return false, err
	}
	key, err := readManagedFile(keyPath, "Elydora private key", maxProtectedSecretBytes)
	if err != nil {
		return false, err
	}
	guard, err := readManagedFile(contract.guardPath, "Elydora guard runtime", maxManagedSourceBytes)
	if err != nil {
		return false, err
	}
	audit, err := readManagedFile(contract.auditPath, "Elydora audit runtime", maxManagedSourceBytes)
	if err != nil {
		return false, err
	}
	if !configExists || key == nil || guard == nil || audit == nil {
		return false, nil
	}
	if err := validateManagedRuntimeConfig(config, contract.agentID, configPath, agentKey, product); err != nil {
		return false, err
	}
	if err := validateManagedPrivateKey(string(key.contents)); err != nil {
		return false, fmt.Errorf("elydora private key at %s: %w", keyPath, err)
	}
	if expected == nil {
		return len(guard.contents) > 0 && len(audit.contents) > 0, nil
	}
	expectedGuard, expectedAudit := expected(contract.agentID, config)
	return string(guard.contents) == string(expectedGuard) &&
		string(audit.contents) == string(expectedAudit), nil
}

func managedRuntimeFilesExist(
	contracts []managedRuntimeContract,
	agentKey, product string,
	expected expectedRuntimeScripts,
) (bool, error) {
	for _, contract := range contracts {
		exists, err := managedRuntimeContractExists(contract, agentKey, product, expected)
		if err != nil || exists {
			return exists, err
		}
	}
	return false, nil
}

// managedRuntimePresent is the presence-only status check; identity mismatch reports false.
func managedRuntimePresent(contract managedRuntimeContract, agentKey string) (bool, error) {
	agentDirectory := filepath.Dir(contract.guardPath)
	directoryExists, err := managedPhysicalDirectoryExists(
		agentDirectory, "Elydora agent runtime directory",
	)
	if err != nil || !directoryExists {
		return false, err
	}
	config, exists, err := readManagedRuntimeConfig(filepath.Join(agentDirectory, "config.json"))
	if err != nil {
		return false, err
	}
	configuredID, idOK := config["agent_id"].(string)
	if !exists || !idOK || config["agent_name"] != agentKey ||
		!sameManagedAgentID(configuredID, contract.agentID) {
		return false, nil
	}
	for _, item := range []managedArtifact{
		{contract.guardPath, "Elydora guard runtime", maxManagedSourceBytes},
		{contract.auditPath, "Elydora audit runtime", maxManagedSourceBytes},
		{filepath.Join(agentDirectory, "private.key"), "Elydora private key", maxProtectedSecretBytes},
	} {
		exists, inspectErr := managedPhysicalFileExists(item.path, item.label, item.limit)
		if inspectErr != nil || !exists {
			return false, inspectErr
		}
	}
	return true, nil
}

func managedRuntimeFilesPresent(contracts []managedRuntimeContract, agentKey string) (bool, error) {
	for _, contract := range contracts {
		present, err := managedRuntimePresent(contract, agentKey)
		if err != nil || present {
			return present, err
		}
	}
	return false, nil
}
