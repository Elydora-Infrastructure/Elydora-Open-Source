package plugins

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQwenInstallRejectsMalformedSettingsBeforeWrites(t *testing.T) {
	tests := []struct {
		name, source, pattern string
	}{
		{"malformed", "{ malformed", "parse Qwen Code settings"},
		{"non-object", "[]", "JSON object"},
		{"trailing comma", `{ "owner": true, }`, "trailing comma"},
		{"duplicate root", `{ "hooks": {}, "hooks": {} }`, "duplicate"},
		{"nested duplicate", `{ "hooks": { "PreToolUse": [{ "hooks": [], "hooks": [] }] } }`, "duplicate"},
		{"invalid disable flag", `{ "disableAllHooks": "yes" }`, "must be a boolean"},
		{"hooks array", `{ "hooks": [] }`, "JSON object"},
		{"unsupported event", `{ "hooks": { "UnknownEvent": [] } }`, "unsupported field"},
		{"null event", `{ "hooks": { "PreToolUse": null } }`, "must be an array"},
		{"null group", `{ "hooks": { "PreToolUse": [null] } }`, "must be an object"},
		{"invalid matcher", `{ "hooks": { "PreToolUse": [{ "matcher": "[", "hooks": [] }] } }`, "regular expression"},
		{"invalid sequential", `{ "hooks": { "PreToolUse": [{ "sequential": "yes", "hooks": [] }] } }`, "must be a boolean"},
		{"null handlers", `{ "hooks": { "PreToolUse": [{ "hooks": null }] } }`, "hooks array"},
		{"missing command", `{ "hooks": { "PreToolUse": [{ "hooks": [{ "type": "command" }] }] } }`, "non-empty string"},
		{"missing url", `{ "hooks": { "PreToolUse": [{ "hooks": [{ "type": "http" }] }] } }`, "non-empty string"},
		{"invalid handler type", `{ "hooks": { "PreToolUse": [{ "hooks": [{ "type": "function", "command": "x" }] }] } }`, "command"},
		{"invalid timeout", `{ "hooks": { "PreToolUse": [{ "hooks": [{ "type": "command", "command": "x", "timeout": "ten" }] }] } }`, "finite number"},
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
			for _, path := range []string{fixture.hookPath, fixture.runtimeConfig, fixture.privateKey} {
				requireMissingQwenFile(t, path)
			}
		})
	}
}

func TestQwenInstallValidatesRoutingAndManagedPathsBeforeWrites(t *testing.T) {
	t.Run("unreadable home env", func(t *testing.T) {
		fixture := prepareQwenFixture(t, qwenFixtureOptions{})
		envPath := filepath.Join(fixture.qwenDir, ".env")
		if err := os.MkdirAll(envPath, 0755); err != nil {
			t.Fatalf("create unreadable env path: %v", err)
		}
		err := fixture.plugin.Install(fixture.config)
		if err == nil || !strings.Contains(err.Error(), "Qwen home environment") {
			t.Fatalf("install error = %v", err)
		}
		requireMissingQwenFile(t, fixture.configPath)
		requireMissingQwenFile(t, fixture.runtimeConfig)
	})

	tests := []struct {
		name    string
		mutate  func(*qwenFixture)
		pattern string
	}{
		{
			name: "missing guard",
			mutate: func(fixture *qwenFixture) {
				if err := os.Remove(fixture.guardPath); err != nil {
					t.Fatalf("remove guard: %v", err)
				}
			},
			pattern: "guard runtime is missing",
		},
		{
			name: "invalid agent id",
			mutate: func(fixture *qwenFixture) {
				fixture.config.AgentID = "../agent"
			},
			pattern: "single non-empty path segment",
		},
		{
			name: "guard outside managed directory",
			mutate: func(fixture *qwenFixture) {
				fixture.config.GuardScriptPath = filepath.Join(fixture.homeDir, "guard.js")
			},
			pattern: "managed agent directory",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := prepareQwenFixture(t, qwenFixtureOptions{})
			test.mutate(fixture)
			err := fixture.plugin.Install(fixture.config)
			if err == nil || !strings.Contains(err.Error(), test.pattern) {
				t.Fatalf("install error = %v, want substring %q", err, test.pattern)
			}
			for _, path := range []string{fixture.configPath, fixture.hookPath, fixture.runtimeConfig, fixture.privateKey} {
				requireMissingQwenFile(t, path)
			}
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
	if err == nil || !strings.Contains(err.Error(), "Write Qwen Code installation") || !failed {
		t.Fatalf("transaction error = %v, failed = %v", err, failed)
	}
	if readQwenTestFile(t, fixture.configPath) != original {
		t.Fatal("transaction rollback did not restore Qwen Code settings")
	}
	for _, path := range []string{fixture.hookPath, fixture.runtimeConfig, fixture.privateKey} {
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
	if _, err := fixture.plugin.Status(); err == nil || !strings.Contains(err.Error(), "parse Elydora runtime config") {
		t.Fatalf("status error = %v", err)
	}
}
