package plugins

import (
	"fmt"
	"os"
	"path/filepath"
)

func geminiConfigurationDirectory() (string, error) {
	configured := os.Getenv("GEMINI_CLI_HOME")
	if configured == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		configured = home
	}
	return filepath.Join(configured, ".gemini"), nil
}

func geminiSettingsPath() (string, error) {
	directory, err := geminiConfigurationDirectory()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, geminiConfigFile), nil
}

func readGeminiDocument() (*geminiDocument, error) {
	filePath, err := geminiSettingsPath()
	if err != nil {
		return nil, err
	}
	if _, err := managedPhysicalDirectoryExists(
		filepath.Dir(filePath),
		"Gemini CLI configuration directory",
	); err != nil {
		return nil, err
	}
	snapshot, err := readManagedFile(
		filePath,
		"Gemini CLI user settings",
		maxManagedSourceBytes,
	)
	if err != nil {
		return nil, err
	}
	if snapshot == nil {
		return createGeminiDocument(filePath)
	}
	return parseGeminiDocument(true, filePath, snapshot.contents)
}

func prepareRenderedGeminiChange(
	rendered *geminiRenderedDocument,
) (*fileChange, error) {
	if rendered == nil || !rendered.changed {
		return nil, nil
	}
	return prepareSourceChange(
		rendered.document.filePath,
		"Gemini CLI user settings",
		rendered.document.raw,
		rendered.document.exists,
		rendered.next,
		0600,
		rendered.remove,
	)
}

func writeGeminiChanges(
	changes []*fileChange,
	label string,
	rename renameFunc,
	runtimeRoot string,
	agentDirectory string,
	settingsDirectory string,
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
		settingsDirectory,
		"Gemini CLI configuration directory",
	); err != nil {
		return err
	}
	return writeChanges(changes, label, rename)
}
