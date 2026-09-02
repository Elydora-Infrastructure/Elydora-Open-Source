package plugins

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func recoverPendingDurableTransactions(
	stateRoot string,
	ops transactionFileOps,
) error {
	entries, err := os.ReadDir(stateRoot)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == "lock" {
			continue
		}
		path := filepath.Join(stateRoot, entry.Name())
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "txn-") {
			return fmt.Errorf("unexpected object in transaction state root: %s", path)
		}
		journal, loadErr := loadDurableJournal(path)
		if loadErr != nil {
			if cleanupErr := cleanupEmptyDurableJournal(path); cleanupErr == nil {
				continue
			} else {
				return errors.Join(loadErr, cleanupErr)
			}
		}
		if err := recoverInitializingDurableTransaction(journal); err != nil {
			return fmt.Errorf(
				"recover transaction initialization %s: %w; recovery journal %s",
				journal.ID,
				err,
				journal.JournalDir,
			)
		}
		if journal.Phase == durablePhaseCommitted {
			if err := finishCommittedDurableTransaction(journal); err != nil {
				return fmt.Errorf(
					"finish committed transaction %s: %w; recovery journal %s",
					journal.ID,
					err,
					journal.JournalDir,
				)
			}
			continue
		}
		journalErr := appendDurableJournal(journal, durablePhaseRollingBack)
		recoveryErr := rollbackDurableJournal(journal, ops)
		if recoveryErr == nil {
			recoveryErr = cleanupRolledBackDurableTransaction(journal)
		}
		if journalErr != nil || recoveryErr != nil {
			return errors.Join(
				fmt.Errorf("recover interrupted transaction %s", journal.ID),
				journalErr,
				recoveryErr,
			)
		}
	}
	return nil
}

func abortDurableTransaction(
	journal *durableJournal,
	ops transactionFileOps,
	cause error,
) error {
	if err := recoverInitializingDurableTransaction(journal); err != nil {
		return errors.Join(
			cause,
			fmt.Errorf(
				"durable initialization recovery failed: %w; recovery journal %s",
				err,
				journal.JournalDir,
			),
		)
	}
	journalErr := appendDurableJournal(journal, durablePhaseRollingBack)
	recoveryErr := rollbackDurableJournal(journal, ops)
	if recoveryErr == nil {
		recoveryErr = cleanupRolledBackDurableTransaction(journal)
	}
	if journalErr == nil && recoveryErr == nil {
		return cause
	}
	return errors.Join(
		cause,
		journalErr,
		fmt.Errorf(
			"durable recovery failed: %w; recovery journal %s",
			recoveryErr,
			journal.JournalDir,
		),
	)
}

func rollbackDurableJournal(journal *durableJournal, ops transactionFileOps) error {
	var failures []error
	for index := len(journal.Entries) - 1; index >= 0; index-- {
		entry := &journal.Entries[index]
		if durableEntryDefinitelyUnattempted(journal, index) {
			if err := verifyDurableEntryUnattempted(entry); err != nil {
				failures = append(failures, err)
			}
			continue
		}
		allowUnattempted := journal.ActiveEntry != nil && *journal.ActiveEntry == index
		var err error
		switch entry.Kind {
		case durableUpdate:
			err = rollbackDurableUpdate(entry, ops, allowUnattempted)
		case durableCreate:
			err = rollbackDurableCreate(entry, ops, allowUnattempted)
		case durableDelete:
			err = rollbackDurableDelete(entry, ops, allowUnattempted)
		default:
			err = fmt.Errorf("unknown durable change kind %q", entry.Kind)
		}
		if err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func durableEntryDefinitelyUnattempted(journal *durableJournal, index int) bool {
	if index < journal.CompletedEntries {
		return false
	}
	return journal.ActiveEntry == nil || *journal.ActiveEntry != index
}

func verifyDurableEntryUnattempted(entry *durableEntry) error {
	for _, item := range []struct {
		path     string
		label    string
		expected *durableArtifact
	}{
		{entry.NextPath, "staged " + entry.Label, &entry.Next},
		{entry.DiscardPath, "rollback discard " + entry.Label, nil},
	} {
		if item.path == "" {
			continue
		}
		current, err := readDurableArtifact(item.path, item.label)
		if err != nil {
			return err
		}
		if current != nil && (item.expected == nil || !ownedArtifactMatches(*item.expected, current)) {
			return fmt.Errorf("uncommitted transaction artifact changed; preserved at %s", item.path)
		}
	}
	if entry.OriginalPath != "" && entry.OriginalPath != entry.NextPath {
		current, err := readDurableArtifact(entry.OriginalPath, "uncommitted original "+entry.Label)
		if err != nil || current != nil {
			return errors.Join(
				fmt.Errorf("uncommitted original path is occupied; preserved at %s", entry.OriginalPath),
				err,
			)
		}
	}
	return nil
}

func durableEntryHasStagedNext(entry *durableEntry) (bool, error) {
	if entry.NextPath == "" {
		return false, nil
	}
	current, err := readDurableArtifact(entry.NextPath, "staged "+entry.Label)
	if err != nil || current == nil {
		return false, err
	}
	if !ownedArtifactMatches(entry.Next, current) {
		return false, fmt.Errorf("staged transaction artifact changed; preserved at %s", entry.NextPath)
	}
	return true, nil
}

func rollbackDurableUpdate(
	entry *durableEntry,
	ops transactionFileOps,
	allowUnattempted bool,
) error {
	target, err := readDurableArtifact(entry.Path, entry.Label)
	if err != nil {
		return err
	}
	if artifactMatchesSnapshot(entry.Original, target) {
		return nil
	}
	if !artifactMatchesSnapshot(entry.Next, target) {
		if allowUnattempted {
			staged, stagedErr := durableEntryHasStagedNext(entry)
			if stagedErr != nil {
				return stagedErr
			}
			if staged {
				return nil
			}
		}
		return fmt.Errorf(
			"%s changed during recovery; concurrent target preserved at %s",
			entry.Label,
			entry.Path,
		)
	}
	backup, err := readDurableArtifact(entry.OriginalPath, "captured original "+entry.Label)
	if err != nil || backup == nil {
		return errors.Join(
			fmt.Errorf("captured original is unavailable at %s", entry.OriginalPath),
			err,
		)
	}
	backupArtifact := artifactFromSnapshot(backup)
	discard := entry.DiscardPath
	if !atomicReplacementUsesSeparateBackup() {
		discard = entry.OriginalPath
	}
	if err := ops.replaceWithBackup(entry.Path, entry.OriginalPath, discard); err != nil {
		return fmt.Errorf("atomically restore %s: %w", entry.Label, err)
	}
	restored, restoredErr := readDurableArtifact(entry.Path, "restored "+entry.Label)
	discarded, discardedErr := readDurableArtifact(discard, "discarded "+entry.Label)
	if restoredErr != nil || discardedErr != nil ||
		!artifactMatchesSnapshot(backupArtifact, restored) ||
		!artifactMatchesSnapshot(entry.Next, discarded) {
		reverseErr := ops.replaceWithBackup(entry.Path, discard, entry.OriginalPath)
		return errors.Join(
			fmt.Errorf("%s changed at atomic rollback boundary", entry.Label),
			restoredErr,
			discardedErr,
			reverseErr,
		)
	}
	if !artifactMatchesSnapshot(entry.Original, restored) {
		return fmt.Errorf(
			"concurrent object restored for %s; expected original is unavailable; recovery paths %s and %s",
			entry.Label,
			entry.OriginalPath,
			discard,
		)
	}
	return syncTransactionDirectory(filepath.Dir(entry.Path))
}

func rollbackDurableCreate(
	entry *durableEntry,
	ops transactionFileOps,
	allowUnattempted bool,
) error {
	target, err := readDurableArtifact(entry.Path, entry.Label)
	if err != nil {
		return err
	}
	if target == nil {
		return nil
	}
	if !artifactMatchesSnapshot(entry.Next, target) {
		if allowUnattempted {
			staged, stagedErr := durableEntryHasStagedNext(entry)
			if stagedErr != nil {
				return stagedErr
			}
			if staged {
				return nil
			}
		}
		return fmt.Errorf(
			"%s changed during recovery; concurrent target preserved at %s",
			entry.Label,
			entry.Path,
		)
	}
	if current, readErr := readDurableArtifact(entry.DiscardPath, "rollback discard "+entry.Label); readErr != nil || current != nil {
		return errors.Join(fmt.Errorf("rollback discard path is occupied: %s", entry.DiscardPath), readErr)
	}
	if err := ops.moveNoReplace(entry.Path, entry.DiscardPath); err != nil {
		return fmt.Errorf("capture created %s for rollback: %w", entry.Label, err)
	}
	captured, err := readDurableArtifact(entry.DiscardPath, "captured created "+entry.Label)
	if err != nil || !artifactMatchesSnapshot(entry.Next, captured) {
		restoreErr := ops.moveNoReplace(entry.DiscardPath, entry.Path)
		return errors.Join(
			fmt.Errorf("%s changed at atomic rollback boundary", entry.Label),
			err,
			restoreErr,
		)
	}
	return syncTransactionDirectory(filepath.Dir(entry.Path))
}

func rollbackDurableDelete(
	entry *durableEntry,
	ops transactionFileOps,
	allowUnattempted bool,
) error {
	target, err := readDurableArtifact(entry.Path, entry.Label)
	if err != nil {
		return err
	}
	if artifactMatchesSnapshot(entry.Original, target) {
		return nil
	}
	original, err := readDurableArtifact(entry.OriginalPath, "captured deleted "+entry.Label)
	if err != nil {
		return err
	}
	if target != nil {
		if allowUnattempted && original == nil {
			return nil
		}
		return fmt.Errorf(
			"%s changed during recovery; concurrent target preserved at %s",
			entry.Label,
			entry.Path,
		)
	}
	if original == nil {
		if allowUnattempted {
			return nil
		}
		return errors.Join(
			fmt.Errorf("captured deleted object is unavailable at %s", entry.OriginalPath),
		)
	}
	capturedArtifact := artifactFromSnapshot(original)
	if err := ops.moveNoReplace(entry.OriginalPath, entry.Path); err != nil {
		return fmt.Errorf("restore deleted %s without replacement: %w", entry.Label, err)
	}
	restored, err := readDurableArtifact(entry.Path, "restored deleted "+entry.Label)
	if err != nil || !artifactMatchesSnapshot(capturedArtifact, restored) {
		preserveErr := ops.moveNoReplace(entry.Path, entry.OriginalPath)
		return errors.Join(
			fmt.Errorf("%s changed at atomic restore boundary", entry.Label),
			err,
			preserveErr,
		)
	}
	if !artifactMatchesSnapshot(entry.Original, restored) {
		return fmt.Errorf(
			"concurrent object restored for %s; expected original is unavailable; recovery path %s",
			entry.Label,
			entry.OriginalPath,
		)
	}
	return syncTransactionDirectory(filepath.Dir(entry.Path))
}
