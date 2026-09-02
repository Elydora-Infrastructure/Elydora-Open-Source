package plugins

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type durableCleanupDisposition string

const (
	cleanupCommitted  durableCleanupDisposition = "committed"
	cleanupRolledBack durableCleanupDisposition = "rolled_back"
)

func finishCommittedDurableTransaction(journal *durableJournal) error {
	if err := verifyCommittedDurableTransactionForCleanup(journal); err != nil {
		return err
	}
	for _, workspace := range journal.Workspaces {
		if err := cleanupDurableWorkspace(journal, workspace, cleanupCommitted); err != nil {
			return err
		}
	}
	return cleanupDurableJournalNamespace(journal)
}

func verifyCommittedDurableTransactionForCleanup(journal *durableJournal) error {
	for index := range journal.Entries {
		entry := &journal.Entries[index]
		wantTarget := entry.Next
		if entry.Kind == durableDelete {
			wantTarget = durableArtifact{}
		}
		if _, err := requireDurableArtifact(entry.Path, entry.Label, wantTarget); err != nil {
			return err
		}
		switch entry.Kind {
		case durableUpdate, durableDelete:
			original, err := readDurableArtifact(entry.OriginalPath, "original "+entry.Label)
			if err != nil {
				return err
			}
			if original != nil && !ownedArtifactMatches(entry.Original, original) {
				return fmt.Errorf("committed original changed; preserved at %s", entry.OriginalPath)
			}
		case durableCreate:
			if _, err := requireDurableArtifact(
				entry.NextPath,
				"consumed staged "+entry.Label,
				durableArtifact{},
			); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown durable change kind %q", entry.Kind)
		}
	}
	return nil
}

func cleanupRolledBackDurableTransaction(journal *durableJournal) error {
	for index := range journal.Entries {
		entry := &journal.Entries[index]
		if durableEntryDefinitelyUnattempted(journal, index) {
			continue
		}
		if journal.ActiveEntry != nil && *journal.ActiveEntry == index {
			uncommitted, err := durableEntryHasUncommittedWorkspaceState(entry)
			if err != nil {
				return err
			}
			if uncommitted {
				continue
			}
		}
		if _, err := requireDurableArtifact(entry.Path, entry.Label, entry.Original); err != nil {
			return fmt.Errorf("rollback verification failed for %s: %w", entry.Label, err)
		}
	}
	for _, workspace := range journal.Workspaces {
		if err := cleanupDurableWorkspace(journal, workspace, cleanupRolledBack); err != nil {
			return err
		}
	}
	return cleanupDurableJournalNamespace(journal)
}

func durableEntryHasUncommittedWorkspaceState(entry *durableEntry) (bool, error) {
	switch entry.Kind {
	case durableCreate, durableUpdate:
		return durableEntryHasStagedNext(entry)
	case durableDelete:
		original, err := readDurableArtifact(entry.OriginalPath, "captured deleted "+entry.Label)
		return original == nil, err
	default:
		return false, fmt.Errorf("unknown durable change kind %q", entry.Kind)
	}
}

func cleanupDurableWorkspace(
	journal *durableJournal,
	workspace durableWorkspace,
	disposition durableCleanupDisposition,
) error {
	info, err := os.Lstat(workspace.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.Join(fmt.Errorf("transaction workspace changed: %s", workspace.Path), err)
	}
	if err := verifyPrivateTransactionDirectory(workspace.Path); err != nil {
		return err
	}
	if workspace.DirectoryID == "" {
		return fmt.Errorf("transaction workspace identity is unavailable; preserved at %s", workspace.Path)
	}
	identity, identityErr := transactionDirectoryIdentity(workspace.Path)
	if identityErr != nil || identity != workspace.DirectoryID {
		return errors.Join(
			fmt.Errorf("transaction workspace identity changed: %s", workspace.Path),
			identityErr,
		)
	}
	marker, err := readManagedFile(
		workspace.MarkerPath,
		"transaction workspace owner",
		maxDurableJournalBytes,
	)
	wantMarker := journal.ID + ":" + workspace.OwnerToken + "\n"
	if err != nil {
		return errors.Join(fmt.Errorf("transaction workspace owner changed: %s", workspace.Path), err)
	}
	if marker == nil {
		entries, readErr := os.ReadDir(workspace.Path)
		if readErr != nil || len(entries) != 0 {
			return errors.Join(
				fmt.Errorf("transaction workspace owner changed: %s", workspace.Path),
				readErr,
			)
		}
		identity, identityErr = transactionDirectoryIdentity(workspace.Path)
		if identityErr != nil || identity != workspace.DirectoryID {
			return errors.Join(
				fmt.Errorf("transaction workspace changed before resumed removal: %s", workspace.Path),
				identityErr,
			)
		}
		if err := os.Remove(workspace.Path); err != nil {
			return fmt.Errorf("remove empty transaction workspace %s: %w", workspace.Path, err)
		}
		return syncTransactionDirectory(filepath.Dir(workspace.Path))
	}
	if string(marker.contents) != wantMarker {
		return fmt.Errorf("transaction workspace owner changed: %s", workspace.Path)
	}
	if err := cleanupInterruptedCapabilityProbe(workspace.Path); err != nil {
		return fmt.Errorf("clean interrupted transaction capability probe: %w", err)
	}
	expected := expectedWorkspaceArtifacts(journal, workspace.Path, disposition)
	entries, err := os.ReadDir(workspace.Path)
	if err != nil {
		return err
	}
	for _, item := range entries {
		path := filepath.Join(workspace.Path, item.Name())
		if path == workspace.MarkerPath {
			continue
		}
		allowed, known := expected[path]
		if !known {
			return fmt.Errorf("unexpected object preserved in transaction workspace: %s", path)
		}
		if err := removeExpectedOwnedArtifact(path, allowed); err != nil {
			return err
		}
	}
	identity, identityErr = transactionDirectoryIdentity(workspace.Path)
	if identityErr != nil || identity != workspace.DirectoryID {
		return errors.Join(
			fmt.Errorf("transaction workspace changed before removal: %s", workspace.Path),
			identityErr,
		)
	}
	if err := syncTransactionDirectory(workspace.Path); err != nil {
		return fmt.Errorf("persist transaction workspace artifact cleanup at %s: %w", workspace.Path, err)
	}
	if err := removePhysicalOwnedFile(workspace.MarkerPath, marker.identity); err != nil {
		return fmt.Errorf("remove transaction workspace owner at %s: %w", workspace.MarkerPath, err)
	}
	if err := syncTransactionDirectory(workspace.Path); err != nil {
		return fmt.Errorf("persist transaction workspace owner cleanup at %s: %w", workspace.Path, err)
	}
	if err := os.Remove(workspace.Path); err != nil {
		return fmt.Errorf("remove empty transaction workspace %s: %w", workspace.Path, err)
	}
	return syncTransactionDirectory(filepath.Dir(workspace.Path))
}

func expectedWorkspaceArtifacts(
	journal *durableJournal,
	workspace string,
	disposition durableCleanupDisposition,
) map[string][]durableArtifact {
	expected := make(map[string][]durableArtifact)
	add := func(path string, artifact durableArtifact) {
		if path != "" && filepath.Clean(filepath.Dir(path)) == filepath.Clean(workspace) {
			expected[path] = append(expected[path], artifact)
		}
	}
	for _, entry := range journal.Entries {
		if filepath.Clean(entry.Workspace) != filepath.Clean(workspace) {
			continue
		}
		if disposition == cleanupCommitted {
			switch entry.Kind {
			case durableUpdate, durableDelete:
				add(entry.OriginalPath, entry.Original)
			}
		} else {
			add(entry.NextPath, entry.Next)
			add(entry.OriginalPath, entry.Next)
			add(entry.OriginalPath, entry.Original)
			add(entry.DiscardPath, entry.Next)
		}
		add(entry.NextPath, durableArtifact{})
		add(entry.OriginalPath, durableArtifact{})
		add(entry.DiscardPath, durableArtifact{})
	}
	captures := make(map[string][]durableArtifact, len(expected))
	for path, artifacts := range expected {
		captures[path+".cleanup"] = artifacts
	}
	for path, artifacts := range captures {
		expected[path] = artifacts
	}
	return expected
}

func removeExpectedOwnedArtifact(path string, allowed []durableArtifact) error {
	current, err := readDurableArtifact(path, "transaction artifact")
	if err != nil {
		return fmt.Errorf("inspect owned transaction artifact at %s: %w", path, err)
	}
	if current == nil {
		return nil
	}
	for _, expected := range allowed {
		if ownedArtifactMatches(expected, current) {
			if strings.HasSuffix(path, ".cleanup") {
				return removePhysicalOwnedFile(path, current.identity)
			}
			return captureAndRemoveExpectedArtifact(path, allowed)
		}
	}
	return fmt.Errorf("changed transaction artifact preserved at %s", path)
}

func captureAndRemoveExpectedArtifact(path string, allowed []durableArtifact) error {
	capture := path + ".cleanup"
	occupied, err := readDurableArtifact(capture, "cleanup capture")
	if err != nil || occupied != nil {
		return errors.Join(fmt.Errorf("cleanup capture path is occupied: %s", capture), err)
	}
	if err := atomicRenameNoReplace(path, capture); err != nil {
		return fmt.Errorf("atomically capture owned transaction artifact at %s: %w", path, err)
	}
	captured, err := readDurableArtifact(capture, "captured transaction artifact")
	if err == nil {
		for _, expected := range allowed {
			if ownedArtifactMatches(expected, captured) {
				return removePhysicalOwnedFile(capture, captured.identity)
			}
		}
	}
	restoreErr := atomicRenameNoReplace(capture, path)
	return errors.Join(
		fmt.Errorf("changed transaction artifact preserved at %s", capture),
		err,
		restoreErr,
	)
}

func ownedArtifactMatches(expected durableArtifact, current *managedFileSnapshot) bool {
	if current == nil || !expected.Exists || expected.Identity == "" {
		return false
	}
	if current.identity != expected.Identity {
		return false
	}
	return artifactDigest(current.contents) == expected.SHA256 &&
		sameManagedFileMode(current.mode, os.FileMode(expected.Mode))
}

func removePhysicalOwnedFile(path string, expectedIdentity ...string) error {
	current, err := readManagedFile(path, "owned transaction artifact", maxDurableJournalBytes)
	if err != nil {
		return err
	}
	if current == nil {
		return nil
	}
	if len(expectedIdentity) > 0 && expectedIdentity[0] != "" &&
		current.identity != expectedIdentity[0] {
		return fmt.Errorf("owned transaction object changed identity; preserved at %s", path)
	}
	latest, err := os.Lstat(path)
	if err != nil || latest.Mode()&os.ModeSymlink != 0 ||
		!latest.Mode().IsRegular() || !os.SameFile(current.info, latest) {
		return errors.Join(
			fmt.Errorf("owned transaction object changed before removal; preserved at %s", path),
			err,
		)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove owned transaction artifact at %s: %w", path, err)
	}
	return nil
}

func cleanupDurableJournalNamespace(journal *durableJournal) error {
	return cleanupOwnedJournalDirectory(
		journal.JournalDir,
		journal.ID,
		journal.OwnerToken,
		journal.Sequence,
	)
}

func cleanupEmptyDurableJournal(directory string) error {
	base := filepath.Base(directory)
	if !strings.HasPrefix(base, "txn-") {
		return fmt.Errorf("invalid empty journal namespace: %s", directory)
	}
	if err := verifyPrivateTransactionDirectory(directory); err != nil {
		return err
	}
	entries, readErr := os.ReadDir(directory)
	if readErr != nil {
		return readErr
	}
	if len(entries) == 0 {
		if err := os.Remove(directory); err != nil {
			return fmt.Errorf("remove empty recovery namespace %s: %w", directory, err)
		}
		return syncTransactionDirectory(filepath.Dir(directory))
	}
	type incompleteJournalFile struct {
		path     string
		identity string
	}
	files := make([]incompleteJournalFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() != "owner" && !isDurableJournalPending(entry.Name()) {
			return fmt.Errorf("invalid journal state preserved for manual recovery at %s", directory)
		}
		path := filepath.Join(directory, entry.Name())
		snapshot, err := readManagedFile(path, "incomplete transaction journal file", maxDurableJournalBytes)
		if err != nil || snapshot == nil {
			return errors.Join(fmt.Errorf("incomplete journal file changed: %s", path), err)
		}
		files = append(files, incompleteJournalFile{path: path, identity: snapshot.identity})
	}
	for _, file := range files {
		if err := removePhysicalOwnedFile(file.path, file.identity); err != nil {
			return err
		}
	}
	if err := syncTransactionDirectory(directory); err != nil {
		return fmt.Errorf("persist incomplete journal file cleanup at %s: %w", directory, err)
	}
	if err := os.Remove(directory); err != nil {
		return fmt.Errorf("remove incomplete recovery namespace %s: %w", directory, err)
	}
	return syncTransactionDirectory(filepath.Dir(directory))
}

func isDurableJournalPending(name string) bool {
	_, pending := durableJournalPendingSequence(name)
	return pending
}

func cleanupOwnedJournalDirectory(
	directory, id, ownerToken string,
	latestSequence uint64,
) error {
	wantOwner := id + ":" + ownerToken + "\n"
	ownerPath := filepath.Join(directory, "owner")
	owner, err := readManagedFile(ownerPath, "transaction journal owner", maxDurableJournalBytes)
	if err != nil || owner == nil || string(owner.contents) != wantOwner {
		return errors.Join(fmt.Errorf("journal owner changed; preserved at %s", directory), err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	type journalFileRemoval struct {
		path     string
		identity string
	}
	recoverableFiles := make([]journalFileRemoval, 0)
	validStates := make([]journalFileRemoval, 0)
	for _, entry := range entries {
		if entry.Name() == "owner" {
			continue
		}
		if entry.IsDir() {
			return fmt.Errorf("unexpected journal object preserved at %s", filepath.Join(directory, entry.Name()))
		}
		path := filepath.Join(directory, entry.Name())
		if _, pending := durableJournalPendingSequence(entry.Name()); pending {
			snapshot, err := inspectRecoverableJournalFile(path)
			if err != nil {
				return err
			}
			recoverableFiles = append(
				recoverableFiles,
				journalFileRemoval{path: path, identity: snapshot.identity},
			)
			continue
		}
		sequence, valid := durableJournalStateSequence(entry.Name())
		if !valid {
			return fmt.Errorf("unexpected journal file preserved at %s", filepath.Join(directory, entry.Name()))
		}
		state, stateErr := readDurableJournalState(path)
		if stateErr != nil {
			if sequence > latestSequence {
				snapshot, err := inspectRecoverableJournalFile(path)
				if err != nil {
					return err
				}
				recoverableFiles = append(
					recoverableFiles,
					journalFileRemoval{path: path, identity: snapshot.identity},
				)
				continue
			}
			return fmt.Errorf("invalid journal file preserved at %s: %w", path, stateErr)
		}
		if err := validateLoadedJournal(state, directory, entry.Name()); err != nil {
			return fmt.Errorf("changed journal file preserved at %s: %w", path, err)
		}
		snapshot, err := readManagedFile(path, "transaction journal state", maxDurableJournalBytes)
		if err != nil || snapshot == nil {
			return errors.Join(fmt.Errorf("transaction journal state changed: %s", path), err)
		}
		validStates = append(
			validStates,
			journalFileRemoval{path: path, identity: snapshot.identity},
		)
	}
	for _, file := range recoverableFiles {
		if err := removePhysicalOwnedFile(file.path, file.identity); err != nil {
			return err
		}
	}
	for _, file := range validStates {
		if err := removePhysicalOwnedFile(file.path, file.identity); err != nil {
			return err
		}
	}
	if err := syncTransactionDirectory(directory); err != nil {
		return fmt.Errorf("persist transaction journal state cleanup at %s: %w", directory, err)
	}
	if err := removePhysicalOwnedFile(ownerPath, owner.identity); err != nil {
		return err
	}
	if err := syncTransactionDirectory(directory); err != nil {
		return fmt.Errorf("persist transaction journal owner cleanup at %s: %w", directory, err)
	}
	if err := os.Remove(directory); err != nil {
		return err
	}
	return syncTransactionDirectory(filepath.Dir(directory))
}

func inspectRecoverableJournalFile(path string) (*managedFileSnapshot, error) {
	snapshot, err := readManagedFile(path, "recoverable transaction journal state", maxDurableJournalBytes)
	if err != nil || snapshot == nil {
		return nil, errors.Join(fmt.Errorf("transaction journal state changed: %s", path), err)
	}
	return snapshot, nil
}
