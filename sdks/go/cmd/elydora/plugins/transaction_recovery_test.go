package plugins

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func transactionTestChanges(t *testing.T) (string, string, []*fileChange) {
	t.Helper()
	directory := t.TempDir()
	firstPath := filepath.Join(directory, "first.json")
	secondPath := filepath.Join(directory, "second.json")
	for _, item := range []struct {
		path    string
		content string
	}{
		{firstPath, "first-original"},
		{secondPath, "second-original"},
	} {
		if err := os.WriteFile(item.path, []byte(item.content), 0600); err != nil {
			t.Fatalf("write transaction fixture: %v", err)
		}
	}
	first, err := prepareFileChange(firstPath, "first test file", []byte("first-next"), 0600)
	if err != nil {
		t.Fatalf("prepare first transaction change: %v", err)
	}
	second, err := prepareFileChange(secondPath, "second test file", []byte("second-next"), 0600)
	if err != nil {
		t.Fatalf("prepare second transaction change: %v", err)
	}
	return firstPath, secondPath, []*fileChange{first, second}
}

func requirePreservedRollback(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read transaction directory: %v", err)
	}
	var rollbackPath string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".rollback") {
			if rollbackPath != "" {
				t.Fatal("multiple rollback files remain")
			}
			rollbackPath = filepath.Join(directory, entry.Name())
		}
	}
	if rollbackPath == "" {
		t.Fatal("original rollback file was removed")
	}
	content, err := os.ReadFile(rollbackPath)
	if err != nil {
		t.Fatalf("read preserved rollback file: %v", err)
	}
	if string(content) != "first-original" {
		t.Fatalf("preserved rollback content = %q", content)
	}
}

func TestTransactionPreservesBackupAfterConcurrentCommittedChange(t *testing.T) {
	firstPath, secondPath, changes := transactionTestChanges(t)
	rename := func(source, destination string) error {
		if destination == secondPath && strings.HasSuffix(source, ".tmp") {
			if err := os.Remove(firstPath); err != nil {
				return err
			}
			if err := os.WriteFile(firstPath, []byte("external"), 0600); err != nil {
				return err
			}
			return errors.New("simulated second commit failure")
		}
		return os.Rename(source, destination)
	}
	err := writeChanges(changes, "Test transaction", rename)
	if err == nil || !strings.Contains(err.Error(), "original content preserved at") {
		t.Fatalf("transaction error = %v", err)
	}
	current, readErr := os.ReadFile(firstPath)
	if readErr != nil || string(current) != "external" {
		t.Fatalf("external content = %q, %v", current, readErr)
	}
	requirePreservedRollback(t, filepath.Dir(firstPath))
}

func TestTransactionPreservesBackupAfterRollbackRenameFailure(t *testing.T) {
	firstPath, secondPath, changes := transactionTestChanges(t)
	rename := func(source, destination string) error {
		if destination == secondPath && strings.HasSuffix(source, ".tmp") {
			return errors.New("simulated second commit failure")
		}
		if destination == firstPath && strings.HasSuffix(source, ".rollback") {
			return errors.New("simulated rollback failure")
		}
		return os.Rename(source, destination)
	}
	err := writeChanges(changes, "Test transaction", rename)
	if err == nil || !strings.Contains(err.Error(), "original content preserved at") {
		t.Fatalf("transaction error = %v", err)
	}
	requirePreservedRollback(t, filepath.Dir(firstPath))
}
