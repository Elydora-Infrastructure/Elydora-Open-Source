package plugins

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func sameTransactionTestPath(left, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func prepareTransactionRemoval(t *testing.T, path, label string) *fileChange {
	t.Helper()
	original, existed, err := readOptionalFile(path, label)
	if err != nil {
		t.Fatalf("read removal source: %v", err)
	}
	change, err := prepareSourceChange(path, label, original, existed, nil, 0600, true)
	if err != nil {
		t.Fatalf("prepare removal: %v", err)
	}
	return change
}

func requireTransactionContent(t *testing.T, path, expected string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(content) != expected {
		t.Fatalf("content at %s = %q, want %q", path, content, expected)
	}
}

func TestTransactionRejectsReplacementAtOriginalCaptureBoundary(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*testing.T, string) *fileChange
	}{
		{
			name: "update",
			change: func(t *testing.T, path string) *fileChange {
				change, err := prepareFileChange(path, "boundary file", []byte("next"), 0600)
				if err != nil {
					t.Fatalf("prepare update: %v", err)
				}
				return change
			},
		},
		{name: "delete", change: func(t *testing.T, path string) *fileChange {
			return prepareTransactionRemoval(t, path, "boundary file")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			target := filepath.Join(directory, "managed.json")
			external := filepath.Join(directory, "original.external")
			if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
				t.Fatalf("write original: %v", err)
			}
			change := test.change(t, target)
			replaced := false
			ops := transactionFileOps{moveNoReplace: func(source, destination string) error {
				if !replaced && sameTransactionTestPath(source, target) &&
					strings.HasSuffix(destination, ".rollback") {
					replaced = true
					if err := os.Rename(target, external); err != nil {
						return err
					}
					if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
						return err
					}
				}
				return atomicRenameNoReplace(source, destination)
			}}

			err := writeChangesWithFileOps(
				[]*fileChange{change},
				"capture boundary transaction",
				ops,
			)
			if err == nil || !strings.Contains(err.Error(), "atomic capture boundary") {
				t.Fatalf("transaction error = %v", err)
			}
			requireTransactionContent(t, target, "original")
			requireTransactionContent(t, external, "original")
		})
	}
}

func TestTransactionCreatePreservesObjectAppearingAtCommitBoundary(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "created.json")
	change, err := prepareFileChange(target, "created file", []byte("managed"), 0600)
	if err != nil {
		t.Fatalf("prepare create: %v", err)
	}
	appeared := false
	ops := transactionFileOps{moveNoReplace: func(source, destination string) error {
		if !appeared && sameTransactionTestPath(destination, target) &&
			strings.HasSuffix(source, ".tmp") {
			appeared = true
			if err := os.WriteFile(target, []byte("concurrent"), 0600); err != nil {
				return err
			}
		}
		return atomicRenameNoReplace(source, destination)
	}}

	err = writeChangesWithFileOps(
		[]*fileChange{change},
		"create boundary transaction",
		ops,
	)
	if err == nil || !strings.Contains(err.Error(), "without replacing another file") {
		t.Fatalf("transaction error = %v", err)
	}
	requireTransactionContent(t, target, "concurrent")
}

func TestTransactionRollbackRestorePreservesObjectAppearingAtBoundary(t *testing.T) {
	directory := t.TempDir()
	first := filepath.Join(directory, "first.json")
	second := filepath.Join(directory, "second.json")
	if err := os.WriteFile(first, []byte("first-original"), 0600); err != nil {
		t.Fatalf("write original: %v", err)
	}
	firstChange, err := prepareFileChange(first, "first file", []byte("first-next"), 0600)
	if err != nil {
		t.Fatalf("prepare first change: %v", err)
	}
	secondChange, err := prepareFileChange(second, "second file", []byte("second-next"), 0600)
	if err != nil {
		t.Fatalf("prepare second change: %v", err)
	}
	appeared := false
	ops := transactionFileOps{moveNoReplace: func(source, destination string) error {
		if sameTransactionTestPath(destination, second) && strings.HasSuffix(source, ".tmp") {
			return errors.New("injected second commit failure")
		}
		if !appeared && sameTransactionTestPath(destination, first) &&
			strings.HasSuffix(source, ".rollback") {
			appeared = true
			if err := os.WriteFile(first, []byte("concurrent"), 0600); err != nil {
				return err
			}
		}
		return atomicRenameNoReplace(source, destination)
	}}

	err = writeChangesWithFileOps(
		[]*fileChange{firstChange, secondChange},
		"rollback restore boundary transaction",
		ops,
	)
	if err == nil || !strings.Contains(err.Error(), "original content preserved at") {
		t.Fatalf("transaction error = %v", err)
	}
	requireTransactionContent(t, first, "concurrent")
	requirePreservedRollback(t, directory)
}

func TestTransactionRollbackRestoreRejectsReplacedSourceAtBoundary(t *testing.T) {
	directory := t.TempDir()
	first := filepath.Join(directory, "first.json")
	second := filepath.Join(directory, "second.json")
	external := filepath.Join(directory, "original.external")
	if err := os.WriteFile(first, []byte("first-original"), 0600); err != nil {
		t.Fatalf("write original: %v", err)
	}
	firstChange, err := prepareFileChange(first, "first file", []byte("first-next"), 0600)
	if err != nil {
		t.Fatalf("prepare first change: %v", err)
	}
	secondChange, err := prepareFileChange(second, "second file", []byte("second-next"), 0600)
	if err != nil {
		t.Fatalf("prepare second change: %v", err)
	}
	rollbackPath := ""
	replaced := false
	ops := transactionFileOps{moveNoReplace: func(source, destination string) error {
		if sameTransactionTestPath(source, first) && strings.HasSuffix(destination, ".rollback") {
			rollbackPath = destination
		}
		if sameTransactionTestPath(destination, second) && strings.HasSuffix(source, ".tmp") {
			return errors.New("injected second commit failure")
		}
		if !replaced && sameTransactionTestPath(source, rollbackPath) &&
			sameTransactionTestPath(destination, first) {
			replaced = true
			if err := os.Rename(rollbackPath, external); err != nil {
				return err
			}
			if err := os.WriteFile(rollbackPath, []byte("concurrent"), 0600); err != nil {
				return err
			}
		}
		return atomicRenameNoReplace(source, destination)
	}}

	err = writeChangesWithFileOps(
		[]*fileChange{firstChange, secondChange},
		"rollback source boundary transaction",
		ops,
	)
	if err == nil || !strings.Contains(err.Error(), "atomic restore boundary") {
		t.Fatalf("transaction error = %v", err)
	}
	if _, statErr := os.Lstat(first); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("target exists after rejected rollback source: %v", statErr)
	}
	requireTransactionContent(t, rollbackPath, "concurrent")
	requireTransactionContent(t, external, "first-original")
}

func TestTransactionRollbackDeletePreservesReplacementAtBoundary(t *testing.T) {
	directory := t.TempDir()
	first := filepath.Join(directory, "first.json")
	second := filepath.Join(directory, "second.json")
	external := filepath.Join(directory, "managed.external")
	firstChange, err := prepareFileChange(first, "first file", []byte("first-next"), 0600)
	if err != nil {
		t.Fatalf("prepare first change: %v", err)
	}
	secondChange, err := prepareFileChange(second, "second file", []byte("second-next"), 0600)
	if err != nil {
		t.Fatalf("prepare second change: %v", err)
	}
	replaced := false
	ops := transactionFileOps{moveNoReplace: func(source, destination string) error {
		if sameTransactionTestPath(destination, second) && strings.HasSuffix(source, ".tmp") {
			return errors.New("injected second commit failure")
		}
		if !replaced && sameTransactionTestPath(source, first) &&
			strings.HasSuffix(destination, ".discard") {
			replaced = true
			if err := os.Rename(first, external); err != nil {
				return err
			}
			if err := os.WriteFile(first, []byte("concurrent"), 0600); err != nil {
				return err
			}
		}
		return atomicRenameNoReplace(source, destination)
	}}

	err = writeChangesWithFileOps(
		[]*fileChange{firstChange, secondChange},
		"rollback delete boundary transaction",
		ops,
	)
	if err == nil || !strings.Contains(err.Error(), "atomic rollback boundary") {
		t.Fatalf("transaction error = %v", err)
	}
	requireTransactionContent(t, first, "concurrent")
	requireTransactionContent(t, external, "first-next")
}

func TestTransactionCleanupPreservesReplacedRollbackObject(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "managed.json")
	external := filepath.Join(directory, "original.external")
	if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
		t.Fatalf("write original: %v", err)
	}
	change, err := prepareFileChange(target, "managed file", []byte("next"), 0600)
	if err != nil {
		t.Fatalf("prepare update: %v", err)
	}
	rollbackPath := ""
	replaced := false
	ops := transactionFileOps{moveNoReplace: func(source, destination string) error {
		if sameTransactionTestPath(source, target) && strings.HasSuffix(destination, ".rollback") {
			rollbackPath = destination
		}
		if err := atomicRenameNoReplace(source, destination); err != nil {
			return err
		}
		if !replaced && sameTransactionTestPath(destination, target) &&
			strings.HasSuffix(source, ".tmp") {
			replaced = true
			if err := os.Rename(rollbackPath, external); err != nil {
				return err
			}
			if err := os.WriteFile(rollbackPath, []byte("concurrent"), 0600); err != nil {
				return err
			}
		}
		return nil
	}}

	err = writeChangesWithFileOps(
		[]*fileChange{change},
		"cleanup boundary transaction",
		ops,
	)
	if err == nil || !strings.Contains(err.Error(), "changed during cleanup") {
		t.Fatalf("transaction error = %v", err)
	}
	requireTransactionContent(t, target, "next")
	requireTransactionContent(t, rollbackPath, "concurrent")
	requireTransactionContent(t, external, "original")
}

func TestDurableJournalStateRejectsOversizedPhysicalFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state-00000000000000000001.json")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxDurableJournalBytes + 1); err != nil {
		t.Fatal(errors.Join(err, file.Close()))
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readDurableJournalState(path); err == nil ||
		!strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized journal error = %v", err)
	}
}

func TestDurableJournalStateRejectsSymlink(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target.json")
	link := filepath.Join(directory, "state-00000000000000000001.json")
	if err := os.WriteFile(target, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := readDurableJournalState(link); err == nil ||
		!strings.Contains(err.Error(), "not a physical file") {
		t.Fatalf("journal symlink error = %v", err)
	}
}

func TestAtomicDestinationExistsRejectsUnrelatedFailure(t *testing.T) {
	if isAtomicDestinationExists(os.ErrPermission) {
		t.Fatal("permission failure was classified as an occupied destination")
	}
}
