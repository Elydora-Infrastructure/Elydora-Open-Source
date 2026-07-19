package plugins

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func kiroConfigPaths() (string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".kiro", "agents", kiroV2AgentName+".json"),
		filepath.Join(home, ".kiro", "hooks", "elydora-audit.json"), nil
}

func readKiroObject(path, label string) (map[string]any, bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, false, nil
		}
		return nil, false, fmt.Errorf("read %s at %s: %w", label, path, err)
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, true, fmt.Errorf("parse %s at %s: %w", label, path, err)
	}
	object, ok := value.(map[string]any)
	if !ok || object == nil {
		return nil, true, fmt.Errorf("%s at %s must contain a JSON object", label, path)
	}
	return object, true, nil
}

func writeKiroObjectAtomic(path string, value map[string]any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	encoded = append(encoded, '\n')
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return fmt.Errorf("create directory %s: %w", directory, err)
	}
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary file for %s: %w", path, err)
	}
	temporaryPath := temporary.Name()
	cleanup := func(cause error) error {
		closeErr := temporary.Close()
		removeErr := os.Remove(temporaryPath)
		if os.IsNotExist(removeErr) {
			removeErr = nil
		}
		return errors.Join(cause, closeErr, removeErr)
	}
	if err := temporary.Chmod(0600); err != nil {
		return cleanup(fmt.Errorf("set permissions for %s: %w", temporaryPath, err))
	}
	if _, err := temporary.Write(encoded); err != nil {
		return cleanup(fmt.Errorf("write temporary file for %s: %w", path, err))
	}
	if err := temporary.Sync(); err != nil {
		return cleanup(fmt.Errorf("sync temporary file for %s: %w", path, err))
	}
	if err := temporary.Close(); err != nil {
		return cleanup(fmt.Errorf("close temporary file for %s: %w", path, err))
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		removeErr := os.Remove(temporaryPath)
		if os.IsNotExist(removeErr) {
			removeErr = nil
		}
		return errors.Join(fmt.Errorf("replace %s: %w", path, err), removeErr)
	}
	return nil
}

func removeKiroFile(path, label string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s at %s: %w", label, path, err)
	}
	return nil
}

func resolveNodeRuntime() (string, error) {
	nodePath, err := exec.LookPath("node")
	if err != nil {
		return "", fmt.Errorf("resolve Node.js runtime: %w", err)
	}
	absolute, err := filepath.Abs(nodePath)
	if err != nil {
		return "", fmt.Errorf("resolve Node.js runtime path: %w", err)
	}
	return absolute, nil
}

func buildKiroCommand(runtimePath, scriptPath string) string {
	if runtime.GOOS == "windows" {
		return quoteWindowsArgument(runtimePath) + " " + quoteWindowsArgument(scriptPath)
	}
	return quotePOSIXArgument(runtimePath) + " " + quotePOSIXArgument(scriptPath)
}

func quotePOSIXArgument(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func quoteWindowsArgument(value string) string {
	if value != "" && !strings.ContainsAny(value, " \t\n\v\"") {
		return value
	}
	var result strings.Builder
	result.WriteByte('"')
	backslashes := 0
	for _, character := range value {
		if character == '\\' {
			backslashes++
			continue
		}
		if character == '"' {
			result.WriteString(strings.Repeat("\\", backslashes*2+1))
			result.WriteRune(character)
			backslashes = 0
			continue
		}
		result.WriteString(strings.Repeat("\\", backslashes))
		backslashes = 0
		result.WriteRune(character)
	}
	result.WriteString(strings.Repeat("\\", backslashes*2))
	result.WriteByte('"')
	return result.String()
}
