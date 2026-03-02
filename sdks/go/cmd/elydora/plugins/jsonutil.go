package plugins

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// expandHome replaces a leading ~ with the user's home directory.
func expandHome(p string) (string, error) {
	if !strings.HasPrefix(p, "~") {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, p[1:]), nil
}

// readJSONFile reads a JSON file into a map. Returns an empty map if the file does not exist.
func readJSONFile(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]interface{}), nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return result, nil
}

// writeJSONFile writes a map to a JSON file with indentation.
func writeJSONFile(path string, data map[string]interface{}) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}
	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(path, encoded, 0644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// hookScriptPath returns the default path for the hook script inside ~/.elydora/<agentId>/.
func hookScriptPath(agentId string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".elydora", agentId, "hook.js"), nil
}

// guardScriptPath returns the default path for the guard script inside ~/.elydora/<agentId>/.
func guardScriptPath(agentId string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".elydora", agentId, "guard.js"), nil
}
