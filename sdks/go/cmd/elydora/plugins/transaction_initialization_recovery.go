package plugins

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func recoverInitializingDurableTransaction(journal *durableJournal) error {
	if journal.Phase != durablePhaseInitializing {
		return nil
	}
	verifiedWorkspaces := make(map[string]bool, len(journal.Workspaces))
	for index := range journal.Workspaces {
		workspace := &journal.Workspaces[index]
		if workspace.DirectoryID == "" {
			if err := cleanupUnpersistedDurableWorkspace(workspace.Path); err != nil {
				return err
			}
			continue
		}
		if err := verifyPersistedDurableWorkspace(journal, workspace); err != nil {
			return err
		}
		if err := cleanupInterruptedCapabilityProbe(workspace.Path); err != nil {
			return fmt.Errorf("clean interrupted transaction capability probe: %w", err)
		}
		verifiedWorkspaces[durablePathKey(workspace.Path)] = true
	}
	for index := range journal.Entries {
		entry := &journal.Entries[index]
		if entry.NextPath == "" || entry.Next.Identity != "" {
			continue
		}
		current, err := readDurableArtifact(entry.NextPath, "unpersisted staged "+entry.Label)
		if err != nil || current == nil {
			if err != nil {
				return err
			}
			continue
		}
		if !verifiedWorkspaces[durablePathKey(entry.Workspace)] {
			return fmt.Errorf(
				"unpersisted staged transaction artifact preserved at %s",
				entry.NextPath,
			)
		}
		if err := removePhysicalOwnedFile(entry.NextPath, current.identity); err != nil {
			return err
		}
		if err := syncTransactionDirectory(entry.Workspace); err != nil {
			return err
		}
	}
	return nil
}

func cleanupUnpersistedDurableWorkspace(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.Join(fmt.Errorf("unpersisted transaction workspace changed: %s", path), err)
	}
	if err := verifyPrivateTransactionDirectory(path); err != nil {
		return err
	}
	directoryID, err := transactionDirectoryIdentity(path)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	if len(entries) > 1 || len(entries) == 1 && entries[0].Name() != "owner" {
		return fmt.Errorf("unexpected object in unpersisted transaction workspace: %s", path)
	}
	if len(entries) == 1 {
		ownerPath := filepath.Join(path, "owner")
		owner, err := readManagedFile(ownerPath, "unpersisted workspace owner", maxDurableJournalBytes)
		if err != nil || owner == nil {
			return errors.Join(fmt.Errorf("unpersisted workspace owner changed: %s", ownerPath), err)
		}
		if err := removePhysicalOwnedFile(ownerPath, owner.identity); err != nil {
			return err
		}
	}
	latestID, err := transactionDirectoryIdentity(path)
	if err != nil || latestID != directoryID {
		return errors.Join(fmt.Errorf("unpersisted transaction workspace changed: %s", path), err)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove unpersisted transaction workspace %s: %w", path, err)
	}
	return syncTransactionDirectory(filepath.Dir(path))
}

func verifyPersistedDurableWorkspace(
	journal *durableJournal,
	workspace *durableWorkspace,
) error {
	if err := verifyPrivateTransactionDirectory(workspace.Path); err != nil {
		return err
	}
	directoryID, err := transactionDirectoryIdentity(workspace.Path)
	if err != nil || directoryID != workspace.DirectoryID {
		return errors.Join(fmt.Errorf("transaction workspace identity changed: %s", workspace.Path), err)
	}
	marker, err := readManagedFile(
		workspace.MarkerPath,
		"transaction workspace owner",
		maxDurableJournalBytes,
	)
	wantMarker := journal.ID + ":" + workspace.OwnerToken + "\n"
	if err != nil || marker == nil || string(marker.contents) != wantMarker {
		return errors.Join(fmt.Errorf("transaction workspace owner changed: %s", workspace.Path), err)
	}
	return nil
}
