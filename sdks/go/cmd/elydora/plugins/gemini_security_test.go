package plugins

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeminiRejectsLinkedSettingsBeforeRuntimeWrites(t *testing.T) {
	fixture := prepareGeminiFixture(t, geminiFixtureOptions{})
	target := filepath.Join(filepath.Dir(fixture.homeDir), "settings-target.json")
	source := []byte("{\"owner\":\"protected\"}\n")
	if err := os.MkdirAll(filepath.Dir(fixture.settingsPath), 0700); err != nil {
		t.Fatalf("create Gemini settings directory: %v", err)
	}
	if err := os.WriteFile(target, source, 0600); err != nil {
		t.Fatalf("write settings target: %v", err)
	}
	geminiSymlinkOrSkip(t, target, fixture.settingsPath)
	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "not a physical file") {
		t.Fatalf("install error = %v", err)
	}
	current, readErr := os.ReadFile(target)
	if readErr != nil || string(current) != string(source) {
		t.Fatalf("linked target changed: %q, %v", current, readErr)
	}
	assertNoGeminiRuntimeWrites(t, fixture)
}

func TestGeminiRejectsLinkedSettingsAndRuntimeDirectories(t *testing.T) {
	for _, kind := range []string{"settings", "runtime"} {
		t.Run(kind, func(t *testing.T) {
			fixture := prepareGeminiFixture(t, geminiFixtureOptions{})
			target := filepath.Join(filepath.Dir(fixture.homeDir), kind+"-target")
			if err := os.MkdirAll(target, 0700); err != nil {
				t.Fatalf("create symlink target: %v", err)
			}
			var linkPath string
			if kind == "settings" {
				linkPath = filepath.Dir(fixture.settingsPath)
				if err := os.MkdirAll(filepath.Dir(linkPath), 0700); err != nil {
					t.Fatalf("create settings parent: %v", err)
				}
			} else {
				linkPath = filepath.Join(fixture.homeDir, ".elydora")
				if err := os.MkdirAll(filepath.Dir(linkPath), 0700); err != nil {
					t.Fatalf("create runtime parent: %v", err)
				}
			}
			geminiSymlinkOrSkip(t, target, linkPath)
			err := fixture.plugin.Install(fixture.config)
			if err == nil || !strings.Contains(err.Error(), "physical directory") {
				t.Fatalf("install error = %v", err)
			}
			if kind == "runtime" {
				if _, err := os.Lstat(fixture.settingsPath); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("settings exist after runtime rejection: %v", err)
				}
			} else {
				assertNoGeminiRuntimeWrites(t, fixture)
			}
		})
	}
}

func TestGeminiRejectsLinkedRuntimeArtifacts(t *testing.T) {
	for _, name := range []string{
		"config.json",
		"private.key",
		"guard.js",
		"hook.js",
		"chain-state.json",
		"status-cache.json",
		"error.log",
	} {
		t.Run(name, func(t *testing.T) {
			fixture := prepareGeminiFixture(t, geminiFixtureOptions{})
			if err := fixture.plugin.Install(fixture.config); err != nil {
				t.Fatalf("install Gemini hooks: %v", err)
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
			geminiSymlinkOrSkip(t, target, path)
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

func TestGeminiRejectsOrphanedAndMismatchedRuntimeIdentity(t *testing.T) {
	for _, kind := range []string{"orphaned", "mismatched"} {
		t.Run(kind, func(t *testing.T) {
			fixture := prepareGeminiFixture(t, geminiFixtureOptions{})
			if err := os.MkdirAll(fixture.agentDir, 0700); err != nil {
				t.Fatalf("create agent runtime: %v", err)
			}
			if kind == "orphaned" {
				if err := os.WriteFile(
					fixture.guardPath,
					[]byte("orphaned\n"),
					0700,
				); err != nil {
					t.Fatalf("write orphaned guard: %v", err)
				}
			} else {
				if err := os.WriteFile(
					fixture.runtimeConfig,
					[]byte(`{"agent_name":"gemini","agent_id":"another-agent"}`),
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
			if _, err := os.Lstat(fixture.settingsPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("settings exist after identity rejection: %v", err)
			}
		})
	}
}

func TestGeminiValidatesManagedRuntimeInputsBeforeWrites(t *testing.T) {
	for _, testCase := range []struct {
		name string
		edit func(*geminiFixture)
		want string
	}{
		{"agent name", func(f *geminiFixture) { f.config.AgentName = "codex" }, "requires agent name gemini"},
		{"organization", func(f *geminiFixture) { f.config.OrgID = "  " }, "organization ID is required"},
		{"key ID", func(f *geminiFixture) { f.config.KID = "\t" }, "key ID is required"},
		{"token", func(f *geminiFixture) { f.config.Token = "  " }, "token must contain"},
		{"private key", func(f *geminiFixture) { f.config.PrivateKey = "invalid" }, "canonical 32-byte"},
		{"base URL", func(f *geminiFixture) {
			f.config.BaseURL = "https://api.elydora.com/path?token=secret"
		}, "query parameters"},
		{"guard path", func(f *geminiFixture) {
			f.config.GuardScriptPath = filepath.Join(f.homeDir, "outside", "guard.js")
		}, "managed Elydora agent directory"},
		{"audit path", func(f *geminiFixture) {
			f.config.HookScript = filepath.Join(f.homeDir, "outside", "hook.js")
		}, "managed Elydora agent directory"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := prepareGeminiFixture(t, geminiFixtureOptions{})
			testCase.edit(fixture)
			err := fixture.plugin.Install(fixture.config)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("install error = %v, want %q", err, testCase.want)
			}
			if _, err := os.Lstat(fixture.settingsPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("settings exist after input rejection: %v", err)
			}
			assertNoGeminiRuntimeWrites(t, fixture)
		})
	}
}

func TestGeminiStatusSurfacesInvalidRuntimeMetadata(t *testing.T) {
	for _, testCase := range []struct{ name, source, want string }{
		{"malformed", "{ malformed", "parse Elydora runtime config"},
		{"unsupported", `{"org_id":"o","agent_id":"agent-1","kid":"k","base_url":"https://api.test","agent_name":"gemini","extra":true}`, "unsupported field"},
		{"identity", `{"org_id":"o","agent_id":"other","kid":"k","base_url":"https://api.test","agent_name":"gemini"}`, "identity does not match"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := prepareGeminiFixture(t, geminiFixtureOptions{})
			if err := fixture.plugin.Install(fixture.config); err != nil {
				t.Fatalf("install Gemini hooks: %v", err)
			}
			if err := os.WriteFile(
				fixture.runtimeConfig,
				[]byte(testCase.source),
				0600,
			); err != nil {
				t.Fatalf("replace runtime config: %v", err)
			}
			_, err := fixture.plugin.Status()
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("status error = %v, want %q", err, testCase.want)
			}
		})
	}
}

func TestGeminiStatusRequiresEveryRuntimeFile(t *testing.T) {
	for _, name := range []string{"config.json", "private.key", "guard.js", "hook.js"} {
		t.Run("missing "+name, func(t *testing.T) {
			fixture := prepareGeminiFixture(t, geminiFixtureOptions{})
			if err := fixture.plugin.Install(fixture.config); err != nil {
				t.Fatalf("install Gemini hooks: %v", err)
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
}

func TestGeminiOwnershipRequiresExactHandlerFields(t *testing.T) {
	fixture := prepareGeminiFixture(t, geminiFixtureOptions{})
	guard := legacyGeminiCommand(fixture.guardPath)
	audit := legacyGeminiCommand(fixture.hookPath)
	writeGeminiTestObject(t, fixture.settingsPath, map[string]any{
		"hooks": map[string]any{
			"BeforeTool": []any{map[string]any{
				"label": "shared",
				"hooks": []any{map[string]any{
					"type": "command", "command": guard,
				}},
			}},
			"AfterTool": []any{map[string]any{
				"hooks": []any{map[string]any{
					"type": "command", "command": audit, "label": "user",
				}},
			}},
		},
	})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Gemini hooks: %v", err)
	}
	if err := fixture.plugin.Uninstall(geminiTestAgentID); err != nil {
		t.Fatalf("uninstall Gemini hooks: %v", err)
	}
	remaining := readGeminiTestObject(t, fixture.settingsPath)
	hooks := requireObject(t, remaining["hooks"])
	before := requireArray(t, hooks["BeforeTool"])
	after := requireArray(t, hooks["AfterTool"])
	if len(before) != 1 || requireObject(t, before[0])["label"] != "shared" ||
		len(requireArray(t, requireObject(t, before[0])["hooks"])) != 0 ||
		len(after) != 1 || len(requireArray(t, requireObject(t, after[0])["hooks"])) != 1 {
		t.Fatalf("ownership lookalikes changed: %#v", hooks)
	}
}
