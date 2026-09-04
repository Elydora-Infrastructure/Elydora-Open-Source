package plugins

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// readJSONFile reads a JSON object; a missing file yields an empty map.
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

// hookScriptPath returns ~/.elydora/<agentId>/hook.js.
func hookScriptPath(agentId string) (string, error) {
	agentDirectory, err := ResolveAgentRuntimeDirectory(agentId)
	if err != nil {
		return "", err
	}
	return filepath.Join(agentDirectory, "hook.js"), nil
}

// removeAgentScripts deletes hook.js and guard.js; missing files are fine.
func removeAgentScripts(agentID string) error {
	for _, resolve := range []func(string) (string, error){hookScriptPath, guardScriptPath} {
		path, err := resolve(agentID)
		if err != nil {
			return err
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", path, err)
		}
	}
	return nil
}

// guardScriptPath returns ~/.elydora/<agentId>/guard.js.
func guardScriptPath(agentId string) (string, error) {
	agentDirectory, err := ResolveAgentRuntimeDirectory(agentId)
	if err != nil {
		return "", err
	}
	return filepath.Join(agentDirectory, "guard.js"), nil
}
