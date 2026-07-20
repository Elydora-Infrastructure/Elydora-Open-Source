package plugins

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQwenInstallRejectsMalformedKnownSettingsBeforeWrites(t *testing.T) {
	tests := []struct {
		name, source, pattern string
	}{
		{"malformed", "{ malformed", "parse Qwen Code user settings"},
		{"non-object", "[]", "JSON object"},
		{"trailing comma", `{ "owner": true, }`, "trailing comma"},
		{"duplicate root", `{ "hooks": {}, "hooks": {} }`, "duplicate"},
		{
			"nested duplicate",
			`{ "hooks": { "PreToolUse": [{ "hooks": [], "hooks": [] }] } }`,
			"duplicate",
		},
		{"invalid disable flag", `{ "disableAllHooks": "yes" }`, "must be a boolean"},
		{"invalid security", `{ "security": [] }`, "security"},
		{
			"invalid folder trust",
			`{ "security": { "folderTrust": { "enabled": "yes" } } }`,
			"must be a boolean",
		},
		{"hooks array", `{ "hooks": [] }`, "JSON object"},
		{"null hooks", `{ "hooks": null }`, "JSON object"},
		{"null event", `{ "hooks": { "PreToolUse": null } }`, "must be an array"},
		{"null group", `{ "hooks": { "PreToolUse": [null] } }`, "must be an object"},
		{
			"invalid matcher type",
			`{ "hooks": { "PreToolUse": [{ "matcher": 1, "hooks": [] }] } }`,
			"matcher must be a string",
		},
		{
			"invalid matcher regex",
			`{ "hooks": { "PreToolUse": [{ "matcher": "[", "hooks": [] }] } }`,
			"regular expression",
		},
		{
			"invalid sequential",
			`{ "hooks": { "PreToolUse": [{ "sequential": "yes", "hooks": [] }] } }`,
			"must be a boolean",
		},
		{
			"null handlers",
			`{ "hooks": { "PreToolUse": [{ "hooks": null }] } }`,
			"hooks array",
		},
		{
			"missing command",
			`{ "hooks": { "PreToolUse": [{ "hooks": [{ "type": "command" }] }] } }`,
			"non-empty command",
		},
		{
			"missing url",
			`{ "hooks": { "PreToolUse": [{ "hooks": [{ "type": "http" }] }] } }`,
			"non-empty url",
		},
		{
			"settings function",
			`{ "hooks": { "PreToolUse": [{ "hooks": [{ "type": "function" }] }] } }`,
			"unsupported type",
		},
		{
			"invalid timeout",
			`{ "hooks": { "PreToolUse": [{ "hooks": [{ "type": "command", "command": "x", "timeout": "ten" }] }] } }`,
			"finite number",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := prepareQwenFixture(t, qwenFixtureOptions{settings: qwenString(test.source)})
			err := fixture.plugin.Install(fixture.config)
			if err == nil || !strings.Contains(err.Error(), test.pattern) {
				t.Fatalf("install error = %v, want substring %q", err, test.pattern)
			}
			if readQwenTestFile(t, fixture.configPath) != test.source {
				t.Fatal("failed install changed Qwen Code settings")
			}
			for _, path := range []string{
				fixture.guardPath,
				fixture.hookPath,
				fixture.runtimeConfig,
				fixture.privateKey,
			} {
				requireMissingQwenFile(t, path)
			}
		})
	}
}

func TestQwenPreservesFutureEventsAndLimitsRegexSemantics(t *testing.T) {
	settings := `{
  "hooks": {
    "FutureEvent": { "shape": "future" },
    "UserPromptSubmit": [{ "matcher": "[", "hooks": [] }]
  }
}`
	fixture := prepareQwenFixture(t, qwenFixtureOptions{settings: qwenString(settings)})
	installQwenFixture(t, fixture)
	raw := readQwenTestFile(t, fixture.configPath)
	for _, marker := range []string{"FutureEvent", `"matcher": "["`} {
		if !strings.Contains(raw, marker) {
			t.Fatalf("future-compatible setting %q was lost: %s", marker, raw)
		}
	}
}

func TestQwenInstallRejectsMalformedReadOnlySourceBeforeWrites(t *testing.T) {
	tests := []struct {
		name string
		path func(*qwenFixture) string
	}{
		{"system defaults", func(f *qwenFixture) string { return f.systemDefaults }},
		{"workspace", func(f *qwenFixture) string { return f.workspaceConfig }},
		{"system override", func(f *qwenFixture) string { return f.systemConfig }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := prepareQwenFixture(t, qwenFixtureOptions{})
			filePath := test.path(fixture)
			writeOptionalQwenFile(t, filePath, qwenString("{ malformed"))
			err := fixture.plugin.Install(fixture.config)
			if err == nil || !strings.Contains(err.Error(), "parse Qwen Code") {
				t.Fatalf("install error = %v", err)
			}
			if readQwenTestFile(t, filePath) != "{ malformed" {
				t.Fatal("failed install changed the read-only source")
			}
			requireMissingQwenFile(t, fixture.guardPath)
		})
	}
}

func TestQwenInstallRejectsEffectiveDisableBeforeWrites(t *testing.T) {
	fixture := prepareQwenFixture(t, qwenFixtureOptions{})
	writeOptionalQwenFile(
		t,
		fixture.systemConfig,
		qwenJSON(map[string]any{"disableAllHooks": true}),
	)
	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "disableAllHooks") ||
		!strings.Contains(err.Error(), fixture.systemConfig) {
		t.Fatalf("install error = %v", err)
	}
	requireMissingQwenFile(t, fixture.guardPath)
	requireMissingQwenFile(t, fixture.configPath)
}

func TestQwenTransactionProtectsEveryReadOnlySource(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*testing.T, *qwenFixture)
		path      func(*qwenFixture) string
		content   string
	}{
		{
			"home environment",
			func(*testing.T, *qwenFixture) {},
			func(f *qwenFixture) string { return filepath.Join(f.qwenDir, ".env") },
			"QWEN_HOME=external\n",
		},
		{
			"system defaults",
			func(*testing.T, *qwenFixture) {},
			func(f *qwenFixture) string { return f.systemDefaults },
			"{}\n",
		},
		{
			"workspace",
			func(*testing.T, *qwenFixture) {},
			func(f *qwenFixture) string { return f.workspaceConfig },
			"{}\n",
		},
		{
			"system override",
			func(*testing.T, *qwenFixture) {},
			func(f *qwenFixture) string { return f.systemConfig },
			"{}\n",
		},
		{
			"trusted folders",
			func(t *testing.T, f *qwenFixture) {
				writeOptionalQwenFile(t, f.configPath, qwenJSON(map[string]any{
					"security": map[string]any{
						"folderTrust": map[string]any{"enabled": true},
					},
				}))
			},
			func(f *qwenFixture) string { return f.trustedFolders },
			"{}\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := prepareQwenFixture(t, qwenFixtureOptions{
				settings: qwenJSON(map[string]any{"owner": "user"}),
			})
			test.configure(t, fixture)
			original := readQwenTestFile(t, fixture.configPath)
			sourcePath := test.path(fixture)
			mutated := false
			fixture.plugin.rename = func(source, destination string) error {
				if !mutated {
					mutated = true
					writeOptionalQwenFile(t, sourcePath, qwenString(test.content))
				}
				return os.Rename(source, destination)
			}
			err := fixture.plugin.Install(fixture.config)
			if err == nil || !strings.Contains(err.Error(), "changed during") || !mutated {
				t.Fatalf("transaction error = %v, mutated = %v", err, mutated)
			}
			if readQwenTestFile(t, fixture.configPath) != original {
				t.Fatal("transaction rollback changed user settings")
			}
			if readQwenTestFile(t, sourcePath) != test.content {
				t.Fatal("transaction rollback overwrote the external mutation")
			}
			for _, path := range []string{
				fixture.guardPath,
				fixture.hookPath,
				fixture.runtimeConfig,
				fixture.privateKey,
			} {
				requireMissingQwenFile(t, path)
			}
			requireNoQwenStagingFiles(t, fixture.homeDir)
		})
	}
}

func TestQwenTransactionRollsBackAllFilesAndCleansStaging(t *testing.T) {
	fixture := prepareQwenFixture(t, qwenFixtureOptions{
		settings: qwenJSON(map[string]any{"owner": "user"}),
	})
	original := readQwenTestFile(t, fixture.configPath)
	failed := false
	fixture.plugin.rename = func(source, destination string) error {
		if !failed && sameQwenPath(destination, fixture.configPath) {
			failed = true
			return errors.New("simulated settings commit failure")
		}
		return os.Rename(source, destination)
	}
	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "Install Qwen Code hooks") || !failed {
		t.Fatalf("transaction error = %v, failed = %v", err, failed)
	}
	if readQwenTestFile(t, fixture.configPath) != original {
		t.Fatal("transaction rollback did not restore Qwen Code settings")
	}
	for _, path := range []string{
		fixture.guardPath,
		fixture.hookPath,
		fixture.runtimeConfig,
		fixture.privateKey,
	} {
		requireMissingQwenFile(t, path)
	}
	requireNoQwenStagingFiles(t, fixture.homeDir)
}

func TestQwenRecoveryCommandsDoNotRequireNodeResolution(t *testing.T) {
	fixture := prepareQwenFixture(t, qwenFixtureOptions{})
	installQwenFixture(t, fixture)
	t.Setenv("PATH", "")
	status, err := fixture.plugin.Status()
	if err != nil || !status.Installed {
		t.Fatalf("status without PATH = %#v, %v", status, err)
	}
	if err := fixture.plugin.Uninstall(qwenTestAgentID); err != nil {
		t.Fatalf("uninstall without PATH: %v", err)
	}
	requireMissingQwenFile(t, fixture.configPath)
}

func TestQwenStatusSurfacesMalformedRuntimeConfig(t *testing.T) {
	fixture := prepareQwenFixture(t, qwenFixtureOptions{})
	installQwenFixture(t, fixture)
	if err := os.WriteFile(fixture.runtimeConfig, []byte("{ malformed"), 0600); err != nil {
		t.Fatalf("corrupt runtime config: %v", err)
	}
	if _, err := fixture.plugin.Status(); err == nil ||
		!strings.Contains(err.Error(), "parse Elydora runtime config") {
		t.Fatalf("status error = %v", err)
	}
}
