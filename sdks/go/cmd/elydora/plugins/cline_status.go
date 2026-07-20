package plugins

import (
	"bytes"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

func readClineRuntimeConfig(path string) (map[string]any, bool, error) {
	snapshot, err := readManagedFile(path, "Elydora runtime config", maxRuntimeConfigBytes)
	if err != nil || snapshot == nil {
		return nil, snapshot != nil, err
	}
	label := fmt.Sprintf("Elydora runtime config at %s", path)
	config, err := decodeStrictJSONObject(snapshot.contents, label)
	return config, true, err
}

func requireClineRuntimeString(
	config map[string]any,
	field string,
	configPath string,
) (string, error) {
	value, ok := config[field].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf(
			"Elydora runtime config %s is invalid: %s",
			field,
			configPath,
		)
	}
	return value, nil
}

func validateClineRuntimeConfig(
	config map[string]any,
	expectedAgentID string,
	configPath string,
) error {
	supported := stringSet(
		"org_id", "agent_id", "kid", "base_url", "token", "agent_name",
	)
	fields := make([]string, 0, len(config))
	for field := range config {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	for _, field := range fields {
		if _, ok := supported[field]; !ok {
			return fmt.Errorf(
				`Elydora runtime config has unsupported field %q: %s`,
				field,
				configPath,
			)
		}
	}
	if _, err := requireClineRuntimeString(config, "org_id", configPath); err != nil {
		return err
	}
	if _, err := requireClineRuntimeString(config, "kid", configPath); err != nil {
		return err
	}
	agentID, err := requireClineRuntimeString(config, "agent_id", configPath)
	if err != nil {
		return err
	}
	if !sameClineAgentID(agentID, expectedAgentID) || config["agent_name"] != clineAgentKey {
		return fmt.Errorf(
			"Elydora runtime identity does not match Cline hooks: %s",
			configPath,
		)
	}
	if _, exists := config["token"]; exists {
		if _, err := requireClineRuntimeString(config, "token", configPath); err != nil {
			return err
		}
	}
	baseURL, err := requireClineRuntimeString(config, "base_url", configPath)
	if err != nil {
		return err
	}
	if err := validateManagedBaseURL(baseURL); err != nil {
		return fmt.Errorf(
			"Elydora runtime config base URL is invalid at %s: %w",
			configPath,
			err,
		)
	}
	return nil
}

func validateClineRuntimeIdentity(agentDirectory, agentID string) error {
	runtimeRoot := filepath.Dir(agentDirectory)
	rootExists, err := managedPhysicalDirectoryExists(
		runtimeRoot,
		"Elydora runtime directory",
	)
	if err != nil || !rootExists {
		return err
	}
	directoryExists, err := managedPhysicalDirectoryExists(
		agentDirectory,
		"Elydora agent runtime directory",
	)
	if err != nil || !directoryExists {
		return err
	}
	configPath := filepath.Join(agentDirectory, "config.json")
	config, configExists, err := readClineRuntimeConfig(configPath)
	if err != nil {
		return err
	}
	artifactExists := false
	for _, item := range []struct {
		path  string
		label string
		limit int64
	}{
		{filepath.Join(agentDirectory, "private.key"), "Elydora private key", maxProtectedSecretBytes},
		{filepath.Join(agentDirectory, clineGuardScript), "Elydora guard runtime", maxManagedSourceBytes},
		{filepath.Join(agentDirectory, clineAuditScript), "Elydora audit runtime", maxManagedSourceBytes},
		{filepath.Join(agentDirectory, "chain-state.json"), "Elydora chain state", maxRuntimeConfigBytes},
		{filepath.Join(agentDirectory, "status-cache.json"), "Elydora status cache", maxRuntimeConfigBytes},
		{filepath.Join(agentDirectory, "error.log"), "Elydora error log", maxManagedSourceBytes},
	} {
		exists, inspectErr := managedPhysicalFileExists(item.path, item.label, item.limit)
		if inspectErr != nil {
			return inspectErr
		}
		artifactExists = artifactExists || exists
	}
	if !configExists {
		if artifactExists {
			return fmt.Errorf(
				"Elydora runtime identity cannot be verified without config.json: %s",
				agentDirectory,
			)
		}
		return nil
	}
	configuredID, ok := config["agent_id"].(string)
	if !ok || config["agent_name"] != clineAgentKey ||
		!sameClineAgentID(configuredID, agentID) {
		return fmt.Errorf(
			"Elydora runtime config identity does not match Cline agent %s: %s",
			agentID,
			configPath,
		)
	}
	return nil
}

func clineRuntimeFilesExist(contract *clineRuntimeContract) (bool, error) {
	runtimeRoot, err := AgentRuntimeRoot()
	if err != nil {
		return false, err
	}
	if !sameClinePathValue(filepath.Dir(contract.agentDirectory), runtimeRoot) ||
		!sameClinePathValue(contract.guardPath, filepath.Join(contract.agentDirectory, clineGuardScript)) ||
		!sameClinePathValue(contract.auditPath, filepath.Join(contract.agentDirectory, clineAuditScript)) {
		return false, nil
	}
	rootExists, err := managedPhysicalDirectoryExists(runtimeRoot, "Elydora runtime directory")
	if err != nil || !rootExists {
		return false, err
	}
	directoryExists, err := managedPhysicalDirectoryExists(
		contract.agentDirectory,
		"Elydora agent runtime directory",
	)
	if err != nil || !directoryExists {
		return false, err
	}
	configPath := filepath.Join(contract.agentDirectory, "config.json")
	keyPath := filepath.Join(contract.agentDirectory, "private.key")
	config, configExists, err := readClineRuntimeConfig(configPath)
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
	if err := validateClineRuntimeConfig(config, contract.agentID, configPath); err != nil {
		return false, err
	}
	if err := validateManagedPrivateKey(string(key.contents)); err != nil {
		return false, fmt.Errorf("Elydora private key at %s: %w", keyPath, err)
	}
	expectedGuard := []byte(generateGuardScript(clineAgentKey, contract.agentID, "", false, ""))
	expectedAudit := []byte(buildHookScriptWithOutput(clineAgentKey, contract.agentID, "", false, true))
	return bytes.Equal(guard.contents, expectedGuard) && bytes.Equal(audit.contents, expectedAudit), nil
}
