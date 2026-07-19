package plugins

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDroidRegistryUsesOfficialGlobalHookSource(t *testing.T) {
	entry := SupportedAgents[droidAgentKey]
	if entry.Name != "Factory Droid" || entry.ConfigDir != "~/.factory" || entry.ConfigFile != "hooks.json" {
		t.Fatalf("Factory Droid registry entry = %#v", entry)
	}
	if _, ok := NewPlugin(droidAgentKey).(*DroidPlugin); !ok {
		t.Fatal("Factory Droid plugin is not registered")
	}
}

func TestDroidInstallPreservesJSONCAndUsesPerEventPrecedence(t *testing.T) {
	hooks := `{
  // root hook source
  "PreToolUse": [
    // keep root group comment
    { "matcher": "Read", "hooks": [{ "type": "command", "command": "root-user" }] }
  ],
  "Notification": []
}
`
	settings := `{
  // general setting
  "theme": "dark",
  "hooks": {
    // settings fallback event
    "PostToolUse": [
      // keep settings group comment
      { "matcher": "Edit", "hooks": [{ "type": "command", "command": "settings-user" }] }
    ],
    "showHookOutput": true,
  },
}
`
	fixture := prepareDroidFixture(t, droidFixtureOptions{
		hooks: droidString(hooks), settings: droidString(settings),
	})
	installDroidFixture(t, fixture)
	firstRoot := readDroidTestFile(t, fixture.configPath)
	firstSettings := readDroidTestFile(t, fixture.settingsPath)
	installDroidFixture(t, fixture)

	if readDroidTestFile(t, fixture.configPath) != firstRoot ||
		readDroidTestFile(t, fixture.settingsPath) != firstSettings {
		t.Fatal("idempotent install changed Factory Droid hook sources")
	}
	for _, marker := range []string{"root hook source", "keep root group comment"} {
		if !strings.Contains(firstRoot, marker) {
			t.Fatalf("root JSONC comment %q was lost", marker)
		}
	}
	for _, marker := range []string{"general setting", "settings fallback event", "keep settings group comment"} {
		if !strings.Contains(firstSettings, marker) {
			t.Fatalf("settings JSONC comment %q was lost", marker)
		}
	}
	root := readDroidTestObject(t, fixture.configPath)
	userSettings := readDroidTestObject(t, fixture.settingsPath)
	preGroups := requireDroidArray(t, root["PreToolUse"])
	if len(preGroups) != 2 || requireDroidObject(t, requireDroidArray(t,
		requireDroidObject(t, preGroups[0])["hooks"])[0])["command"] != "root-user" {
		t.Fatalf("root PreToolUse groups = %#v", preGroups)
	}
	if _, exists := root["PostToolUse"]; exists {
		t.Fatalf("PostToolUse leaked into root source: %#v", root)
	}
	if userSettings["theme"] != "dark" {
		t.Fatalf("settings theme = %#v", userSettings["theme"])
	}
	settingsHooks := requireDroidObject(t, userSettings["hooks"])
	postGroups := requireDroidArray(t, settingsHooks["PostToolUse"])
	if len(postGroups) != 2 {
		t.Fatalf("settings PostToolUse groups = %#v", postGroups)
	}
	if _, exists := settingsHooks["PreToolUse"]; exists {
		t.Fatalf("PreToolUse leaked into settings source: %#v", settingsHooks)
	}
	for _, item := range []struct {
		groups any
		path   string
	}{{preGroups, fixture.guardPath}, {postGroups, fixture.hookPath}} {
		handler := droidManagedHandler(t, item.groups, item.path)
		if len(handler) != 3 || handler["type"] != "command" || handler["timeout"] != float64(10) {
			t.Fatalf("managed handler = %#v", handler)
		}
	}
	requireMissingDroidFile(t, filepath.Join(fixture.workspaceDir, ".factory", "hooks.json"))
}

func TestDroidInstallKeepsActiveLegacySource(t *testing.T) {
	fixture := prepareDroidFixture(t, droidFixtureOptions{
		legacyHooks: droidJSON(map[string]any{"PreToolUse": []any{}}),
		settings:    droidJSON(map[string]any{"hooks": map[string]any{"PostToolUse": []any{}}}),
	})
	installDroidFixture(t, fixture)
	requireMissingDroidFile(t, fixture.configPath)
	droidManagedHandler(t, readDroidTestObject(t, fixture.legacyPath)["PreToolUse"], fixture.guardPath)
	settings := readDroidTestObject(t, fixture.settingsPath)
	droidManagedHandler(t, requireDroidObject(t, settings["hooks"])["PostToolUse"], fixture.hookPath)
}

func TestDroidInstallReusesSettingsContainerAndFormatting(t *testing.T) {
	source := "{\r\n\t\"owner\": \"user\",\r\n\t\"hooks\": {}\r\n}\r\n"
	fixture := prepareDroidFixture(t, droidFixtureOptions{settings: droidString(source)})
	installDroidFixture(t, fixture)
	requireMissingDroidFile(t, fixture.configPath)
	raw := readDroidTestFile(t, fixture.settingsPath)
	if !strings.Contains(raw, "\r\n\t\t\"PreToolUse\"") ||
		!strings.Contains(raw, "\r\n\t\t\"PostToolUse\"") {
		t.Fatalf("settings formatting was not preserved:\n%s", raw)
	}
	if readDroidTestObject(t, fixture.settingsPath)["owner"] != "user" {
		t.Fatal("settings owner was lost")
	}
}

func TestDroidInstallReportsEventSourcesAndReviewCommand(t *testing.T) {
	fixture := prepareDroidFixture(t, droidFixtureOptions{
		hooks:    droidJSON(map[string]any{"PreToolUse": []any{}}),
		settings: droidJSON(map[string]any{"hooks": map[string]any{"PostToolUse": []any{}}}),
	})
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create output pipe: %v", err)
	}
	original := os.Stdout
	os.Stdout = writer
	installErr := fixture.plugin.Install(fixture.config)
	os.Stdout = original
	closeErr := writer.Close()
	output, readErr := io.ReadAll(reader)
	_ = reader.Close()
	if installErr != nil || closeErr != nil || readErr != nil {
		t.Fatalf("capture install output: install=%v close=%v read=%v", installErr, closeErr, readErr)
	}
	text := string(output)
	for _, marker := range []string{fixture.configPath, fixture.settingsPath, "PreToolUse", "PostToolUse", "/hooks"} {
		if !strings.Contains(text, marker) {
			t.Fatalf("install output is missing %q: %s", marker, text)
		}
	}
}

func TestDroidCommandsBlockAndForwardOfficialInputByteForByte(t *testing.T) {
	fixture := prepareDroidFixture(t, droidFixtureOptions{})
	installDroidFixture(t, fixture)
	capturePath := filepath.Join(t.TempDir(), "captured-event.json")
	encodedPath, err := json.Marshal(capturePath)
	if err != nil {
		t.Fatalf("marshal capture path: %v", err)
	}
	captureScript := "const fs = require('node:fs'); const chunks = []; " +
		"process.stdin.on('data', chunk => chunks.push(chunk)); " +
		"process.stdin.on('end', () => fs.writeFileSync(" + string(encodedPath) + ", Buffer.concat(chunks)));\n"
	if err := os.WriteFile(fixture.hookPath, []byte(captureScript), 0700); err != nil {
		t.Fatalf("write capture audit runtime: %v", err)
	}
	root := readDroidTestObject(t, fixture.configPath)
	guard := droidManagedHandler(t, root["PreToolUse"], fixture.guardPath)
	audit := droidManagedHandler(t, root["PostToolUse"], fixture.hookPath)
	prePayload := `{"session_id":"session-1","cwd":"C:/workspace","permission_mode":"auto-high","hook_event_name":"PreToolUse","tool_name":"Execute","tool_input":{"command":"echo test"}}`
	guardResult := runDroidCommand(t, guard["command"].(string), fixture.homeDir, prePayload)
	if guardResult.exitCode != 2 || !strings.Contains(guardResult.stderr, "Agent is frozen by Elydora") {
		t.Fatalf("guard result = %#v", guardResult)
	}
	postPayload := "{\n  \"session_id\": \"session-1\",\n  \"hook_event_name\": \"PostToolUse\",\n  \"tool_response\": {\"success\": true}\n}"
	auditResult := runDroidCommand(t, audit["command"].(string), fixture.homeDir, postPayload)
	if auditResult.exitCode != 0 {
		t.Fatalf("audit result = %#v", auditResult)
	}
	if readDroidTestFile(t, capturePath) != postPayload {
		t.Fatal("audit hook changed the official Factory Droid payload")
	}
}

func TestDroidStatusRequiresEnabledPairAndRuntimeFiles(t *testing.T) {
	fixture := prepareDroidFixture(t, droidFixtureOptions{
		hooks:    droidJSON(map[string]any{"PreToolUse": []any{}}),
		settings: droidJSON(map[string]any{"hooks": map[string]any{"PostToolUse": []any{}}}),
	})
	installDroidFixture(t, fixture)
	status, err := fixture.plugin.Status()
	if err != nil || !status.Installed || !status.HookConfigured || !status.HookScriptExists {
		t.Fatalf("installed status = %#v, %v", status, err)
	}
	if err := os.Remove(fixture.hookPath); err != nil {
		t.Fatalf("remove audit runtime: %v", err)
	}
	status, err = fixture.plugin.Status()
	if err != nil || status.Installed || !status.HookConfigured || status.HookScriptExists {
		t.Fatalf("missing-runtime status = %#v, %v", status, err)
	}
	root := readDroidTestObject(t, fixture.configPath)
	root["hooksDisabled"] = true
	disabled, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		t.Fatalf("encode disabled hooks: %v", err)
	}
	if err := os.WriteFile(fixture.configPath, append(disabled, '\n'), 0600); err != nil {
		t.Fatalf("disable hooks: %v", err)
	}
	status, err = fixture.plugin.Status()
	if err != nil || status.Installed || status.HookConfigured {
		t.Fatalf("disabled status = %#v, %v", status, err)
	}
}

func TestDroidUninstallPreservesUserSourcesAndExactOwnership(t *testing.T) {
	hooks := "{\n  // keep root comment\n  \"PreToolUse\": []\n}\n"
	settings := "{\n  \"theme\": \"dark\",\n  \"hooks\": {\n" +
		"    // keep settings comment\n    \"PostToolUse\": []\n  }\n}\n"
	fixture := prepareDroidFixture(t, droidFixtureOptions{
		hooks: droidString(hooks), settings: droidString(settings),
	})
	installDroidFixture(t, fixture)
	if err := fixture.plugin.Uninstall("agent-10"); err != nil {
		t.Fatalf("uninstall lookalike agent: %v", err)
	}
	droidManagedHandler(t, readDroidTestObject(t, fixture.configPath)["PreToolUse"], fixture.guardPath)
	agentID := droidTestAgentID
	if runtime.GOOS == "windows" {
		agentID = "AGENT-1"
	}
	if err := fixture.plugin.Uninstall(agentID); err != nil {
		t.Fatalf("uninstall Factory Droid hooks: %v", err)
	}
	rootRaw := readDroidTestFile(t, fixture.configPath)
	settingsRaw := readDroidTestFile(t, fixture.settingsPath)
	if !strings.Contains(rootRaw, "keep root comment") || !strings.Contains(settingsRaw, "keep settings comment") {
		t.Fatal("uninstall removed user JSONC comments")
	}
	if len(requireDroidArray(t, readDroidTestObject(t, fixture.configPath)["PreToolUse"])) != 0 {
		t.Fatal("uninstall removed the user-owned empty event key")
	}
	currentSettings := readDroidTestObject(t, fixture.settingsPath)
	if currentSettings["theme"] != "dark" ||
		len(requireDroidArray(t, requireDroidObject(t, currentSettings["hooks"])["PostToolUse"])) != 0 {
		t.Fatalf("settings after uninstall = %#v", currentSettings)
	}
}

func TestDroidUninstallDeletesOnlyOwnedEmptyHookFile(t *testing.T) {
	fixture := prepareDroidFixture(t, droidFixtureOptions{})
	installDroidFixture(t, fixture)
	if !strings.HasPrefix(readDroidTestFile(t, fixture.configPath), droidOwnedFileMarker) {
		t.Fatal("created hook source is missing its ownership marker")
	}
	if err := fixture.plugin.Uninstall(droidTestAgentID); err != nil {
		t.Fatalf("uninstall Factory Droid hooks: %v", err)
	}
	requireMissingDroidFile(t, fixture.configPath)
}
