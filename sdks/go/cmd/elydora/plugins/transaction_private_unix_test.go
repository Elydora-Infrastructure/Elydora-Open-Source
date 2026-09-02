//go:build linux || darwin

package plugins

import (
	"os"
	"strings"
	"testing"
)

func TestPrivateTransactionDirectoryRejectsDifferentEffectiveOwner(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("changing directory ownership requires an elevated test process")
	}
	path := t.TempDir()
	if err := os.Chown(path, 1, -1); err != nil {
		t.Fatal(err)
	}
	if err := verifyPrivateTransactionDirectory(path); err == nil ||
		!strings.Contains(err.Error(), "effective user") {
		t.Fatalf("owner mismatch error = %v", err)
	}
}

func TestTransactionNamespaceParentRejectsDifferentEffectiveOwner(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("changing directory ownership requires an elevated test process")
	}
	path := t.TempDir()
	if err := os.Chown(path, 1, -1); err != nil {
		t.Fatal(err)
	}
	if err := verifyTransactionNamespaceParent(path); err == nil ||
		!strings.Contains(err.Error(), "effective user") {
		t.Fatalf("parent owner mismatch error = %v", err)
	}
}
