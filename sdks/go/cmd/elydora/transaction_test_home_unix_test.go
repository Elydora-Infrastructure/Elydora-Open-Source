//go:build !windows

package main

import (
	"os"
	"testing"
)

func preparePrivateCLITestHome(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatalf("create private CLI test home %s: %v", path, err)
	}
	if err := os.Chmod(path, 0700); err != nil {
		t.Fatalf("protect CLI test home %s: %v", path, err)
	}
}
