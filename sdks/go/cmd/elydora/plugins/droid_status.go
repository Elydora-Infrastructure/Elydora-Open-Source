package plugins

import (
	"bytes"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

func readDroidRuntimeConfig(path string) (map[string]any, bool, error) {
	snapshot, err := readManagedFile(path, "Elydora runtime config", maxRuntimeConfigBytes)
	if err != nil || snapshot == nil {
		return nil, snapshot != nil, err
	}
	config, err := decodeStrictJSONObject(
		snapshot.contents,
		fmt.Sprintf("Elydora runtime config at %s", path),
	)
	return config, true, err
}

func requireDroidRuntimeString(
	config map[string]any,
	field, configPath string,
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

func validateDroidRuntimeConfig(
	config map[string]any,
	expectedAgentID, configPath string,
) error {
	supported := stringSet(
		"org_id",
		"agent_id",
		"kid",
		"base_url",
		"token",
		"agent_name",
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
	if _, err := requireDroidRuntimeString(config, "org_id", configPath); err != nil {
		return err
	}
	if _, err := requireDroidRuntimeString(config, "kid", configPath); err != nil {
		return err
	}
	agentID, err := requireDroidRuntimeString(config, "agent_id", configPath)
	if err != nil {
		return err
	}
	if !sameDroidAgentID(agentID, expectedAgentID) || config["agent_name"] != droidAgentKey {
		return fmt.Errorf(
			"Elydora runtime identity does not match Factory Droid hooks: %s",
			configPath,
		)
	}
	if _, exists := config["token"]; exists {
		if _, err := requireDroidRuntimeString(config, "token", configPath); err != nil {
			return err
		}
	}
	baseURL, err := requireDroidRuntimeString(config, "base_url", configPath)
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

func validateDroidRuntimeIdentity(agentDirectory, agentID string) error {
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
	config, configExists, err := readDroidRuntimeConfig(configPath)
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
		{filepath.Join(agentDirectory, droidGuardScript), "Elydora guard runtime", maxManagedSourceBytes},
		{filepath.Join(agentDirectory, droidAuditScript), "Elydora audit runtime", maxManagedSourceBytes},
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
	if !ok || config["agent_name"] != droidAgentKey ||
		!sameDroidAgentID(configuredID, agentID) {
		return fmt.Errorf(
			"Elydora runtime config identity does not match Factory Droid agent %s: %s",
			agentID,
			configPath,
		)
	}
	return nil
}

func validDroidContractPaths(contract droidRuntimeContract) (bool, error) {
	runtimeRoot, err := AgentRuntimeRoot()
	if err != nil {
		return false, err
	}
	agentDirectory := filepath.Dir(contract.guardPath)
	return sameDroidPath(filepath.Dir(agentDirectory), runtimeRoot) &&
		sameDroidPath(
			contract.guardPath,
			filepath.Join(agentDirectory, droidGuardScript),
		) &&
		sameDroidPath(
			contract.auditPath,
			filepath.Join(agentDirectory, droidAuditScript),
		), nil
}

func droidRuntimeContractExists(contract droidRuntimeContract) (bool, error) {
	validPaths, err := validDroidContractPaths(contract)
	if err != nil || !validPaths {
		return false, err
	}
	runtimeRoot, err := AgentRuntimeRoot()
	if err != nil {
		return false, err
	}
	agentDirectory := filepath.Dir(contract.guardPath)
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
	config, configExists, err := readDroidRuntimeConfig(configPath)
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
	if err := validateDroidRuntimeConfig(config, contract.agentID, configPath); err != nil {
		return false, err
	}
	if err := validateManagedPrivateKey(string(key.contents)); err != nil {
		return false, fmt.Errorf("Elydora private key at %s: %w", keyPath, err)
	}
	expectedGuard := []byte(generateGuardScript(droidAgentKey, contract.agentID, "", false, ""))
	expectedAudit := []byte(buildHookScriptWithOutput(
		droidAgentKey,
		contract.agentID,
		"",
		false,
		true,
	))
	return bytes.Equal(guard.contents, expectedGuard) &&
		bytes.Equal(audit.contents, expectedAudit), nil
}

func droidRuntimeFilesExist(contracts []droidRuntimeContract) (bool, error) {
	for _, contract := range contracts {
		exists, err := droidRuntimeContractExists(contract)
		if err != nil {
			return false, err
		}
		if exists {
			return true, nil
		}
	}
	return false, nil
}
