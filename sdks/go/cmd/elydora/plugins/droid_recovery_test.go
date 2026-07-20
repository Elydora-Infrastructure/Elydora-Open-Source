package plugins

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDroidStatusSurfacesMalformedRuntimeIdentity(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*droidFixture)
		pattern string
	}{
		{
			"malformed config",
			func(fixture *droidFixture) {
				if err := os.WriteFile(fixture.runtimeConfig, []byte("{ malformed"), 0600); err != nil {
					t.Fatalf("corrupt runtime config: %v", err)
				}
			},
			"parse Elydora runtime config",
		},
		{
			"unsupported config field",
			func(fixture *droidFixture) {
				config := readDroidTestObject(t, fixture.runtimeConfig)
				config["future"] = true
				writeDroidTestObject(t, fixture.runtimeConfig, config)
			},
			"unsupported field",
		},
		{
			"noncanonical key",
			func(fixture *droidFixture) {
				if err := os.WriteFile(fixture.privateKey, []byte("invalid"), 0600); err != nil {
					t.Fatalf("corrupt private key: %v", err)
				}
			},
			"canonical 32-byte",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := prepareDroidFixture(t, droidFixtureOptions{})
			installDroidFixture(t, fixture)
			test.mutate(fixture)
			_, err := fixture.plugin.Status()
			if err == nil || !strings.Contains(err.Error(), test.pattern) {
				t.Fatalf("status error = %v, want %q", err, test.pattern)
			}
		})
	}
}

func TestDroidInstallRejectsMalformedSourcesBeforeWrites(t *testing.T) {
	tests := []struct {
		name     string
		root     *string
		legacy   *string
		settings *string
		local    *string
		pattern  string
	}{
		{"malformed root", droidString("{ malformed"), nil, nil, nil, "parse Factory Droid hooks"},
		{"non-object root", droidJSON([]any{}), nil, nil, nil, "JSON object"},
		{"duplicate event", droidString(`{"hooks":{"PreToolUse":[],"PreToolUse":[]}}`), nil, nil, nil, "duplicate"},
		{"null event", droidString(`{"hooks":{"PreToolUse":null}}`), nil, nil, nil, "must be an array"},
		{"null group", droidString(`{"hooks":{"PreToolUse":[null]}}`), nil, nil, nil, "must be an object"},
		{
			"invalid matcher",
			droidString(`{"hooks":{"PreToolUse":[{"matcher":"[","hooks":[]}]}}`),
			nil, nil, nil, "regular expression",
		},
		{
			"empty handler command",
			droidString(`{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":""}]}]}}`),
			nil, nil, nil, "non-empty",
		},
		{
			"nonpositive timeout",
			droidString(`{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"user","timeout":0}]}]}}`),
			nil, nil, nil, "positive",
		},
		{"malformed legacy", nil, droidString("{ malformed"), nil, nil, "legacy hooks"},
		{"null settings hooks", nil, nil, droidString(`{"hooks":null}`), nil, "JSON object"},
		{"invalid local flag", nil, nil, nil, droidString(`{"hooksDisabled":"yes"}`), "boolean"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := prepareDroidFixture(t, droidFixtureOptions{
				root:          test.root,
				legacy:        test.legacy,
				settings:      test.settings,
				localSettings: test.local,
			})
			originals := existingDroidSources(t, fixture)
			err := fixture.plugin.Install(fixture.config)
			if err == nil || !strings.Contains(err.Error(), test.pattern) {
				t.Fatalf("install error = %v, want substring %q", err, test.pattern)
			}
			for path, original := range originals {
				if readDroidTestFile(t, path) != original {
					t.Fatalf("failed install changed %s", path)
				}
			}
			for _, path := range []string{
				fixture.guardPath,
				fixture.hookPath,
				fixture.runtimeConfig,
				fixture.privateKey,
			} {
				requireMissingDroidFile(t, path)
			}
		})
	}
}

func TestDroidUnknownEventsAndExtensionFieldsRemainValid(t *testing.T) {
	fixture := prepareDroidFixture(t, droidFixtureOptions{root: droidJSON(map[string]any{
		"hooks": map[string]any{
			"FutureFactoryEvent": []any{map[string]any{
				"matcher":      ".*",
				"commandRegex": "^factory",
				"owner":        "user",
				"hooks": []any{map[string]any{
					"type":    "command",
					"command": "user-command",
					"timeout": 1,
					"future":  true,
				}},
			}},
		},
	})})
	installDroidFixture(t, fixture)
	hooks := droidCurrentHooks(t, fixture.configPath)
	future := requireDroidObject(t, requireDroidArray(t, hooks["FutureFactoryEvent"])[0])
	if future["owner"] != "user" {
		t.Fatalf("future event = %#v", future)
	}
}

func TestDroidInstallValidatesConfigurationBeforeWrites(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*droidFixture)
		pattern string
	}{
		{"missing organization", func(f *droidFixture) { f.config.OrgID = "" }, "organization ID is required"},
		{"blank key ID", func(f *droidFixture) { f.config.KID = " " }, "key ID is required"},
		{"invalid private key", func(f *droidFixture) { f.config.PrivateKey = "invalid" }, "canonical 32-byte"},
		{"invalid base URL", func(f *droidFixture) { f.config.BaseURL = "relative" }, "absolute HTTP or HTTPS"},
		{"invalid agent ID", func(f *droidFixture) { f.config.AgentID = "../agent" }, "single non-empty path segment"},
		{
			"guard outside managed directory",
			func(f *droidFixture) { f.config.GuardScriptPath = filepath.Join(f.homeDir, "guard.js") },
			"managed agent directory",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := prepareDroidFixture(t, droidFixtureOptions{})
			test.mutate(fixture)
			err := fixture.plugin.Install(fixture.config)
			if err == nil || !strings.Contains(err.Error(), test.pattern) {
				t.Fatalf("install error = %v, want substring %q", err, test.pattern)
			}
			requireMissingDroidFile(t, fixture.configPath)
			requireMissingDroidFile(t, fixture.runtimeConfig)
		})
	}
}

func TestDroidOrphanedRuntimeIsPreservedWithoutVerifiableIdentity(t *testing.T) {
	fixture := prepareDroidFixture(t, droidFixtureOptions{})
	if err := os.MkdirAll(fixture.agentDir, 0700); err != nil {
		t.Fatalf("create orphaned runtime: %v", err)
	}
	if err := os.WriteFile(fixture.guardPath, []byte("orphaned guard\n"), 0700); err != nil {
		t.Fatalf("write orphaned guard: %v", err)
	}
	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "identity cannot be verified") {
		t.Fatalf("orphaned runtime error = %v", err)
	}
	if readDroidTestFile(t, fixture.guardPath) != "orphaned guard\n" {
		t.Fatal("orphaned guard changed")
	}
	requireMissingDroidFile(t, fixture.configPath)
}

func TestDroidUninstallPreservesUsersAndExactOwnership(t *testing.T) {
	fixture := prepareDroidFixture(t, droidFixtureOptions{root: droidJSON(map[string]any{
		"hooks": map[string]any{"Notification": []any{}},
	})})
	installDroidFixture(t, fixture)
	root := readDroidTestObject(t, fixture.configPath)
	hooks := requireDroidObject(t, root["hooks"])
	group := droidManagedGroup(t, hooks["PreToolUse"], fixture.guardPath)
	command := droidManagedHandler(t, []any{group}, fixture.guardPath)["command"].(string)
	group["hooks"] = append(requireDroidArray(t, group["hooks"]), map[string]any{
		"type": "command", "command": "user-command",
	})
	group["owner"] = "user"
	hooks["PreToolUse"] = append(requireDroidArray(t, hooks["PreToolUse"]),
		map[string]any{
			"matcher": "*",
			"hooks": []any{map[string]any{
				"type":    "command",
				"command": strings.Replace(command, droidGuardScript, droidGuardScript+".backup", 1),
				"timeout": 10,
			}},
		},
		map[string]any{
			"matcher": "*",
			"hooks": []any{map[string]any{
				"type":    "command",
				"command": strings.Replace(command, droidTestAgentID, "agent-10", 1),
				"timeout": 10,
			}},
		},
	)
	writeDroidTestObject(t, fixture.configPath, root)
	agentID := droidTestAgentID
	if runtime.GOOS == "windows" {
		agentID = strings.ToUpper(agentID)
	}
	if err := fixture.plugin.Uninstall(agentID); err != nil {
		t.Fatalf("uninstall Factory Droid hooks: %v", err)
	}
	remaining := readDroidTestFile(t, fixture.configPath)
	for _, marker := range []string{"user-command", droidGuardScript + ".backup", "agent-10"} {
		if !strings.Contains(remaining, marker) {
			t.Fatalf("uninstall removed %q", marker)
		}
	}
	currentHooks := droidCurrentHooks(t, fixture.configPath)
	if _, exists := currentHooks["PostToolUse"]; exists {
		t.Fatalf("managed PostToolUse remains: %#v", currentHooks)
	}
}

func TestDroidUninstallRemovesOwnedEmptyFileAndKeepsAbsence(t *testing.T) {
	fixture := prepareDroidFixture(t, droidFixtureOptions{})
	installDroidFixture(t, fixture)
	if err := fixture.plugin.Uninstall(droidTestAgentID); err != nil {
		t.Fatalf("uninstall Factory Droid hooks: %v", err)
	}
	requireMissingDroidFile(t, fixture.configPath)

	empty := prepareDroidFixture(t, droidFixtureOptions{})
	if err := empty.plugin.Uninstall(droidTestAgentID); err != nil {
		t.Fatalf("uninstall absent hooks: %v", err)
	}
	requireMissingDroidFile(t, empty.configPath)
}

func TestDroidInstallRejectsLinkedPathsBeforeWrites(t *testing.T) {
	for _, kind := range []string{"factory", "hook", "runtime"} {
		t.Run(kind, func(t *testing.T) {
			fixture := prepareDroidFixture(t, droidFixtureOptions{})
			target := filepath.Join(t.TempDir(), kind+"-target")
			if err := os.MkdirAll(target, 0755); err != nil {
				t.Fatalf("create symlink target: %v", err)
			}
			var linkErr error
			switch kind {
			case "factory":
				linkErr = os.MkdirAll(fixture.homeDir, 0755)
				if linkErr == nil {
					linkErr = os.Symlink(target, fixture.factoryDir)
				}
			case "hook":
				if err := os.MkdirAll(fixture.factoryDir, 0755); err != nil {
					t.Fatalf("create Factory directory: %v", err)
				}
				targetFile := filepath.Join(target, "hooks.json")
				writeDroidTestObject(t, targetFile, map[string]any{"hooks": map[string]any{}})
				linkErr = os.Symlink(targetFile, fixture.configPath)
			case "runtime":
				linkErr = os.MkdirAll(fixture.homeDir, 0755)
				if linkErr == nil {
					linkErr = os.Symlink(target, filepath.Join(fixture.homeDir, ".elydora"))
				}
			}
			if linkErr != nil {
				t.Skipf("symbolic links unavailable: %v", linkErr)
			}
			err := fixture.plugin.Install(fixture.config)
			if err == nil || !strings.Contains(err.Error(), "physical") {
				t.Fatalf("linked-path error = %v", err)
			}
			requireMissingDroidFile(t, fixture.runtimeConfig)
		})
	}
}

func existingDroidSources(t *testing.T, fixture *droidFixture) map[string]string {
	t.Helper()
	result := map[string]string{}
	for _, path := range []string{
		fixture.configPath,
		fixture.legacyPath,
		fixture.settingsPath,
		fixture.localSettingsPath,
	} {
		if raw, err := os.ReadFile(path); err == nil {
			result[path] = string(raw)
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read source %s: %v", path, err)
		}
	}
	return result
}
