package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAugmentCLIPreflightPreservesMalformedSettings(t *testing.T) {
	homeDir := filepath.Join(t.TempDir(), "home")
	settingsPath := filepath.Join(homeDir, ".augment", "settings.json")
	keyPath := filepath.Join(t.TempDir(), "private.key")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0700); err != nil {
		t.Fatalf("create Auggie settings directory: %v", err)
	}
	if err := os.WriteFile(settingsPath, []byte("{ malformed"), 0600); err != nil {
		t.Fatalf("write malformed Auggie settings: %v", err)
	}
	privateKey := "BwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwc"
	if err := os.WriteFile(keyPath, []byte(privateKey+"\n"), 0600); err != nil {
		t.Fatalf("write private key: %v", err)
	}
	command := exec.Command(
		"go",
		"run",
		".",
		"install",
		"--agent",
		"augment",
		"--org-id",
		"org-1",
		"--agent-id",
		"agent-1",
		"--private-key-file",
		keyPath,
		"--kid",
		"kid-1",
		"--base-url",
		"https://api.elydora.com",
	)
	command.Env = append(
		os.Environ(),
		"HOME="+homeDir,
		"USERPROFILE="+homeDir,
	)
	output, err := command.CombinedOutput()
	if err == nil || !strings.Contains(
		string(output),
		"parse Auggie user settings",
	) {
		t.Fatalf("CLI preflight result = %v\n%s", err, output)
	}
	raw, readErr := os.ReadFile(settingsPath)
	if readErr != nil || string(raw) != "{ malformed" {
		t.Fatalf("malformed settings changed: %q, %v", raw, readErr)
	}
	if _, statErr := os.Lstat(filepath.Join(homeDir, ".elydora")); !errors.Is(
		statErr,
		os.ErrNotExist,
	) {
		t.Fatalf("runtime directory created during failed preflight: %v", statErr)
	}
}
