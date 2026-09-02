package plugins

import (
	"errors"
	"fmt"
	"path/filepath"
)

func commitChange(item *stagedChange, ops transactionFileOps) error {
	if err := assertFileUnchanged(item.change); err != nil {
		return err
	}
	if item.change.existed {
		if err := captureOriginalFile(item, ops); err != nil {
			return err
		}
	}
	if item.change.remove {
		item.committed = true
		return nil
	}
	if item.temporaryPath == "" || item.temporaryInfo == nil {
		return fmt.Errorf("missing staged file for %s", item.change.label)
	}
	if err := ops.moveNoReplace(item.temporaryPath, item.change.filePath); err != nil {
		failure := fmt.Errorf(
			"commit %s at %s without replacing another file: %w",
			item.change.label,
			item.change.filePath,
			err,
		)
		return errors.Join(failure, compensateFailedCommit(item, ops))
	}
	item.temporaryPath = ""
	item.committedInfo = item.temporaryInfo
	item.committed = true
	if err := assertCommittedFileUnchanged(item); err != nil {
		return fmt.Errorf("%w immediately after commit", err)
	}
	return nil
}

func compensateFailedCommit(item *stagedChange, ops transactionFileOps) error {
	if !item.originalCaptured {
		return nil
	}
	restoreErr := ops.moveNoReplace(item.rollbackPath, item.change.filePath)
	if restoreErr == nil {
		return finishOriginalRestore(item, ops)
	}
	current, inspectErr := readManagedFile(
		item.change.filePath,
		item.change.label,
		maxManagedSourceBytes,
	)
	if inspectErr == nil && snapshotMatches(
		current,
		true,
		item.change.original,
		item.change.originalInfo,
		item.change.originalMode,
	) {
		item.rollbackPath = ""
		item.originalCaptured = false
		return nil
	}
	var fallbackErr error
	if inspectErr == nil && current == nil {
		compensate := ops.compensateNoReplace
		if compensate == nil {
			compensate = atomicRenameNoReplace
		}
		fallbackErr = compensate(item.rollbackPath, item.change.filePath)
		if fallbackErr == nil {
			return finishOriginalRestore(item, ops)
		}
	}
	cause := errors.Join(
		fmt.Errorf("restore %s after failed commit at %s", item.change.label, item.change.filePath),
		restoreErr,
		fallbackErr,
		inspectErr,
	)
	return preserveRollbackFile(item, cause)
}

func captureOriginalFile(item *stagedChange, ops transactionFileOps) error {
	if item.rollbackPath == "" {
		return fmt.Errorf("missing rollback path for %s", item.change.label)
	}
	if err := ops.moveNoReplace(item.change.filePath, item.rollbackPath); err != nil {
		return fmt.Errorf(
			"capture original %s at %s: %w",
			item.change.label,
			item.change.filePath,
			err,
		)
	}
	current, err := readManagedFile(
		item.rollbackPath,
		"captured "+item.change.label,
		maxManagedSourceBytes,
	)
	if err != nil || !snapshotMatches(
		current,
		true,
		item.change.original,
		item.change.originalInfo,
		item.change.originalMode,
	) {
		cause := err
		if cause == nil {
			cause = fmt.Errorf(
				"%s changed at the atomic capture boundary: %s",
				item.change.label,
				item.change.filePath,
			)
		}
		return restoreUnexpectedCapture(item, current, cause, ops)
	}
	item.originalCaptured = true
	return nil
}

func restoreUnexpectedCapture(
	item *stagedChange,
	captured *managedFileSnapshot,
	cause error,
	ops transactionFileOps,
) error {
	path := item.rollbackPath
	if err := ops.moveNoReplace(path, item.change.filePath); err != nil {
		item.rollbackPath = ""
		return errors.Join(
			cause,
			fmt.Errorf(
				"restore concurrently captured %s at %s: %w; captured object preserved at %s",
				item.change.label,
				item.change.filePath,
				err,
				path,
			),
		)
	}
	if captured != nil {
		restored, err := readManagedFile(
			item.change.filePath,
			"restored concurrent "+item.change.label,
			maxManagedSourceBytes,
		)
		if err != nil || !snapshotMatches(
			restored,
			true,
			captured.contents,
			captured.info,
			captured.mode,
		) {
			if preserveErr := ops.moveNoReplace(item.change.filePath, path); preserveErr != nil {
				item.rollbackPath = ""
				return errors.Join(
					cause,
					err,
					fmt.Errorf(
						"concurrent %s changed while restoring: %w; objects left at %s and %s",
						item.change.label,
						preserveErr,
						item.change.filePath,
						path,
					),
				)
			}
			item.rollbackPath = ""
			return errors.Join(
				cause,
				err,
				fmt.Errorf("changed concurrent object preserved at %s", path),
			)
		}
	}
	item.rollbackPath = ""
	return cause
}

func assertCommittedChanges(staged []stagedChange) error {
	for index := range staged {
		item := &staged[index]
		if item.committed {
			if err := assertCommittedFileUnchanged(item); err != nil {
				return err
			}
		}
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
		if current == nil {
			return nil
		}
	} else if snapshotMatches(
		current,
		true,
		item.change.next,
		item.committedInfo,
		item.change.mode,
	) {
		return nil
	}
	return fmt.Errorf(
		"%s changed during transaction recovery: %s",
		item.change.label,
		item.change.filePath,
	)
}

func rollbackChanges(staged []stagedChange, ops transactionFileOps) []error {
	failures := make([]error, 0)
	for index := len(staged) - 1; index >= 0; index-- {
		item := &staged[index]
		if !item.committed && !item.originalCaptured {
			continue
		}
		var err error
		switch {
		case item.committed && !item.change.remove:
			err = rollbackWrittenFile(item, ops)
		default:
			err = restoreOriginalFile(item, ops)
		}
		if err != nil {
			failures = append(failures, err)
		}
	}
	return failures
}

func rollbackWrittenFile(item *stagedChange, ops transactionFileOps) error {
	discardPath, err := reserveStagingPath(
		filepath.Dir(item.change.filePath),
		filepath.Base(item.change.filePath),
		".discard",
	)
	if err != nil {
		return preserveOriginalAfterRollbackFailure(
			item,
			fmt.Errorf("reserve recovery path for %s: %w", item.change.label, err),
		)
	}
	if err := ops.moveNoReplace(item.change.filePath, discardPath); err != nil {
		return preserveOriginalAfterRollbackFailure(
			item,
			fmt.Errorf("capture committed %s for rollback: %w", item.change.label, err),
		)
	}
	captured, readErr := readManagedFile(
		discardPath,
		"captured committed "+item.change.label,
		maxManagedSourceBytes,
	)
	if readErr != nil || !snapshotMatches(
		captured,
		true,
		item.change.next,
		item.committedInfo,
		item.change.mode,
	) {
		cause := readErr
		if cause == nil {
			cause = fmt.Errorf(
				"%s changed at the atomic rollback boundary: %s",
				item.change.label,
				item.change.filePath,
			)
		}
		restoreErr := ops.moveNoReplace(discardPath, item.change.filePath)
		if restoreErr != nil {
			cause = errors.Join(
				cause,
				fmt.Errorf(
					"restore concurrently captured %s: %w; captured object preserved at %s",
					item.change.label,
					restoreErr,
					discardPath,
				),
			)
		} else if captured != nil {
			restored, inspectErr := readManagedFile(
				item.change.filePath,
				"restored concurrent "+item.change.label,
				maxManagedSourceBytes,
			)
			if inspectErr != nil || !snapshotMatches(
				restored,
				true,
				captured.contents,
				captured.info,
				captured.mode,
			) {
				preserveErr := ops.moveNoReplace(item.change.filePath, discardPath)
				preservation := fmt.Errorf(
					"changed concurrent object preserved at %s",
					discardPath,
				)
				if preserveErr != nil {
					preservation = fmt.Errorf(
						"preserve changed concurrent %s: %w; objects left at %s and %s",
						item.change.label,
						preserveErr,
						item.change.filePath,
						discardPath,
					)
				}
				cause = errors.Join(
					cause,
					inspectErr,
					preservation,
				)
			}
		}
		return preserveOriginalAfterRollbackFailure(item, cause)
	}

	item.committed = false
	var restoreErr error
	if item.change.existed {
		restoreErr = restoreOriginalFile(item, ops)
	}
	cleanupErr := removeVerifiedStagedFile(
		discardPath,
		"captured committed "+item.change.label,
		item.change.next,
		item.committedInfo,
		item.change.mode,
	)
	return errors.Join(restoreErr, cleanupErr)
}

func restoreOriginalFile(item *stagedChange, ops transactionFileOps) error {
	if !item.originalCaptured {
		return nil
	}
	path := item.rollbackPath
	if err := ops.moveNoReplace(path, item.change.filePath); err != nil {
		cause := fmt.Errorf(
			"restore %s at %s without replacing another file: %w",
			item.change.label,
			item.change.filePath,
			err,
		)
		current, inspectErr := readManagedFile(
			path,
			"rollback "+item.change.label,
			maxManagedSourceBytes,
		)
		if inspectErr == nil && snapshotMatches(
			current,
			true,
			item.change.original,
			item.change.originalInfo,
			item.change.originalMode,
		) {
			return preserveRollbackFile(item, cause)
		}
		return preserveChangedRollback(item, errors.Join(cause, inspectErr))
	}
	return finishOriginalRestore(item, ops)
}

func finishOriginalRestore(item *stagedChange, ops transactionFileOps) error {
	current, err := readManagedFile(
		item.change.filePath,
		item.change.label,
		maxManagedSourceBytes,
	)
	if err == nil && snapshotMatches(
		current,
		true,
		item.change.original,
		item.change.originalInfo,
		item.change.originalMode,
	) {
		item.rollbackPath = ""
		item.originalCaptured = false
		item.committed = false
		return nil
	}
	cause := err
	if cause == nil {
		cause = fmt.Errorf(
			"rollback %s changed at the atomic restore boundary: %s",
			item.change.label,
			item.change.filePath,
		)
	}
	path := item.rollbackPath
	if restoreErr := ops.moveNoReplace(item.change.filePath, path); restoreErr != nil {
		item.rollbackPath = ""
		item.originalCaptured = false
		return errors.Join(
			cause,
			fmt.Errorf(
				"preserve changed rollback object: %w; objects left at %s and %s",
				restoreErr,
				item.change.filePath,
				path,
			),
		)
	}
	item.rollbackPath = ""
	item.originalCaptured = false
	return fmt.Errorf("%w; changed rollback object preserved at %s", cause, path)
}

func preserveChangedRollback(item *stagedChange, cause error) error {
	path := item.rollbackPath
	item.rollbackPath = ""
	item.originalCaptured = false
	return fmt.Errorf("%w; changed rollback object preserved at %s", cause, path)
}

func preserveOriginalAfterRollbackFailure(item *stagedChange, cause error) error {
	if !item.change.existed || !item.originalCaptured {
		return cause
	}
	return preserveRollbackFile(item, cause)
}

func preserveRollbackFile(item *stagedChange, cause error) error {
	if item.rollbackPath == "" {
		return cause
	}
	path := item.rollbackPath
	current, err := readManagedFile(
		path,
		"rollback "+item.change.label,
		maxManagedSourceBytes,
	)
	if err != nil || !snapshotMatches(
		current,
		true,
		item.change.original,
		item.change.originalInfo,
		item.change.originalMode,
	) {
		return preserveChangedRollback(
			item,
			errors.Join(cause, err, fmt.Errorf("rollback %s changed", item.change.label)),
		)
	}
	item.rollbackPath = ""
	item.originalCaptured = false
	return fmt.Errorf("%w; original content preserved at %s", cause, path)
}
