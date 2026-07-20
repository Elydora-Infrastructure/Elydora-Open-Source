package plugins

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestGeminiRegistryFactoryAndRuntimeOwnership(t *testing.T) {
	entry := SupportedAgents[geminiAgentKey]
	if entry.Name != "Gemini CLI" || entry.ConfigDir != "~/.gemini" ||
		entry.ConfigFile != geminiConfigFile {
		t.Fatalf("Gemini registry entry = %#v", entry)
	}
	plugin, ok := NewPlugin(geminiAgentKey).(*GeminiPlugin)
	if !ok || !plugin.ManagesGuardRuntime() {
		t.Fatalf("Gemini plugin = %#v", NewPlugin(geminiAgentKey))
	}
}

func TestGeminiCommandRoundTripAndRejectsInjection(t *testing.T) {
	fixture := prepareGeminiFixture(t, geminiFixtureOptions{})
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		t.Fatalf("resolve Node.js runtime: %v", err)
	}
	command, err := buildGeminiCommand(nodePath, fixture.guardPath)
	if err != nil {
		t.Fatalf("build Gemini command: %v", err)
	}
	executable, script, ok := parseGeminiCommand(command)
	if !ok || !sameGeminiPath(executable, nodePath) ||
		!sameGeminiPath(script, fixture.guardPath) {
		t.Fatalf("parsed Gemini command = %q, %q, %v", executable, script, ok)
	}
	if runtime.GOOS == "windows" && (strings.Contains(command, "%GEMINI_CWD%") ||
		strings.Contains(command, "$GEMINI_CWD")) {
		t.Fatalf("Windows Gemini command exposes expandable path text: %q", command)
	}
	if _, err := buildGeminiCommand("node", fixture.guardPath); err == nil {
		t.Fatal("accepted a relative Gemini runtime path")
	}
	if _, err := buildGeminiCommand(nodePath, "guard.js"); err == nil {
		t.Fatal("accepted a relative Gemini script path")
	}
	for _, invalid := range []string{
		command + " --inspect",
		command + "\nexit 0",
		"node " + fixture.guardPath,
	} {
		if _, _, ok := parseGeminiCommand(invalid); ok {
			t.Fatalf("accepted non-canonical command %q", invalid)
		}
	}
}

func TestGeminiInstallPreservesJSONCAndIsIdempotent(t *testing.T) {
	existing := strings.Join([]string{
		"{",
		"  // Keep this user preference.",
		`  "theme": "GitHub",`,
		`  "hooks": {`,
		`    "FutureEvent": [null],`,
		`    "BeforeTool": [{ "matcher": "read_file", "hooks": [{ "type": "command", "command": "user-hook" }] }]`,
		"  }",
		"}",
		"",
	}, "\r\n")
	fixture := prepareGeminiFixture(
		t,
		geminiFixtureOptions{existingRaw: &existing},
	)
	projectSettings := filepath.Join(
		fixture.projectDir,
		".gemini",
		geminiConfigFile,
	)
	systemSettings := filepath.Join(filepath.Dir(fixture.homeDir), "system.json")
	for path, source := range map[string]string{
		projectSettings: `{ "owner": "project" }` + "\n",
		systemSettings:  `{ "owner": "system" }` + "\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatalf("create source directory: %v", err)
		}
		if err := os.WriteFile(path, []byte(source), 0600); err != nil {
			t.Fatalf("write integration source: %v", err)
		}
	}
	t.Setenv("GEMINI_CLI_SYSTEM_SETTINGS_PATH", systemSettings)
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Gemini hooks: %v", err)
	}
	first, err := os.ReadFile(fixture.settingsPath)
	if err != nil {
		t.Fatalf("read first Gemini settings: %v", err)
	}
	if !strings.Contains(string(first), "Keep this user preference") ||
		!strings.Contains(string(first), "\r\n") {
		t.Fatalf("JSONC formatting changed: %q", first)
	}
	settings := readGeminiTestObject(t, fixture.settingsPath)
	if settings["theme"] != "GitHub" || !reflect.DeepEqual(
		requireObject(t, settings["hooks"])["FutureEvent"],
		[]any{nil},
	) {
		t.Fatalf("user settings changed: %#v", settings)
	}
	beforeTool := requireArray(t, requireObject(t, settings["hooks"])["BeforeTool"])
	if requireObject(t, requireArray(
		t,
		requireObject(t, beforeTool[0])["hooks"],
	)[0])["command"] != "user-hook" {
		t.Fatalf("user hook changed: %#v", beforeTool)
	}
	requireStrictGeminiPair(t, settings)
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("repeat Gemini install: %v", err)
	}
	second, err := os.ReadFile(fixture.settingsPath)
	if err != nil || string(second) != string(first) {
		t.Fatalf("repeat install changed settings: %v", err)
	}
	for path, want := range map[string]string{
		projectSettings: `{ "owner": "project" }` + "\n",
		systemSettings:  `{ "owner": "system" }` + "\n",
	} {
		actual, readErr := os.ReadFile(path)
		if readErr != nil || string(actual) != want {
			t.Fatalf("integration source changed at %s: %q, %v", path, actual, readErr)
		}
	}
	for _, path := range []string{
		fixture.guardPath,
		fixture.hookPath,
		fixture.runtimeConfig,
		fixture.privateKey,
	} {
		info, statErr := os.Lstat(path)
		if statErr != nil || !info.Mode().IsRegular() {
			t.Fatalf("managed runtime %s = %v, %v", path, info, statErr)
		}
	}
	assertNoGeminiTransactionArtifacts(t, fixture.homeDir)
}

func TestGeminiHomeResolutionMatchesOfficialSemantics(t *testing.T) {
	fixture := prepareGeminiFixture(
		t,
		geminiFixtureOptions{useDefaultHome: true},
	)
	want := filepath.Join(fixture.homeDir, ".gemini")
	if actual, err := geminiConfigurationDirectory(); err != nil || actual != want {
		t.Fatalf("empty override directory = %q, %v; want %q", actual, err, want)
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("read working directory: %v", err)
	}
	if err := os.Chdir(fixture.projectDir); err != nil {
		t.Fatalf("change working directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
	for _, testCase := range []struct{ value, expected string }{
		{"relative gemini", filepath.Join("relative gemini", ".gemini")},
		{"~", filepath.Join("~", ".gemini")},
	} {
		t.Setenv("GEMINI_CLI_HOME", testCase.value)
		actual, resolveErr := geminiConfigurationDirectory()
		if resolveErr != nil || actual != testCase.expected {
			t.Fatalf("override %q = %q, %v; want %q", testCase.value, actual, resolveErr, testCase.expected)
		}
	}
}

func TestGeminiInstallAppendsToExistingEmptyManagedEvents(t *testing.T) {
	source := `{"hooks":{"BeforeTool":[],"AfterTool":[]}}`
	fixture := prepareGeminiFixture(
		t,
		geminiFixtureOptions{existingRaw: &source},
	)
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Gemini hooks into empty events: %v", err)
	}
	settings := readGeminiTestObject(t, fixture.settingsPath)
	requireStrictGeminiPair(t, settings)
	hooks := requireObject(t, settings["hooks"])
	if len(requireArray(t, hooks["BeforeTool"])) != 1 ||
		len(requireArray(t, hooks["AfterTool"])) != 1 {
		t.Fatalf("managed event groups = %#v", hooks)
	}
}

func TestGeminiRejectsInvalidSettingsBeforeRuntimeWrites(t *testing.T) {
	for _, testCase := range []struct{ name, raw, want string }{
		{"syntax", "{ malformed", "parse Gemini CLI user settings"},
		{"root", "[]", "must contain a JSON object"},
		{"trailing comma", `{ "theme": true, }`, "trailing comma"},
		{"duplicate", `{ "hooks": {}, "hooks": {} }`, "duplicate key"},
		{"hooks", `{ "hooks": null }`, `field "hooks" must be an object`},
		{"event", `{ "hooks": { "BeforeTool": null } }`, "must be an array"},
		{"group", `{ "hooks": { "BeforeTool": [null] } }`, "must be an object"},
		{"matcher", `{ "hooks": { "BeforeTool": [{ "matcher": 1, "hooks": [] }] } }`, "matcher must be a string"},
		{"sequential", `{ "hooks": { "BeforeTool": [{ "sequential": 1, "hooks": [] }] } }`, "sequential must be a boolean"},
		{"missing handlers", `{ "hooks": { "BeforeTool": [{}] } }`, "must contain a hooks array"},
		{"handler", `{ "hooks": { "BeforeTool": [{ "hooks": [null] }] } }`, "must be an object"},
		{"type", `{ "hooks": { "BeforeTool": [{ "hooks": [{ "type": "http" }] }] } }`, "unsupported type"},
		{"command", `{ "hooks": { "BeforeTool": [{ "hooks": [{ "type": "command", "command": "" }] }] } }`, "non-empty command"},
		{"timeout", `{ "hooks": { "BeforeTool": [{ "hooks": [{ "type": "command", "command": "x", "timeout": -1 }] }] } }`, "non-negative finite number"},
		{"environment", `{ "hooks": { "BeforeTool": [{ "hooks": [{ "type": "command", "command": "x", "env": { "A": 1 } }] }] } }`, "env must map names to strings"},
		{"controls", `{ "hooksConfig": null }`, `hooksConfig" must be an object`},
		{"control field", `{ "hooksConfig": { "future": true } }`, "unsupported field"},
		{"enabled", `{ "hooksConfig": { "enabled": "yes" } }`, "must be a boolean"},
		{"disabled", `{ "hooksConfig": { "disabled": [1] } }`, "array of strings"},
		{"notifications", `{ "hooksConfig": { "notifications": 1 } }`, "must be a boolean"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := prepareGeminiFixture(
				t,
				geminiFixtureOptions{existingRaw: geminiString(testCase.raw)},
			)
			err := fixture.plugin.Install(fixture.config)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("install error = %v, want %q", err, testCase.want)
			}
			raw, readErr := os.ReadFile(fixture.settingsPath)
			if readErr != nil || string(raw) != testCase.raw {
				t.Fatalf("original settings changed: %q, %v", raw, readErr)
			}
			assertNoGeminiRuntimeWrites(t, fixture)
		})
	}
}

func TestGeminiRespectsOfficialHookControls(t *testing.T) {
	for _, testCase := range []struct{ raw, want string }{
		{`{"hooksConfig":{"enabled":false}}`, "hooksConfig.enabled"},
		{`{"hooksConfig":{"disabled":["elydora-guard"]}}`, geminiGuardHookName},
		{`{"hooksConfig":{"disabled":["elydora-audit"]}}`, geminiAuditHookName},
	} {
		fixture := prepareGeminiFixture(
			t,
			geminiFixtureOptions{existingRaw: geminiString(testCase.raw)},
		)
		err := fixture.plugin.Install(fixture.config)
		if err == nil || !strings.Contains(err.Error(), testCase.want) {
			t.Fatalf("hook control error = %v, want %q", err, testCase.want)
		}
		assertNoGeminiRuntimeWrites(t, fixture)
	}
	legacy := prepareGeminiFixture(t, geminiFixtureOptions{})
	writeGeminiTestObject(t, legacy.settingsPath, map[string]any{
		"hooksConfig": map[string]any{
			"disabled": []any{legacyGeminiCommand(legacy.guardPath)},
		},
	})
	err := legacy.plugin.Install(legacy.config)
	if err == nil || !strings.Contains(err.Error(), "hooksConfig.disabled") {
		t.Fatalf("legacy disabled command error = %v", err)
	}
	assertNoGeminiRuntimeWrites(t, legacy)
}

func TestGeminiMigratesLegacyHandlersAndPreservesLookalikes(t *testing.T) {
	fixture := prepareGeminiFixture(t, geminiFixtureOptions{})
	legacyGuard := legacyGeminiCommand(fixture.guardPath)
	legacyAudit := legacyGeminiCommand(fixture.hookPath)
	lookalike := legacyGuard + " --inspect"
	writeGeminiTestObject(t, fixture.settingsPath, map[string]any{
		"hooks": map[string]any{
			"BeforeTool": []any{
				map[string]any{"hooks": []any{map[string]any{"type": "command", "command": legacyGuard}}},
				map[string]any{"hooks": []any{map[string]any{"type": "command", "command": lookalike}}},
			},
			"AfterTool": []any{
				map[string]any{"hooks": []any{map[string]any{"type": "command", "command": legacyAudit}}},
			},
		},
	})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("migrate Gemini hooks: %v", err)
	}
	settings := readGeminiTestObject(t, fixture.settingsPath)
	requireStrictGeminiPair(t, settings)
	raw, _ := os.ReadFile(fixture.settingsPath)
	if !strings.Contains(string(raw), "--inspect") {
		t.Fatalf("ownership lookalike was removed: %s", raw)
	}
	hooks := requireObject(t, settings["hooks"])
	groups := requireArray(t, hooks["BeforeTool"])
	managed := requireObject(t, groups[len(groups)-1])
	managed["hooks"] = append(
		requireArray(t, managed["hooks"]),
		map[string]any{"type": "command", "command": "user-command"},
	)
	writeGeminiTestObject(t, fixture.settingsPath, settings)
	if err := fixture.plugin.Uninstall(geminiTestAgentID); err != nil {
		t.Fatalf("uninstall Gemini hooks: %v", err)
	}
	remaining := readGeminiTestObject(t, fixture.settingsPath)
	remainingHooks := requireObject(t, remaining["hooks"])
	if _, exists := remainingHooks["AfterTool"]; exists {
		t.Fatalf("managed audit hook remains: %#v", remainingHooks)
	}
	commands := []string{}
	for _, groupValue := range requireArray(t, remainingHooks["BeforeTool"]) {
		for _, handlerValue := range requireArray(
			t,
			requireObject(t, groupValue)["hooks"],
		) {
			commands = append(
				commands,
				requireObject(t, handlerValue)["command"].(string),
			)
		}
	}
	if !reflect.DeepEqual(commands, []string{lookalike, "user-command"}) {
		t.Fatalf("remaining commands = %#v", commands)
	}
}

func TestGeminiUninstallPreservesUserSettingsAndRemovesOwnedFile(t *testing.T) {
	userSource := `{"theme":"GitHub","hooks":{"Notification":[]}}`
	user := prepareGeminiFixture(
		t,
		geminiFixtureOptions{existingRaw: &userSource},
	)
	if err := user.plugin.Install(user.config); err != nil {
		t.Fatalf("install user Gemini hooks: %v", err)
	}
	if err := user.plugin.Uninstall(geminiTestAgentID); err != nil {
		t.Fatalf("uninstall user Gemini hooks: %v", err)
	}
	remaining := readGeminiTestObject(t, user.settingsPath)
	if remaining["theme"] != "GitHub" || !reflect.DeepEqual(
		requireObject(t, remaining["hooks"])["Notification"],
		[]any{},
	) {
		t.Fatalf("user settings changed: %#v", remaining)
	}
	owned := prepareGeminiFixture(t, geminiFixtureOptions{})
	if err := owned.plugin.Install(owned.config); err != nil {
		t.Fatalf("install owned Gemini hooks: %v", err)
	}
	if err := owned.plugin.Uninstall(geminiTestAgentID); err != nil {
		t.Fatalf("uninstall owned Gemini hooks: %v", err)
	}
	if _, err := os.Lstat(owned.settingsPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owned Gemini settings remain: %v", err)
	}
}

func TestGeminiStatusRequiresExactPairControlsAndRuntimeIdentity(t *testing.T) {
	fixture := prepareGeminiFixture(t, geminiFixtureOptions{})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Gemini hooks: %v", err)
	}
	assertInstalled := func(want bool) {
		t.Helper()
		status, err := fixture.plugin.Status()
		if err != nil || status.Installed != want {
			t.Fatalf("Gemini status = %#v, %v; want installed %v", status, err, want)
		}
	}
	assertInstalled(true)
	settings := readGeminiTestObject(t, fixture.settingsPath)
	hooks := requireObject(t, settings["hooks"])
	hooks["AfterTool"] = append(
		requireArray(t, hooks["AfterTool"]),
		requireArray(t, hooks["AfterTool"])[0],
	)
	writeGeminiTestObject(t, fixture.settingsPath, settings)
	assertInstalled(false)
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("repair duplicate Gemini hooks: %v", err)
	}
	settings = readGeminiTestObject(t, fixture.settingsPath)
	settings["hooksConfig"] = map[string]any{"disabled": []any{geminiAuditHookName}}
	writeGeminiTestObject(t, fixture.settingsPath, settings)
	status, err := fixture.plugin.Status()
	if err != nil || status.Installed || status.HookConfigured {
		t.Fatalf("disabled Gemini status = %#v, %v", status, err)
	}
	settings["hooksConfig"] = map[string]any{"disabled": []any{}}
	writeGeminiTestObject(t, fixture.settingsPath, settings)
	if err := os.WriteFile(fixture.privateKey, []byte("invalid"), 0600); err != nil {
		t.Fatalf("corrupt private key: %v", err)
	}
	if _, err := fixture.plugin.Status(); err == nil ||
		!strings.Contains(err.Error(), "private key") {
		t.Fatalf("invalid private key status error = %v", err)
	}
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("repair Gemini runtime: %v", err)
	}
	if err := os.Remove(fixture.guardPath); err != nil {
		t.Fatalf("remove guard runtime: %v", err)
	}
	status, err = fixture.plugin.Status()
	if err != nil || status.Installed || !status.HookConfigured ||
		status.HookScriptExists {
		t.Fatalf("missing guard status = %#v, %v", status, err)
	}
}

func TestGeminiRuntimeConfigOmitsEmptyOptionalToken(t *testing.T) {
	fixture := prepareGeminiFixture(t, geminiFixtureOptions{})
	fixture.config.Token = ""
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Gemini hooks without token: %v", err)
	}
	config := readGeminiTestObject(t, fixture.runtimeConfig)
	if _, exists := config["token"]; exists {
		t.Fatalf("empty optional token was persisted: %#v", config)
	}
}

func TestGeminiAgentIDMatchingIsPlatformCorrect(t *testing.T) {
	if runtime.GOOS == "windows" {
		if !sameGeminiAgentID("AGENT-1", "agent-1") {
			t.Fatal("Windows Gemini agent IDs should compare case-insensitively")
		}
		return
	}
	if sameGeminiAgentID("AGENT-1", "agent-1") {
		t.Fatal("POSIX Gemini agent IDs should preserve case")
	}
}
