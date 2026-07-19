package plugins

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type droidRenameFunc func(source, destination string) error

type droidFileChange struct {
	filePath     string
	label        string
	original     []byte
	originalMode os.FileMode
	existed      bool
	next         []byte
	mode         os.FileMode
	remove       bool
}

type droidStagedChange struct {
	change        droidFileChange
	temporaryPath string
	rollbackPath  string
	committed     bool
}

func prepareDroidFileChange(
	filePath, label string,
	next []byte,
	mode os.FileMode,
) (*droidFileChange, error) {
	original, exists, err := readDroidOptional(filePath, label)
	if err != nil {
		return nil, err
	}
	if exists && bytes.Equal(original, next) {
		return nil, nil
	}
	if !exists && next == nil {
		return nil, nil
	}
	originalMode := mode
	if exists {
		info, statErr := os.Stat(filePath)
		if statErr != nil {
			return nil, fmt.Errorf("read file mode for %s at %s: %w", label, filePath, statErr)
		}
		originalMode = info.Mode().Perm()
	}
	return &droidFileChange{
		filePath: filePath, label: label, original: original, originalMode: originalMode,
		existed: exists, next: append([]byte(nil), next...), mode: mode,
	}, nil
}

func prepareRenderedDroidChange(rendered *droidRenderedDocument) (*droidFileChange, error) {
	if rendered == nil || !rendered.changed {
		return nil, nil
	}
	label := "Factory Droid hooks"
	if rendered.document.kind == "legacy" {
		label = "Factory Droid legacy hooks"
	} else if rendered.document.kind == "settings" {
		label = "Factory Droid settings"
	}
	originalMode := os.FileMode(0600)
	var original []byte
	if rendered.document.exists {
		info, err := os.Stat(rendered.document.filePath)
		if err != nil {
			return nil, fmt.Errorf("read file mode for %s at %s: %w", label, rendered.document.filePath, err)
		}
		originalMode = info.Mode().Perm()
		original = append([]byte(nil), rendered.document.raw...)
	}
	return &droidFileChange{
		filePath: rendered.document.filePath, label: label,
		original: original, existed: rendered.document.exists,
		next: append([]byte(nil), rendered.next...), mode: 0600, originalMode: originalMode,
		remove: rendered.remove,
	}, nil
}

func writeDroidChanges(changes []*droidFileChange, label string, rename droidRenameFunc) error {
	filtered := make([]droidFileChange, 0, len(changes))
	targets := map[string]bool{}
	for _, change := range changes {
		if change == nil || (change.existed && !change.remove && bytes.Equal(change.original, change.next)) {
			continue
		}
		target := filepath.Clean(change.filePath)
		if targets[target] {
			return fmt.Errorf("%s contains duplicate file target %s", label, change.filePath)
		}
		targets[target] = true
		filtered = append(filtered, *change)
	}
	if len(filtered) == 0 {
		return nil
	}
	if rename == nil {
		rename = os.Rename
	}
	staged := make([]droidStagedChange, 0, len(filtered))
	for _, change := range filtered {
		item, err := stageDroidChange(change)
		if err != nil {
			cleanupErrors := cleanupDroidStaging(staged)
			return joinDroidFailure(fmt.Errorf("%s: %w", label, err), cleanupErrors, "cleanup failed")
		}
		staged = append(staged, item)
	}
	for index := range staged {
		if err := commitDroidChange(&staged[index], rename); err != nil {
			recoveryErrors := rollbackDroidChanges(staged, rename)
			recoveryErrors = append(recoveryErrors, cleanupDroidStaging(staged)...)
			return joinDroidFailure(fmt.Errorf("%s: %w", label, err), recoveryErrors, "recovery failed")
		}
	}
	cleanupErrors := cleanupDroidStaging(staged)
	if len(cleanupErrors) > 0 {
		return joinDroidFailure(fmt.Errorf("%s cleanup failed", label), cleanupErrors, "cleanup failed")
	}
	return nil
}

func stageDroidChange(change droidFileChange) (droidStagedChange, error) {
	if err := assertDroidUnchanged(change); err != nil {
		return droidStagedChange{}, err
	}
	directory := filepath.Dir(change.filePath)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return droidStagedChange{}, fmt.Errorf("create directory for %s at %s: %w", change.label, directory, err)
	}
	staged := droidStagedChange{change: change}
	var err error
	if change.remove {
		staged.rollbackPath, err = reserveDroidPath(directory, filepath.Base(change.filePath), ".rollback")
	} else {
		staged.temporaryPath, err = writeDroidStagedFile(
			directory, filepath.Base(change.filePath), ".tmp", change.next, change.mode,
		)
		if err == nil && change.existed {
			staged.rollbackPath, err = writeDroidStagedFile(
				directory, filepath.Base(change.filePath), ".rollback", change.original, change.originalMode,
			)
		}
	}
	if err != nil {
		cleanupErrors := cleanupDroidStaging([]droidStagedChange{staged})
		return droidStagedChange{}, joinDroidFailure(
			fmt.Errorf("stage %s: %w", change.label, err), cleanupErrors, "cleanup failed",
		)
	}
	return staged, nil
}

func writeDroidStagedFile(
	directory, basename, suffix string,
	content []byte,
	mode os.FileMode,
) (string, error) {
	file, err := os.CreateTemp(directory, "."+basename+".*"+suffix)
	if err != nil {
		return "", err
	}
	path := file.Name()
	failed := func(cause error) (string, error) {
		return "", errors.Join(cause, file.Close(), removeDroidOptional(path))
	}
	if err := file.Chmod(mode); err != nil {
		return failed(err)
	}
	written, err := file.Write(content)
	if err != nil {
		return failed(err)
	}
	if written != len(content) {
		return failed(io.ErrShortWrite)
	}
	if err := file.Sync(); err != nil {
		return failed(err)
	}
	if err := file.Close(); err != nil {
		return "", errors.Join(err, removeDroidOptional(path))
	}
	return path, nil
}

func reserveDroidPath(directory, basename, suffix string) (string, error) {
	file, err := os.CreateTemp(directory, "."+basename+".*"+suffix)
	if err != nil {
		return "", err
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		return "", errors.Join(err, removeDroidOptional(path))
	}
	if err := os.Remove(path); err != nil {
		return "", err
	}
	return path, nil
}

func assertDroidUnchanged(change droidFileChange) error {
	current, exists, err := readDroidOptional(change.filePath, change.label)
	if err != nil {
		return err
	}
	if exists != change.existed || !bytes.Equal(current, change.original) {
		return fmt.Errorf("%s changed during installation: %s", change.label, change.filePath)
	}
	return nil
}

func commitDroidChange(staged *droidStagedChange, rename droidRenameFunc) error {
	if err := assertDroidUnchanged(staged.change); err != nil {
		return err
	}
	if staged.change.remove {
		if staged.rollbackPath == "" {
			return fmt.Errorf("missing rollback path for %s", staged.change.label)
		}
		if err := rename(staged.change.filePath, staged.rollbackPath); err != nil {
			return fmt.Errorf("remove %s at %s: %w", staged.change.label, staged.change.filePath, err)
		}
	} else {
		if staged.temporaryPath == "" {
			return fmt.Errorf("missing staged file for %s", staged.change.label)
		}
		if err := rename(staged.temporaryPath, staged.change.filePath); err != nil {
			return fmt.Errorf("commit %s at %s: %w", staged.change.label, staged.change.filePath, err)
		}
	}
	staged.committed = true
	return nil
}

func rollbackDroidChanges(staged []droidStagedChange, rename droidRenameFunc) []error {
	failures := make([]error, 0)
	for index := len(staged) - 1; index >= 0; index-- {
		item := &staged[index]
		if !item.committed {
			continue
		}
		var err error
		switch {
		case item.change.remove:
			err = rename(item.rollbackPath, item.change.filePath)
		case item.change.existed:
			err = rename(item.rollbackPath, item.change.filePath)
		default:
			err = removeDroidOptional(item.change.filePath)
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("restore %s at %s: %w", item.change.label, item.change.filePath, err))
		}
	}
	return failures
}

func cleanupDroidStaging(staged []droidStagedChange) []error {
	failures := make([]error, 0)
	for _, item := range staged {
		for _, path := range []string{item.temporaryPath, item.rollbackPath} {
			if err := removeDroidOptional(path); err != nil {
				failures = append(failures, err)
			}
		}
	}
	return failures
}

func removeDroidOptional(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove staged Factory Droid file at %s: %w", path, err)
	}
	return nil
}

func joinDroidFailure(cause error, related []error, label string) error {
	if len(related) == 0 {
		return cause
	}
	errorsWithContext := []error{cause, fmt.Errorf("%s: %w", label, errors.Join(related...))}
	return errors.Join(errorsWithContext...)
}

func droidRuntimeFilesExist(contracts []droidRuntimeContract) (bool, error) {
	for _, contract := range contracts {
		configPath := filepath.Join(filepath.Dir(contract.guardPath), "config.json")
		raw, exists, err := readDroidOptional(configPath, "Elydora runtime config")
		if err != nil {
			return false, err
		}
		if !exists {
			continue
		}
		var config map[string]any
		if err := json.Unmarshal(raw, &config); err != nil {
			return false, fmt.Errorf("parse Elydora runtime config at %s: %w", configPath, err)
		}
		agentID, ok := config["agent_id"].(string)
		if !ok || config["agent_name"] != droidAgentKey || !sameDroidAgentID(agentID, contract.agentID) {
			continue
		}
		guardExists, err := regularFileExists(contract.guardPath, "Elydora guard runtime")
		if err != nil {
			return false, err
		}
		auditExists, err := regularFileExists(contract.auditPath, "Elydora audit runtime")
		if err != nil {
			return false, err
		}
		if guardExists && auditExists {
			return true, nil
		}
	}
	return false, nil
}
