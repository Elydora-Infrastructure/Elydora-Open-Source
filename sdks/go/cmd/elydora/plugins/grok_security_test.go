package plugins

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGrokRejectsLinkedHookFileBeforeRuntimeWrites(t *testing.T) {
	fixture := prepareGrokFixture(t, grokFixtureOptions{})
	target := filepath.Join(filepath.Dir(fixture.homeDir), "hooks-target.json")
	source := []byte("{\"owner\":\"protected\"}\n")
	if err := os.MkdirAll(filepath.Dir(fixture.configPath), 0700); err != nil {
		t.Fatalf("create Grok hooks directory: %v", err)
	}
	if err := os.WriteFile(target, source, 0600); err != nil {
		t.Fatalf("write hook target: %v", err)
	}
	grokSymlinkOrSkip(t, target, fixture.configPath)
	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "not a physical file") {
		t.Fatalf("install error = %v", err)
	}
	current, readErr := os.ReadFile(target)
	if readErr != nil || string(current) != string(source) {
		t.Fatalf("linked target changed: %q, %v", current, readErr)
	}
	assertNoGrokRuntimeWrites(t, fixture)
}

func TestGrokRejectsLinkedHomeHooksAndRuntimeDirectories(t *testing.T) {
	for _, kind := range []string{"home", "hooks", "runtime"} {
		t.Run(kind, func(t *testing.T) {
			fixture := prepareGrokFixture(t, grokFixtureOptions{})
			target := filepath.Join(filepath.Dir(fixture.homeDir), kind+"-target")
			if err := os.MkdirAll(target, 0700); err != nil {
				t.Fatalf("create symlink target: %v", err)
			}
			var linkPath string
			switch kind {
			case "home":
				linkPath = fixture.grokHome
				if err := os.MkdirAll(filepath.Join(target, "hooks"), 0700); err != nil {
					t.Fatalf("create target hooks: %v", err)
				}
				if err := os.MkdirAll(filepath.Dir(linkPath), 0700); err != nil {
					t.Fatalf("create home parent: %v", err)
				}
			case "hooks":
				linkPath = filepath.Dir(fixture.configPath)
				if err := os.MkdirAll(fixture.grokHome, 0700); err != nil {
					t.Fatalf("create Grok home: %v", err)
				}
			case "runtime":
				linkPath = filepath.Join(fixture.homeDir, ".elydora")
				if err := os.MkdirAll(filepath.Dir(linkPath), 0700); err != nil {
					t.Fatalf("create runtime parent: %v", err)
				}
			}
			grokSymlinkOrSkip(t, target, linkPath)
			err := fixture.plugin.Install(fixture.config)
			if err == nil || !strings.Contains(err.Error(), "physical directory") {
				t.Fatalf("install error = %v", err)
			}
			if kind == "runtime" {
				if _, err := os.Lstat(fixture.configPath); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("hook config exists after runtime rejection: %v", err)
				}
			} else {
				assertNoGrokRuntimeWrites(t, fixture)
			}
		})
	}
}

func TestGrokRejectsLinkedRuntimeArtifacts(t *testing.T) {
	for _, name := range []string{
		"config.json", "private.key", "guard.js", "hook.js", "chain-state.json",
		"status-cache.json", "error.log",
	} {
		t.Run(name, func(t *testing.T) {
			fixture := prepareGrokFixture(t, grokFixtureOptions{})
			if err := fixture.plugin.Install(fixture.config); err != nil {
				t.Fatalf("install Grok hooks: %v", err)
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
			grokSymlinkOrSkip(t, target, path)
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

func TestGrokRejectsOrphanedAndMismatchedRuntimeIdentity(t *testing.T) {
	for _, kind := range []string{"orphaned", "mismatched"} {
		t.Run(kind, func(t *testing.T) {
			fixture := prepareGrokFixture(t, grokFixtureOptions{})
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
					[]byte(`{"agent_name":"grok","agent_id":"another-agent"}`),
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
				t.Fatalf("hook config exists after identity rejection: %v", err)
			}
		})
	}
}

func TestGrokValidatesManagedRuntimeInputsBeforeWrites(t *testing.T) {
	for _, testCase := range []struct {
		name string
		edit func(*grokFixture)
		want string
	}{
		{"agent name", func(f *grokFixture) { f.config.AgentName = "codex" }, "requires agent name grok"},
		{"organization", func(f *grokFixture) { f.config.OrgID = "  " }, "organization ID is required"},
		{"key ID", func(f *grokFixture) { f.config.KID = "\t" }, "key ID is required"},
		{"token", func(f *grokFixture) { f.config.Token = "  " }, "token must contain"},
		{"private key", func(f *grokFixture) { f.config.PrivateKey = "invalid" }, "canonical 32-byte"},
		{"base URL", func(f *grokFixture) {
			f.config.BaseURL = "https://api.elydora.com/path?token=secret"
		}, "query parameters"},
		{"guard path", func(f *grokFixture) {
			f.config.GuardScriptPath = filepath.Join(f.homeDir, "outside", "guard.js")
		}, "managed Elydora agent directory"},
		{"audit path", func(f *grokFixture) {
			f.config.HookScript = filepath.Join(f.homeDir, "outside", "hook.js")
		}, "managed Elydora agent directory"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := prepareGrokFixture(t, grokFixtureOptions{})
			testCase.edit(fixture)
			err := fixture.plugin.Install(fixture.config)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("install error = %v, want %q", err, testCase.want)
			}
			if _, err := os.Lstat(fixture.configPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("hook config exists after input rejection: %v", err)
			}
			assertNoGrokRuntimeWrites(t, fixture)
		})
	}
}

func TestGrokStatusSurfacesMalformedAndInvalidRuntimeMetadata(t *testing.T) {
	for _, testCase := range []struct{ name, source, want string }{
		{"malformed", "{ malformed", "parse Elydora runtime config"},
		{"unsupported", `{"org_id":"o","agent_id":"agent-1","kid":"k","base_url":"https://api.test","agent_name":"grok","extra":true}`, "unsupported field"},
		{"identity", `{"org_id":"o","agent_id":"other","kid":"k","base_url":"https://api.test","agent_name":"grok"}`, "identity does not match"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := prepareGrokFixture(t, grokFixtureOptions{})
			if err := fixture.plugin.Install(fixture.config); err != nil {
				t.Fatalf("install Grok hooks: %v", err)
			}
			if err := os.WriteFile(fixture.runtimeConfig, []byte(testCase.source), 0600); err != nil {
				t.Fatalf("replace runtime config: %v", err)
			}
			_, err := fixture.plugin.Status()
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("status error = %v, want %q", err, testCase.want)
			}
		})
	}
}

func TestGrokStatusRequiresEveryRuntimeFileAndCanonicalPrivateKey(t *testing.T) {
	for _, name := range []string{"config.json", "private.key", "guard.js", "hook.js"} {
		t.Run("missing "+name, func(t *testing.T) {
			fixture := prepareGrokFixture(t, grokFixtureOptions{})
			if err := fixture.plugin.Install(fixture.config); err != nil {
				t.Fatalf("install Grok hooks: %v", err)
			}
			if err := os.Remove(filepath.Join(fixture.agentDir, name)); err != nil {
				t.Fatalf("remove %s: %v", name, err)
			}
			status, err := fixture.plugin.Status()
			if err != nil || status.Installed || !status.HookConfigured ||
				status.HookScriptExists {
				t.Fatalf("missing %s status = %#v, %v", name, status, err)
			}
		})
	}
	fixture := prepareGrokFixture(t, grokFixtureOptions{})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Grok hooks: %v", err)
	}
	if err := os.WriteFile(fixture.privateKey, []byte("invalid"), 0600); err != nil {
		t.Fatalf("corrupt private key: %v", err)
	}
	if _, err := fixture.plugin.Status(); err == nil ||
		!strings.Contains(err.Error(), "canonical 32-byte") {
		t.Fatalf("invalid key status error = %v", err)
	}
}

func TestGrokOwnershipRequiresExactGroupAndHandlerFields(t *testing.T) {
	fixture := prepareGrokFixture(t, grokFixtureOptions{})
	guard := legacyGrokCommand(t, fixture.guardPath)
	audit := legacyGrokCommand(t, fixture.hookPath)
	existing := map[string]any{"hooks": map[string]any{
		"PreToolUse": []any{map[string]any{
			"label": "user", "hooks": []any{buildGrokHandler(guard)},
		}},
		"PostToolUse": []any{
			map[string]any{"hooks": []any{map[string]any{
				"type": "command", "command": audit, "timeout": float64(10),
				"label": "user",
			}}},
		},
	}}
	if err := os.MkdirAll(filepath.Dir(fixture.configPath), 0700); err != nil {
		t.Fatalf("create Grok hooks directory: %v", err)
	}
	writeGrokTestObject(t, fixture.configPath, existing)
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Grok hooks: %v", err)
	}
	if err := fixture.plugin.Uninstall(grokTestAgentID); err != nil {
		t.Fatalf("uninstall Grok hooks: %v", err)
	}
	remaining := readGrokTestObject(t, fixture.configPath)
	hooks := requireObject(t, remaining["hooks"])
	if len(requireArray(t, hooks["PreToolUse"])) != 1 ||
		len(requireArray(t, hooks["PostToolUse"])) != 1 {
		t.Fatalf("ownership lookalikes changed: %#v", hooks)
	}
	if _, exists := hooks["PostToolUseFailure"]; exists {
		t.Fatalf("managed failure hook remains: %#v", hooks)
	}
}
