package plugins

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

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
	if !change.remove {
		staged.temporaryPath, err = writeStagedFile(
			directory,
			filepath.Base(change.filePath),
			".tmp",
			change.next,
			change.mode,
		)
		if err == nil {
			var snapshot *managedFileSnapshot
			snapshot, err = readManagedFile(
				staged.temporaryPath,
				"staged "+change.label,
				maxManagedSourceBytes,
			)
			if err == nil && snapshot == nil {
				err = fmt.Errorf("staged %s disappeared: %s", change.label, staged.temporaryPath)
			}
			if err == nil {
				staged.temporaryInfo = snapshot.info
			}
		}
	}
	if err == nil && change.existed {
		staged.rollbackPath, err = reserveStagingPath(
			directory,
			filepath.Base(change.filePath),
			".rollback",
		)
	}
	if err != nil {
		cleanupErrors := cleanupStaging([]stagedChange{staged})
		return stagedChange{}, joinTransactionFailure(
			fmt.Errorf("stage %s: %w", change.label, err),
			cleanupErrors,
			"cleanup failed",
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
		return "", errors.Join(cause, file.Close(), removeStagedPath(path))
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
		return "", errors.Join(err, removeStagedPath(path))
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
		return "", errors.Join(err, removeStagedPath(path))
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
	if !snapshotMatches(
		current,
		change.existed,
		change.original,
		change.originalInfo,
		change.originalMode,
	) {
		return fmt.Errorf("%s changed during installation: %s", change.label, change.filePath)
	}
	return nil
}

func snapshotMatches(
	current *managedFileSnapshot,
	expectedExists bool,
	expectedContents []byte,
	expectedInfo os.FileInfo,
	expectedMode os.FileMode,
) bool {
	if (current != nil) != expectedExists {
		return false
	}
	if current == nil {
		return true
	}
	return expectedInfo != nil &&
		bytes.Equal(current.contents, expectedContents) &&
		os.SameFile(current.info, expectedInfo) &&
		sameManagedFileMode(current.mode, expectedMode)
}

func cleanupStaging(staged []stagedChange) []error {
	failures := make([]error, 0)
	for index := range staged {
		item := &staged[index]
		if err := removeVerifiedStagedFile(
			item.temporaryPath,
			"staged "+item.change.label,
			item.change.next,
			item.temporaryInfo,
			item.change.mode,
		); err != nil {
			failures = append(failures, err)
		}
		if err := removeVerifiedStagedFile(
			item.rollbackPath,
			"rollback "+item.change.label,
			item.change.original,
			capturedOriginalInfo(item),
			item.change.originalMode,
		); err != nil {
			failures = append(failures, err)
		}
	}
	return failures
}

func capturedOriginalInfo(item *stagedChange) os.FileInfo {
	if !item.originalCaptured {
		return nil
	}
	return item.change.originalInfo
}

func removeVerifiedStagedFile(
	path, label string,
	expectedContents []byte,
	expectedInfo os.FileInfo,
	expectedMode os.FileMode,
) error {
	if path == "" {
		return nil
	}
	current, err := readManagedFile(path, label, maxManagedSourceBytes)
	if err != nil {
		return fmt.Errorf("%w; staged object preserved at %s", err, path)
	}
	if current == nil {
		return nil
	}
	if expectedInfo == nil || !snapshotMatches(
		current,
		true,
		expectedContents,
		expectedInfo,
		expectedMode,
	) {
		return fmt.Errorf("%s changed during cleanup; staged object preserved at %s", label, path)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove %s at %s: %w", label, path, err)
	}
	return nil
}

func removeStagedPath(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove staged file at %s: %w", path, err)
	}
	return nil
}
