package plugins

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func prepareInitializingWorkerJournal(t *testing.T, target string) *durableJournal {
	t.Helper()
	change, err := prepareFileChange(target, "initialization crash target", []byte("next"), 0600)
	if err != nil {
		t.Fatal(err)
	}
	changes, err := filterDurableChanges([]*fileChange{change}, "initialization crash")
	if err != nil {
		t.Fatal(err)
	}
	stateRoot, err := durableTransactionStateRoot()
	if err != nil {
		t.Fatal(err)
	}
	journal, err := createDurableJournal(stateRoot, "initialization crash")
	if err != nil {
		t.Fatal(err)
	}
	if err := initializeDurableLayout(journal, changes); err != nil {
		t.Fatal(err)
	}
	if err := appendDurableJournal(journal, durablePhaseInitializing); err != nil {
		t.Fatal(err)
	}
	return journal
}

func runDurableInitializationWorkspaceCrashWorker(
	t *testing.T,
	target string,
	partialOwner bool,
) {
	journal := prepareInitializingWorkerJournal(t, target)
	workspace := journal.Workspaces[0]
	if err := createPrivateTransactionDirectory(workspace.Path); err != nil {
		t.Fatal(err)
	}
	if partialOwner {
		writePartialDurableWorkerFile(t, workspace.MarkerPath, []byte(journal.ID+":"))
	}
	if err := syncTransactionDirectory(filepath.Dir(workspace.Path)); err != nil {
		t.Fatal(err)
	}
	os.Exit(92)
}

func runDurableInitializationStageCrashWorker(
	t *testing.T,
	target string,
	partial bool,
) {
	journal := prepareInitializingWorkerJournal(t, target)
	if err := createDurableWorkspaces(journal); err != nil {
		t.Fatal(err)
	}
	entry := journal.Entries[0]
	if partial {
		writePartialDurableWorkerFile(t, entry.NextPath, []byte("par"))
	} else if err := writeOwnedFile(entry.NextPath, []byte("next"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := syncTransactionDirectory(entry.Workspace); err != nil {
		t.Fatal(err)
	}
	os.Exit(93)
}

func runDurableInitializationProbeCrashWorker(t *testing.T, target string) {
	journal := prepareInitializingWorkerJournal(t, target)
	workspace := &journal.Workspaces[0]
	if err := createPrivateTransactionDirectory(workspace.Path); err != nil {
		t.Fatal(err)
	}
	marker := []byte(journal.ID + ":" + workspace.OwnerToken + "\n")
	if err := writeOwnedFile(workspace.MarkerPath, marker, 0600); err != nil {
		t.Fatal(err)
	}
	identity, err := transactionDirectoryIdentity(workspace.Path)
	if err != nil {
		t.Fatal(err)
	}
	workspace.DirectoryID = identity
	if err := appendDurableJournal(journal, durablePhaseInitializing); err != nil {
		t.Fatal(err)
	}
	writePartialDurableWorkerFile(
		t,
		filepath.Join(workspace.Path, "probe-target"),
		[]byte("o"),
	)
	os.Exit(94)
}

func runDurableJournalOwnerCrashWorker(t *testing.T) {
	stateRoot, err := durableTransactionStateRoot()
	if err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("a", 48)
	directory := filepath.Join(stateRoot, "txn-"+id)
	if err := createPrivateTransactionDirectory(directory); err != nil {
		t.Fatal(err)
	}
	writePartialDurableWorkerFile(t, filepath.Join(directory, "owner"), []byte(id+":"))
	if err := syncTransactionDirectory(stateRoot); err != nil {
		t.Fatal(err)
	}
	os.Exit(95)
}

func runDurableJournalPendingCrashWorker(t *testing.T, target string, partial bool) {
	journal := prepareInitializingWorkerJournalWithoutState(t, target)
	journal.Sequence = 1
	journal.Phase = durablePhaseInitializing
	path := filepath.Join(journal.JournalDir, "state-00000000000000000001.pending")
	if partial {
		writePartialDurableWorkerFile(t, path, []byte("{"))
	} else {
		raw, err := json.Marshal(journal)
		if err != nil {
			t.Fatal(err)
		}
		if err := writeOwnedFile(path, append(raw, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := syncTransactionDirectory(journal.JournalDir); err != nil {
		t.Fatal(err)
	}
	os.Exit(96)
}

func prepareInitializingWorkerJournalWithoutState(t *testing.T, target string) *durableJournal {
	t.Helper()
	change, err := prepareFileChange(target, "pending journal target", []byte("next"), 0600)
	if err != nil {
		t.Fatal(err)
	}
	changes, err := filterDurableChanges([]*fileChange{change}, "pending journal")
	if err != nil {
		t.Fatal(err)
	}
	stateRoot, err := durableTransactionStateRoot()
	if err != nil {
		t.Fatal(err)
	}
	journal, err := createDurableJournal(stateRoot, "pending journal")
	if err != nil {
		t.Fatal(err)
	}
	if err := initializeDurableLayout(journal, changes); err != nil {
		t.Fatal(err)
	}
	return journal
}

func writePartialDurableWorkerFile(t *testing.T, path string, content []byte) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(content); err != nil {
		t.Fatal(errors.Join(err, file.Close()))
	}
	if err := errors.Join(file.Sync(), file.Close()); err != nil {
		t.Fatal(err)
	}
	if err := syncTransactionDirectory(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
}

func prepareBlockedCommittedWorkerJournal(t *testing.T, target string) *durableJournal {
	t.Helper()
	change, err := prepareFileChange(target, "cleanup marker target", []byte("committed"), 0600)
	if err != nil {
		t.Fatal(err)
	}
	ops := newTransactionFileOps(nil)
	replace := ops.replaceWithBackup
	blocker := ""
	ops.replaceWithBackup = func(current, replacement, backup string) error {
		if err := replace(current, replacement, backup); err != nil {
			return err
		}
		blocker = filepath.Join(filepath.Dir(backup), "cleanup-blocker")
		return os.WriteFile(blocker, []byte("block"), 0600)
	}
	err = writeChangesWithFileOps([]*fileChange{change}, "cleanup marker", ops)
	if err == nil || !strings.Contains(err.Error(), "committed; durable cleanup failed") {
		t.Fatalf("committed cleanup error = %v", err)
	}
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	journal := loadOnlyDurableWorkerJournal(t)
	if journal.Phase != durablePhaseCommitted {
		t.Fatalf("journal phase = %s", journal.Phase)
	}
	return journal
}

func loadOnlyDurableWorkerJournal(t *testing.T) *durableJournal {
	t.Helper()
	stateRoot, err := durableTransactionStateRoot()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	var directory string
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "txn-") {
			if directory != "" {
				t.Fatal("multiple durable journals")
			}
			directory = filepath.Join(stateRoot, entry.Name())
		}
	}
	if directory == "" {
		t.Fatal("durable journal is unavailable")
	}
	journal, err := loadDurableJournal(directory)
	if err != nil {
		t.Fatal(err)
	}
	return journal
}

func runDurableWorkspaceMarkerCleanupCrashWorker(t *testing.T, target string) {
	journal := prepareBlockedCommittedWorkerJournal(t, target)
	workspace := journal.Workspaces[0]
	marker, err := readManagedFile(
		workspace.MarkerPath,
		"transaction workspace owner",
		maxDurableJournalBytes,
	)
	if err != nil || marker == nil {
		t.Fatal(errors.Join(fmt.Errorf("read workspace marker"), err))
	}
	if err := removePhysicalOwnedFile(workspace.MarkerPath, marker.identity); err != nil {
		t.Fatal(err)
	}
	if err := syncTransactionDirectory(workspace.Path); err != nil {
		t.Fatal(err)
	}
	os.Exit(97)
}

func runDurableJournalMarkerCleanupCrashWorker(t *testing.T, target string) {
	journal := prepareBlockedCommittedWorkerJournal(t, target)
	for _, workspace := range journal.Workspaces {
		if err := cleanupDurableWorkspace(journal, workspace, cleanupCommitted); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(journal.JournalDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() == "owner" {
			continue
		}
		path := filepath.Join(journal.JournalDir, entry.Name())
		snapshot, err := readManagedFile(path, "journal state", maxDurableJournalBytes)
		if err != nil || snapshot == nil {
			t.Fatal(errors.Join(fmt.Errorf("read journal state"), err))
		}
		if err := removePhysicalOwnedFile(path, snapshot.identity); err != nil {
			t.Fatal(err)
		}
	}
	ownerPath := filepath.Join(journal.JournalDir, "owner")
	owner, err := readManagedFile(ownerPath, "journal owner", maxDurableJournalBytes)
	if err != nil || owner == nil {
		t.Fatal(errors.Join(fmt.Errorf("read journal owner"), err))
	}
	if err := removePhysicalOwnedFile(ownerPath, owner.identity); err != nil {
		t.Fatal(err)
	}
	if err := syncTransactionDirectory(journal.JournalDir); err != nil {
		t.Fatal(err)
	}
	os.Exit(98)
}

func runDurableTornJournalTailCrashWorker(t *testing.T, target string) {
	journal := prepareBlockedCommittedWorkerJournal(t, target)
	path := filepath.Join(
		journal.JournalDir,
		fmt.Sprintf("state-%020d.json", journal.Sequence+1),
	)
	writePartialDurableWorkerFile(t, path, []byte("{"))
	os.Exit(99)
}

func TestDurableTransactionRecoversInitializationTerminationBoundaries(t *testing.T) {
	for _, mode := range []string{
		"crash-init-workspace-empty",
		"crash-init-workspace-owner",
		"crash-init-stage-full",
		"crash-init-stage-partial",
		"crash-init-probe",
		"crash-journal-owner",
		"crash-journal-pending-full",
		"crash-journal-pending-partial",
	} {
		t.Run(mode, func(t *testing.T) {
			home := filepath.Join(t.TempDir(), "home")
			if err := createPrivateTransactionDirectory(home); err != nil {
				t.Fatal(err)
			}
			directory := t.TempDir()
			target := filepath.Join(directory, "managed.json")
			if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			requireDurableWorkerTermination(t, durableWorkerCommand(t, home, mode, target))
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			if err := writeChanges(nil, "initialization restart recovery", nil); err != nil {
				t.Fatal(err)
			}
			requireTransactionContent(t, target, "original")
			requireNoDurableTransactionState(t, home, directory)
		})
	}
}

func TestDurableTransactionRecoversCleanupMarkerAndTornTailTermination(t *testing.T) {
	for _, mode := range []string{
		"crash-workspace-marker-cleanup",
		"crash-journal-marker-cleanup",
		"crash-torn-journal-tail",
	} {
		t.Run(mode, func(t *testing.T) {
			home := filepath.Join(t.TempDir(), "home")
			if err := createPrivateTransactionDirectory(home); err != nil {
				t.Fatal(err)
			}
			directory := t.TempDir()
			target := filepath.Join(directory, "managed.json")
			if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			requireDurableWorkerTermination(t, durableWorkerCommand(t, home, mode, target))
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			if err := writeChanges(nil, "cleanup restart recovery", nil); err != nil {
				t.Fatal(err)
			}
			requireTransactionContent(t, target, "committed")
			requireNoDurableTransactionState(t, home, directory)
		})
	}
}

func TestDurableTransactionRecoversRelativeTargetFromDifferentWorkingDirectory(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if err := createPrivateTransactionDirectory(home); err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	target := filepath.Join(directory, "managed.json")
	if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	command := durableWorkerCommand(t, home, "crash-update", filepath.Base(target))
	command.Dir = directory
	requireDurableWorkerTermination(t, command)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := writeChanges(nil, "relative target restart recovery", nil); err != nil {
		t.Fatal(err)
	}
	requireTransactionContent(t, target, "original")
	requireNoDurableTransactionState(t, home, directory)
}

func requireDurableWorkerTermination(t *testing.T, command *exec.Cmd) {
	t.Helper()
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("worker completed without termination: %s", output)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("worker error = %v, output=%s", err, output)
	}
}
