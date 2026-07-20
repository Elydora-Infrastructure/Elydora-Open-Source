package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const droidCLIPrivateKey = "DQ0NDQ0NDQ0NDQ0NDQ0NDQ0NDQ0NDQ0NDQ0NDQ0NDQ0"

func prepareDroidCLIEnvironment(t *testing.T) (string, string, []string) {
	t.Helper()
	root := t.TempDir()
	homeDir := filepath.Join(root, "home with spaces and 'quote %DROID%")
	workspace := filepath.Join(root, "workspace with spaces")
	if err := os.MkdirAll(filepath.Join(workspace, ".git"), 0700); err != nil {
		t.Fatalf("create Factory Droid CLI workspace: %v", err)
	}
	return homeDir, workspace, []string{
		"HOME=" + homeDir,
		"USERPROFILE=" + homeDir,
	}
}

func TestDroidCLIPreflightBlocksPolicyBeforeRuntimeCreation(t *testing.T) {
	binary := buildClineCLIBinary(t)
	homeDir, workspace, environment := prepareDroidCLIEnvironment(t)
	settingsPath := filepath.Join(homeDir, ".factory", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0700); err != nil {
		t.Fatalf("create Factory settings directory: %v", err)
	}
	original := "{\"hooksDisabled\":true}\n"
	if err := os.WriteFile(settingsPath, []byte(original), 0600); err != nil {
		t.Fatalf("write Factory settings: %v", err)
	}
	keyPath := filepath.Join(t.TempDir(), "private.key")
	writeClineCLISecret(t, keyPath, droidCLIPrivateKey)
	_, stderr, exit := runClineCLI(
		t,
		binary,
		workspace,
		environment,
		"install",
		"--agent", "droid",
		"--org-id", "org-1",
		"--agent-id", "agent-1",
		"--private-key-file", keyPath,
		"--kid", "kid-1",
		"--base-url", "https://api.elydora.test",
	)
	if exit == 0 || !strings.Contains(stderr, "hooksDisabled") {
		t.Fatalf("Factory Droid CLI preflight = %d, %q", exit, stderr)
	}
	raw, err := os.ReadFile(settingsPath)
	if err != nil || string(raw) != original {
		t.Fatalf("Factory settings changed: %q, %v", raw, err)
	}
	for _, path := range []string{
		filepath.Join(homeDir, ".elydora"),
		filepath.Join(homeDir, ".factory", "hooks.json"),
	} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("path created during failed preflight at %s: %v", path, err)
		}
	}
}

func TestDroidCLIInstallStatusAndUninstallEndToEnd(t *testing.T) {
	binary := buildClineCLIBinary(t)
	homeDir, workspace, environment := prepareDroidCLIEnvironment(t)
	keyPath := filepath.Join(t.TempDir(), "private.key")
	tokenPath := filepath.Join(t.TempDir(), "token")
	writeClineCLISecret(t, keyPath, droidCLIPrivateKey)
	writeClineCLISecret(t, tokenPath, "token-1")
	stdout, stderr, exit := runClineCLI(
		t,
		binary,
		workspace,
		environment,
		"install",
		"--agent", "droid",
		"--org-id", "org-1",
		"--agent-id", "agent-1",
		"--private-key-file", keyPath,
		"--token-file", tokenPath,
		"--kid", "kid-1",
		"--base-url", "http://127.0.0.1:9",
	)
	if exit != 0 || stderr != "" || !strings.Contains(stdout, "Factory Droid") {
		t.Fatalf("Factory Droid CLI install = %d, %q, %q", exit, stdout, stderr)
	}
	agentDir := filepath.Join(homeDir, ".elydora", "agent-1")
	hooksPath := filepath.Join(homeDir, ".factory", "hooks.json")
	for _, path := range []string{
		filepath.Join(agentDir, "guard.js"),
		filepath.Join(agentDir, "config.json"),
		filepath.Join(agentDir, "private.key"),
		filepath.Join(agentDir, "hook.js"),
		hooksPath,
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("installed file missing at %s: %v", path, err)
		}
	}
	hooks, err := os.ReadFile(hooksPath)
	if err != nil || !strings.Contains(string(hooks), `"hooks"`) ||
		!strings.Contains(string(hooks), `"PreToolUse"`) ||
		!strings.Contains(string(hooks), `"PostToolUse"`) {
		t.Fatalf("Factory Droid hooks = %q, %v", hooks, err)
	}
	stdout, stderr, exit = runClineCLI(
		t,
		binary,
		workspace,
		environment,
		"status",
		"--agent", "droid",
	)
	if exit != 0 || stderr != "" || !strings.Contains(stdout, "installed") {
		t.Fatalf("Factory Droid CLI status = %d, %q, %q", exit, stdout, stderr)
	}
	stdout, stderr, exit = runClineCLI(
		t,
		binary,
		workspace,
		environment,
		"uninstall",
		"--agent", "droid",
		"--agent-id", "agent-1",
	)
	if exit != 0 || stderr != "" || !strings.Contains(stdout, "Removed agent directory") {
		t.Fatalf("Factory Droid CLI uninstall = %d, %q, %q", exit, stdout, stderr)
	}
	for _, path := range []string{agentDir, hooksPath} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("managed path remains at %s: %v", path, err)
		}
	}
}
