package plugins

import (
	"fmt"
	"os"
	"path/filepath"
)

func claudeConfigDirectory() (string, error) {
	configured, exists := os.LookupEnv("CLAUDE_CONFIG_DIR")
	if !exists {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		return filepath.Join(home, ".claude"), nil
	}
	resolved, err := filepath.Abs(configured)
	if err != nil {
		return "", fmt.Errorf("resolve CLAUDE_CONFIG_DIR at %s: %w", configured, err)
	}
	return resolved, nil
}

func claudeSettingsPath() (string, error) {
	directory, err := claudeConfigDirectory()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, claudeConfigFile), nil
}

func readClaudeDocument() (*claudeDocument, error) {
	filePath, err := claudeSettingsPath()
	if err != nil {
		return nil, err
	}
	if _, err := managedPhysicalDirectoryExists(
		filepath.Dir(filePath),
		"Claude Code configuration directory",
	); err != nil {
		return nil, err
	}
	snapshot, err := readManagedFile(
		filePath,
		"Claude Code user settings",
		maxManagedSourceBytes,
	)
	if err != nil {
		return nil, err
	}
	if snapshot == nil {
		return createClaudeDocument(filePath), nil
	}
	return parseClaudeDocument(filePath, snapshot.contents)
}

func prepareRenderedClaudeChange(
	rendered *claudeRenderedDocument,
) (*fileChange, error) {
	if rendered == nil || !rendered.changed {
		return nil, nil
	}
	return prepareSourceChange(
		rendered.document.filePath,
		"Claude Code user settings",
		rendered.document.raw,
		rendered.document.exists,
		rendered.next,
		0600,
		rendered.remove,
	)
}

func writeClaudeChanges(
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
		"Claude Code configuration directory",
	); err != nil {
		return err
	}
	return writeChanges(changes, label, rename)
}
