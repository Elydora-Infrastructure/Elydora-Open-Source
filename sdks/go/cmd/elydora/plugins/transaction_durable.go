package plugins

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

func writeDurableChanges(
	changes []*fileChange,
	label string,
	ops transactionFileOps,
	preconditions ...filePrecondition,
) (result error) {
	if err := transactionPlatformSupported(); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	if ops.moveNoReplace == nil || ops.replaceWithBackup == nil {
		return fmt.Errorf("%s is missing durable atomic file operations", label)
	}
	stateRoot, err := durableTransactionStateRoot()
	if err != nil {
		return fmt.Errorf("%s: prepare durable transaction state: %w", label, err)
	}
	lock, err := acquireDurableTransactionLock(stateRoot)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	defer func() {
		result = errors.Join(result, lock.close())
	}()
	if err := recoverPendingDurableTransactions(stateRoot, ops); err != nil {
		return fmt.Errorf("%s: pending transaction recovery failed: %w", label, err)
	}
	filtered, err := filterDurableChanges(changes, label)
	if err != nil {
		return err
	}
	if err := assertFilePreconditions(preconditions, label); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	if len(filtered) == 0 {
		return nil
	}
	journal, err := createDurableJournal(stateRoot, label)
	if err != nil {
		return err
	}
	if err := initializeDurableLayout(journal, filtered); err != nil {
		return preserveUnjournaledNamespace(journal, err)
	}
	if err := appendDurableJournal(journal, durablePhaseInitializing); err != nil {
		return preserveUnjournaledNamespace(journal, err)
	}
	abort := func(cause error) error {
		return abortDurableTransaction(journal, ops, cause)
	}
	if err := createDurableWorkspaces(journal); err != nil {
		return abort(err)
	}
	if err := stageDurableEntries(journal, filtered); err != nil {
		return abort(err)
	}
	if err := appendDurableJournal(journal, durablePhasePrepared); err != nil {
		return abort(err)
	}
	if err := assertDurableOriginals(journal); err != nil {
		return abort(err)
	}
	if err := assertFilePreconditions(preconditions, label); err != nil {
		return abort(fmt.Errorf("%s: %w", label, err))
	}
	for index := range journal.Entries {
		if err := assertFilePreconditions(preconditions, label); err != nil {
			return abort(fmt.Errorf("%s: %w", label, err))
		}
		activeIndex := index
		journal.ActiveEntry = &activeIndex
		if err := appendDurableJournal(journal, durablePhaseCommitting); err != nil {
			return abort(err)
		}
		if err := commitDurableEntry(&journal.Entries[index], ops); err != nil {
			return abort(err)
		}
		journal.CompletedEntries = index + 1
		journal.ActiveEntry = nil
		if err := appendDurableJournal(journal, durablePhaseCommitting); err != nil {
			return abort(err)
		}
	}
	if err := assertFilePreconditions(preconditions, label); err != nil {
		return abort(fmt.Errorf("%s: %w", label, err))
	}
	if err := verifyDurableJournalCommitted(journal); err != nil {
		return abort(err)
	}
	if err := appendDurableJournal(journal, durablePhaseCommitted); err != nil {
		return abort(err)
	}
	if err := finishCommittedDurableTransaction(journal); err != nil {
		return fmt.Errorf(
			"%s committed; durable cleanup failed: %w; recovery journal %s",
			label,
			err,
			journal.JournalDir,
		)
	}
	return nil
}

func filterDurableChanges(changes []*fileChange, label string) ([]fileChange, error) {
	filtered := make([]fileChange, 0, len(changes))
	targets := make(map[string]bool)
	for _, change := range changes {
		if change == nil || change.existed && !change.remove &&
			bytes.Equal(change.original, change.next) &&
			sameManagedFileMode(change.originalMode, change.mode) {
			continue
		}
		target, err := filepath.Abs(change.filePath)
		if err != nil {
			return nil, fmt.Errorf(
				"%s cannot resolve target %s: %w",
				label,
				change.filePath,
				err,
			)
		}
		target = filepath.Clean(target)
		change.filePath = target
		if runtime.GOOS == "windows" {
			target = strings.ToLower(target)
		}
		if targets[target] {
			return nil, fmt.Errorf("%s contains duplicate file target %s", label, change.filePath)
		}
		targets[target] = true
		filtered = append(filtered, *change)
	}
	return filtered, nil
}

func preserveUnjournaledNamespace(journal *durableJournal, cause error) error {
	return fmt.Errorf(
		"%w; transaction namespace preserved at %s",
		cause,
		journal.JournalDir,
	)
}

func assertDurableOriginals(journal *durableJournal) error {
	for _, entry := range journal.Entries {
		if _, err := requireDurableArtifact(entry.Path, entry.Label, entry.Original); err != nil {
			return fmt.Errorf("%s changed during installation: %w", entry.Label, err)
		}
		if entry.NextPath != "" {
			if _, err := requireDurableArtifact(
				entry.NextPath,
				"staged "+entry.Label,
				entry.Next,
			); err != nil {
				return err
			}
		}
	}
	return nil
}

func commitDurableEntry(entry *durableEntry, ops transactionFileOps) error {
	if _, err := requireDurableArtifact(entry.Path, entry.Label, entry.Original); err != nil {
		return fmt.Errorf("%s changed at commit boundary: %w", entry.Label, err)
	}
	switch entry.Kind {
	case durableCreate:
		if err := ops.moveNoReplace(entry.NextPath, entry.Path); err != nil {
			return fmt.Errorf("create %s without replacement: %w", entry.Label, err)
		}
	case durableUpdate:
		if _, err := requireDurableArtifact(
			entry.NextPath,
			"staged "+entry.Label,
			entry.Next,
		); err != nil {
			return err
		}
		if entry.OriginalPath != entry.NextPath {
			if current, err := readDurableArtifact(entry.OriginalPath, "backup "+entry.Label); err != nil || current != nil {
				return errors.Join(fmt.Errorf("backup path is occupied: %s", entry.OriginalPath), err)
			}
		}
		if err := ops.replaceWithBackup(entry.Path, entry.NextPath, entry.OriginalPath); err != nil {
			return fmt.Errorf("atomically replace %s: %w", entry.Label, err)
		}
	case durableDelete:
		if err := ops.moveNoReplace(entry.Path, entry.OriginalPath); err != nil {
			return fmt.Errorf("capture deleted %s: %w", entry.Label, err)
		}
	default:
		return fmt.Errorf("unknown durable change kind %q", entry.Kind)
	}
	if err := verifyDurableEntryCommitted(entry); err != nil {
		return fmt.Errorf("%s changed immediately after durable commit: %w", entry.Label, err)
	}
	return syncTransactionDirectory(filepath.Dir(entry.Path))
}

func verifyDurableEntryCommitted(entry *durableEntry) error {
	wantTarget := entry.Next
	if entry.Kind == durableDelete {
		wantTarget = durableArtifact{}
	}
	if _, err := requireDurableArtifact(entry.Path, entry.Label, wantTarget); err != nil {
		return err
	}
	switch entry.Kind {
	case durableUpdate, durableDelete:
		_, err := requireDurableArtifact(entry.OriginalPath, "original "+entry.Label, entry.Original)
		return err
	case durableCreate:
		_, err := requireDurableArtifact(entry.NextPath, "consumed staged "+entry.Label, durableArtifact{})
		return err
	}
	return nil
}

func verifyDurableJournalCommitted(journal *durableJournal) error {
	for index := range journal.Entries {
		if err := verifyDurableEntryCommitted(&journal.Entries[index]); err != nil {
			return err
		}
	}
	return nil
}
