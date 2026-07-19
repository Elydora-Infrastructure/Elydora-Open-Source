package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestAugmentRegistryAndFactory(t *testing.T) {
	entry := SupportedAgents[augmentAgentKey]
	if entry.Name != "Augment Code CLI" ||
		entry.ConfigDir != "~/.augment" ||
		entry.ConfigFile != "settings.json" {
		t.Fatalf("Auggie registry entry = %#v", entry)
	}
	if _, ok := NewPlugin(augmentAgentKey).(*AugmentPlugin); !ok {
		t.Fatalf("Auggie plugin factory returned %T", NewPlugin(augmentAgentKey))
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
		t, augmentFixtureOptions{existingRaw: &existing},
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
	preGroups := requireArray(t, hooks["PreToolUse"])
	managedGroup := requireObject(t, preGroups[len(preGroups)-1])
	if managedGroup["matcher"] != ".*" {
		t.Fatalf("managed matcher = %#v", managedGroup["matcher"])
	}
	guard := augmentTestManagedHandler(
		t, settings, "PreToolUse", fixture.guardWrapper,
	)
	audit := augmentTestManagedHandler(
		t, settings, "PostToolUse", fixture.auditWrapper,
	)
	for _, handler := range []map[string]any{guard, audit} {
		wantKeys := map[string]struct{}{"type": {}, "command": {}, "timeout": {}}
		if len(handler) != len(wantKeys) {
			t.Fatalf("handler has extra fields: %#v", handler)
		}
		for key := range handler {
			if _, exists := wantKeys[key]; !exists {
				t.Fatalf("handler has unexpected field %q", key)
			}
		}
		if handler["type"] != "command" || handler["timeout"] != augmentHookTimeout {
			t.Fatalf("unexpected handler: %#v", handler)
		}
	}
	guardWrapper, err := os.ReadFile(fixture.guardWrapper)
	if err != nil || !strings.Contains(string(guardWrapper), augmentGuardScript) {
		t.Fatalf("guard wrapper = %q, %v", guardWrapper, err)
	}
	auditWrapper, err := os.ReadFile(fixture.auditWrapper)
	if err != nil || !strings.Contains(string(auditWrapper), augmentAuditScript) {
		t.Fatalf("audit wrapper = %q, %v", auditWrapper, err)
	}
	if runtime.GOOS == "windows" {
		if !strings.HasPrefix(string(guardWrapper), "@echo off\r\n") ||
			!strings.Contains(string(guardWrapper), "exit /b %errorlevel%") {
			t.Fatalf("invalid Windows wrapper: %q", guardWrapper)
		}
	} else {
		info, statErr := os.Stat(fixture.guardWrapper)
		if statErr != nil || info.Mode().Perm()&0100 == 0 ||
			!strings.HasPrefix(string(guardWrapper), "#!/bin/sh\nexec ") {
			t.Fatalf("invalid POSIX wrapper: %q, %#v, %v", guardWrapper, info, statErr)
		}
	}
	runtimeConfig := readAugmentTestObject(t, fixture.runtimeConfig)
	if runtimeConfig["agent_name"] != augmentAgentKey {
		t.Fatalf("runtime agent name = %#v", runtimeConfig["agent_name"])
	}
	for _, path := range []string{
		filepath.Join(fixture.workspaceDir, ".augment", "settings.json"),
		filepath.Join(fixture.workspaceDir, ".augment", "settings.local.json"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("workspace settings written at %s: %v", path, err)
		}
	}
}

func TestAugmentWrappersBlockAndForwardPayloadByteForByte(t *testing.T) {
	fixture := prepareAugmentFixture(t, augmentFixtureOptions{})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Auggie hooks: %v", err)
	}
	capturePath := filepath.Join(t.TempDir(), "captured-event.json")
	captureJSON, err := json.Marshal(capturePath)
	if err != nil {
		t.Fatalf("marshal capture path: %v", err)
	}
	captureScript := "const fs = require('node:fs'); const chunks = []; " +
		"process.stdin.on('data', chunk => chunks.push(chunk)); " +
		"process.stdin.on('end', () => fs.writeFileSync(" +
		string(captureJSON) + ", Buffer.concat(chunks)));\n"
	if err := os.WriteFile(fixture.hookPath, []byte(captureScript), 0700); err != nil {
		t.Fatalf("write capture hook: %v", err)
	}
	settings := readAugmentTestObject(t, fixture.configPath)
	prePayload := `{"hook_event_name":"PreToolUse","conversation_id":"conversation-1","workspace_roots":["workspace"],"tool_name":"launch-process","tool_input":{"command":"echo test"},"is_mcp_tool":false}`
	guard := augmentTestManagedHandler(
		t, settings, "PreToolUse", fixture.guardWrapper,
	)
	exitCode, stderr := runAugmentCommand(
		t, guard["command"].(string), fixture.homeDir, prePayload,
	)
	if exitCode != 2 || !strings.Contains(stderr, "Agent is frozen by Elydora") {
		t.Fatalf("guard exit = %d, stderr = %q", exitCode, stderr)
	}
	postPayload := `{"hook_event_name":"PostToolUse","conversation_id":"conversation-1","workspace_roots":["workspace"],"tool_name":"launch-process","tool_input":{"command":"echo test"},"tool_output":"test","is_mcp_tool":false}`
	audit := augmentTestManagedHandler(
		t, settings, "PostToolUse", fixture.auditWrapper,
	)
	exitCode, stderr = runAugmentCommand(
		t, audit["command"].(string), fixture.homeDir, postPayload,
	)
	if exitCode != 0 {
		t.Fatalf("audit exit = %d, stderr = %q", exitCode, stderr)
	}
	captured, err := os.ReadFile(capturePath)
	if err != nil || string(captured) != postPayload {
		t.Fatalf("captured payload = %q, %v", captured, err)
	}
}

func TestAugmentStatusRequiresPairCoreRuntimesAndWrappers(t *testing.T) {
	fixture := prepareAugmentFixture(t, augmentFixtureOptions{})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Auggie hooks: %v", err)
	}
	status, err := fixture.plugin.Status()
	if err != nil || !status.Installed || !status.HookConfigured ||
		!status.HookScriptExists {
		t.Fatalf("installed status = %#v, %v", status, err)
	}
	if err := os.Remove(fixture.guardWrapper); err != nil {
		t.Fatalf("remove guard wrapper: %v", err)
	}
	status, err = fixture.plugin.Status()
	if err != nil || status.Installed || !status.HookConfigured ||
		status.HookScriptExists {
		t.Fatalf("missing-wrapper status = %#v, %v", status, err)
	}
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("restore Auggie hooks: %v", err)
	}
	if err := os.Remove(fixture.hookPath); err != nil {
		t.Fatalf("remove hook runtime: %v", err)
	}
	status, err = fixture.plugin.Status()
	if err != nil || status.Installed || status.HookScriptExists {
		t.Fatalf("missing-runtime status = %#v, %v", status, err)
	}
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("restore Auggie hooks: %v", err)
	}
	settings := readAugmentTestObject(t, fixture.configPath)
	delete(requireObject(t, settings["hooks"]), "PostToolUse")
	writeAugmentTestObject(t, fixture.configPath, settings)
	status, err = fixture.plugin.Status()
	if err != nil || status.Installed || status.HookConfigured {
		t.Fatalf("incomplete-pair status = %#v, %v", status, err)
	}
}

func TestAugmentStatusSkipsIncompleteEarlierContract(t *testing.T) {
	fixture := prepareAugmentFixture(t, augmentFixtureOptions{})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Auggie hooks: %v", err)
	}
	settings := readAugmentTestObject(t, fixture.configPath)
	hooks := requireObject(t, settings["hooks"])
	for _, item := range []struct{ event, wrapper string }{
		{"PreToolUse", augmentGuardWrapperName()},
		{"PostToolUse", augmentAuditWrapperName()},
	} {
		hooks[item.event] = append(
			requireArray(t, hooks[item.event]),
			map[string]any{"hooks": []any{buildAugmentHandler(filepath.Join(
				filepath.Dir(fixture.agentDir), "agent-0", item.wrapper,
			))}},
		)
	}
	writeAugmentTestObject(t, fixture.configPath, settings)
	status, err := fixture.plugin.Status()
	if err != nil || !status.Installed {
		t.Fatalf("status with earlier incomplete contract = %#v, %v", status, err)
	}
}

func TestAugmentUninstallPreservesExactUserOwnership(t *testing.T) {
	existing := `{"owner":"user","hooks":{"Notification":[]}}`
	fixture := prepareAugmentFixture(
		t, augmentFixtureOptions{existingRaw: &existing},
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
	preGroups = append(preGroups,
		map[string]any{"hooks": []any{buildAugmentHandler(
			fixture.guardWrapper + ".backup",
		)}},
		map[string]any{"hooks": []any{buildAugmentHandler(filepath.Join(
			filepath.Dir(fixture.agentDir), "agent-10", augmentGuardWrapperName(),
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
	if !reflect.DeepEqual(requireArray(t, remainingHooks["Notification"]), []any{}) {
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

func TestAugmentInstallReplacesStaleAndPreservesEmptyGroups(t *testing.T) {
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
				filepath.Dir(fixture.agentDir), "agent-old", item.wrapper,
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
	if _, err := os.Stat(fixture.configPath); !os.IsNotExist(err) {
		t.Fatalf("owned Auggie settings remain: %v", err)
	}
}

func TestAugmentMalformedSettingsPreventRuntimeWrites(t *testing.T) {
	for _, testCase := range []struct{ name, raw, want string }{
		{"malformed", "{ malformed", "parse Auggie settings"},
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
				t, augmentFixtureOptions{existingRaw: &testCase.raw},
			)
			err := fixture.plugin.Install(fixture.config)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("install error = %v, want %q", err, testCase.want)
			}
			raw, readErr := os.ReadFile(fixture.configPath)
			if readErr != nil || string(raw) != testCase.raw {
				t.Fatalf("original settings changed: %q, %v", raw, readErr)
			}
			for _, path := range []string{
				fixture.hookPath,
				fixture.runtimeConfig,
				fixture.privateKey,
				fixture.guardWrapper,
				fixture.auditWrapper,
			} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("runtime write occurred at %s: %v", path, err)
				}
			}
		})
	}
}

func TestAugmentMissingGuardPreventsWrites(t *testing.T) {
	fixture := prepareAugmentFixture(
		t, augmentFixtureOptions{withoutGuard: true},
	)
	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "guard runtime is missing") {
		t.Fatalf("install error = %v", err)
	}
	for _, path := range []string{
		fixture.configPath,
		fixture.hookPath,
		fixture.runtimeConfig,
		fixture.privateKey,
		fixture.guardWrapper,
		fixture.auditWrapper,
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("write occurred at %s: %v", path, err)
		}
	}
}

func TestAugmentStatusSurfacesMalformedRuntimeMetadata(t *testing.T) {
	fixture := prepareAugmentFixture(t, augmentFixtureOptions{})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Auggie hooks: %v", err)
	}
	if err := os.WriteFile(
		fixture.runtimeConfig, []byte("{ malformed"), 0600,
	); err != nil {
		t.Fatalf("corrupt runtime config: %v", err)
	}
	if _, err := fixture.plugin.Status(); err == nil ||
		!strings.Contains(err.Error(), "parse Elydora runtime config") {
		t.Fatalf("status error = %v", err)
	}
}

func TestAugmentAtomicWritesLeaveNoTemporaryFiles(t *testing.T) {
	fixture := prepareAugmentFixture(t, augmentFixtureOptions{})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Auggie hooks: %v", err)
	}
	for _, directory := range []string{
		fixture.agentDir, filepath.Dir(fixture.configPath),
	} {
		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Fatalf("read directory %s: %v", directory, err)
		}
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".tmp") {
				t.Fatalf("temporary file remains: %s", entry.Name())
			}
		}
	}
}
