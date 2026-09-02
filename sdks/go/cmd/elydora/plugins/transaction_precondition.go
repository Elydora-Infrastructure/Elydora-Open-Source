package plugins

import (
	"bytes"
	"fmt"
	"os"
)

type filePrecondition struct {
	filePath    string
	label       string
	snapshot    *managedFileSnapshot
	directory   os.FileInfo
	maximumSize int64
}

func prepareSnapshotSourceChange(
	filePath, label string,
	original *managedFileSnapshot,
	next []byte,
	mode os.FileMode,
	remove bool,
) (*fileChange, error) {
	if !remove && int64(len(next)) > maxManagedSourceBytes {
		return nil, fmt.Errorf(
			"%s exceeds %d bytes: %s",
			label,
			maxManagedSourceBytes,
			filePath,
		)
	}
	current, err := readManagedFile(filePath, label, maxManagedSourceBytes)
	if err != nil {
		return nil, err
	}
	if !sameManagedSnapshot(current, original) {
		return nil, fmt.Errorf("%s changed before update: %s", label, filePath)
	}
	if original == nil && (remove || next == nil) {
		return nil, nil
	}
	if original != nil && !remove && bytes.Equal(original.contents, next) &&
		sameManagedFileMode(original.mode, mode) {
		return nil, nil
	}
	change := &fileChange{
		filePath: filePath,
		label:    label,
		next:     append([]byte(nil), next...),
		mode:     mode,
		remove:   remove,
	}
	if original != nil {
		change.original = append([]byte(nil), original.contents...)
		change.originalInfo = original.info
		change.originalID = original.identity
		change.originalMode = original.mode
		change.existed = true
	}
	return change, nil
}

func sameManagedSnapshot(current, expected *managedFileSnapshot) bool {
	if current == nil || expected == nil {
		return current == expected
	}
	return bytes.Equal(current.contents, expected.contents) &&
		current.identity == expected.identity &&
		os.SameFile(current.info, expected.info) &&
		sameManagedFileMode(current.mode, expected.mode)
}

func assertFilePreconditions(
	preconditions []filePrecondition,
	operation string,
) error {
	for _, condition := range preconditions {
		if condition.directory != nil {
			current, err := os.Lstat(condition.filePath)
			if err != nil {
				return fmt.Errorf(
					"inspect %s at %s: %w",
					condition.label,
					condition.filePath,
					err,
				)
			}
			if current.Mode()&os.ModeSymlink != 0 || !current.IsDir() ||
				!os.SameFile(condition.directory, current) {
				return fmt.Errorf(
					"%s changed during %s: %s",
					condition.label,
					operation,
					condition.filePath,
				)
			}
			continue
		}
		maximumSize := condition.maximumSize
		if maximumSize <= 0 {
			maximumSize = maxManagedSourceBytes
		}
		current, err := readManagedFile(
			condition.filePath,
			condition.label,
			maximumSize,
		)
		if err != nil {
			return err
		}
		if !sameManagedSnapshot(current, condition.snapshot) {
			return fmt.Errorf(
				"%s changed during %s: %s",
				condition.label,
				operation,
				condition.filePath,
			)
		}
	}
	return nil
}
