package plugins

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

func initializeDurableLayout(journal *durableJournal, changes []fileChange) error {
	directories := make(map[string]int)
	for index, change := range changes {
		directory := filepath.Dir(change.filePath)
		if err := ensureManagedDirectory(directory, "directory for "+change.label); err != nil {
			return err
		}
		key := durablePathKey(directory)
		workspaceIndex, exists := directories[key]
		if !exists {
			workspacePath := filepath.Join(directory, ".elydora-txn-"+journal.ID)
			workspaceIndex = len(journal.Workspaces)
			directories[key] = workspaceIndex
			journal.Workspaces = append(journal.Workspaces, durableWorkspace{
				Path: workspacePath, MarkerPath: filepath.Join(workspacePath, "owner"),
				OwnerToken: journal.OwnerToken,
			})
		}
		workspace := journal.Workspaces[workspaceIndex].Path
		entry := durableEntry{
			Path: change.filePath, Label: change.label, Workspace: workspace,
			DiscardPath: filepath.Join(workspace, fmt.Sprintf("%04d.discard", index)),
			Original:    artifactFromChangeOriginal(change),
		}
		switch {
		case change.remove:
			entry.Kind = durableDelete
			entry.OriginalPath = filepath.Join(workspace, fmt.Sprintf("%04d.original", index))
		default:
			entry.NextPath = filepath.Join(workspace, fmt.Sprintf("%04d.next", index))
			entry.Next = durableArtifact{
				Exists: true, SHA256: artifactDigest(change.next), Mode: uint32(change.mode.Perm()),
			}
			if change.existed {
				entry.Kind = durableUpdate
				if atomicReplacementUsesSeparateBackup() {
					entry.OriginalPath = filepath.Join(
						workspace,
						fmt.Sprintf("%04d.original", index),
					)
				} else {
					entry.OriginalPath = entry.NextPath
				}
			} else {
				entry.Kind = durableCreate
			}
		}
		journal.Entries = append(journal.Entries, entry)
	}
	sort.Slice(journal.Workspaces, func(left, right int) bool {
		return durablePathKey(journal.Workspaces[left].Path) <
			durablePathKey(journal.Workspaces[right].Path)
	})
	return nil
}

func durablePathKey(path string) string {
	key := filepath.Clean(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(filepath.ToSlash(key))
	}
	return key
}

func createDurableWorkspaces(journal *durableJournal) error {
	for index := range journal.Workspaces {
		workspace := &journal.Workspaces[index]
		if err := createPrivateTransactionDirectory(workspace.Path); err != nil {
			return fmt.Errorf(
				"create transaction workspace %s: %w; recovery namespace %s",
				workspace.Path,
				err,
				journal.JournalDir,
			)
		}
		marker := []byte(journal.ID + ":" + workspace.OwnerToken + "\n")
		if err := writeOwnedFile(workspace.MarkerPath, marker, 0600); err != nil {
			return fmt.Errorf("write transaction workspace owner at %s: %w", workspace.Path, err)
		}
		if err := syncTransactionDirectory(workspace.Path); err != nil {
			return fmt.Errorf("persist transaction workspace owner at %s: %w", workspace.Path, err)
		}
		if err := syncTransactionDirectory(filepath.Dir(workspace.Path)); err != nil {
			return fmt.Errorf("persist transaction workspace at %s: %w", workspace.Path, err)
		}
		identity, err := transactionDirectoryIdentity(workspace.Path)
		if err != nil {
			return err
		}
		workspace.DirectoryID = identity
		if err := appendDurableJournal(journal, durablePhaseInitializing); err != nil {
			return fmt.Errorf("persist transaction workspace identity at %s: %w", workspace.Path, err)
		}
		if err := probeAtomicTransactionCapabilities(workspace.Path); err != nil {
			return fmt.Errorf(
				"filesystem at %s lacks required durable transaction primitives: %w",
				workspace.Path,
				err,
			)
		}
	}
	return nil
}

func transactionDirectoryIdentity(path string) (string, error) {
	directory, err := os.Open(path)
	if err != nil {
		return "", err
	}
	info, statErr := directory.Stat()
	if statErr != nil || !info.IsDir() {
		return "", errors.Join(
			fmt.Errorf("transaction workspace is not a directory: %s", path),
			statErr,
			directory.Close(),
		)
	}
	identity, identityErr := transactionFileIdentity(directory, info)
	return identity, errors.Join(identityErr, directory.Close())
}

func stageDurableEntries(journal *durableJournal, changes []fileChange) error {
	for index := range journal.Entries {
		entry := &journal.Entries[index]
		change := changes[index]
		if entry.Kind == durableDelete {
			continue
		}
		if err := writeOwnedFile(entry.NextPath, change.next, change.mode); err != nil {
			return fmt.Errorf("stage %s at %s: %w", change.label, entry.NextPath, err)
		}
		snapshot, err := readDurableArtifact(entry.NextPath, "staged "+change.label)
		if err != nil {
			return err
		}
		entry.Next = artifactFromSnapshot(snapshot)
		if entry.Next.SHA256 != artifactDigest(change.next) ||
			!sameManagedFileMode(snapshot.mode, change.mode) {
			return fmt.Errorf("staged %s changed before commit: %s", change.label, entry.NextPath)
		}
		if err := appendDurableJournal(journal, durablePhaseInitializing); err != nil {
			return fmt.Errorf("persist staged %s identity: %w", change.label, err)
		}
	}
	return nil
}

func probeAtomicTransactionCapabilities(workspace string) (result error) {
	target := filepath.Join(workspace, "probe-target")
	replacement := filepath.Join(workspace, "probe-replacement")
	backup := replacement
	if atomicReplacementUsesSeparateBackup() {
		backup = filepath.Join(workspace, "probe-backup")
	}
	discard := filepath.Join(workspace, "probe-discard")
	left := filepath.Join(workspace, "probe-left")
	right := filepath.Join(workspace, "probe-right")
	defer func() {
		result = errors.Join(
			result,
			cleanupInterruptedCapabilityProbe(workspace),
		)
	}()
	for path, content := range map[string]string{target: "old", replacement: "new"} {
		if err := writeOwnedFile(path, []byte(content), 0600); err != nil {
			return err
		}
	}
	if err := atomicReplaceWithBackup(target, replacement, backup); err != nil {
		return err
	}
	installed, err := readDurableArtifact(target, "atomic replacement probe")
	if err != nil || installed == nil || string(installed.contents) != "new" {
		return errors.Join(fmt.Errorf("atomic replacement probe failed"), err)
	}
	old, err := readDurableArtifact(backup, "atomic backup probe")
	if err != nil || old == nil || string(old.contents) != "old" {
		return errors.Join(fmt.Errorf("atomic replacement backup probe failed"), err)
	}
	if err := atomicReplaceWithBackup(target, backup, discardPathForProbe(backup, discard)); err != nil {
		return fmt.Errorf("atomic rollback probe failed: %w", err)
	}
	if err := writeOwnedFile(left, []byte("left"), 0600); err != nil {
		return err
	}
	if err := writeOwnedFile(right, []byte("right"), 0600); err != nil {
		return err
	}
	leftBefore, err := readDurableArtifact(left, "atomic no-replace source probe")
	if err != nil || leftBefore == nil {
		return errors.Join(fmt.Errorf("read atomic no-replace source probe"), err)
	}
	rightBefore, err := readDurableArtifact(right, "atomic no-replace destination probe")
	if err != nil || rightBefore == nil {
		return errors.Join(fmt.Errorf("read atomic no-replace destination probe"), err)
	}
	renameErr := atomicRenameNoReplace(left, right)
	if renameErr == nil {
		return fmt.Errorf("atomic no-replace probe replaced an existing file")
	}
	if !isAtomicDestinationExists(renameErr) {
		return fmt.Errorf("atomic no-replace probe returned an unsupported error: %w", renameErr)
	}
	if _, err := requireDurableArtifact(
		left,
		"atomic no-replace source probe",
		artifactFromSnapshot(leftBefore),
	); err != nil {
		return err
	}
	if _, err := requireDurableArtifact(
		right,
		"atomic no-replace destination probe",
		artifactFromSnapshot(rightBefore),
	); err != nil {
		return err
	}
	return nil
}

func discardPathForProbe(backup, discard string) string {
	if atomicReplacementUsesSeparateBackup() {
		return discard
	}
	return backup
}

func cleanupInterruptedCapabilityProbe(workspace string) error {
	var failures []error
	for _, name := range []string{
		"probe-target",
		"probe-replacement",
		"probe-backup",
		"probe-discard",
		"probe-left",
		"probe-right",
	} {
		path := filepath.Join(workspace, name)
		snapshot, err := readManagedFile(path, "transaction capability probe", 64)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if snapshot != nil {
			failures = append(failures, removePhysicalOwnedFile(path, snapshot.identity))
		}
	}
	return errors.Join(errors.Join(failures...), syncTransactionDirectory(workspace))
}
