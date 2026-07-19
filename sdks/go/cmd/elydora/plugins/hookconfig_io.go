package plugins

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func readHookJSONObject(path, label string) (map[string]any, bool, error) {
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

func writeHookJSONObjectAtomic(path string, value map[string]any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	encoded = append(encoded, '\n')
	return writeHookFileAtomic(path, encoded, 0600)
}

func writeHookFileAtomic(path string, contents []byte, mode os.FileMode) error {
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
	if err := temporary.Chmod(mode); err != nil {
		return cleanup(fmt.Errorf("set permissions for %s: %w", temporaryPath, err))
	}
	if _, err := temporary.Write(contents); err != nil {
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

func removeHookFile(path, label string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s at %s: %w", label, path, err)
	}
	return nil
}
