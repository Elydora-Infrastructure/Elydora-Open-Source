package plugins

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDroidUninstallPreservesMixedGroupsAndLookalikes(t *testing.T) {
	fixture := prepareDroidFixture(t, droidFixtureOptions{})
	installDroidFixture(t, fixture)
	root := readDroidTestObject(t, fixture.configPath)
	group := droidManagedGroup(t, root["PreToolUse"], fixture.guardPath)
	command := droidManagedHandler(t, []any{group}, fixture.guardPath)["command"].(string)
	group["hooks"] = append(requireDroidArray(t, group["hooks"]), map[string]any{
		"type": "command", "command": "user-command",
	})
	group["owner"] = "user"
	root["PreToolUse"] = append(requireDroidArray(t, root["PreToolUse"]),
		map[string]any{
			"matcher": "*",
			"hooks": []any{map[string]any{
				"type": "command", "command": strings.Replace(command, droidGuardScript, droidGuardScript+".backup", 1), "timeout": 10,
			}},
		},
		map[string]any{
			"matcher": "*",
			"hooks": []any{map[string]any{
				"type": "command", "command": strings.Replace(command, "agent-1", "agent-10", 1), "timeout": 10,
			}},
		},
	)
	writeDroidTestObject(t, fixture.configPath, root)
	if err := fixture.plugin.Uninstall(droidTestAgentID); err != nil {
		t.Fatalf("uninstall Factory Droid hooks: %v", err)
	}
	remainingRaw := readDroidTestFile(t, fixture.configPath)
	for _, marker := range []string{"user-command", droidGuardScript + ".backup", "agent-10"} {
		if !strings.Contains(remainingRaw, marker) {
			t.Fatalf("uninstall removed lookalike %q", marker)
		}
	}
	remaining := readDroidTestObject(t, fixture.configPath)
	if len(requireDroidArray(t, remaining["PostToolUse"])) != 0 {
		t.Fatalf("PostToolUse after uninstall = %#v", remaining["PostToolUse"])
	}
}

func TestDroidInstallRejectsMalformedSourcesBeforeWrites(t *testing.T) {
	tests := []struct {
		name     string
		hooks    *string
		legacy   *string
		settings *string
		pattern  string
	}{
		{"malformed hooks", droidString("{ malformed"), nil, nil, "parse Factory Droid hooks"},
		{"non-object hooks", droidJSON([]any{}), nil, nil, "JSON object"},
		{"duplicate event", droidString(`{ "PreToolUse": [], "PreToolUse": [] }`), nil, nil, "duplicate"},
		{"nested duplicate", droidString(`{ "PreToolUse": [{ "hooks": [], "hooks": [] }] }`), nil, nil, "duplicate"},
		{"null event", droidString(`{ "PreToolUse": null }`), nil, nil, "must be an array"},
		{"null group", droidString(`{ "PreToolUse": [null] }`), nil, nil, "must be an object"},
		{"invalid matcher", droidString(`{ "PreToolUse": [{ "matcher": "[", "hooks": [] }] }`), nil, nil, "regular expression"},
		{"invalid handler", droidString(`{ "PreToolUse": [{ "hooks": [{ "type": "command", "command": 1 }] }] }`), nil, nil, "must be a string"},
		{"invalid flag", droidString(`{ "hooksDisabled": "yes" }`), nil, nil, "must be a boolean"},
		{"unsupported root field", droidString(`{ "theme": "dark" }`), nil, nil, "unsupported field"},
		{"malformed legacy", nil, droidString("{ malformed"), nil, "legacy hooks"},
		{"malformed settings", droidString(`{ "PreToolUse": [] }`), nil, droidString("{ malformed"), "settings"},
		{"null settings hooks", nil, nil, droidString(`{ "hooks": null }`), "JSON object"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := prepareDroidFixture(t, droidFixtureOptions{
				hooks: test.hooks, legacyHooks: test.legacy, settings: test.settings,
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
			for _, path := range []string{fixture.hookPath, fixture.runtimeConfig, fixture.privateKey} {
				requireMissingDroidFile(t, path)
			}
		})
	}
}

func TestDroidInstallValidatesManagedPathsBeforeWrites(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*droidFixture)
		pattern string
	}{
		{
			name: "missing guard",
			mutate: func(fixture *droidFixture) {
				if err := os.Remove(fixture.guardPath); err != nil {
					t.Fatalf("remove guard: %v", err)
				}
			},
			pattern: "guard runtime is missing",
		},
		{
			name: "invalid agent id",
			mutate: func(fixture *droidFixture) {
				fixture.config.AgentID = "../agent"
			},
			pattern: "single non-empty path segment",
		},
		{
			name: "guard outside managed directory",
			mutate: func(fixture *droidFixture) {
				fixture.config.GuardScriptPath = filepath.Join(fixture.homeDir, "guard.js")
			},
			pattern: "managed agent directory",
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
			for _, path := range []string{fixture.configPath, fixture.hookPath, fixture.runtimeConfig, fixture.privateKey} {
				requireMissingDroidFile(t, path)
			}
		})
	}
}

func TestDroidTransactionRollsBackAllFilesAndCleansStaging(t *testing.T) {
	fixture := prepareDroidFixture(t, droidFixtureOptions{
		hooks:    droidJSON(map[string]any{"PreToolUse": []any{}}),
		settings: droidJSON(map[string]any{"hooks": map[string]any{"PostToolUse": []any{}}}),
	})
	originalRoot := readDroidTestFile(t, fixture.configPath)
	originalSettings := readDroidTestFile(t, fixture.settingsPath)
	failed := false
	fixture.plugin.rename = func(source, destination string) error {
		if !failed && sameDroidPath(destination, fixture.settingsPath) {
			failed = true
			return errors.New("simulated settings commit failure")
		}
		return os.Rename(source, destination)
	}
	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "Write Factory Droid installation") || !failed {
		t.Fatalf("transaction error = %v, failed = %v", err, failed)
	}
	if readDroidTestFile(t, fixture.configPath) != originalRoot ||
		readDroidTestFile(t, fixture.settingsPath) != originalSettings {
		t.Fatal("transaction rollback did not restore hook sources")
	}
	for _, path := range []string{fixture.hookPath, fixture.runtimeConfig, fixture.privateKey} {
		requireMissingDroidFile(t, path)
	}
	requireNoDroidStagingFiles(t, fixture.homeDir)
}

func TestDroidTransactionDetectsExternalChangesAndPreservesThem(t *testing.T) {
	fixture := prepareDroidFixture(t, droidFixtureOptions{
		hooks:    droidJSON(map[string]any{"PreToolUse": []any{}}),
		settings: droidJSON(map[string]any{"hooks": map[string]any{"PostToolUse": []any{}}}),
	})
	external := "{\n  \"external\": true,\n  \"hooks\": { \"PostToolUse\": [] }\n}\n"
	mutated := false
	fixture.plugin.rename = func(source, destination string) error {
		if !mutated {
			mutated = true
			if err := os.WriteFile(fixture.settingsPath, []byte(external), 0600); err != nil {
				return err
			}
		}
		return os.Rename(source, destination)
	}
	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "changed during installation") {
		t.Fatalf("external-change error = %v", err)
	}
	if readDroidTestFile(t, fixture.settingsPath) != external {
		t.Fatal("transaction rollback overwrote an external settings change")
	}
	for _, path := range []string{fixture.hookPath, fixture.runtimeConfig, fixture.privateKey} {
		requireMissingDroidFile(t, path)
	}
	requireNoDroidStagingFiles(t, fixture.homeDir)
}

func TestDroidRecoveryCommandsDoNotRequireNodeResolution(t *testing.T) {
	fixture := prepareDroidFixture(t, droidFixtureOptions{})
	installDroidFixture(t, fixture)
	t.Setenv("PATH", "")
	status, err := fixture.plugin.Status()
	if err != nil || !status.Installed {
		t.Fatalf("status without PATH = %#v, %v", status, err)
	}
	if err := fixture.plugin.Uninstall(droidTestAgentID); err != nil {
		t.Fatalf("uninstall without PATH: %v", err)
	}
	requireMissingDroidFile(t, fixture.configPath)
}

func TestDroidStatusSurfacesMalformedRuntimeConfig(t *testing.T) {
	fixture := prepareDroidFixture(t, droidFixtureOptions{})
	installDroidFixture(t, fixture)
	if err := os.WriteFile(fixture.runtimeConfig, []byte("{ malformed"), 0600); err != nil {
		t.Fatalf("corrupt runtime config: %v", err)
	}
	if _, err := fixture.plugin.Status(); err == nil || !strings.Contains(err.Error(), "parse Elydora runtime config") {
		t.Fatalf("status error = %v", err)
	}
}

func writeDroidTestObject(t *testing.T, path string, value map[string]any) {
	t.Helper()
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("encode %s: %v", path, err)
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func existingDroidSources(t *testing.T, fixture *droidFixture) map[string]string {
	t.Helper()
	result := map[string]string{}
	for _, path := range []string{fixture.configPath, fixture.legacyPath, fixture.settingsPath} {
		if raw, err := os.ReadFile(path); err == nil {
			result[path] = string(raw)
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read source %s: %v", path, err)
		}
	}
	return result
}

func requireNoDroidStagingFiles(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasSuffix(entry.Name(), ".tmp") || strings.HasSuffix(entry.Name(), ".rollback") {
			t.Errorf("staging file remains: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk staging files: %v", err)
	}
}
