package plugins

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestClaudeCodeRegistryUsesOfficialSettingsContract(t *testing.T) {
	entry := SupportedAgents[claudeAgentKey]
	if entry.Name != "Claude Code" || entry.ConfigDir != "$CLAUDE_CONFIG_DIR" ||
		entry.ConfigFile != claudeConfigFile {
		t.Fatalf("Claude Code registry entry = %#v", entry)
	}
}

func TestClaudeInstallPreservesSettingsAndWritesExactTriple(t *testing.T) {
	existing := `{
  "$schema": "https://json.schemastore.org/claude-code-settings.json",
  "model": "sonnet",
  "disableAllHooks": false,
  "hooks": {
    "Notification": [{"matcher":"permission_prompt","hooks":[{"type":"http","url":"https://example.test/hook","timeout":1}] }],
    "PreToolUse": [{"matcher":"Bash","hooks":[{"type":"command","command":"existing-command","timeout":5}]}]
  }
}
`
	fixture := prepareClaudeFixture(
		t,
		claudeFixtureOptions{existingRaw: &existing},
	)
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Claude hooks: %v", err)
	}
	settings := readClaudeTestObject(t, fixture.configPath)
	if settings["model"] != "sonnet" || settings["disableAllHooks"] != false {
		t.Fatalf("user settings changed: %#v", settings)
	}
	hooks := requireObject(t, settings["hooks"])
	if len(requireArray(t, hooks["Notification"])) != 1 ||
		len(requireArray(t, hooks["PreToolUse"])) != 2 {
		t.Fatalf("user hooks changed: %#v", hooks)
	}
	requireStrictClaudeTriple(t, fixture)
	first, err := os.ReadFile(fixture.configPath)
	if err != nil {
		t.Fatalf("read installed settings: %v", err)
	}
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("repeat Claude install: %v", err)
	}
	second, err := os.ReadFile(fixture.configPath)
	if err != nil || string(second) != string(first) {
		t.Fatalf("idempotent settings = %q, %v", second, err)
	}
	status, err := fixture.plugin.Status()
	if err != nil || !status.Installed || !status.HookConfigured ||
		!status.HookScriptExists {
		t.Fatalf("Claude status = %#v, %v", status, err)
	}
}

func TestClaudeConfigDirMatchesOfficialResolution(t *testing.T) {
	tests := []struct {
		name          string
		present       bool
		configured    string
		expectedParts []string
	}{
		{"default", false, "", []string{".claude", "settings.json"}},
		{"absolute", true, "ABSOLUTE", []string{"custom claude", "settings.json"}},
		{"relative", true, "relative claude", []string{"relative claude", "settings.json"}},
		{"empty", true, "", []string{"settings.json"}},
		{"literal tilde", true, "~", []string{"~", "settings.json"}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			options := claudeFixtureOptions{configEnvPresent: testCase.present}
			if testCase.configured == "ABSOLUTE" {
				root := t.TempDir()
				options.configEnvOverride = filepath.Join(root, "custom claude")
			} else {
				options.configEnvOverride = testCase.configured
			}
			fixture := prepareClaudeFixture(t, options)
			if err := fixture.plugin.Install(fixture.config); err != nil {
				t.Fatalf("install Claude hooks: %v", err)
			}
			if _, err := os.Stat(fixture.configPath); err != nil {
				t.Fatalf("resolved settings missing: %v", err)
			}
			for _, part := range testCase.expectedParts {
				if !strings.Contains(fixture.configPath, part) {
					t.Fatalf("settings path %q lacks %q", fixture.configPath, part)
				}
			}
		})
	}
}

func TestClaudeMalformedSettingsFailBeforeRuntimeCreation(t *testing.T) {
	source := "{ malformed"
	fixture := prepareClaudeFixture(
		t,
		claudeFixtureOptions{existingRaw: &source},
	)
	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "parse Claude Code user settings") {
		t.Fatalf("install error = %v", err)
	}
	actual, readErr := os.ReadFile(fixture.configPath)
	if readErr != nil || string(actual) != source {
		t.Fatalf("malformed settings changed: %q, %v", actual, readErr)
	}
	assertNoClaudeRuntimeWrites(t, fixture)
}

func TestClaudePreservesEveryOfficialHandlerType(t *testing.T) {
	source := `{"hooks":{
  "SessionStart":[{"hooks":[{"type":"prompt","prompt":"Review context","model":"haiku","continueOnBlock":true,"if":"always","once":false,"statusMessage":"Reviewing","timeout":0.5}]}],
  "PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"user-command","args":["--safe"],"async":false,"asyncRewake":true,"rewakeMessage":"Background validation failed","rewakeSummary":"Validation feedback","shell":"powershell"}]}],
  "Stop":[{"hooks":[{"type":"agent","prompt":"Verify completion","model":"sonnet"}]}],
  "Notification":[{"hooks":[{"type":"http","url":"https://example.test/hook","headers":{"Authorization":"Bearer ${TOKEN}"},"allowedEnvVars":["TOKEN"]}]}],
  "PostToolUse":[{"hooks":[{"type":"mcp_tool","server":"audit","tool":"record","input":{"source":"claude"}}]}]
}}`
	var existing map[string]any
	if err := json.Unmarshal([]byte(source), &existing); err != nil {
		t.Fatalf("decode user hooks: %v", err)
	}
	userHooks := requireObject(t, existing["hooks"])
	fixture := prepareClaudeFixture(t, claudeFixtureOptions{existingRaw: &source})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Claude hooks: %v", err)
	}
	installed := requireObject(t, readClaudeTestObject(t, fixture.configPath)["hooks"])
	for event, groups := range userHooks {
		actual := requireArray(t, installed[event])[0]
		want := groups.([]any)[0]
		if !reflect.DeepEqual(actual, want) {
			t.Fatalf("%s user hook = %#v, want %#v", event, actual, want)
		}
	}
}

func TestClaudeInvalidOfficialHookShapesFailBeforeWrites(t *testing.T) {
	tests := []struct{ name, source, want string }{
		{"duplicate", `{"hooks":{},"hooks":{}}`, "duplicate key"},
		{"disable", `{"disableAllHooks":"yes"}`, "must be a boolean"},
		{"hooks", `{"hooks":null}`, "must be an object"},
		{"event", `{"hooks":{"MadeUp":[]}}`, "unsupported hook event"},
		{"event groups", `{"hooks":{"PreToolUse":null}}`, "must be an array"},
		{"group", `{"hooks":{"PreToolUse":[null]}}`, "must be an object"},
		{"group field", `{"hooks":{"PreToolUse":[{"hooks":[],"label":"x"}]}}`, "unsupported field"},
		{"matcher", `{"hooks":{"PreToolUse":[{"matcher":1,"hooks":[]}]}}`, "matcher must be a string"},
		{"group hooks", `{"hooks":{"PreToolUse":[{}]}}`, "hooks array"},
		{"handler", `{"hooks":{"PreToolUse":[{"hooks":[null]}]}}`, "handler"},
		{"type", `{"hooks":{"PreToolUse":[{"hooks":[{"type":"file"}]}]}}`, "unsupported type"},
		{"handler field", `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"x","invented":true}]}]}}`, "unsupported field"},
		{"command", `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":""}]}]}}`, "non-empty string"},
		{"args", `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"x","args":[1]}]}]}}`, "array of strings"},
		{"timeout", `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"x","timeout":0}]}]}}`, "positive finite"},
		{"rewake message", `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"x","rewakeMessage":""}]}]}}`, `field "rewakeMessage" must be a non-empty string`},
		{"rewake summary", `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"x","rewakeSummary":""}]}]}}`, `field "rewakeSummary" must be a non-empty string`},
		{"headers", `{"hooks":{"PreToolUse":[{"hooks":[{"type":"http","url":"https://example.test","headers":{"A":1}}]}]}}`, "map names to strings"},
		{"input", `{"hooks":{"PreToolUse":[{"hooks":[{"type":"mcp_tool","server":"s","tool":"t","input":[]}]}]}}`, "must be an object"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := prepareClaudeFixture(
				t,
				claudeFixtureOptions{existingRaw: &testCase.source},
			)
			err := fixture.plugin.Install(fixture.config)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("install error = %v, want %q", err, testCase.want)
			}
			actual, readErr := os.ReadFile(fixture.configPath)
			if readErr != nil || string(actual) != testCase.source {
				t.Fatalf("invalid settings changed: %q, %v", actual, readErr)
			}
			assertNoClaudeRuntimeWrites(t, fixture)
		})
	}
}

func TestClaudeDisabledHooksBlockInstallationAndHealth(t *testing.T) {
	source := `{"disableAllHooks":true}`
	fixture := prepareClaudeFixture(t, claudeFixtureOptions{existingRaw: &source})
	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "disabled by disableAllHooks") {
		t.Fatalf("install error = %v", err)
	}
	status, statusErr := fixture.plugin.Status()
	if statusErr != nil || status.Installed || status.HookConfigured {
		t.Fatalf("disabled status = %#v, %v", status, statusErr)
	}
	assertNoClaudeRuntimeWrites(t, fixture)
}

func TestClaudeExactLegacyHooksMigrateAndLookalikesSurvive(t *testing.T) {
	fixture := prepareClaudeFixture(t, claudeFixtureOptions{})
	guardCommand := "node " + fixture.guardPath
	auditCommand := "node " + fixture.hookPath
	lookalike := guardCommand + " --inspect"
	writeClaudeTestObject(t, fixture.configPath, map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{"hooks": []any{map[string]any{
					"type": "command", "command": guardCommand,
				}}},
				map[string]any{"hooks": []any{map[string]any{
					"type": "command", "command": lookalike,
				}}},
			},
			"PostToolUse": []any{map[string]any{"hooks": []any{map[string]any{
				"type": "command", "command": auditCommand,
			}}}},
		},
	})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("migrate Claude hooks: %v", err)
	}
	requireStrictClaudeTriple(t, fixture)
	if err := fixture.plugin.Uninstall(claudeTestAgentID); err != nil {
		t.Fatalf("uninstall Claude hooks: %v", err)
	}
	remaining := readClaudeTestObject(t, fixture.configPath)
	hooks := requireObject(t, remaining["hooks"])
	groups := requireArray(t, hooks["PreToolUse"])
	if len(groups) != 1 {
		t.Fatalf("legacy lookalikes = %#v", hooks)
	}
	handler := requireObject(t, requireArray(t, requireObject(t, groups[0])["hooks"])[0])
	if handler["command"] != lookalike {
		t.Fatalf("legacy lookalike changed: %#v", handler)
	}
	if _, exists := hooks["PostToolUse"]; exists {
		t.Fatalf("managed success hook remains: %#v", hooks)
	}
	if _, exists := hooks["PostToolUseFailure"]; exists {
		t.Fatalf("managed failure hook remains: %#v", hooks)
	}
}

func TestClaudeUninstallPreservesUserSettings(t *testing.T) {
	source := `{"model":"sonnet","hooks":{"Notification":[]}}`
	fixture := prepareClaudeFixture(t, claudeFixtureOptions{existingRaw: &source})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Claude hooks: %v", err)
	}
	if err := fixture.plugin.Uninstall(claudeTestAgentID); err != nil {
		t.Fatalf("uninstall Claude hooks: %v", err)
	}
	settings := readClaudeTestObject(t, fixture.configPath)
	want := map[string]any{
		"model": "sonnet", "hooks": map[string]any{"Notification": []any{}},
	}
	if !reflect.DeepEqual(settings, want) {
		t.Fatalf("remaining user settings = %#v", settings)
	}
}

func TestClaudeUninstallRemovesEntirelyManagedSettingsFile(t *testing.T) {
	fixture := prepareClaudeFixture(t, claudeFixtureOptions{})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Claude hooks: %v", err)
	}
	if err := fixture.plugin.Uninstall(claudeTestAgentID); err != nil {
		t.Fatalf("uninstall Claude hooks: %v", err)
	}
	if _, err := os.Lstat(fixture.configPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed settings remain: %v", err)
	}
}

func TestClaudeStatusRequiresEnabledUniqueTriple(t *testing.T) {
	fixture := prepareClaudeFixture(t, claudeFixtureOptions{})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Claude hooks: %v", err)
	}
	settings := readClaudeTestObject(t, fixture.configPath)
	hooks := requireObject(t, settings["hooks"])
	delete(hooks, "PostToolUseFailure")
	writeClaudeTestObject(t, fixture.configPath, settings)
	status, err := fixture.plugin.Status()
	if err != nil || status.Installed || status.HookConfigured {
		t.Fatalf("incomplete status = %#v, %v", status, err)
	}
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("restore Claude hooks: %v", err)
	}
	settings = readClaudeTestObject(t, fixture.configPath)
	hooks = requireObject(t, settings["hooks"])
	groups := requireArray(t, hooks["PostToolUseFailure"])
	hooks["PostToolUseFailure"] = append(groups, groups[len(groups)-1])
	writeClaudeTestObject(t, fixture.configPath, settings)
	status, err = fixture.plugin.Status()
	if err != nil || status.Installed || status.HookConfigured {
		t.Fatalf("duplicate status = %#v, %v", status, err)
	}
}

func TestClaudeProjectAndLocalSettingsStayUnchanged(t *testing.T) {
	fixture := prepareClaudeFixture(t, claudeFixtureOptions{})
	projectSettings := filepath.Join(fixture.projectDir, ".claude", "settings.json")
	localSettings := filepath.Join(fixture.projectDir, ".claude", "settings.local.json")
	if err := os.MkdirAll(filepath.Dir(projectSettings), 0700); err != nil {
		t.Fatalf("create project settings directory: %v", err)
	}
	projectSource := []byte(`{"hooks":{"PreToolUse":[]}}`)
	localSource := []byte(`{"model":"haiku"}`)
	if err := os.WriteFile(projectSettings, projectSource, 0600); err != nil {
		t.Fatalf("write project settings: %v", err)
	}
	if err := os.WriteFile(localSettings, localSource, 0600); err != nil {
		t.Fatalf("write local settings: %v", err)
	}
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Claude hooks: %v", err)
	}
	for path, want := range map[string][]byte{
		projectSettings: projectSource,
		localSettings:   localSource,
	} {
		actual, err := os.ReadFile(path)
		if err != nil || string(actual) != string(want) {
			t.Fatalf("read-only source changed at %s: %q, %v", path, actual, err)
		}
	}
}
