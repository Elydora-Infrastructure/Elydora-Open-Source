package plugins

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type renameFunc func(source, destination string) error

type fileChange struct {
	filePath     string
	label        string
	original     []byte
	originalMode os.FileMode
	existed      bool
	next         []byte
	mode         os.FileMode
	remove       bool
}

type stagedChange struct {
	change        fileChange
	temporaryPath string
	rollbackPath  string
	committed     bool
}

func readOptionalFile(path, label string) ([]byte, bool, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- callers constrain paths to managed user configuration files.
	if err == nil {
		return raw, true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	return nil, false, fmt.Errorf("read %s at %s: %w", label, path, err)
}

func prepareFileChange(filePath, label string, next []byte, mode os.FileMode) (*fileChange, error) {
	original, existed, err := readOptionalFile(filePath, label)
	if err != nil {
		return nil, err
	}
	return prepareSourceChange(filePath, label, original, existed, next, mode, false)
}

func prepareSourceChange(
	filePath, label string,
	original []byte,
	existed bool,
	next []byte,
	mode os.FileMode,
	remove bool,
) (*fileChange, error) {
	if !existed {
		original = nil
	}
	if existed && !remove && bytes.Equal(original, next) {
		return nil, nil
	}
	if !existed && (remove || next == nil) {
		return nil, nil
	}
	originalMode := mode
	if existed {
		info, err := os.Stat(filePath)
		if err != nil {
			return nil, fmt.Errorf("read file mode for %s at %s: %w", label, filePath, err)
		}
		originalMode = info.Mode().Perm()
	}
	return &fileChange{
		filePath: filePath, label: label, original: append([]byte(nil), original...),
		originalMode: originalMode, existed: existed, next: append([]byte(nil), next...),
		mode: mode, remove: remove,
	}, nil
}

func writeChanges(changes []*fileChange, label string, rename renameFunc) error {
	filtered := make([]fileChange, 0, len(changes))
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
	staged := make([]stagedChange, 0, len(filtered))
	for _, change := range filtered {
		item, err := stageChange(change)
		if err != nil {
			cleanupErrors := cleanupStaging(staged)
			return joinTransactionFailure(fmt.Errorf("%s: %w", label, err), cleanupErrors, "cleanup failed")
		}
		staged = append(staged, item)
	}
	for index := range staged {
		if err := commitChange(&staged[index], rename); err != nil {
			recoveryErrors := rollbackChanges(staged, rename)
			recoveryErrors = append(recoveryErrors, cleanupStaging(staged)...)
			return joinTransactionFailure(fmt.Errorf("%s: %w", label, err), recoveryErrors, "recovery failed")
		}
	}
	cleanupErrors := cleanupStaging(staged)
	if len(cleanupErrors) > 0 {
		return joinTransactionFailure(fmt.Errorf("%s cleanup failed", label), cleanupErrors, "cleanup failed")
	}
	return nil
}

func stageChange(change fileChange) (stagedChange, error) {
	if err := assertFileUnchanged(change); err != nil {
		return stagedChange{}, err
	}
	directory := filepath.Dir(change.filePath)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return stagedChange{}, fmt.Errorf("create directory for %s at %s: %w", change.label, directory, err)
	}
	staged := stagedChange{change: change}
	var err error
	if change.remove {
		staged.rollbackPath, err = reserveStagingPath(directory, filepath.Base(change.filePath), ".rollback")
	} else {
		staged.temporaryPath, err = writeStagedFile(
			directory, filepath.Base(change.filePath), ".tmp", change.next, change.mode,
		)
		if err == nil && change.existed {
			staged.rollbackPath, err = writeStagedFile(
				directory, filepath.Base(change.filePath), ".rollback", change.original, change.originalMode,
			)
		}
	}
	if err != nil {
		cleanupErrors := cleanupStaging([]stagedChange{staged})
		return stagedChange{}, joinTransactionFailure(
			fmt.Errorf("stage %s: %w", change.label, err), cleanupErrors, "cleanup failed",
		)
	}
	return staged, nil
}

func writeStagedFile(
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
		return "", errors.Join(cause, file.Close(), removeOptionalFile(path))
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
		return "", errors.Join(err, removeOptionalFile(path))
	}
	return path, nil
}

func reserveStagingPath(directory, basename, suffix string) (string, error) {
	file, err := os.CreateTemp(directory, "."+basename+".*"+suffix)
	if err != nil {
		return "", err
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		return "", errors.Join(err, removeOptionalFile(path))
	}
	if err := os.Remove(path); err != nil {
		return "", err
	}
	return path, nil
}

func assertFileUnchanged(change fileChange) error {
	current, existed, err := readOptionalFile(change.filePath, change.label)
	if err != nil {
		return err
	}
	if existed != change.existed || !bytes.Equal(current, change.original) {
		return fmt.Errorf("%s changed during installation: %s", change.label, change.filePath)
	}
	return nil
}

func commitChange(staged *stagedChange, rename renameFunc) error {
	if err := assertFileUnchanged(staged.change); err != nil {
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

func rollbackChanges(staged []stagedChange, rename renameFunc) []error {
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
			err = removeOptionalFile(item.change.filePath)
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("restore %s at %s: %w", item.change.label, item.change.filePath, err))
		}
	}
	return failures
}

func cleanupStaging(staged []stagedChange) []error {
	failures := make([]error, 0)
	for _, item := range staged {
		for _, path := range []string{item.temporaryPath, item.rollbackPath} {
			if err := removeOptionalFile(path); err != nil {
				failures = append(failures, err)
			}
		}
	}
	return failures
}

func removeOptionalFile(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove staged file at %s: %w", path, err)
	}
	return nil
}

func joinTransactionFailure(cause error, related []error, label string) error {
	if len(related) == 0 {
		return cause
	}
	return errors.Join(cause, fmt.Errorf("%s: %w", label, errors.Join(related...)))
}
