package plugins

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	maxProtectedSecretBytes = 64 * 1024
	maxRuntimeConfigBytes   = 512 * 1024
)

// WriteRuntimeFileAtomic replaces one managed runtime file atomically.
func WriteRuntimeFileAtomic(
	path string,
	label string,
	content []byte,
	mode os.FileMode,
) error {
	if err := validateRuntimeFileTarget(path, label); err != nil {
		return err
	}
	change, err := prepareFileChange(path, label, content, mode)
	if err != nil {
		return err
	}
	return writeChanges([]*fileChange{change}, "write "+label, nil)
}

// GenerateHookScript commits the config, private key, and audit runtime together.
func GenerateHookScript(destPath string, config InstallConfig) error {
	return generateHookScriptWithRename(destPath, config, nil)
}

func generateHookScriptWithRename(
	destPath string,
	config InstallConfig,
	rename renameFunc,
) error {
	agentDirectory, err := ResolveAgentRuntimeDirectory(config.AgentID)
	if err != nil {
		return err
	}

	if config.BaseURL == "" {
		return fmt.Errorf("base URL is required")
	}
	encodedConfig, err := buildManagedRuntimeConfig(config, config.AgentName)
	if err != nil {
		return err
	}

	items := []struct {
		path    string
		label   string
		content []byte
		mode    os.FileMode
	}{
		{
			path: filepath.Join(agentDirectory, "config.json"), label: "Elydora runtime config",
			content: encodedConfig, mode: 0600,
		},
		{
			path: filepath.Join(agentDirectory, "private.key"), label: "Elydora private key",
			content: []byte(config.PrivateKey), mode: 0600,
		},
		{
			path: destPath, label: "Elydora audit runtime",
			content: []byte(buildHookScript(config.AgentName, config.AgentID)), mode: 0700,
		},
	}

	for _, item := range items {
		if err := validateRuntimeFileTarget(item.path, item.label); err != nil {
			return err
		}
	}
	if _, err := PrepareAgentRuntimeDirectory(config.AgentID); err != nil {
		return err
	}
	if err := EnsurePrivateDirectory(filepath.Dir(destPath)); err != nil {
		return fmt.Errorf("create hook script directory: %w", err)
	}

	changes := make([]*fileChange, 0, len(items))
	for _, item := range items {
		change, prepareErr := prepareFileChange(
			item.path,
			item.label,
			item.content,
			item.mode,
		)
		if prepareErr != nil {
			return prepareErr
		}
		changes = append(changes, change)
	}
	if err := writeChanges(changes, "write Elydora agent runtime", rename); err != nil {
		return fmt.Errorf("commit Elydora agent runtime: %w", err)
	}
	return nil
}

func validateRuntimeFileTarget(path, label string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect %s at %s: %w", label, path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("%s path is not a physical file: %s", label, path)
	}
	return nil
}
