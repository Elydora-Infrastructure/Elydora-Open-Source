package plugins

import (
	"fmt"
	"os"
	"path/filepath"
)

func grokHomePath() (string, error) {
	configured := os.Getenv("GROK_HOME")
	if configured == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		return filepath.Join(home, ".grok"), nil
	}
	home, err := filepath.Abs(configured)
	if err != nil {
		return "", fmt.Errorf("resolve GROK_HOME at %s: %w", configured, err)
	}
	return home, nil
}

func grokConfigPath() (string, error) {
	home, err := grokHomePath()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "hooks", grokConfigFile), nil
}

func readGrokDocument() (*grokDocument, error) {
	configPath, err := grokConfigPath()
	if err != nil {
		return nil, err
	}
	home := filepath.Dir(filepath.Dir(configPath))
	if _, err := managedPhysicalDirectoryExists(home, "Grok home directory"); err != nil {
		return nil, err
	}
	hooksDirectory := filepath.Dir(configPath)
	if _, err := managedPhysicalDirectoryExists(
		hooksDirectory,
		"Grok hooks directory",
	); err != nil {
		return nil, err
	}
	snapshot, err := readManagedFile(
		configPath,
		"Grok user hooks",
		maxManagedSourceBytes,
	)
	if err != nil {
		return nil, err
	}
	if snapshot == nil {
		return createGrokDocument(configPath), nil
	}
	return parseGrokDocument(configPath, snapshot.contents)
}

func prepareRenderedGrokChange(
	rendered *grokRenderedDocument,
) (*fileChange, error) {
	if rendered == nil || !rendered.changed {
		return nil, nil
	}
	return prepareSourceChange(
		rendered.document.configPath,
		"Grok user hooks",
		rendered.document.raw,
		rendered.document.exists,
		rendered.next,
		0600,
		rendered.remove,
	)
}

func writeGrokChanges(
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
	if err := ensureManagedDirectory(
		filepath.Dir(hooksDirectory),
		"Grok home directory",
	); err != nil {
		return err
	}
	if err := ensureManagedDirectory(hooksDirectory, "Grok hooks directory"); err != nil {
		return err
	}
	return writeChanges(changes, label, rename)
}
