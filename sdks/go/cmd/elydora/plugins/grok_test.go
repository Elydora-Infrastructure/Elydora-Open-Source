package plugins

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestGrokRegistryFactoryAndRuntimeOwnership(t *testing.T) {
	entry := SupportedAgents[grokAgentKey]
	if entry.Name != "Grok Build" || entry.ConfigDir != "~/.grok/hooks" ||
		entry.ConfigFile != grokConfigFile {
		t.Fatalf("Grok registry entry = %#v", entry)
	}
	plugin, ok := NewPlugin(grokAgentKey).(*GrokPlugin)
	if !ok || !plugin.ManagesGuardRuntime() {
		t.Fatalf("Grok plugin = %#v", NewPlugin(grokAgentKey))
	}
}

func TestGrokCommandRoundTripAndRejectsInjection(t *testing.T) {
	fixture := prepareGrokFixture(t, grokFixtureOptions{})
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		t.Fatalf("resolve Node.js runtime: %v", err)
	}
	command, err := buildGrokCommand(nodePath, fixture.guardPath)
	if err != nil {
		t.Fatalf("build Grok command: %v", err)
	}
	executable, script, ok := parseGrokCommand(command)
	if !ok || !sameGrokPath(executable, nodePath) ||
		!sameGrokPath(script, fixture.guardPath) {
		t.Fatalf("parsed Grok command = %q, %q, %v", executable, script, ok)
	}
	if strings.Contains(command, "%ELYDORA_HOOK_PATH%") {
		t.Fatalf("Grok command exposes an expandable path: %q", command)
	}
	if _, err := buildGrokCommand("node", fixture.guardPath); err == nil {
		t.Fatal("accepted a relative Grok runtime path")
	}
	if _, err := buildGrokCommand(nodePath, "guard.js"); err == nil {
		t.Fatal("accepted a relative Grok script path")
	}
	for _, invalid := range []string{
		command + " --inspect",
		command + "\nexit 0",
		"node " + fixture.guardPath,
	} {
		if _, _, ok := parseGrokCommand(invalid); ok {
			t.Fatalf("accepted non-canonical command %q", invalid)
		}
	}
}

func TestGrokInstallPreservesConfigAndIsIdempotent(t *testing.T) {
	existing := `{
  "schemaVersion": 1,
  "hooks": {
    "SessionStart": [{"hooks":[{"type":"http","url":"https://example.test/hook","timeout":0}],"label":"keep"}],
    "PreToolUse": [{"matcher":"Bash|run_terminal_command","hooks":[{"type":"command","command":"existing-command","timeout":5,"env":{"A":"B"}}]}]
  }
}`
	fixture := prepareGrokFixture(t, grokFixtureOptions{existingRaw: &existing})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Grok hooks: %v", err)
	}
	first, err := os.ReadFile(fixture.configPath)
	if err != nil {
		t.Fatalf("read first Grok config: %v", err)
	}
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("repeat Grok install: %v", err)
	}
	second, err := os.ReadFile(fixture.configPath)
	if err != nil || string(second) != string(first) {
		t.Fatalf("repeat install changed config: %v", err)
	}
	settings := readGrokTestObject(t, fixture.configPath)
	if settings["schemaVersion"] != float64(1) {
		t.Fatalf("schema version changed: %#v", settings["schemaVersion"])
	}
	hooks := requireObject(t, settings["hooks"])
	if len(requireArray(t, hooks["SessionStart"])) != 1 ||
		len(requireArray(t, hooks["PreToolUse"])) != 2 {
		t.Fatalf("user hooks changed: %#v", hooks)
	}
	requireStrictGrokTriple(t, settings)
	for _, path := range []string{
		filepath.Join(fixture.homeDir, ".grok", "hooks", grokConfigFile),
		filepath.Join(fixture.homeDir, ".claude", "settings.json"),
		filepath.Join(fixture.homeDir, ".cursor", "hooks.json"),
	} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("compatibility config was written at %s: %v", path, err)
		}
	}
	for _, path := range []string{
		fixture.guardPath, fixture.hookPath, fixture.runtimeConfig, fixture.privateKey,
	} {
		if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
			t.Fatalf("managed runtime %s = %v, %v", path, info, err)
		}
	}
	assertNoGrokTransactionArtifacts(t, fixture.homeDir)
}

func TestGrokEmptyHomeOverrideUsesDefault(t *testing.T) {
	fixture := prepareGrokFixture(t, grokFixtureOptions{useDefaultHome: true})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Grok hooks: %v", err)
	}
	status, err := fixture.plugin.Status()
	if err != nil || !status.Installed || status.ConfigPath != fixture.configPath {
		t.Fatalf("default-home status = %#v, %v", status, err)
	}
}

func TestGrokMigratesLegacyCommandsAndPreservesLookalikes(t *testing.T) {
	fixture := prepareGrokFixture(t, grokFixtureOptions{})
	legacyGuard := legacyGrokCommand(t, fixture.guardPath)
	legacyAudit := legacyGrokCommand(t, fixture.hookPath)
	lookalike := legacyGuard + " --inspect"
	existing := map[string]any{"hooks": map[string]any{
		"PreToolUse": []any{
			map[string]any{"hooks": []any{buildGrokHandler(legacyGuard)}},
			map[string]any{"hooks": []any{buildGrokHandler(lookalike)}},
		},
		"PostToolUse": []any{
			map[string]any{"hooks": []any{buildGrokHandler(legacyAudit)}},
		},
	}}
	if err := os.MkdirAll(filepath.Dir(fixture.configPath), 0700); err != nil {
		t.Fatalf("create Grok hooks directory: %v", err)
	}
	writeGrokTestObject(t, fixture.configPath, existing)
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("migrate Grok hooks: %v", err)
	}
	settings := readGrokTestObject(t, fixture.configPath)
	requireStrictGrokTriple(t, settings)
	raw, _ := os.ReadFile(fixture.configPath)
	if !strings.Contains(string(raw), "--inspect") {
		t.Fatalf("ownership lookalike was removed: %s", raw)
	}

	hooks := requireObject(t, settings["hooks"])
	groups := requireArray(t, hooks["PreToolUse"])
	managed := requireObject(t, groups[len(groups)-1])
	managed["hooks"] = append(
		requireArray(t, managed["hooks"]),
		map[string]any{"type": "command", "command": "user-command", "timeout": float64(10)},
	)
	writeGrokTestObject(t, fixture.configPath, settings)
	if err := fixture.plugin.Uninstall(grokTestAgentID); err != nil {
		t.Fatalf("uninstall Grok hooks: %v", err)
	}
	remaining := readGrokTestObject(t, fixture.configPath)
	remainingHooks := requireObject(t, remaining["hooks"])
	if _, exists := remainingHooks["PostToolUse"]; exists {
		t.Fatalf("managed success hook remains: %#v", remainingHooks)
	}
	if _, exists := remainingHooks["PostToolUseFailure"]; exists {
		t.Fatalf("managed failure hook remains: %#v", remainingHooks)
	}
	commands := []string{}
	for _, groupValue := range requireArray(t, remainingHooks["PreToolUse"]) {
		for _, handlerValue := range requireArray(t, requireObject(t, groupValue)["hooks"]) {
			commands = append(commands, requireObject(t, handlerValue)["command"].(string))
		}
	}
	if !reflect.DeepEqual(commands, []string{lookalike, "user-command"}) {
		t.Fatalf("remaining commands = %#v", commands)
	}
}

func TestGrokUninstallPreservesUserConfigAndRemovesOwnedConfig(t *testing.T) {
	userSource := `{"owner":"user","hooks":{"Notification":[]}}`
	fixture := prepareGrokFixture(t, grokFixtureOptions{existingRaw: &userSource})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install user Grok hooks: %v", err)
	}
	if err := fixture.plugin.Uninstall(grokTestAgentID); err != nil {
		t.Fatalf("uninstall user Grok hooks: %v", err)
	}
	remaining := readGrokTestObject(t, fixture.configPath)
	if remaining["owner"] != "user" || !reflect.DeepEqual(
		requireObject(t, remaining["hooks"])["Notification"],
		[]any{},
	) {
		t.Fatalf("user config changed: %#v", remaining)
	}

	owned := prepareGrokFixture(t, grokFixtureOptions{})
	if err := owned.plugin.Install(owned.config); err != nil {
		t.Fatalf("install owned Grok hooks: %v", err)
	}
	if err := owned.plugin.Uninstall(grokTestAgentID); err != nil {
		t.Fatalf("uninstall owned Grok hooks: %v", err)
	}
	if _, err := os.Lstat(owned.configPath); !os.IsNotExist(err) {
		t.Fatalf("owned Grok config remains: %v", err)
	}
}

func TestGrokStatusRequiresExactTripleIdentityAndRuntimeFiles(t *testing.T) {
	fixture := prepareGrokFixture(t, grokFixtureOptions{})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Grok hooks: %v", err)
	}
	assertInstalled := func(want bool) {
		t.Helper()
		status, err := fixture.plugin.Status()
		if err != nil || status.Installed != want {
			t.Fatalf("Grok status = %#v, %v; want installed %v", status, err, want)
		}
	}
	assertInstalled(true)
	settings := readGrokTestObject(t, fixture.configPath)
	hooks := requireObject(t, settings["hooks"])
	delete(hooks, "PostToolUseFailure")
	writeGrokTestObject(t, fixture.configPath, settings)
	assertInstalled(false)
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("repair Grok hooks: %v", err)
	}
	settings = readGrokTestObject(t, fixture.configPath)
	hooks = requireObject(t, settings["hooks"])
	hooks["PostToolUseFailure"] = append(
		requireArray(t, hooks["PostToolUseFailure"]),
		requireArray(t, hooks["PostToolUseFailure"])[0],
	)
	writeGrokTestObject(t, fixture.configPath, settings)
	assertInstalled(false)
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("repair duplicate Grok hooks: %v", err)
	}
	if err := os.Remove(fixture.guardPath); err != nil {
		t.Fatalf("remove guard runtime: %v", err)
	}
	status, err := fixture.plugin.Status()
	if err != nil || status.Installed || !status.HookConfigured ||
		status.HookScriptExists {
		t.Fatalf("missing guard status = %#v, %v", status, err)
	}
}

func TestGrokRejectsInvalidHookShapesBeforeRuntimeWrites(t *testing.T) {
	for _, testCase := range []struct{ name, raw, want string }{
		{"malformed", "{ malformed", "parse Grok user hooks"},
		{"comments", "{\"hooks\":{} // comment\n}", "invalid character"},
		{"trailing comma", `{"hooks":{},}`, "invalid character"},
		{"non-finite timeout", `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"x","timeout":NaN}]}]}}`, "invalid character"},
		{"duplicate", `{"hooks":{},"hooks":{}}`, "duplicate"},
		{"hooks", `{"hooks":null}`, `field "hooks" must be an object`},
		{"event", `{"hooks":{"PreToolUse":null}}`, "must be an array"},
		{"group", `{"hooks":{"PreToolUse":[null]}}`, "must be an object"},
		{"matcher", `{"hooks":{"PreToolUse":[{"matcher":1,"hooks":[]}]}}`, "matcher must be a string"},
		{"lifecycle matcher", `{"hooks":{"SessionStart":[{"matcher":"x","hooks":[]}]}}`, "cannot declare a matcher"},
		{"handler", `{"hooks":{"PreToolUse":[{"hooks":[null]}]}}`, "must be an object"},
		{"type", `{"hooks":{"PreToolUse":[{"hooks":[{"type":"file"}]}]}}`, "unsupported type"},
		{"command", `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":""}]}]}}`, "non-empty command"},
		{"http", `{"hooks":{"PostToolUse":[{"hooks":[{"type":"http","url":""}]}]}}`, "non-empty url"},
		{"negative timeout", `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"x","timeout":-1}]}]}}`, "non-negative integer"},
		{"fraction timeout", `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"x","timeout":1.5}]}]}}`, "non-negative integer"},
		{"unsafe timeout", `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"x","timeout":9007199254740992}]}]}}`, "non-negative integer"},
		{"environment", `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"x","env":{"A":1}}]}]}}`, "env must map names to strings"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := prepareGrokFixture(
				t,
				grokFixtureOptions{existingRaw: grokString(testCase.raw)},
			)
			err := fixture.plugin.Install(fixture.config)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("install error = %v, want %q", err, testCase.want)
			}
			raw, readErr := os.ReadFile(fixture.configPath)
			if readErr != nil || string(raw) != testCase.raw {
				t.Fatalf("original config changed: %q, %v", raw, readErr)
			}
			assertNoGrokRuntimeWrites(t, fixture)
		})
	}
}

func TestGrokRejectsRenderedConfigBeyondManagedSourceLimit(t *testing.T) {
	prefix := `{"padding":"`
	suffix := `"}`
	padding := strings.Repeat(
		"a",
		maxManagedSourceBytes-len(prefix)-len(suffix),
	)
	raw := prefix + padding + suffix
	fixture := prepareGrokFixture(t, grokFixtureOptions{existingRaw: &raw})
	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("install error = %v", err)
	}
	current, readErr := os.ReadFile(fixture.configPath)
	if readErr != nil || string(current) != raw {
		t.Fatalf("large source changed: size=%d, err=%v", len(current), readErr)
	}
	assertNoGrokRuntimeWrites(t, fixture)
}

func TestGrokAcceptsOfficialZeroTimeoutAndStringEnvironment(t *testing.T) {
	raw := `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"user","timeout":0,"env":{"A":"B"}}]}]}}`
	fixture := prepareGrokFixture(t, grokFixtureOptions{existingRaw: &raw})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Grok hooks with official fields: %v", err)
	}
	hooks := requireObject(t, readGrokTestObject(t, fixture.configPath)["hooks"])
	user := requireObject(t, requireArray(t, requireObject(
		t,
		requireArray(t, hooks["PreToolUse"])[0],
	)["hooks"])[0])
	if user["timeout"] != float64(0) {
		t.Fatalf("zero timeout changed: %#v", user)
	}
}

func TestGrokRuntimeConfigOmitsEmptyOptionalToken(t *testing.T) {
	fixture := prepareGrokFixture(t, grokFixtureOptions{})
	fixture.config.Token = ""
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Grok hooks without token: %v", err)
	}
	config := readGrokTestObject(t, fixture.runtimeConfig)
	if _, exists := config["token"]; exists {
		t.Fatalf("empty optional token was persisted: %#v", config)
	}
}

func TestGrokWindowsAgentIDMatchingIsPlatformCorrect(t *testing.T) {
	if runtime.GOOS == "windows" {
		if !sameGrokAgentID("AGENT-1", "agent-1") {
			t.Fatal("Windows Grok agent IDs should compare case-insensitively")
		}
		return
	}
	if sameGrokAgentID("AGENT-1", "agent-1") {
		t.Fatal("POSIX Grok agent IDs should preserve case")
	}
}
