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

func codexRuntimeFilesExist(contracts []managedRuntimeContract) (bool, error) {
	return managedRuntimeFilesPresent(contracts, codexAgentKey)
}
