package plugins

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func setRuntimeFilesTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func runtimeFilesTestConfig() InstallConfig {
	return InstallConfig{
		AgentName:  "opencode",
		OrgID:      "org-1",
		AgentID:    "agent-1",
		PrivateKey: "BwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwc",
		KID:        "key-1",
		Token:      "ely_test_token",
		BaseURL:    "https://api.elydora.test",
	}
}

func TestGenerateHookScriptCommitsProtectedRuntimeFiles(t *testing.T) {
	home := setRuntimeFilesTestHome(t)
	config := runtimeFilesTestConfig()
	agentDirectory := filepath.Join(home, ".elydora", config.AgentID)
	hookPath := filepath.Join(agentDirectory, "hook.js")

	if err := GenerateHookScript(hookPath, config); err != nil {
		t.Fatalf("generate hook script: %v", err)
	}

	keyPath := filepath.Join(agentDirectory, "private.key")
	key, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read private key: %v", err)
	}
	if string(key) != config.PrivateKey {
		t.Fatalf("private key content changed")
	}
	hook, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("read hook runtime: %v", err)
	}
	if strings.Contains(string(hook), config.PrivateKey) {
		t.Fatalf("hook runtime embeds the private key")
	}
	if !strings.Contains(string(hook), "readProtectedFile") {
		t.Fatalf("hook runtime is missing protected file validation")
	}
	if runtime.GOOS != "windows" {
		assertRuntimeFileMode(t, filepath.Join(agentDirectory, "config.json"), 0600)
		assertRuntimeFileMode(t, keyPath, 0600)
		assertRuntimeFileMode(t, hookPath, 0700)
	}
	assertNoRuntimeStagingFiles(t, agentDirectory)
}

func TestGenerateHookScriptSupportsMaxEscapedCredentialInput(t *testing.T) {
	home := setRuntimeFilesTestHome(t)
	config := runtimeFilesTestConfig()
	config.Token = strings.Repeat("\x01", 64*1024)
	agentDirectory := filepath.Join(home, ".elydora", config.AgentID)
	hookPath := filepath.Join(agentDirectory, "hook.js")

	if err := GenerateHookScript(hookPath, config); err != nil {
		t.Fatalf("generate hook script with maximum escaped token: %v", err)
	}
	info, err := os.Stat(filepath.Join(agentDirectory, "config.json"))
	if err != nil {
		t.Fatalf("stat runtime config: %v", err)
	}
	if info.Size() <= 64*1024 || info.Size() > maxRuntimeConfigBytes {
		t.Fatalf("encoded runtime config size = %d", info.Size())
	}
	hook, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("read hook runtime: %v", err)
	}
	if !strings.Contains(string(hook), "MAX_PROTECTED_CONFIG_BYTES") {
		t.Fatalf("hook runtime is missing the config size contract")
	}
}

func TestGenerateHookScriptRejectsOversizedRuntimeConfig(t *testing.T) {
	home := setRuntimeFilesTestHome(t)
	config := runtimeFilesTestConfig()
	config.Token = strings.Repeat("\x01", maxRuntimeConfigBytes)
	agentDirectory := filepath.Join(home, ".elydora", config.AgentID)

	err := GenerateHookScript(filepath.Join(agentDirectory, "hook.js"), config)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("generate oversized runtime config error = %v", err)
	}
	if _, statErr := os.Lstat(agentDirectory); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("oversized config created runtime state: %v", statErr)
	}
}

func TestGenerateHookScriptRollsBackRuntimeTransaction(t *testing.T) {
	home := setRuntimeFilesTestHome(t)
	config := runtimeFilesTestConfig()
	agentDirectory := filepath.Join(home, ".elydora", config.AgentID)
	if err := os.MkdirAll(agentDirectory, 0700); err != nil {
		t.Fatalf("create runtime directory: %v", err)
	}
	paths := []string{
		filepath.Join(agentDirectory, "config.json"),
		filepath.Join(agentDirectory, "private.key"),
		filepath.Join(agentDirectory, "hook.js"),
	}
	for _, path := range paths {
		if err := os.WriteFile(path, []byte("original:"+filepath.Base(path)), 0600); err != nil {
			t.Fatalf("write original runtime file: %v", err)
		}
	}

	commits := 0
	rename := func(source, destination string) error {
		if strings.HasSuffix(source, ".tmp") {
			commits++
			if commits == 2 {
				return errors.New("injected runtime commit failure")
			}
		}
		return os.Rename(source, destination)
	}
	err := generateHookScriptWithRename(paths[2], config, rename)
	if err == nil || !strings.Contains(err.Error(), "injected runtime commit failure") {
		t.Fatalf("generate hook script error = %v", err)
	}
	for _, path := range paths {
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("read restored runtime file: %v", readErr)
		}
		want := "original:" + filepath.Base(path)
		if string(raw) != want {
			t.Fatalf("restored %s = %q, want %q", path, raw, want)
		}
	}
	assertNoRuntimeStagingFiles(t, agentDirectory)
}

func TestGenerateHookScriptRejectsLinkedSecretTarget(t *testing.T) {
	home := setRuntimeFilesTestHome(t)
	config := runtimeFilesTestConfig()
	agentDirectory := filepath.Join(home, ".elydora", config.AgentID)
	if err := os.MkdirAll(agentDirectory, 0700); err != nil {
		t.Fatalf("create runtime directory: %v", err)
	}
	target := filepath.Join(t.TempDir(), "private.key")
	if err := os.WriteFile(target, []byte("external"), 0600); err != nil {
		t.Fatalf("write linked target: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(agentDirectory, "private.key")); err != nil {
		t.Skipf("file symbolic links unavailable: %v", err)
	}

	err := GenerateHookScript(filepath.Join(agentDirectory, "hook.js"), config)
	if err == nil || !strings.Contains(err.Error(), "physical file") {
		t.Fatalf("generate hook script error = %v", err)
	}
	raw, readErr := os.ReadFile(target)
	if readErr != nil || string(raw) != "external" {
		t.Fatalf("linked target changed: %q, %v", raw, readErr)
	}
}

func assertRuntimeFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat runtime file: %v", err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("mode for %s = %04o, want %04o", path, got, want)
	}
}

func assertNoRuntimeStagingFiles(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read runtime directory: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") &&
			(strings.HasSuffix(entry.Name(), ".tmp") || strings.HasSuffix(entry.Name(), ".rollback")) {
			t.Fatalf("staging file remains: %s", entry.Name())
		}
	}
}
