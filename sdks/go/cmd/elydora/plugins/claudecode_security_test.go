package plugins

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeRejectsLinkedSettingsBeforeRuntimeWrites(t *testing.T) {
	fixture := prepareClaudeFixture(t, claudeFixtureOptions{})
	target := filepath.Join(filepath.Dir(fixture.homeDir), "settings-target.json")
	source := []byte("{\"owner\":\"protected\"}\n")
	if err := os.MkdirAll(fixture.configDir, 0700); err != nil {
		t.Fatalf("create Claude config directory: %v", err)
	}
	if err := os.WriteFile(target, source, 0600); err != nil {
		t.Fatalf("write settings target: %v", err)
	}
	claudeSymlinkOrSkip(t, target, fixture.configPath)
	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "not a physical file") {
		t.Fatalf("install error = %v", err)
	}
	current, readErr := os.ReadFile(target)
	if readErr != nil || string(current) != string(source) {
		t.Fatalf("linked target changed: %q, %v", current, readErr)
	}
	assertNoClaudeRuntimeWrites(t, fixture)
}

func TestClaudeRejectsLinkedConfigAndRuntimeDirectories(t *testing.T) {
	for _, kind := range []string{"config", "runtime"} {
		t.Run(kind, func(t *testing.T) {
			fixture := prepareClaudeFixture(t, claudeFixtureOptions{})
			target := filepath.Join(filepath.Dir(fixture.homeDir), kind+"-target")
			if err := os.MkdirAll(target, 0700); err != nil {
				t.Fatalf("create symlink target: %v", err)
			}
			var linkPath string
			if kind == "config" {
				linkPath = fixture.configDir
			} else {
				linkPath = filepath.Join(fixture.homeDir, ".elydora")
			}
			if err := os.MkdirAll(filepath.Dir(linkPath), 0700); err != nil {
				t.Fatalf("create link parent: %v", err)
			}
			claudeSymlinkOrSkip(t, target, linkPath)
			err := fixture.plugin.Install(fixture.config)
			if err == nil || !strings.Contains(err.Error(), "physical directory") {
				t.Fatalf("install error = %v", err)
			}
			if kind == "config" {
				assertNoClaudeRuntimeWrites(t, fixture)
			} else if _, err := os.Lstat(fixture.configPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("settings exist after runtime rejection: %v", err)
			}
		})
	}
}

func TestClaudeRejectsLinkedRuntimeArtifacts(t *testing.T) {
	for _, name := range []string{
		"config.json", "private.key", "guard.js", "hook.js", "chain-state.json",
		"status-cache.json", "error.log",
	} {
		t.Run(name, func(t *testing.T) {
			fixture := prepareClaudeFixture(t, claudeFixtureOptions{})
			if err := fixture.plugin.Install(fixture.config); err != nil {
				t.Fatalf("install Claude hooks: %v", err)
			}
			path := filepath.Join(fixture.agentDir, name)
			target := filepath.Join(filepath.Dir(fixture.homeDir), "target-"+name)
			source := []byte("protected")
			if current, err := os.ReadFile(path); err == nil {
				source = current
			}
			if err := os.WriteFile(target, source, 0600); err != nil {
				t.Fatalf("write runtime target: %v", err)
			}
			_ = os.Remove(path)
			claudeSymlinkOrSkip(t, target, path)
			var err error
			if name == "private.key" {
				_, err = fixture.plugin.Status()
			} else {
				err = fixture.plugin.PreflightInstall(fixture.config)
			}
			if err == nil || !strings.Contains(err.Error(), "not a physical file") {
				t.Fatalf("linked %s error = %v", name, err)
			}
			current, readErr := os.ReadFile(target)
			if readErr != nil || string(current) != string(source) {
				t.Fatalf("linked target changed: %q, %v", current, readErr)
			}
		})
	}
}

func TestClaudeRejectsOrphanedAndMismatchedRuntimeIdentity(t *testing.T) {
	for _, kind := range []string{"orphaned", "mismatched"} {
		t.Run(kind, func(t *testing.T) {
			fixture := prepareClaudeFixture(t, claudeFixtureOptions{})
			if err := os.MkdirAll(fixture.agentDir, 0700); err != nil {
				t.Fatalf("create agent runtime: %v", err)
			}
			if kind == "orphaned" {
				if err := os.WriteFile(fixture.guardPath, []byte("orphaned\n"), 0700); err != nil {
					t.Fatalf("write orphaned guard: %v", err)
				}
			} else {
				if err := os.WriteFile(
					fixture.runtimeConfig,
					[]byte(`{"agent_name":"claudecode","agent_id":"another-agent"}`),
					0600,
				); err != nil {
					t.Fatalf("write mismatched config: %v", err)
				}
			}
			err := fixture.plugin.Install(fixture.config)
			want := "identity cannot be verified"
			if kind == "mismatched" {
				want = "identity does not match"
			}
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("install error = %v, want %q", err, want)
			}
			if _, err := os.Lstat(fixture.configPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("settings exist after identity rejection: %v", err)
			}
		})
	}
}

func TestClaudeValidatesManagedRuntimeInputsBeforeWrites(t *testing.T) {
	tests := []struct {
		name string
		edit func(*claudeFixture)
		want string
	}{
		{"agent name", func(f *claudeFixture) { f.config.AgentName = "codex" }, "requires agent name claudecode"},
		{"organization", func(f *claudeFixture) { f.config.OrgID = "  " }, "organization ID is required"},
		{"key ID", func(f *claudeFixture) { f.config.KID = "\t" }, "key ID is required"},
		{"token", func(f *claudeFixture) { f.config.Token = "  " }, "token must contain"},
		{"private key", func(f *claudeFixture) { f.config.PrivateKey = "invalid" }, "canonical 32-byte"},
		{"base URL", func(f *claudeFixture) {
			f.config.BaseURL = "https://api.elydora.com/path?token=secret"
		}, "query parameters"},
		{"guard path", func(f *claudeFixture) {
			f.config.GuardScriptPath = filepath.Join(f.homeDir, "outside", "guard.js")
		}, "managed Elydora agent directory"},
		{"audit path", func(f *claudeFixture) {
			f.config.HookScript = filepath.Join(f.homeDir, "outside", "hook.js")
		}, "managed Elydora agent directory"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := prepareClaudeFixture(t, claudeFixtureOptions{})
			testCase.edit(fixture)
			err := fixture.plugin.Install(fixture.config)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("install error = %v, want %q", err, testCase.want)
			}
			if _, err := os.Lstat(fixture.configPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("settings exist after input rejection: %v", err)
			}
			assertNoClaudeRuntimeWrites(t, fixture)
		})
	}
}

func TestClaudeStatusRejectsInvalidPrivateKey(t *testing.T) {
	fixture := prepareClaudeFixture(t, claudeFixtureOptions{})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Claude hooks: %v", err)
	}
	if err := os.WriteFile(fixture.privateKey, []byte("invalid"), 0600); err != nil {
		t.Fatalf("corrupt private key: %v", err)
	}
	if _, err := fixture.plugin.Status(); err == nil ||
		!strings.Contains(err.Error(), "canonical 32-byte") {
		t.Fatalf("invalid key status error = %v", err)
	}
}

func TestClaudeOwnershipRequiresExactGroupAndHandlerFields(t *testing.T) {
	fixture := prepareClaudeFixture(t, claudeFixtureOptions{})
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		t.Fatalf("resolve Node.js runtime: %v", err)
	}
	guard := buildClaudeGroup(nodePath, fixture.guardPath, claudeGuardStatusMessage)
	audit := buildClaudeGroup(nodePath, fixture.hookPath, claudeAuditStatusMessage)
	audit.handlers[0]["once"] = false
	writeClaudeTestObject(t, fixture.configPath, map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{map[string]any{
				"matcher": "Bash", "hooks": []any{guard.handlers[0]},
			}},
			"PostToolUse": []any{map[string]any{
				"hooks": []any{audit.handlers[0]},
			}},
		},
	})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Claude hooks: %v", err)
	}
	if err := fixture.plugin.Uninstall(claudeTestAgentID); err != nil {
		t.Fatalf("uninstall Claude hooks: %v", err)
	}
	hooks := requireObject(t, readClaudeTestObject(t, fixture.configPath)["hooks"])
	if len(requireArray(t, hooks["PreToolUse"])) != 1 ||
		len(requireArray(t, hooks["PostToolUse"])) != 1 {
		t.Fatalf("ownership lookalikes changed: %#v", hooks)
	}
	if _, exists := hooks["PostToolUseFailure"]; exists {
		t.Fatalf("managed failure hook remains: %#v", hooks)
	}
}
