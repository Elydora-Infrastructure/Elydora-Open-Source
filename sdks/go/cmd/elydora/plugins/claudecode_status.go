package plugins

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

func readClaudeRuntimeConfig(path string) (map[string]any, bool, error) {
	snapshot, err := readManagedFile(path, "Elydora runtime config", maxRuntimeConfigBytes)
	if err != nil || snapshot == nil {
		return nil, snapshot != nil, err
	}
	label := fmt.Sprintf("Elydora runtime config at %s", path)
	config, err := decodeStrictJSONObject(snapshot.contents, label)
	return config, true, err
}

func requireClaudeRuntimeString(
	config map[string]any,
	field string,
	configPath string,
) (string, error) {
	value, ok := config[field].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf(
			"elydora runtime config %s is invalid: %s",
			field,
			configPath,
		)
	}
	return value, nil
}

func validateClaudeRuntimeConfig(
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
				`elydora runtime config has unsupported field %q: %s`,
				field,
				configPath,
			)
		}
	}
	if _, err := requireClaudeRuntimeString(config, "org_id", configPath); err != nil {
		return err
	}
	if _, err := requireClaudeRuntimeString(config, "kid", configPath); err != nil {
		return err
	}
	agentID, err := requireClaudeRuntimeString(config, "agent_id", configPath)
	if err != nil {
		return err
	}
	if !sameClaudeAgentID(agentID, expectedAgentID) ||
		config["agent_name"] != claudeAgentKey {
		return fmt.Errorf(
			"elydora runtime identity does not match Claude Code hooks: %s",
			configPath,
		)
	}
	if _, exists := config["token"]; exists {
		if _, err := requireClaudeRuntimeString(config, "token", configPath); err != nil {
			return err
		}
	}
	baseURL, err := requireClaudeRuntimeString(config, "base_url", configPath)
	if err != nil {
		return err
	}
	if err := validateManagedBaseURL(baseURL); err != nil {
		return fmt.Errorf(
			"elydora runtime config base URL is invalid at %s: %w",
			configPath,
			err,
		)
	}
	return nil
}

func validateClaudeRuntimeIdentity(agentDirectory, agentID string) error {
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
	config, configExists, err := readClaudeRuntimeConfig(configPath)
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
		{filepath.Join(agentDirectory, claudeGuardScript), "Elydora guard runtime", maxManagedSourceBytes},
		{filepath.Join(agentDirectory, claudeAuditScript), "Elydora audit runtime", maxManagedSourceBytes},
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
				"elydora runtime identity cannot be verified without config.json: %s",
				agentDirectory,
			)
		}
		return nil
	}
	configuredID, ok := config["agent_id"].(string)
	if !ok || config["agent_name"] != claudeAgentKey ||
		!sameClaudeAgentID(configuredID, agentID) {
		return fmt.Errorf(
			"elydora runtime config identity does not match Claude Code agent %s: %s",
			agentID,
			configPath,
		)
	}
	return nil
}

func claudeRuntimeContractExists(contract claudeRuntimeContract) (bool, error) {
	runtimeRoot, err := AgentRuntimeRoot()
	if err != nil {
		return false, err
	}
	agentDirectory := filepath.Dir(contract.guardPath)
	if !sameClaudePath(filepath.Dir(agentDirectory), runtimeRoot) ||
		!sameClaudePath(
			contract.auditPath,
			filepath.Join(agentDirectory, claudeAuditScript),
		) {
		return false, nil
	}
	rootExists, err := managedPhysicalDirectoryExists(
		runtimeRoot,
		"Elydora runtime directory",
	)
	if err != nil || !rootExists {
		return false, err
	}
	directoryExists, err := managedPhysicalDirectoryExists(
		agentDirectory,
		"Elydora agent runtime directory",
	)
	if err != nil || !directoryExists {
		return false, err
	}
	configPath := filepath.Join(agentDirectory, "config.json")
	keyPath := filepath.Join(agentDirectory, "private.key")
	config, configExists, err := readClaudeRuntimeConfig(configPath)
	if err != nil {
		return false, err
	}
	key, err := readManagedFile(keyPath, "Elydora private key", maxProtectedSecretBytes)
	if err != nil {
		return false, err
	}
	guard, err := readManagedFile(
		contract.guardPath,
		"Elydora guard runtime",
		maxManagedSourceBytes,
	)
	if err != nil {
		return false, err
	}
	audit, err := readManagedFile(
		contract.auditPath,
		"Elydora audit runtime",
		maxManagedSourceBytes,
	)
	if err != nil {
		return false, err
	}
	if !configExists || key == nil || guard == nil || audit == nil {
		return false, nil
	}
	if err := validateClaudeRuntimeConfig(config, contract.agentID, configPath); err != nil {
		return false, err
	}
	if err := validateManagedPrivateKey(string(key.contents)); err != nil {
		return false, fmt.Errorf("elydora private key at %s: %w", keyPath, err)
	}
	return len(guard.contents) > 0 && len(audit.contents) > 0, nil
}

func claudeRuntimeFilesExist(contracts []claudeRuntimeContract) (bool, error) {
	for _, contract := range contracts {
		exists, err := claudeRuntimeContractExists(contract)
		if err != nil {
			return false, err
		}
		if exists {
			return true, nil
		}
	}
	return false, nil
}
