package plugins

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCursorInstallRejectsSymlinkRuntimeTargets(t *testing.T) {
	for _, name := range []string{"config", "private key", "guard", "audit"} {
		t.Run(name, func(t *testing.T) {
			fixture := prepareCursorFixture(t, nil, false)
			path := map[string]string{
				"config": fixture.runtimeConfig, "private key": fixture.privateKey,
				"guard": fixture.guardPath, "audit": fixture.hookPath,
			}[name]
			target := filepath.Join(fixture.homeDir, strings.ReplaceAll(name, " ", "-")+"-target")
			if err := os.WriteFile(target, []byte("target\n"), 0600); err != nil {
				t.Fatalf("write %s target: %v", name, err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Skipf("create %s symlink: %v", name, err)
			}
			if err := fixture.plugin.Install(fixture.config); err == nil ||
				!strings.Contains(err.Error(), "physical file") {
				t.Fatalf("symlink %s install error = %v", name, err)
			}
			if _, err := os.Stat(fixture.configPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("Cursor config exists after rejection: %v", err)
			}
			content, err := os.ReadFile(target)
			if err != nil || string(content) != "target\n" {
				t.Fatalf("%s target = %q, %v", name, content, err)
			}
		})
	}
}

func TestCursorInstallRejectsSymlinkUserConfig(t *testing.T) {
	fixture := prepareCursorFixture(t, nil, false)
	target := filepath.Join(fixture.homeDir, "cursor-hooks-target.json")
	source := `{"version":1,"hooks":{}}` + "\n"
	if err := os.WriteFile(target, []byte(source), 0600); err != nil {
		t.Fatalf("write Cursor config target: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(fixture.configPath), 0700); err != nil {
		t.Fatalf("create Cursor config directory: %v", err)
	}
	if err := os.Symlink(target, fixture.configPath); err != nil {
		t.Skipf("create Cursor config symlink: %v", err)
	}

	if err := fixture.plugin.Install(fixture.config); err == nil ||
		!strings.Contains(err.Error(), "physical file") {
		t.Fatalf("symlink config install error = %v", err)
	}
	content, err := os.ReadFile(target)
	if err != nil || string(content) != source {
		t.Fatalf("Cursor config target = %q, %v", content, err)
	}
}

func TestCursorInstallRejectsSymlinkUserConfigDirectory(t *testing.T) {
	fixture := prepareCursorFixture(t, nil, false)
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Dir(fixture.configPath)); err != nil {
		t.Skipf("create Cursor config directory symlink: %v", err)
	}

	if err := fixture.plugin.Install(fixture.config); err == nil ||
		!strings.Contains(err.Error(), "physical directory") {
		t.Fatalf("symlink config directory install error = %v", err)
	}
	assertCursorRuntimeAbsent(t, fixture)
	entries, err := os.ReadDir(target)
	if err != nil || len(entries) != 0 {
		t.Fatalf("Cursor config target entries = %#v, %v", entries, err)
	}
}

func TestCursorInstallRejectsRuntimeIdentityBeforeWrites(t *testing.T) {
	fixture := prepareCursorFixture(t, nil, true)
	writeCursorObject(t, fixture.runtimeConfig, map[string]any{
		"agent_name": "codex", "agent_id": cursorTestAgentID,
	})
	guardBefore, err := os.ReadFile(fixture.guardPath)
	if err != nil {
		t.Fatalf("read guard before install: %v", err)
	}

	err = fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "identity does not match") {
		t.Fatalf("runtime identity install error = %v", err)
	}
	guardAfter, readErr := os.ReadFile(fixture.guardPath)
	if readErr != nil || string(guardAfter) != string(guardBefore) {
		t.Fatalf("guard changed to %q, %v", guardAfter, readErr)
	}
	if _, err := os.Stat(fixture.configPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Cursor config exists after identity rejection: %v", err)
	}
}

func TestCursorInstallRejectsOrphanRuntimeBeforeWrites(t *testing.T) {
	fixture := prepareCursorFixture(t, nil, true)
	guardBefore, err := os.ReadFile(fixture.guardPath)
	if err != nil {
		t.Fatalf("read orphan guard: %v", err)
	}

	err = fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "identity cannot be verified") {
		t.Fatalf("orphan runtime install error = %v", err)
	}
	guardAfter, readErr := os.ReadFile(fixture.guardPath)
	if readErr != nil || string(guardAfter) != string(guardBefore) {
		t.Fatalf("orphan guard changed to %q, %v", guardAfter, readErr)
	}
	if _, err := os.Stat(fixture.configPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Cursor config exists after orphan rejection: %v", err)
	}
}

func TestCursorStatusRejectsSymlinkRuntimeFiles(t *testing.T) {
	for _, name := range []string{"config", "private key", "guard", "audit"} {
		t.Run(name, func(t *testing.T) {
			fixture := prepareCursorFixture(t, nil, false)
			installCursorFixture(t, fixture)
			path := map[string]string{
				"config": fixture.runtimeConfig, "private key": fixture.privateKey,
				"guard": fixture.guardPath, "audit": fixture.hookPath,
			}[name]
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s runtime: %v", name, err)
			}
			target := filepath.Join(fixture.homeDir, "status-"+strings.ReplaceAll(name, " ", "-"))
			if err := os.WriteFile(target, content, 0600); err != nil {
				t.Fatalf("write %s target: %v", name, err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatalf("remove %s runtime: %v", name, err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Skipf("create %s runtime symlink: %v", name, err)
			}

			status, err := fixture.plugin.Status()
			if err == nil || status.Installed {
				t.Fatalf("Cursor status = %#v, %v", status, err)
			}
		})
	}
}

func TestCursorInstallRequiresNodeBeforeWrites(t *testing.T) {
	fixture := prepareCursorFixture(t, nil, false)
	t.Setenv("PATH", "")

	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "Node.js") {
		t.Fatalf("install error = %v", err)
	}
	assertCursorRuntimeAbsent(t, fixture)
	if _, err := os.Stat(fixture.configPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Cursor config exists after Node.js rejection: %v", err)
	}
}

func TestCursorInstallCreatesRuntimeDirectoryTransactionally(t *testing.T) {
	fixture := prepareCursorFixture(t, nil, false)
	if err := os.Remove(fixture.agentDir); err != nil {
		t.Fatalf("remove empty agent directory: %v", err)
	}

	installCursorFixture(t, fixture)
	for _, path := range []string{
		fixture.runtimeConfig,
		fixture.privateKey,
		fixture.guardPath,
		fixture.hookPath,
	} {
		if exists, err := cursorPhysicalFileExists(path, "Cursor runtime"); err != nil || !exists {
			t.Fatalf("runtime file %s exists = %v, %v", path, exists, err)
		}
	}
}

func TestCursorInstallRollsBackEveryFileOnConfigCommitFailure(t *testing.T) {
	fixture := prepareCursorFixture(t, cursorJSON(map[string]any{
		"version": float64(1),
		"hooks":   map[string]any{"sessionStart": []any{map[string]any{"command": "keep"}}},
	}), false)
	before, err := os.ReadFile(fixture.configPath)
	if err != nil {
		t.Fatalf("read Cursor config: %v", err)
	}
	failed := false
	fixture.plugin.rename = func(source, destination string) error {
		if !failed && sameCursorPath(destination, fixture.configPath) {
			failed = true
			return errors.New("injected Cursor config commit failure")
		}
		return os.Rename(source, destination)
	}

	err = fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "injected Cursor config commit failure") {
		t.Fatalf("install error = %v", err)
	}
	assertCursorFileBytes(t, fixture.configPath, before)
	assertCursorRuntimeAbsent(t, fixture)
	assertNoCursorTransactionArtifacts(t, fixture.homeDir)
}

func TestCursorInstallDetectsConcurrentSourceChangeAndRollsBack(t *testing.T) {
	fixture := prepareCursorFixture(t, cursorJSON(map[string]any{
		"version": float64(1), "hooks": map[string]any{},
	}), false)
	concurrent := []byte(`{"version":1,"hooks":{"sessionStart":[{"command":"concurrent"}]}}` + "\n")
	mutated := false
	fixture.plugin.rename = func(source, destination string) error {
		if !mutated && strings.HasSuffix(source, ".tmp") {
			mutated = true
			if err := os.WriteFile(fixture.configPath, concurrent, 0600); err != nil {
				return err
			}
		}
		return os.Rename(source, destination)
	}

	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "changed during installation") {
		t.Fatalf("install error = %v", err)
	}
	assertCursorFileBytes(t, fixture.configPath, concurrent)
	assertCursorRuntimeAbsent(t, fixture)
	assertNoCursorTransactionArtifacts(t, fixture.homeDir)
}

func TestCursorAtomicConfigIsPrivateAndLeavesNoStagingFiles(t *testing.T) {
	fixture := prepareCursorFixture(t, nil, false)
	installCursorFixture(t, fixture)
	if runtime.GOOS != "windows" {
		info, err := os.Stat(fixture.configPath)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("Cursor config mode = %v, %v", info.Mode().Perm(), err)
		}
	}
	assertNoCursorTransactionArtifacts(t, fixture.homeDir)
}

func assertCursorRuntimeAbsent(t *testing.T, fixture *cursorFixture) {
	t.Helper()
	for _, path := range []string{
		fixture.runtimeConfig, fixture.privateKey, fixture.guardPath, fixture.hookPath,
	} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("runtime file exists at %s: %v", path, err)
		}
	}
}

func assertCursorFileBytes(t *testing.T, path string, expected []byte) {
	t.Helper()
	actual, err := os.ReadFile(path)
	if err != nil || string(actual) != string(expected) {
		t.Fatalf("file %s = %q, %v; want %q", path, actual, err, expected)
	}
}

func assertNoCursorTransactionArtifacts(t *testing.T, roots ...string) {
	t.Helper()
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if strings.HasSuffix(entry.Name(), ".tmp") || strings.HasSuffix(entry.Name(), ".rollback") {
				t.Errorf("transaction artifact remains at %s", path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk transaction root %s: %v", root, err)
		}
	}
}
