package plugins

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCopilotPrepareRejectsSameContentStaleHookSnapshot(t *testing.T) {
	fixture := prepareCopilotFixture(t, copilotFixtureOptions{
		userRaw: copilotJSON(map[string]any{
			"version": float64(1),
			"hooks":   map[string]any{"notification": []any{}},
		}),
	})
	sources, _, err := readCopilotSources()
	if err != nil {
		t.Fatalf("read GitHub Copilot sources: %v", err)
	}
	paths, nodePath, err := preflightCopilotInstallation(fixture.config, sources)
	if err != nil {
		t.Fatalf("preflight GitHub Copilot installation: %v", err)
	}
	rendered, err := renderCopilotInstallation(
		sources,
		paths.guardPath,
		paths.auditPath,
		nodePath,
	)
	if err != nil {
		t.Fatalf("render GitHub Copilot installation: %v", err)
	}
	original, err := os.ReadFile(fixture.configPath)
	if err != nil {
		t.Fatalf("read user hooks: %v", err)
	}
	replacement := fixture.configPath + ".replacement"
	if err := os.WriteFile(replacement, original, 0600); err != nil {
		t.Fatalf("write replacement hooks: %v", err)
	}
	if err := os.Rename(replacement, fixture.configPath); err != nil {
		t.Fatalf("replace user hooks: %v", err)
	}

	_, err = prepareCopilotInstallation(fixture.config, sources, rendered)
	if err == nil || !strings.Contains(err.Error(), "changed before update") {
		t.Fatalf("prepare error = %v", err)
	}
	assertCopilotRuntimeAbsent(t, fixture)
}

func TestCopilotInstallRollsBackWhenEffectiveSettingsChange(t *testing.T) {
	fixture := prepareCopilotFixture(t, copilotFixtureOptions{
		userSettingsRaw: copilotJSON(map[string]any{"disableAllHooks": false}),
	})
	commits := 0
	mutated := false
	fixture.plugin.rename = func(source, destination string) error {
		if err := os.Rename(source, destination); err != nil {
			return err
		}
		if !mutated && strings.HasSuffix(source, ".tmp") {
			commits++
			if commits == 3 {
				mutated = true
				writeCopilotObject(t, fixture.userSettings, map[string]any{
					"disableAllHooks": true,
				})
			}
		}
		return nil
	}

	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "user settings changed") {
		t.Fatalf("install error = %v", err)
	}
	settings := readCopilotObject(t, fixture.userSettings)
	if settings["disableAllHooks"] != true {
		t.Fatalf("concurrent settings = %#v", settings)
	}
	if _, err := os.Lstat(fixture.configPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("hook source exists after rollback: %v", err)
	}
	assertCopilotRuntimeAbsent(t, fixture)
	assertNoCopilotTransactionArtifacts(t, fixture.homeDir, fixture.projectDir)
}

func TestCopilotIdempotentCommitChecksSameContentSettingsIdentity(t *testing.T) {
	fixture := prepareCopilotFixture(t, copilotFixtureOptions{
		userSettingsRaw: copilotJSON(map[string]any{"disableAllHooks": false}),
	})
	installCopilotFixture(t, fixture)
	sources, _, err := readCopilotSources()
	if err != nil {
		t.Fatalf("read GitHub Copilot sources: %v", err)
	}
	paths, nodePath, err := preflightCopilotInstallation(fixture.config, sources)
	if err != nil {
		t.Fatalf("preflight GitHub Copilot installation: %v", err)
	}
	rendered, err := renderCopilotInstallation(
		sources,
		paths.guardPath,
		paths.auditPath,
		nodePath,
	)
	if err != nil {
		t.Fatalf("render GitHub Copilot installation: %v", err)
	}
	prepared, err := prepareCopilotInstallation(fixture.config, sources, rendered)
	if err != nil {
		t.Fatalf("prepare GitHub Copilot installation: %v", err)
	}
	original, err := os.ReadFile(fixture.userSettings)
	if err != nil {
		t.Fatalf("read user settings: %v", err)
	}
	replacement := fixture.userSettings + ".replacement"
	if err := os.WriteFile(replacement, original, 0600); err != nil {
		t.Fatalf("write replacement settings: %v", err)
	}
	if err := os.Rename(replacement, fixture.userSettings); err != nil {
		t.Fatalf("replace user settings: %v", err)
	}

	err = commitCopilotInstallation(prepared, fixture.plugin.rename)
	if err == nil || !strings.Contains(err.Error(), "user settings changed") {
		t.Fatalf("commit error = %v", err)
	}
}

func TestCopilotInstallPreservesOrphanedRuntimeWithoutIdentity(t *testing.T) {
	fixture := prepareCopilotFixture(t, copilotFixtureOptions{})
	if err := os.MkdirAll(fixture.agentDir, 0700); err != nil {
		t.Fatalf("create agent runtime directory: %v", err)
	}
	if err := os.WriteFile(fixture.hookPath, []byte("orphaned\n"), 0700); err != nil {
		t.Fatalf("write orphaned runtime: %v", err)
	}

	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "cannot be verified") {
		t.Fatalf("install error = %v", err)
	}
	current, readErr := os.ReadFile(fixture.hookPath)
	if readErr != nil || string(current) != "orphaned\n" {
		t.Fatalf("orphaned runtime = %q, %v", current, readErr)
	}
	for _, path := range []string{
		fixture.configPath, fixture.guardPath, fixture.runtimeConfig, fixture.privateKey,
	} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("unexpected file at %s: %v", path, err)
		}
	}
}

func TestCopilotInstallRejectsLinkedHomeAndRuntimeDirectories(t *testing.T) {
	for _, location := range []string{"home", "runtime"} {
		t.Run(location, func(t *testing.T) {
			fixture := prepareCopilotFixture(t, copilotFixtureOptions{})
			target := filepath.Join(filepath.Dir(fixture.homeDir), location+"-target")
			if err := os.MkdirAll(target, 0700); err != nil {
				t.Fatalf("create symlink target: %v", err)
			}
			linkPath := fixture.copilotHome
			if location == "runtime" {
				linkPath = fixture.agentDir
				if err := os.MkdirAll(filepath.Dir(linkPath), 0700); err != nil {
					t.Fatalf("create runtime root: %v", err)
				}
			}
			if err := os.Symlink(target, linkPath); err != nil {
				t.Skipf("create directory symlink: %v", err)
			}

			err := fixture.plugin.Install(fixture.config)
			if err == nil || !strings.Contains(err.Error(), "physical directory") {
				t.Fatalf("install error = %v", err)
			}
		})
	}
}

func TestCopilotStatusRequiresExactRuntimeSourcesAndPrivateKey(t *testing.T) {
	for _, name := range []string{"guard", "audit"} {
		t.Run(name, func(t *testing.T) {
			fixture := prepareCopilotFixture(t, copilotFixtureOptions{})
			installCopilotFixture(t, fixture)
			path := fixture.guardPath
			if name == "audit" {
				path = fixture.hookPath
			}
			if err := os.WriteFile(path, []byte("// tampered\n"), 0700); err != nil {
				t.Fatalf("tamper %s runtime: %v", name, err)
			}
			status, err := fixture.plugin.Status()
			if err != nil || status.Installed || status.HookScriptExists {
				t.Fatalf("tampered %s status = %#v, %v", name, status, err)
			}
		})
	}
	fixture := prepareCopilotFixture(t, copilotFixtureOptions{})
	installCopilotFixture(t, fixture)
	if err := os.WriteFile(fixture.privateKey, []byte("invalid"), 0600); err != nil {
		t.Fatalf("corrupt private key: %v", err)
	}
	if _, err := fixture.plugin.Status(); err == nil ||
		!strings.Contains(err.Error(), "canonical 32-byte") {
		t.Fatalf("invalid key status error = %v", err)
	}
}

func TestCopilotValidatesManagedRuntimeInputsBeforeWrites(t *testing.T) {
	for _, testCase := range []struct {
		name string
		edit func(*copilotFixture)
		want string
	}{
		{"agent name", func(f *copilotFixture) { f.config.AgentName = "codex" }, "requires agent name copilot"},
		{"organization", func(f *copilotFixture) { f.config.OrgID = "  " }, "organization ID is required"},
		{"key ID", func(f *copilotFixture) { f.config.KID = "\t" }, "key ID is required"},
		{"token", func(f *copilotFixture) { f.config.Token = "  " }, "token must contain"},
		{"private key", func(f *copilotFixture) { f.config.PrivateKey = "invalid" }, "canonical 32-byte"},
		{"base URL", func(f *copilotFixture) {
			f.config.BaseURL = "https://api.elydora.com/path?token=secret"
		}, "query parameters"},
		{"guard path", func(f *copilotFixture) {
			f.config.GuardScriptPath = filepath.Join(f.homeDir, "outside", "guard.js")
		}, "managed agent directory"},
		{"audit path", func(f *copilotFixture) {
			f.config.HookScript = filepath.Join(f.homeDir, "outside", "hook.js")
		}, "managed agent directory"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := prepareCopilotFixture(t, copilotFixtureOptions{})
			testCase.edit(fixture)
			err := fixture.plugin.Install(fixture.config)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("install error = %v, want %q", err, testCase.want)
			}
			assertCopilotRuntimeAbsent(t, fixture)
		})
	}
}
