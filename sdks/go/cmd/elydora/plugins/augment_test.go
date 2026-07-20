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

func TestAugmentRegistryFactoryAndGuardOwnership(t *testing.T) {
	entry := SupportedAgents[augmentAgentKey]
	if entry.Name != "Augment Code CLI" ||
		entry.ConfigDir != "~/.augment" ||
		entry.ConfigFile != "settings.json" {
		t.Fatalf("Auggie registry entry = %#v", entry)
	}
	plugin, ok := NewPlugin(augmentAgentKey).(*AugmentPlugin)
	if !ok {
		t.Fatalf("Auggie plugin factory returned %T", NewPlugin(augmentAgentKey))
	}
	if !plugin.ManagesGuardRuntime() {
		t.Fatal("Auggie plugin must own guard runtime generation")
	}
}

func TestAugmentGeneratedArgumentParsers(t *testing.T) {
	posixPath := "/tmp/home with spaces/agent's/augment-guard.sh"
	posixCommand := quotePOSIXArgument(posixPath)
	parsed, ok := readAugmentPOSIXArgument(posixCommand)
	if !ok || parsed != posixPath {
		t.Fatalf("parse POSIX wrapper = %q, %v", parsed, ok)
	}
	windowsPath := `C:\home with spaces\.elydora\agent-1\augment-guard.cmd`
	windowsCommand := quoteAugmentWindowsCommand(windowsPath)
	parsed, ok = readAugmentWindowsArgument(windowsCommand)
	if !ok || parsed != windowsPath {
		t.Fatalf("parse Windows wrapper = %q, %v", parsed, ok)
	}
}

func TestAugmentInstallPreservesSettingsAndIsIdempotent(t *testing.T) {
	existing := `{
  "telemetryEnabled": false,
  "hooks": {
    "SessionStart": [{"hooks":[{"type":"command","command":"existing-command","args":["one"],"timeout":5000}],"metadata":{"includeUserContext":true},"label":"keep"}],
    "PreToolUse": [{"matcher":"launch-process","hooks":[{"type":"command","command":"user-command"}]}]
  }
}`
	fixture := prepareAugmentFixture(
		t,
		augmentFixtureOptions{existingRaw: &existing},
	)
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Auggie hooks: %v", err)
	}
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("repeat Auggie install: %v", err)
	}
	settings := readAugmentTestObject(t, fixture.configPath)
	if settings["telemetryEnabled"] != false {
		t.Fatalf("telemetry setting changed: %#v", settings)
	}
	hooks := requireObject(t, settings["hooks"])
	if len(requireArray(t, hooks["SessionStart"])) != 1 ||
		len(requireArray(t, hooks["PreToolUse"])) != 2 ||
		len(requireArray(t, hooks["PostToolUse"])) != 1 {
		t.Fatalf("unexpected hooks after repeat install: %#v", hooks)
	}
	managedGroup := requireObject(
		t,
		requireArray(t, hooks["PreToolUse"])[1],
	)
	if managedGroup["matcher"] != ".*" {
		t.Fatalf("managed matcher = %#v", managedGroup["matcher"])
	}
	guard := augmentTestManagedHandler(
		t,
		settings,
		"PreToolUse",
		fixture.guardWrapper,
	)
	audit := augmentTestManagedHandler(
		t,
		settings,
		"PostToolUse",
		fixture.auditWrapper,
	)
	for _, handler := range []map[string]any{guard, audit} {
		want := map[string]any{
			"type": "command", "command": handler["command"],
			"timeout": augmentHookTimeout,
		}
		if !reflect.DeepEqual(handler, want) {
			t.Fatalf("managed handler = %#v", handler)
		}
	}
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		t.Fatalf("resolve Node.js runtime: %v", err)
	}
	for _, item := range []struct {
		path, script string
	}{
		{fixture.guardWrapper, fixture.guardPath},
		{fixture.auditWrapper, fixture.hookPath},
	} {
		actual, err := os.ReadFile(item.path)
		if err != nil {
			t.Fatalf("read wrapper %s: %v", item.path, err)
		}
		want := buildAugmentWrapper(nodePath, item.script)
		if string(actual) != string(want) {
			t.Fatalf("wrapper %s = %q, want %q", item.path, actual, want)
		}
	}
	runtimeConfig := readAugmentTestObject(t, fixture.runtimeConfig)
	if runtimeConfig["agent_name"] != augmentAgentKey ||
		runtimeConfig["agent_id"] != augmentTestAgentID {
		t.Fatalf("runtime identity = %#v", runtimeConfig)
	}
	auditSource, err := os.ReadFile(fixture.hookPath)
	if err != nil || !strings.Contains(string(auditSource), "const NATIVE_PAYLOAD = true;") {
		t.Fatalf("audit runtime native payload = %v, %v", err, strings.Contains(string(auditSource), "const NATIVE_PAYLOAD = true;"))
	}
	for _, path := range []string{
		filepath.Join(fixture.projectDir, ".augment", "settings.json"),
		filepath.Join(fixture.projectDir, ".augment", "settings.local.json"),
	} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("workspace settings written at %s: %v", path, err)
		}
	}
}

func TestAugmentUninstallPreservesExactUserOwnership(t *testing.T) {
	existing := `{"owner":"user","hooks":{"Notification":[]}}`
	fixture := prepareAugmentFixture(
		t,
		augmentFixtureOptions{existingRaw: &existing},
	)
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Auggie hooks: %v", err)
	}
	settings := readAugmentTestObject(t, fixture.configPath)
	hooks := requireObject(t, settings["hooks"])
	preGroups := requireArray(t, hooks["PreToolUse"])
	preGroups = append([]any{map[string]any{
		"hooks": []any{}, "label": "keep empty group",
	}}, preGroups...)
	managedGroup := requireObject(t, preGroups[1])
	managedGroup["hooks"] = append(
		requireArray(t, managedGroup["hooks"]),
		map[string]any{"type": "command", "command": "user-command"},
	)
	preGroups = append(
		preGroups,
		map[string]any{"hooks": []any{buildAugmentHandler(
			fixture.guardWrapper + ".backup",
		)}},
		map[string]any{"hooks": []any{buildAugmentHandler(filepath.Join(
			filepath.Dir(fixture.agentDir),
			"agent-10",
			augmentGuardWrapperName(),
		))}},
	)
	hooks["PreToolUse"] = preGroups
	writeAugmentTestObject(t, fixture.configPath, settings)
	uninstallID := augmentTestAgentID
	if runtime.GOOS == "windows" {
		uninstallID = "AGENT-1"
	}
	if err := fixture.plugin.Uninstall(uninstallID); err != nil {
		t.Fatalf("uninstall Auggie hooks: %v", err)
	}
	remaining := readAugmentTestObject(t, fixture.configPath)
	remainingHooks := requireObject(t, remaining["hooks"])
	if remaining["owner"] != "user" ||
		len(requireArray(t, remainingHooks["PreToolUse"])) != 4 {
		t.Fatalf("user settings changed: %#v", remaining)
	}
	if !reflect.DeepEqual(
		requireArray(t, remainingHooks["Notification"]),
		[]any{},
	) {
		t.Fatalf("empty notification changed: %#v", remainingHooks["Notification"])
	}
	raw, _ := os.ReadFile(fixture.configPath)
	if !strings.Contains(string(raw), "backup") ||
		!strings.Contains(string(raw), "agent-10") {
		t.Fatalf("similar handlers were removed: %s", raw)
	}
	if _, exists := remainingHooks["PostToolUse"]; exists {
		t.Fatalf("managed PostToolUse remains: %#v", remainingHooks["PostToolUse"])
	}
}

func TestAugmentInstallReplacesStaleHooksAndPreservesEmptyGroups(t *testing.T) {
	fixture := prepareAugmentFixture(t, augmentFixtureOptions{})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Auggie hooks: %v", err)
	}
	settings := readAugmentTestObject(t, fixture.configPath)
	hooks := requireObject(t, settings["hooks"])
	hooks["PreToolUse"] = append(
		[]any{map[string]any{"hooks": []any{}, "label": "keep empty group"}},
		requireArray(t, hooks["PreToolUse"])...,
	)
	for _, item := range []struct{ event, wrapper string }{
		{"PreToolUse", augmentGuardWrapperName()},
		{"PostToolUse", augmentAuditWrapperName()},
	} {
		hooks[item.event] = append(
			requireArray(t, hooks[item.event]),
			map[string]any{"hooks": []any{buildAugmentHandler(filepath.Join(
				filepath.Dir(fixture.agentDir),
				"agent-old",
				item.wrapper,
			))}},
		)
	}
	writeAugmentTestObject(t, fixture.configPath, settings)
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("replace stale Auggie hooks: %v", err)
	}
	raw, _ := os.ReadFile(fixture.configPath)
	current := readAugmentTestObject(t, fixture.configPath)
	currentHooks := requireObject(t, current["hooks"])
	if strings.Contains(string(raw), "agent-old") ||
		len(requireArray(t, currentHooks["PreToolUse"])) != 2 ||
		len(requireArray(t, currentHooks["PostToolUse"])) != 1 {
		t.Fatalf("stale hooks remain: %s", raw)
	}
}

func TestAugmentUninstallRemovesOwnedSettings(t *testing.T) {
	fixture := prepareAugmentFixture(t, augmentFixtureOptions{})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Auggie hooks: %v", err)
	}
	if err := fixture.plugin.Uninstall(augmentTestAgentID); err != nil {
		t.Fatalf("uninstall Auggie hooks: %v", err)
	}
	if _, err := os.Stat(fixture.configPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owned Auggie settings remain: %v", err)
	}
}

func TestAugmentMalformedSettingsPreventRuntimeWrites(t *testing.T) {
	for _, testCase := range []struct{ name, raw, want string }{
		{"malformed", "{ malformed", "parse Auggie user settings"},
		{"duplicate", `{"hooks":{},"hooks":{}}`, "duplicate key"},
		{"comments", "{\"hooks\":{} // comment\n}", "invalid character"},
		{"trailing-comma", `{"hooks":{},}`, "invalid character"},
		{"null-root", "null", "must contain a JSON object"},
		{"null-hooks", `{"hooks":null}`, `field "hooks" must be an object`},
		{"unknown-event", `{"hooks":{"UnknownEvent":[]}}`, "unsupported event"},
		{"null-event", `{"hooks":{"PreToolUse":null}}`, "must be an array"},
		{"null-group", `{"hooks":{"PreToolUse":[null]}}`, "must be an object"},
		{"session-matcher", `{"hooks":{"SessionStart":[{"matcher":".*","hooks":[]}]}}`, "only supported for tool events"},
		{"null-matcher", `{"hooks":{"PreToolUse":[{"matcher":null,"hooks":[]}]}}`, "matcher must be a string"},
		{"invalid-matcher", `{"hooks":{"PreToolUse":[{"matcher":"[","hooks":[]}]}}`, "valid JavaScript regular expression"},
		{"null-handlers", `{"hooks":{"PreToolUse":[{"hooks":null}]}}`, "must contain a hooks array"},
		{"http-handler", `{"hooks":{"PreToolUse":[{"hooks":[{"type":"http","command":"x"}]}]}}`, `type must be "command"`},
		{"empty-command", `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":""}]}]}}`, "non-empty command"},
		{"bad-args", `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"x","args":[1]}]}]}}`, "array of strings"},
		{"zero-timeout", `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"x","timeout":0}]}]}}`, "positive finite number"},
		{"bad-metadata", `{"hooks":{"PreToolUse":[{"hooks":[],"metadata":{"includeUserContext":"yes"}}]}}`, "must be a boolean"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := prepareAugmentFixture(
				t,
				augmentFixtureOptions{existingRaw: &testCase.raw},
			)
			err := fixture.plugin.Install(fixture.config)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("install error = %v, want %q", err, testCase.want)
			}
			raw, readErr := os.ReadFile(fixture.configPath)
			if readErr != nil || string(raw) != testCase.raw {
				t.Fatalf("original settings changed: %q, %v", raw, readErr)
			}
			assertNoAugmentRuntimeWrites(t, fixture)
		})
	}
}
