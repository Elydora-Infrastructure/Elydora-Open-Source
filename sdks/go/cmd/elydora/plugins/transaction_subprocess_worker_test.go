package plugins

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const durableWorkerEnvironment = "ELYDORA_DURABLE_TRANSACTION_WORKER"

func TestDurableTransactionSubprocessWorker(t *testing.T) {
	mode := os.Getenv(durableWorkerEnvironment)
	if mode == "" {
		return
	}
	target := os.Getenv("ELYDORA_DURABLE_TARGET")
	switch mode {
	case "crash-update":
		runDurableCrashWorker(t, target)
	case "crash-multi-update":
		runDurableMultiCrashWorker(t, target, os.Getenv("ELYDORA_DURABLE_SECOND_TARGET"))
	case "crash-create":
		runDurableCreateCrashWorker(t, target)
	case "crash-delete":
		runDurableDeleteCrashWorker(t, target)
	case "crash-committed-cleanup":
		runDurableCommittedCleanupCrashWorker(t, target)
	case "crash-rollback-hardlink":
		runDurableRollbackHardlinkCrashWorker(t)
	case "crash-init-workspace-empty":
		runDurableInitializationWorkspaceCrashWorker(t, target, false)
	case "crash-init-workspace-owner":
		runDurableInitializationWorkspaceCrashWorker(t, target, true)
	case "crash-init-stage-full":
		runDurableInitializationStageCrashWorker(t, target, false)
	case "crash-init-stage-partial":
		runDurableInitializationStageCrashWorker(t, target, true)
	case "crash-init-probe":
		runDurableInitializationProbeCrashWorker(t, target)
	case "crash-journal-owner":
		runDurableJournalOwnerCrashWorker(t)
	case "crash-journal-pending-full":
		runDurableJournalPendingCrashWorker(t, target, false)
	case "crash-journal-pending-partial":
		runDurableJournalPendingCrashWorker(t, target, true)
	case "crash-workspace-marker-cleanup":
		runDurableWorkspaceMarkerCleanupCrashWorker(t, target)
	case "crash-journal-marker-cleanup":
		runDurableJournalMarkerCleanupCrashWorker(t, target)
	case "crash-torn-journal-tail":
		runDurableTornJournalTailCrashWorker(t, target)
	case "reader":
		runDurableReaderWorker(t, target)
	case "writer":
		runDurableWriterWorker(t, target)
	default:
		t.Fatalf("unknown durable transaction worker %q", mode)
	}
}

func runDurableRollbackHardlinkCrashWorker(t *testing.T) {
	stateRoot, err := durableTransactionStateRoot()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	journalPath := ""
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "txn-") {
			if journalPath != "" {
				t.Fatal("multiple recovery journals found")
			}
			journalPath = filepath.Join(stateRoot, entry.Name())
		}
	}
	if journalPath == "" {
		t.Fatal("recovery journal is unavailable")
	}
	journal, err := loadDurableJournal(journalPath)
	if err != nil || len(journal.Entries) != 1 {
		t.Fatalf("load recovery journal: %v", err)
	}
	entry := journal.Entries[0]
	if err := os.Link(entry.Path, entry.DiscardPath); err != nil {
		t.Fatal(err)
	}
	if err := syncTransactionDirectory(filepath.Dir(entry.DiscardPath)); err != nil {
		t.Fatal(err)
	}
	os.Exit(91)
}

func runDurableMultiCrashWorker(t *testing.T, firstTarget, secondTarget string) {
	changes := make([]*fileChange, 0, 2)
	for _, item := range []struct {
		path  string
		value string
	}{{firstTarget, "first-next"}, {secondTarget, "second-next"}} {
		change, err := prepareFileChange(item.path, "multi crash target", []byte(item.value), 0600)
		if err != nil {
			t.Fatal(err)
		}
		changes = append(changes, change)
	}
	ops := newTransactionFileOps(nil)
	replace := ops.replaceWithBackup
	commits := 0
	ops.replaceWithBackup = func(current, replacement, backup string) error {
		if err := replace(current, replacement, backup); err != nil {
			return err
		}
		commits++
		if commits == 2 {
			os.Exit(87)
		}
		return nil
	}
	if err := writeChangesWithFileOps(changes, "multi crash transaction", ops); err != nil {
		t.Fatal(err)
	}
	t.Fatal("multi crash worker completed without terminating")
}

func runDurableCreateCrashWorker(t *testing.T, target string) {
	change, err := prepareFileChange(target, "crash create target", []byte("created"), 0600)
	if err != nil {
		t.Fatal(err)
	}
	ops := newTransactionFileOps(nil)
	move := ops.moveNoReplace
	ops.moveNoReplace = func(source, destination string) error {
		if err := move(source, destination); err != nil {
			return err
		}
		if sameTransactionTestPath(destination, target) {
			os.Exit(88)
		}
		return nil
	}
	if err := writeChangesWithFileOps([]*fileChange{change}, "crash create", ops); err != nil {
		t.Fatal(err)
	}
	t.Fatal("create crash worker completed without terminating")
}

func runDurableDeleteCrashWorker(t *testing.T, target string) {
	change, err := prepareSourceChange(
		target,
		"crash delete target",
		[]byte("delete-original"),
		true,
		nil,
		0600,
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	ops := newTransactionFileOps(nil)
	move := ops.moveNoReplace
	ops.moveNoReplace = func(source, destination string) error {
		if err := move(source, destination); err != nil {
			return err
		}
		if sameTransactionTestPath(source, target) {
			os.Exit(89)
		}
		return nil
	}
	if err := writeChangesWithFileOps([]*fileChange{change}, "crash delete", ops); err != nil {
		t.Fatal(err)
	}
	t.Fatal("delete crash worker completed without terminating")
}

func runDurableCommittedCleanupCrashWorker(t *testing.T, target string) {
	change, err := prepareFileChange(target, "committed cleanup target", []byte("committed"), 0600)
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
	err = writeChangesWithFileOps([]*fileChange{change}, "committed cleanup", ops)
	if err == nil || !strings.Contains(err.Error(), "committed; durable cleanup failed") {
		t.Fatalf("committed cleanup error = %v", err)
	}
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	os.Exit(90)
}

func runDurableCrashWorker(t *testing.T, target string) {
	change, err := prepareFileChange(target, "crash target", []byte("next"), 0600)
	if err != nil {
		t.Fatal(err)
	}
	ops := newTransactionFileOps(nil)
	replace := ops.replaceWithBackup
	absoluteTarget, err := filepath.Abs(target)
	if err != nil {
		t.Fatal(err)
	}
	ops.replaceWithBackup = func(current, replacement, backup string) error {
		if err := replace(current, replacement, backup); err != nil {
			return err
		}
		if sameTransactionTestPath(current, absoluteTarget) {
			os.Exit(86)
		}
		return nil
	}
	if err := writeChangesWithFileOps([]*fileChange{change}, "crash transaction", ops); err != nil {
		t.Fatal(err)
	}
	t.Fatal("crash worker completed without terminating")
}

func runDurableReaderWorker(t *testing.T, target string) {
	stop := os.Getenv("ELYDORA_DURABLE_STOP")
	for {
		if _, err := os.Lstat(stop); err == nil {
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		content, err := os.ReadFile(target)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				t.Fatalf("reader observed missing target: %v", err)
			}
			if _, statErr := os.Lstat(target); statErr != nil {
				t.Fatalf("reader observed unavailable target: %v; stat=%v", err, statErr)
			}
			time.Sleep(time.Millisecond)
			continue
		}
		if string(content) != "value-a" && string(content) != "value-b" {
			t.Fatalf("reader observed partial value %q", content)
		}
		time.Sleep(time.Millisecond)
	}
}

func runDurableWriterWorker(t *testing.T, target string) {
	value := os.Getenv("ELYDORA_DURABLE_VALUE")
	ready := os.Getenv("ELYDORA_DURABLE_READY")
	start := os.Getenv("ELYDORA_DURABLE_START")
	change, err := prepareFileChange(target, "writer target", []byte(value), 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ready, []byte("ready"), 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := os.Lstat(start); err == nil {
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("writer start barrier timed out")
		}
		time.Sleep(time.Millisecond)
	}
	if err := writeChanges([]*fileChange{change}, "subprocess writer", nil); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(23)
	}
}
