package plugins

import (
	"fmt"
	"os"
	"path/filepath"
)

func codexHomePath() (string, error) {
	configured := os.Getenv("CODEX_HOME")
	if configured == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		return filepath.Join(home, ".codex"), nil
	}
	info, err := os.Stat(configured)
	if err != nil {
		return "", fmt.Errorf("resolve CODEX_HOME at %s: %w", configured, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("CODEX_HOME is not a directory: %s", configured)
	}
	canonical, err := filepath.EvalSymlinks(configured)
	if err != nil {
		return "", fmt.Errorf("canonicalize CODEX_HOME at %s: %w", configured, err)
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return "", fmt.Errorf("resolve canonical CODEX_HOME at %s: %w", configured, err)
	}
	exists, err := managedPhysicalDirectoryExists(canonical, "CODEX_HOME")
	if err != nil {
		return "", err
	}
	if !exists {
		return "", fmt.Errorf("CODEX_HOME is missing: %s", canonical)
	}
	return canonical, nil
}

func codexConfigPath() (string, error) {
	home, err := codexHomePath()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, codexConfigFile), nil
}

func readCodexDocument() (*codexDocument, error) {
	filePath, err := codexConfigPath()
	if err != nil {
		return nil, err
	}
	directory := filepath.Dir(filePath)
	if _, err := managedPhysicalDirectoryExists(directory, "Codex hooks directory"); err != nil {
		return nil, err
	}
	snapshot, err := readManagedFile(filePath, "Codex user hooks", maxManagedSourceBytes)
	if err != nil {
		return nil, err
	}
	if snapshot == nil {
		return createCodexDocument(filePath), nil
	}
	return parseCodexDocument(filePath, snapshot.contents)
}

func prepareRenderedCodexChange(rendered *codexRenderedDocument) (*fileChange, error) {
	if rendered == nil || !rendered.changed {
		return nil, nil
	}
	return prepareSourceChange(
		rendered.document.filePath,
		"Codex user hooks",
		rendered.document.raw,
		rendered.document.exists,
		rendered.next,
		0600,
		rendered.remove,
	)
}

func writeCodexChanges(
	changes []*fileChange,
	label string,
	rename renameFunc,
	runtimeRoot string,
	agentDirectory string,
	hooksDirectory string,
) error {
	hasChanges := false
	for _, change := range changes {
		if change != nil {
			hasChanges = true
			break
		}
	}
	if !hasChanges {
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
	if err := ensureManagedDirectory(hooksDirectory, "Codex hooks directory"); err != nil {
		return err
	}
	return writeChanges(changes, label, rename)
}

func readCodexRuntimeConfig(path string) (map[string]any, bool, error) {
	snapshot, err := readManagedFile(path, "Elydora runtime config", maxRuntimeConfigBytes)
	if err != nil || snapshot == nil {
		return nil, snapshot != nil, err
	}
	label := fmt.Sprintf("Elydora runtime config at %s", path)
	config, err := decodeStrictJSONObject(snapshot.contents, label)
	return config, true, err
}

func validateCodexRuntimeIdentity(agentDirectory, agentID string) error {
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
	config, configExists, err := readCodexRuntimeConfig(configPath)
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
		{filepath.Join(agentDirectory, codexGuardScript), "Elydora guard runtime", maxManagedSourceBytes},
		{filepath.Join(agentDirectory, codexAuditScript), "Elydora audit runtime", maxManagedSourceBytes},
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
	if !ok || config["agent_name"] != codexAgentKey ||
		!sameCodexAgentID(configuredID, agentID) {
		return fmt.Errorf(
			"elydora runtime config identity does not match Codex agent %s: %s",
			agentID,
			configPath,
		)
	}
	return nil
}

func codexRuntimeFilesExist(contracts []codexRuntimeContract) (bool, error) {
	for _, contract := range contracts {
		agentDirectory := filepath.Dir(contract.guardPath)
		directoryExists, err := managedPhysicalDirectoryExists(
			agentDirectory,
			"Elydora agent runtime directory",
		)
		if err != nil {
			return false, err
		}
		if !directoryExists {
			continue
		}
		config, exists, err := readCodexRuntimeConfig(
			filepath.Join(agentDirectory, "config.json"),
		)
		if err != nil {
			return false, err
		}
		configuredID, idOK := config["agent_id"].(string)
		if !exists || !idOK || config["agent_name"] != codexAgentKey ||
			!sameCodexAgentID(configuredID, contract.agentID) {
			continue
		}
		complete := true
		for _, item := range []struct {
			path  string
			label string
			limit int64
		}{
			{contract.guardPath, "Elydora guard runtime", maxManagedSourceBytes},
			{contract.auditPath, "Elydora audit runtime", maxManagedSourceBytes},
			{filepath.Join(agentDirectory, "private.key"), "Elydora private key", maxProtectedSecretBytes},
		} {
			exists, inspectErr := managedPhysicalFileExists(item.path, item.label, item.limit)
			if inspectErr != nil {
				return false, inspectErr
			}
			complete = complete && exists
		}
		if complete {
			return true, nil
		}
	}
	return false, nil
}
