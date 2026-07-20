package plugins

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type renameFunc func(source, destination string) error

type fileChange struct {
	filePath     string
	label        string
	original     []byte
	originalInfo os.FileInfo
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
	committedInfo os.FileInfo
	committed     bool
}

func readOptionalFile(path, label string) ([]byte, bool, error) {
	snapshot, err := readManagedFile(path, label, maxManagedSourceBytes)
	if err != nil {
		return nil, false, err
	}
	if snapshot == nil {
		return nil, false, nil
	}
	return append([]byte(nil), snapshot.contents...), true, nil
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
	snapshot, err := readManagedFile(filePath, label, maxManagedSourceBytes)
	if err != nil {
		return nil, err
	}
	currentExists := snapshot != nil
	if currentExists != existed ||
		(currentExists && !bytes.Equal(snapshot.contents, original)) {
		return nil, fmt.Errorf("%s changed before update: %s", label, filePath)
	}
	if existed && !remove && bytes.Equal(original, next) {
		return nil, nil
	}
	if !existed && (remove || next == nil) {
		return nil, nil
	}
	originalMode := mode
	var originalInfo os.FileInfo
	if existed {
		originalInfo = snapshot.info
		originalMode = snapshot.mode
	}
	return &fileChange{
		filePath: filePath, label: label, original: append([]byte(nil), original...),
		originalInfo: originalInfo, originalMode: originalMode, existed: existed,
		next: append([]byte(nil), next...),
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
		if runtime.GOOS == "windows" {
			target = strings.ToLower(target)
		}
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
	if err := ensureManagedDirectory(directory, "directory for "+change.label); err != nil {
		return stagedChange{}, err
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
	current, err := readManagedFile(change.filePath, change.label, maxManagedSourceBytes)
	if err != nil {
		return err
	}
	currentExists := current != nil
	if currentExists != change.existed ||
		(currentExists && (!bytes.Equal(current.contents, change.original) ||
			!os.SameFile(current.info, change.originalInfo))) {
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
	if !staged.change.remove {
		current, err := readManagedFile(
			staged.change.filePath,
			staged.change.label,
			maxManagedSourceBytes,
		)
		if err != nil {
			return err
		}
		if current == nil || !bytes.Equal(current.contents, staged.change.next) {
			return fmt.Errorf(
				"%s changed immediately after commit: %s",
				staged.change.label,
				staged.change.filePath,
			)
		}
		staged.committedInfo = current.info
	}
	return nil
}

func assertCommittedFileUnchanged(item *stagedChange) error {
	current, err := readManagedFile(
		item.change.filePath,
		item.change.label,
		maxManagedSourceBytes,
	)
	if err != nil {
		return err
	}
	if item.change.remove {
		if current != nil {
			return fmt.Errorf(
				"%s changed during transaction recovery: %s",
				item.change.label,
				item.change.filePath,
			)
		}
		return nil
	}
	if current == nil || item.committedInfo == nil ||
		!bytes.Equal(current.contents, item.change.next) ||
		!os.SameFile(current.info, item.committedInfo) {
		return fmt.Errorf(
			"%s changed during transaction recovery: %s",
			item.change.label,
			item.change.filePath,
		)
	}
	return nil
}

func rollbackChanges(staged []stagedChange, rename renameFunc) []error {
	failures := make([]error, 0)
	for index := len(staged) - 1; index >= 0; index-- {
		item := &staged[index]
		if !item.committed {
			continue
		}
		if err := assertCommittedFileUnchanged(item); err != nil {
			failures = append(failures, preserveRollbackFile(item, err))
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
			failure := fmt.Errorf(
				"restore %s at %s: %w",
				item.change.label,
				item.change.filePath,
				err,
			)
			failures = append(failures, preserveRollbackFile(item, failure))
		}
	}
	return failures
}

func preserveRollbackFile(item *stagedChange, cause error) error {
	if item.rollbackPath == "" {
		return cause
	}
	path := item.rollbackPath
	item.rollbackPath = ""
	return fmt.Errorf("%w; original content preserved at %s", cause, path)
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
