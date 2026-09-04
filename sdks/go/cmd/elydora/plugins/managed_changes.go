package plugins

import (
	"encoding/json"
	"fmt"
	"os"
)

func buildManagedRuntimeConfig(config InstallConfig, agentKey string) ([]byte, error) {
	value := map[string]any{
		"org_id": config.OrgID, "agent_id": config.AgentID, "kid": config.KID,
		"base_url": config.BaseURL, "agent_name": agentKey,
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

// managedRuntimeFileChanges prepares the guard, config, key, and audit runtime changes.
func managedRuntimeFileChanges(
	config InstallConfig,
	paths *managedRuntimePaths,
	agentKey, guardScript, auditScript string,
) ([]*fileChange, error) {
	runtimeConfig, err := buildManagedRuntimeConfig(config, agentKey)
	if err != nil {
		return nil, err
	}
	items := []struct {
		path, label string
		content     []byte
		mode        os.FileMode
	}{
		{paths.guardPath, "Elydora guard runtime", []byte(guardScript), 0700},
		{paths.configPath, "Elydora runtime config", runtimeConfig, 0600},
		{paths.keyPath, "Elydora private key", []byte(config.PrivateKey), 0600},
		{paths.auditPath, "Elydora audit runtime", []byte(auditScript), 0700},
	}
	changes := make([]*fileChange, 0, len(items)+1)
	for _, item := range items {
		change, err := prepareFileChange(item.path, item.label, item.content, item.mode)
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}
	return changes, nil
}

func hasFileChanges(changes []*fileChange) bool {
	for _, change := range changes {
		if change != nil {
			return true
		}
	}
	return false
}

// writeManagedChanges creates the runtime and settings directories, then commits.
func writeManagedChanges(
	changes []*fileChange,
	label string,
	rename renameFunc,
	runtimeRoot, agentDirectory, settingsDirectory, settingsLabel string,
) error {
	if !hasFileChanges(changes) {
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
	if settingsDirectory != "" {
		if err := ensureManagedDirectory(settingsDirectory, settingsLabel); err != nil {
			return err
		}
	}
	return writeChanges(changes, label, rename)
}
