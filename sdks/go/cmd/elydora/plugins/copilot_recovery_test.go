package plugins

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCopilotInstallRejectsInvalidUserSourcesBeforeRuntimeWrites(t *testing.T) {
	tests := map[string]string{
		"malformed JSON":      "{",
		"array root":          "[]\n",
		"missing version":     `{"hooks":{}}`,
		"unsupported version": `{"version":2,"hooks":{}}`,
		"null hooks":          `{"version":1,"hooks":null}`,
		"null event":          `{"version":1,"hooks":{"preToolUse":null}}`,
		"null handler":        `{"version":1,"hooks":{"preToolUse":[null]}}`,
		"invalid disable flag": `{"version":1,"disableAllHooks":"false",` +
			`"hooks":{}}`,
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			fixture := prepareCopilotFixture(t, copilotFixtureOptions{
				userRaw: copilotString(source),
			})
			if err := fixture.plugin.Install(fixture.config); err == nil {
				t.Fatal("install accepted an invalid GitHub Copilot user source")
			}
			assertCopilotFileEquals(t, fixture.configPath, source)
			assertCopilotRuntimeAbsent(t, fixture)
		})
	}
}

func TestCopilotInstallRejectsDisabledUserSource(t *testing.T) {
	source := `{"version":1,"disableAllHooks":true,"hooks":{}}`
	fixture := prepareCopilotFixture(t, copilotFixtureOptions{
		userRaw: copilotString(source),
	})

	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "disableAllHooks") {
		t.Fatalf("install error = %v", err)
	}
	assertCopilotFileEquals(t, fixture.configPath, source)
	assertCopilotRuntimeAbsent(t, fixture)
}

func TestCopilotInstallRejectsInvalidProjectSourceBeforeWrites(t *testing.T) {
	source := `{"version":1,"hooks":{"preToolUse":[null]}}`
	fixture := prepareCopilotFixture(t, copilotFixtureOptions{
		legacyRaw: copilotString(source),
	})

	if err := fixture.plugin.Install(fixture.config); err == nil {
		t.Fatal("install accepted an invalid GitHub Copilot project source")
	}
	assertCopilotFileEquals(t, fixture.legacyPath, source)
	if _, err := os.Stat(fixture.configPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("user hook source exists after rejection: %v", err)
	}
	assertCopilotRuntimeAbsent(t, fixture)
}

func TestCopilotInstallGeneratesManagedGuardAndRejectsUnmanagedPath(t *testing.T) {
	fixture := prepareCopilotFixture(t, copilotFixtureOptions{})
	installCopilotFixture(t, fixture)
	expected := generateGuardScript(copilotAgentKey, copilotTestAgentID, "", false, "")
	actual, err := os.ReadFile(fixture.guardPath)
	if err != nil || string(actual) != expected {
		t.Fatalf("generated guard = %q, %v", actual, err)
	}

	fixture = prepareCopilotFixture(t, copilotFixtureOptions{})
	fixture.config.GuardScriptPath = filepath.Join(fixture.homeDir, "unmanaged-guard.js")
	if err := fixture.plugin.Install(fixture.config); err == nil {
		t.Fatal("install accepted an unmanaged guard runtime")
	}
	assertCopilotRuntimeAbsent(t, fixture)
}

func TestCopilotInstallRejectsSymlinkFiles(t *testing.T) {
	t.Run("guard runtime", func(t *testing.T) {
		fixture := prepareCopilotFixture(t, copilotFixtureOptions{})
		target := filepath.Join(fixture.homeDir, "guard-target.js")
		if err := os.WriteFile(target, []byte("process.exit(2);\n"), 0700); err != nil {
			t.Fatalf("write guard target: %v", err)
		}
		if err := os.MkdirAll(fixture.agentDir, 0700); err != nil {
			t.Fatalf("create agent runtime directory: %v", err)
		}
		if err := os.Symlink(target, fixture.guardPath); err != nil {
			t.Skipf("create guard symlink: %v", err)
		}
		if err := fixture.plugin.Install(fixture.config); err == nil {
			t.Fatal("install accepted a symlink guard runtime")
		}
		for _, path := range []string{
			fixture.runtimeConfig, fixture.privateKey, fixture.hookPath,
		} {
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("runtime file exists at %s: %v", path, err)
			}
		}
	})
	t.Run("user source", func(t *testing.T) {
		fixture := prepareCopilotFixture(t, copilotFixtureOptions{})
		target := filepath.Join(fixture.homeDir, "copilot-target.json")
		writeCopilotObject(t, target, map[string]any{"version": float64(1), "hooks": map[string]any{}})
		if err := os.MkdirAll(filepath.Dir(fixture.configPath), 0700); err != nil {
			t.Fatalf("create user hooks directory: %v", err)
		}
		if err := os.Symlink(target, fixture.configPath); err != nil {
			t.Skipf("create source symlink: %v", err)
		}
		if err := fixture.plugin.Install(fixture.config); err == nil {
			t.Fatal("install accepted a symlink hook source")
		}
		assertCopilotRuntimeAbsent(t, fixture)
	})
}

func TestCopilotInstallRequiresNodeBeforeWrites(t *testing.T) {
	fixture := prepareCopilotFixture(t, copilotFixtureOptions{})
	t.Setenv("PATH", "")

	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "Node.js") {
		t.Fatalf("install error = %v", err)
	}
	assertCopilotRuntimeAbsent(t, fixture)
}

func TestCopilotStatusReportsMalformedRuntimeConfig(t *testing.T) {
	fixture := prepareCopilotFixture(t, copilotFixtureOptions{})
	installCopilotFixture(t, fixture)
	if err := os.WriteFile(fixture.runtimeConfig, []byte("{"), 0600); err != nil {
		t.Fatalf("write malformed runtime config: %v", err)
	}

	status, err := fixture.plugin.Status()
	if err == nil || status.Installed {
		t.Fatalf("Copilot status = %#v, %v", status, err)
	}
}

func TestCopilotStatusRejectsSymlinkRuntimeFiles(t *testing.T) {
	for _, name := range []string{"config", "guard", "audit"} {
		t.Run(name, func(t *testing.T) {
			fixture := prepareCopilotFixture(t, copilotFixtureOptions{})
			installCopilotFixture(t, fixture)
			path := map[string]string{
				"config": fixture.runtimeConfig,
				"guard":  fixture.guardPath,
				"audit":  fixture.hookPath,
			}[name]
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s runtime: %v", name, err)
			}
			target := filepath.Join(fixture.homeDir, name+"-runtime-target")
			if err := os.WriteFile(target, content, 0600); err != nil {
				t.Fatalf("write %s runtime target: %v", name, err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatalf("remove %s runtime: %v", name, err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Skipf("create %s runtime symlink: %v", name, err)
			}

			status, err := fixture.plugin.Status()
			if err == nil || status.Installed {
				t.Fatalf("Copilot status = %#v, %v", status, err)
			}
		})
	}
}

func TestCopilotInstallRollsBackEveryFileOnCommitFailure(t *testing.T) {
	fixture := prepareCopilotFixture(t, copilotFixtureOptions{
		userRaw: copilotJSON(map[string]any{
			"version": float64(1),
			"hooks": map[string]any{
				"notification": []any{map[string]any{"type": "command", "command": "keep-user"}},
			},
		}),
	})
	writeCopilotObject(t, fixture.legacyPath, legacyCopilotConfig(fixture, map[string]any{
		"notification": []any{map[string]any{"type": "command", "command": "keep-project"}},
	}))
	userBefore, err := os.ReadFile(fixture.configPath)
	if err != nil {
		t.Fatalf("read user source: %v", err)
	}
	projectBefore, err := os.ReadFile(fixture.legacyPath)
	if err != nil {
		t.Fatalf("read project source: %v", err)
	}
	commits := 0
	failed := false
	fixture.plugin.rename = func(source, destination string) error {
		if !failed && strings.HasSuffix(source, ".tmp") {
			commits++
			if commits == 5 {
				failed = true
				return errors.New("injected Copilot commit failure")
			}
		}
		return os.Rename(source, destination)
	}

	err = fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "injected Copilot commit failure") {
		t.Fatalf("install error = %v", err)
	}
	assertCopilotFileBytes(t, fixture.configPath, userBefore)
	assertCopilotFileBytes(t, fixture.legacyPath, projectBefore)
	assertCopilotRuntimeAbsent(t, fixture)
	assertNoCopilotTransactionArtifacts(t, fixture.homeDir, fixture.projectDir)
}

func TestCopilotInstallDetectsConcurrentSourceChangeAndRollsBack(t *testing.T) {
	fixture := prepareCopilotFixture(t, copilotFixtureOptions{
		userRaw: copilotJSON(map[string]any{
			"version": float64(1),
			"hooks":   map[string]any{},
		}),
	})
	concurrent := []byte(`{"version":1,"hooks":{"notification":[{"type":"command","command":"concurrent"}]}}` + "\n")
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
	assertCopilotFileBytes(t, fixture.configPath, concurrent)
	assertCopilotRuntimeAbsent(t, fixture)
	assertNoCopilotTransactionArtifacts(t, fixture.homeDir, fixture.projectDir)
}

func assertCopilotRuntimeAbsent(t *testing.T, fixture *copilotFixture) {
	t.Helper()
	for _, path := range []string{
		fixture.runtimeConfig, fixture.privateKey, fixture.guardPath, fixture.hookPath,
	} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("runtime file exists at %s: %v", path, err)
		}
	}
}

func assertCopilotFileEquals(t *testing.T, path, expected string) {
	t.Helper()
	assertCopilotFileBytes(t, path, []byte(expected))
}

func assertCopilotFileBytes(t *testing.T, path string, expected []byte) {
	t.Helper()
	actual, err := os.ReadFile(path)
	if err != nil || string(actual) != string(expected) {
		t.Fatalf("file %s = %q, %v; want %q", path, actual, err, expected)
	}
}

func assertNoCopilotTransactionArtifacts(t *testing.T, roots ...string) {
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
