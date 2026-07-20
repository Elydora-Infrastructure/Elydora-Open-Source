package plugins

import (
	"bytes"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

func managedAugmentIDs(
	groups []augmentGroup,
	wrapperName string,
	runtimeRoot string,
) map[string]string {
	result := map[string]string{}
	for _, group := range groups {
		for _, handler := range group.handlers {
			agentID, managed := managedAugmentAgentID(
				handler,
				wrapperName,
				runtimeRoot,
			)
			if !managed {
				continue
			}
			key := agentID
			if runtime.GOOS == "windows" {
				key = strings.ToLower(key)
			}
			result[key] = agentID
		}
	}
	return result
}

func augmentRuntimeContracts(
	hooks augmentHooks,
	runtimeRoot string,
) []augmentRuntimeContract {
	guards := managedAugmentIDs(
		hooks["PreToolUse"],
		augmentGuardWrapperName(),
		runtimeRoot,
	)
	audits := managedAugmentIDs(
		hooks["PostToolUse"],
		augmentAuditWrapperName(),
		runtimeRoot,
	)
	keys := make([]string, 0, len(guards))
	for key := range guards {
		if _, exists := audits[key]; exists {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	contracts := make([]augmentRuntimeContract, 0, len(keys))
	for _, key := range keys {
		agentID := guards[key]
		agentDirectory := filepath.Join(runtimeRoot, agentID)
		wrappers := resolveAugmentWrapperPaths(agentDirectory)
		contracts = append(contracts, augmentRuntimeContract{
			agentID:      agentID,
			guardPath:    filepath.Join(agentDirectory, augmentGuardScript),
			auditPath:    filepath.Join(agentDirectory, augmentAuditScript),
			guardWrapper: wrappers.guard,
			auditWrapper: wrappers.audit,
		})
	}
	return contracts
}

func readAugmentRuntimeConfig(path string) (map[string]any, bool, error) {
	snapshot, err := readManagedFile(
		path,
		"Elydora runtime config",
		maxRuntimeConfigBytes,
	)
	if err != nil || snapshot == nil {
		return nil, snapshot != nil, err
	}
	label := fmt.Sprintf("Elydora runtime config at %s", path)
	config, err := decodeStrictJSONObject(snapshot.contents, label)
	return config, true, err
}

func requireAugmentRuntimeString(
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

func validateAugmentRuntimeConfig(
	config map[string]any,
	expectedAgentID string,
	configPath string,
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
	if _, err := requireAugmentRuntimeString(config, "org_id", configPath); err != nil {
		return err
	}
	if _, err := requireAugmentRuntimeString(config, "kid", configPath); err != nil {
		return err
	}
	agentID, err := requireAugmentRuntimeString(config, "agent_id", configPath)
	if err != nil {
		return err
	}
	if !sameAugmentAgentID(agentID, expectedAgentID) ||
		config["agent_name"] != augmentAgentKey {
		return fmt.Errorf(
			"Elydora runtime identity does not match Auggie hooks: %s",
			configPath,
		)
	}
	if _, exists := config["token"]; exists {
		if _, err := requireAugmentRuntimeString(config, "token", configPath); err != nil {
			return err
		}
	}
	baseURL, err := requireAugmentRuntimeString(config, "base_url", configPath)
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

func validateAugmentRuntimeIdentity(agentDirectory, agentID string) error {
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
	config, configExists, err := readAugmentRuntimeConfig(configPath)
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
		{filepath.Join(agentDirectory, augmentGuardScript), "Elydora guard runtime", maxManagedSourceBytes},
		{filepath.Join(agentDirectory, augmentAuditScript), "Elydora audit runtime", maxManagedSourceBytes},
		{filepath.Join(agentDirectory, augmentGuardWrapperName()), "Auggie guard wrapper", maxManagedSourceBytes},
		{filepath.Join(agentDirectory, augmentAuditWrapperName()), "Auggie audit wrapper", maxManagedSourceBytes},
		{filepath.Join(agentDirectory, "chain-state.json"), "Elydora chain state", maxRuntimeConfigBytes},
		{filepath.Join(agentDirectory, "status-cache.json"), "Elydora status cache", maxRuntimeConfigBytes},
		{filepath.Join(agentDirectory, "error.log"), "Elydora error log", maxManagedSourceBytes},
	} {
		exists, inspectErr := managedPhysicalFileExists(
			item.path,
			item.label,
			item.limit,
		)
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
	if !ok || config["agent_name"] != augmentAgentKey ||
		!sameAugmentAgentID(configuredID, agentID) {
		return fmt.Errorf(
			"Elydora runtime config identity does not match Auggie agent %s: %s",
			agentID,
			configPath,
		)
	}
	return nil
}

func augmentRuntimeContractExists(
	contract augmentRuntimeContract,
	runtimeRoot string,
	nodePath string,
) (bool, error) {
	agentDirectory := filepath.Dir(contract.guardPath)
	wrappers := resolveAugmentWrapperPaths(agentDirectory)
	if !sameAugmentPath(filepath.Dir(agentDirectory), runtimeRoot) ||
		!sameAugmentPath(
			contract.auditPath,
			filepath.Join(agentDirectory, augmentAuditScript),
		) ||
		!sameAugmentPath(contract.guardWrapper, wrappers.guard) ||
		!sameAugmentPath(contract.auditWrapper, wrappers.audit) {
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
	config, configExists, err := readAugmentRuntimeConfig(configPath)
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
	guardWrapper, err := readManagedFile(
		contract.guardWrapper,
		"Auggie guard wrapper",
		maxManagedSourceBytes,
	)
	if err != nil {
		return false, err
	}
	auditWrapper, err := readManagedFile(
		contract.auditWrapper,
		"Auggie audit wrapper",
		maxManagedSourceBytes,
	)
	if err != nil {
		return false, err
	}
	if !configExists || key == nil || guard == nil || audit == nil ||
		guardWrapper == nil || auditWrapper == nil {
		return false, nil
	}
	if err := validateAugmentRuntimeConfig(
		config,
		contract.agentID,
		configPath,
	); err != nil {
		return false, err
	}
	if err := validateManagedPrivateKey(string(key.contents)); err != nil {
		return false, fmt.Errorf("Elydora private key at %s: %w", keyPath, err)
	}
	return len(guard.contents) > 0 &&
		len(audit.contents) > 0 &&
		bytes.Equal(
			guardWrapper.contents,
			buildAugmentWrapper(nodePath, contract.guardPath),
		) &&
		bytes.Equal(
			auditWrapper.contents,
			buildAugmentWrapper(nodePath, contract.auditPath),
		), nil
}

func augmentRuntimeFilesExist(
	contracts []augmentRuntimeContract,
	runtimeRoot string,
) (bool, error) {
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		return false, err
	}
	if err := requireAugmentAbsoluteNode(nodePath); err != nil {
		return false, err
	}
	for _, contract := range contracts {
		exists, err := augmentRuntimeContractExists(contract, runtimeRoot, nodePath)
		if err != nil {
			return false, err
		}
		if exists {
			return true, nil
		}
	}
	return false, nil
}
