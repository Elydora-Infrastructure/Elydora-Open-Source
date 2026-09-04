package plugins

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"runtime"
)

type renameFunc func(source, destination string) error
type replaceWithBackupFunc func(target, replacement, backup string) error

type fileChange struct {
	filePath     string
	label        string
	original     []byte
	originalInfo os.FileInfo
	originalID   string
	originalMode os.FileMode
	existed      bool
	next         []byte
	mode         os.FileMode
	remove       bool
}

type stagedChange struct {
	change           fileChange
	temporaryPath    string
	temporaryInfo    os.FileInfo
	rollbackPath     string
	originalCaptured bool
	committedInfo    os.FileInfo
	committed        bool
}

type transactionFileOps struct {
	moveNoReplace       renameFunc
	compensateNoReplace renameFunc
	replaceWithBackup   replaceWithBackupFunc
	production          bool
}

func newTransactionFileOps(rename renameFunc) transactionFileOps {
	if rename == nil {
		rename = atomicRenameNoReplace
		return transactionFileOps{
			moveNoReplace:       rename,
			compensateNoReplace: rename,
			replaceWithBackup:   atomicReplaceWithBackup,
			production:          true,
		}
	}
	return transactionFileOps{
		moveNoReplace:       rename,
		compensateNoReplace: atomicRenameNoReplace,
	}
}

func sameManagedFileMode(current, expected os.FileMode) bool {
	return runtime.GOOS == "windows" || current.Perm() == expected.Perm()
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
	if !remove && int64(len(next)) > maxManagedSourceBytes {
		return nil, fmt.Errorf(
			"%s exceeds %d bytes: %s",
			label,
			maxManagedSourceBytes,
			filePath,
		)
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
	if existed && !remove && bytes.Equal(original, next) &&
		sameManagedFileMode(snapshot.mode, mode) {
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
		originalInfo: originalInfo, originalID: snapshotIdentity(snapshot),
		originalMode: originalMode, existed: existed,
		next: append([]byte(nil), next...),
		mode: mode, remove: remove,
	}, nil
}

func snapshotIdentity(snapshot *managedFileSnapshot) string {
	if snapshot == nil {
		return ""
	}
	return snapshot.identity
}

func writeChanges(
	changes []*fileChange,
	label string,
	rename renameFunc,
	preconditions ...filePrecondition,
) error {
	return writeChangesWithFileOps(
		changes,
		label,
		newTransactionFileOps(rename),
		preconditions...,
	)
}

func writeChangesWithFileOps(
	changes []*fileChange,
	label string,
	ops transactionFileOps,
	preconditions ...filePrecondition,
) error {
	if ops.production {
		return writeDurableChanges(changes, label, ops, preconditions...)
	}
	if ops.moveNoReplace == nil {
		return fmt.Errorf("%s is missing atomic file operations", label)
	}
	filtered, err := filterDurableChanges(changes, label)
	if err != nil {
		return err
	}
	checkPreconditions := func() error {
		if err := assertFilePreconditions(preconditions, label); err != nil {
			return fmt.Errorf("%s: %w", label, err)
		}
		return nil
	}
	if err := checkPreconditions(); err != nil {
		return err
	}
	if len(filtered) == 0 {
		return nil
	}
	staged := make([]stagedChange, 0, len(filtered))
	abortStaging := func(cause error) error {
		return joinTransactionFailure(cause, cleanupStaging(staged), "cleanup failed")
	}
	for _, change := range filtered {
		if err := checkPreconditions(); err != nil {
			return abortStaging(err)
		}
		item, err := stageChange(change)
		if err != nil {
			return abortStaging(fmt.Errorf("%s: %w", label, err))
		}
		staged = append(staged, item)
	}
	if err := checkPreconditions(); err != nil {
		return abortStaging(err)
	}
	recoverStaged := func(cause error) error {
		failures := rollbackChanges(staged, ops)
		failures = append(failures, cleanupStaging(staged)...)
		return joinTransactionFailure(cause, failures, "recovery failed")
	}
	for index := range staged {
		if err := checkPreconditions(); err != nil {
			return recoverStaged(err)
		}
		if err := commitChange(&staged[index], ops); err != nil {
			return recoverStaged(fmt.Errorf("%s: %w", label, err))
		}
	}
	if err := checkPreconditions(); err != nil {
		return recoverStaged(err)
	}
	if err := assertCommittedChanges(staged); err != nil {
		return recoverStaged(fmt.Errorf("%s: %w", label, err))
	}
	if failures := cleanupStaging(staged); len(failures) > 0 {
		return joinTransactionFailure(fmt.Errorf("%s cleanup failed", label), failures, "cleanup failed")
	}
	return nil
}

func joinTransactionFailure(cause error, related []error, label string) error {
	if len(related) == 0 {
		return cause
	}
	return errors.Join(cause, fmt.Errorf("%s: %w", label, errors.Join(related...)))
}
