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

func TestGrokRegistryAndFactory(t *testing.T) {
	entry := SupportedAgents[grokAgentKey]
	if entry.Name != "Grok Build" || entry.ConfigDir != "~/.grok/hooks" || entry.ConfigFile != grokConfigFile {
		t.Fatalf("Grok registry entry = %#v", entry)
	}
	if _, ok := NewPlugin(grokAgentKey).(*GrokPlugin); !ok {
		t.Fatalf("Grok plugin factory returned %T", NewPlugin(grokAgentKey))
	}
}

func TestGrokGeneratedArgumentParsers(t *testing.T) {
	posixCommand := quotePOSIXArgument("/tmp/node's runtime") + " " +
		quotePOSIXArgument("/tmp/home's agent/guard.js")
	executable, next, ok := readGrokPOSIXArgument(posixCommand, 0)
	if !ok || executable != "/tmp/node's runtime" || posixCommand[next] != ' ' {
		t.Fatalf("parse POSIX executable = %q, %d, %v", executable, next, ok)
	}
	script, end, ok := readGrokPOSIXArgument(posixCommand, next+1)
	if !ok || script != "/tmp/home's agent/guard.js" || end != len(posixCommand) {
		t.Fatalf("parse POSIX script = %q, %d, %v", script, end, ok)
	}

	windowsCommand := quoteGrokWindowsArgument(`C:\Program Files\node.exe`) + " " +
		quoteGrokWindowsArgument(`C:\home with spaces\.elydora\agent-1\guard.js`)
	executable, next, ok = readGrokWindowsArgument(windowsCommand, 0)
	if !ok || executable != `C:\Program Files\node.exe` || windowsCommand[next] != ' ' {
		t.Fatalf("parse Windows executable = %q, %d, %v", executable, next, ok)
	}
	script, end, ok = readGrokWindowsArgument(windowsCommand, next+1)
	if !ok || script != `C:\home with spaces\.elydora\agent-1\guard.js` || end != len(windowsCommand) {
		t.Fatalf("parse Windows script = %q, %d, %v", script, end, ok)
	}
}

func TestGrokInstallPreservesConfigAndIsIdempotent(t *testing.T) {
	existing := `{
  "schemaVersion": 1,
  "hooks": {
    "SessionStart": [{"matcher":"startup","hooks":[{"type":"http","url":"https://example.test/hook","timeout":5,"headers":{"x":"keep"}}],"label":"keep"}],
    "PreToolUse": [{"matcher":"Bash","hooks":[{"type":"command","command":"existing-command","timeout":5}]}]
  }
}`
	fixture := prepareGrokFixture(t, grokFixtureOptions{existingRaw: &existing})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Grok hooks: %v", err)
	}
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("repeat Grok install: %v", err)
	}
	settings := readGrokTestObject(t, fixture.configPath)
	if settings["schemaVersion"] != float64(1) {
		t.Fatalf("schema version changed: %#v", settings["schemaVersion"])
	}
	hooks := requireObject(t, settings["hooks"])
	if len(requireArray(t, hooks["SessionStart"])) != 1 ||
		len(requireArray(t, hooks["PreToolUse"])) != 2 ||
		len(requireArray(t, hooks["PostToolUse"])) != 1 {
		t.Fatalf("unexpected hooks after repeat install: %#v", hooks)
	}
	guard := grokTestManagedHandler(t, settings, "PreToolUse", grokGuardScript)
	audit := grokTestManagedHandler(t, settings, "PostToolUse", grokAuditScript)
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
		if handler["type"] != "command" || handler["timeout"] != grokHookTimeout {
			t.Fatalf("unexpected handler: %#v", handler)
		}
	}
	runtimeConfig := readGrokTestObject(t, fixture.runtimeConfig)
	if runtimeConfig["agent_name"] != grokAgentKey {
		t.Fatalf("runtime agent name = %#v", runtimeConfig["agent_name"])
	}
	for _, path := range []string{
		filepath.Join(fixture.homeDir, ".claude", "settings.json"),
		filepath.Join(fixture.homeDir, ".cursor", "hooks.json"),
		filepath.Join(fixture.homeDir, ".grok", "hooks", grokConfigFile),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("compatibility or default config was written at %s: %v", path, err)
		}
	}
}

func TestGrokEmptyHomeOverrideUsesDefault(t *testing.T) {
	fixture := prepareGrokFixture(t, grokFixtureOptions{useDefaultHome: true})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Grok hooks: %v", err)
	}
	status, err := fixture.plugin.Status()
	if err != nil || !status.Installed {
		t.Fatalf("default-home status = %#v, %v", status, err)
	}
}

func TestGrokCommandsBlockAndForwardPayloadByteForByte(t *testing.T) {
	fixture := prepareGrokFixture(t, grokFixtureOptions{})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Grok hooks: %v", err)
	}
	capturePath := filepath.Join(t.TempDir(), "captured-event.json")
	captureJSON, err := json.Marshal(capturePath)
	if err != nil {
		t.Fatalf("marshal capture path: %v", err)
	}
	captureScript := "const fs = require('node:fs'); const chunks = []; " +
		"process.stdin.on('data', chunk => chunks.push(chunk)); " +
		"process.stdin.on('end', () => fs.writeFileSync(" + string(captureJSON) + ", Buffer.concat(chunks)));\n"
	if err := os.WriteFile(fixture.hookPath, []byte(captureScript), 0700); err != nil {
		t.Fatalf("write capture hook: %v", err)
	}
	settings := readGrokTestObject(t, fixture.configPath)
	prePayload := `{"hookEventName":"PreToolUse","sessionId":"session-1","cwd":"workspace","workspaceRoot":"workspace","toolName":"Bash","toolInput":{"command":"echo test"}}`
	guard := grokTestManagedHandler(t, settings, "PreToolUse", grokGuardScript)
	exitCode, stderr := runGrokCommand(t, guard["command"].(string), fixture.homeDir, prePayload)
	if exitCode != 2 || !strings.Contains(stderr, "Agent is frozen by Elydora") {
		t.Fatalf("guard exit = %d, stderr = %q", exitCode, stderr)
	}
	postPayload := `{"hookEventName":"PostToolUse","sessionId":"session-1","cwd":"workspace","workspaceRoot":"workspace","toolName":"Bash","toolInput":{"command":"echo test"},"toolResult":{"output":"test"}}`
	audit := grokTestManagedHandler(t, settings, "PostToolUse", grokAuditScript)
	exitCode, stderr = runGrokCommand(t, audit["command"].(string), fixture.homeDir, postPayload)
	if exitCode != 0 {
		t.Fatalf("audit exit = %d, stderr = %q", exitCode, stderr)
	}
	captured, err := os.ReadFile(capturePath)
	if err != nil || string(captured) != postPayload {
		t.Fatalf("captured payload = %q, %v", captured, err)
	}
}

func TestGrokStatusRequiresPairAndBothRuntimes(t *testing.T) {
	fixture := prepareGrokFixture(t, grokFixtureOptions{})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Grok hooks: %v", err)
	}
	status, err := fixture.plugin.Status()
	if err != nil || !status.Installed || !status.HookConfigured || !status.HookScriptExists {
		t.Fatalf("installed status = %#v, %v", status, err)
	}
	settings := readGrokTestObject(t, fixture.configPath)
	hooks := requireObject(t, settings["hooks"])
	delete(hooks, "PostToolUse")
	writeGrokTestObject(t, fixture.configPath, settings)
	status, err = fixture.plugin.Status()
	if err != nil || status.Installed || status.HookConfigured {
		t.Fatalf("incomplete status = %#v, %v", status, err)
	}
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("restore Grok hooks: %v", err)
	}
	if err := os.Remove(fixture.guardPath); err != nil {
		t.Fatalf("remove guard: %v", err)
	}
	status, err = fixture.plugin.Status()
	if err != nil || status.Installed || !status.HookConfigured || status.HookScriptExists {
		t.Fatalf("missing-runtime status = %#v, %v", status, err)
	}
}

func TestGrokUninstallPreservesMixedHandlersAndExactAgent(t *testing.T) {
	existing := `{"owner":"user","hooks":{"Notification":[]}}`
	fixture := prepareGrokFixture(t, grokFixtureOptions{existingRaw: &existing})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Grok hooks: %v", err)
	}
	settings := readGrokTestObject(t, fixture.configPath)
	hooks := requireObject(t, settings["hooks"])
	preGroups := requireArray(t, hooks["PreToolUse"])
	managedGroup := requireObject(t, preGroups[len(preGroups)-1])
	managedGroup["hooks"] = append(requireArray(t, managedGroup["hooks"]), map[string]any{
		"type": "command", "command": "user-command", "timeout": grokHookTimeout,
	})
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		t.Fatalf("resolve Node.js: %v", err)
	}
	preGroups = append(preGroups,
		map[string]any{"hooks": []any{buildGrokHandler(buildGrokCommand(nodePath, filepath.Join(fixture.agentDir, "guard.js.backup")))}},
		map[string]any{"hooks": []any{buildGrokHandler(buildGrokCommand(nodePath, filepath.Join(filepath.Dir(fixture.agentDir), "agent-10", grokGuardScript)))}},
	)
	hooks["PreToolUse"] = preGroups
	writeGrokTestObject(t, fixture.configPath, settings)
	uninstallID := grokTestAgentID
	if runtime.GOOS == "windows" {
		uninstallID = "AGENT-1"
	}
	if err := fixture.plugin.Uninstall(uninstallID); err != nil {
		t.Fatalf("uninstall Grok hooks: %v", err)
	}
	remaining := readGrokTestObject(t, fixture.configPath)
	remainingHooks := requireObject(t, remaining["hooks"])
	if remaining["owner"] != "user" || len(requireArray(t, remainingHooks["PreToolUse"])) != 3 {
		t.Fatalf("user config changed: %#v", remaining)
	}
	if !reflect.DeepEqual(requireArray(t, remainingHooks["Notification"]), []any{}) {
		t.Fatalf("empty notification changed: %#v", remainingHooks["Notification"])
	}
	raw, _ := os.ReadFile(fixture.configPath)
	if !strings.Contains(string(raw), "guard.js.backup") || !strings.Contains(string(raw), "agent-10") {
		t.Fatalf("similar handlers were removed: %s", raw)
	}
	if _, exists := remainingHooks["PostToolUse"]; exists {
		t.Fatalf("managed PostToolUse remains: %#v", remainingHooks["PostToolUse"])
	}
}

func TestGrokInstallReplacesStaleHandlersForEveryAgent(t *testing.T) {
	fixture := prepareGrokFixture(t, grokFixtureOptions{})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Grok hooks: %v", err)
	}
	settings := readGrokTestObject(t, fixture.configPath)
	hooks := requireObject(t, settings["hooks"])
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		t.Fatalf("resolve Node.js: %v", err)
	}
	for _, contract := range []struct{ event, script string }{
		{"PreToolUse", grokGuardScript}, {"PostToolUse", grokAuditScript},
	} {
		hooks[contract.event] = append(requireArray(t, hooks[contract.event]), map[string]any{
			"hooks": []any{buildGrokHandler(buildGrokCommand(
				nodePath, filepath.Join(filepath.Dir(fixture.agentDir), "agent-old", contract.script),
			))},
		})
	}
	writeGrokTestObject(t, fixture.configPath, settings)
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("replace stale Grok hooks: %v", err)
	}
	raw, _ := os.ReadFile(fixture.configPath)
	current := readGrokTestObject(t, fixture.configPath)
	currentHooks := requireObject(t, current["hooks"])
	if strings.Contains(string(raw), "agent-old") ||
		len(requireArray(t, currentHooks["PreToolUse"])) != 1 ||
		len(requireArray(t, currentHooks["PostToolUse"])) != 1 {
		t.Fatalf("stale hooks remain: %s", raw)
	}
}

func TestGrokUninstallPreservesUntouchedEmptyGroup(t *testing.T) {
	existing := `{"owner":"user"}`
	fixture := prepareGrokFixture(t, grokFixtureOptions{existingRaw: &existing})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Grok hooks: %v", err)
	}
	settings := readGrokTestObject(t, fixture.configPath)
	hooks := requireObject(t, settings["hooks"])
	hooks["PreToolUse"] = []any{map[string]any{"hooks": []any{}}}
	writeGrokTestObject(t, fixture.configPath, settings)
	if err := fixture.plugin.Uninstall(grokTestAgentID); err != nil {
		t.Fatalf("uninstall Grok hook: %v", err)
	}
	remaining := requireObject(t, readGrokTestObject(t, fixture.configPath)["hooks"])
	preGroups := requireArray(t, remaining["PreToolUse"])
	if len(preGroups) != 1 || len(requireArray(t, requireObject(t, preGroups[0])["hooks"])) != 0 {
		t.Fatalf("untouched empty group changed: %#v", remaining)
	}
	if _, exists := remaining["PostToolUse"]; exists {
		t.Fatalf("managed event remains: %#v", remaining)
	}
}

func TestGrokUninstallRemovesOwnedConfig(t *testing.T) {
	fixture := prepareGrokFixture(t, grokFixtureOptions{})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Grok hooks: %v", err)
	}
	if err := fixture.plugin.Uninstall(grokTestAgentID); err != nil {
		t.Fatalf("uninstall Grok hooks: %v", err)
	}
	if _, err := os.Stat(fixture.configPath); !os.IsNotExist(err) {
		t.Fatalf("owned Grok config remains: %v", err)
	}
}

func TestGrokMalformedConfigAndShapesPreventRuntimeWrites(t *testing.T) {
	for _, testCase := range []struct{ name, raw, want string }{
		{"malformed", "{ malformed", "parse Grok hooks config"},
		{"null-root", "null", "must contain a JSON object"},
		{"null-hooks", `{"hooks":null}`, `field "hooks" must be an object`},
		{"null-event", `{"hooks":{"PreToolUse":null}}`, `field "hooks.PreToolUse" must be an array`},
		{"null-group", `{"hooks":{"PreToolUse":[null]}}`, "must be an object"},
		{"null-matcher", `{"hooks":{"PreToolUse":[{"matcher":null,"hooks":[]}]}}`, "matcher must be a string"},
		{"null-handlers", `{"hooks":{"PreToolUse":[{"hooks":null}]}}`, "must contain a hooks array"},
		{"empty-command", `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":""}]}]}}`, "requires a non-empty command"},
		{"object-type", `{"hooks":{"PreToolUse":[{"hooks":[{"type":{},"command":"x"}]}]}}`, "unsupported type"},
		{"empty-url", `{"hooks":{"PreToolUse":[{"hooks":[{"type":"http","url":""}]}]}}`, "requires a non-empty url"},
		{"zero-timeout", `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"x","timeout":0}]}]}}`, "positive finite number"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := prepareGrokFixture(t, grokFixtureOptions{existingRaw: &testCase.raw})
			err := fixture.plugin.Install(fixture.config)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("install error = %v, want %q", err, testCase.want)
			}
			raw, readErr := os.ReadFile(fixture.configPath)
			if readErr != nil || string(raw) != testCase.raw {
				t.Fatalf("original config changed: %q, %v", raw, readErr)
			}
			for _, path := range []string{fixture.hookPath, fixture.runtimeConfig, fixture.privateKey} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("runtime write occurred at %s: %v", path, err)
				}
			}
		})
	}
}

func TestGrokMissingGuardPreventsWrites(t *testing.T) {
	fixture := prepareGrokFixture(t, grokFixtureOptions{withoutGuard: true})
	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "guard runtime is missing") {
		t.Fatalf("install error = %v", err)
	}
	for _, path := range []string{fixture.configPath, fixture.hookPath, fixture.runtimeConfig, fixture.privateKey} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("write occurred at %s: %v", path, err)
		}
	}
}

func TestGrokStatusSurfacesMalformedRuntimeMetadata(t *testing.T) {
	fixture := prepareGrokFixture(t, grokFixtureOptions{})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Grok hooks: %v", err)
	}
	if err := os.WriteFile(fixture.runtimeConfig, []byte("{ malformed"), 0600); err != nil {
		t.Fatalf("corrupt runtime config: %v", err)
	}
	if _, err := fixture.plugin.Status(); err == nil || !strings.Contains(err.Error(), "parse Elydora runtime config") {
		t.Fatalf("status error = %v", err)
	}
}

func TestGrokAtomicWritesLeaveNoTemporaryFiles(t *testing.T) {
	fixture := prepareGrokFixture(t, grokFixtureOptions{})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Grok hooks: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(fixture.configPath))
	if err != nil {
		t.Fatalf("read Grok config directory: %v", err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Fatalf("temporary file remains: %s", entry.Name())
		}
	}
}
