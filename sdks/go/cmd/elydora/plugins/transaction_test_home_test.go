package plugins

import (
	"os"
	"testing"
)

func preparePrivateTransactionTestDirectory(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatalf("create private test directory %s: %v", path, err)
	}
	if err := ensurePrivateTransactionDirectory(path); err != nil {
		t.Fatalf("protect private test directory %s: %v", path, err)
	}
}
