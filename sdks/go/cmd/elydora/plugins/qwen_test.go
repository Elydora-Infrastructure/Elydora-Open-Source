package plugins

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestQwenRegistryAndRuntimeOwnership(t *testing.T) {
	entry := SupportedAgents[qwenAgentKey]
	if entry.Name != "Qwen Code" || entry.ConfigDir != "~/.qwen" ||
		entry.ConfigFile != "settings.json" {
		t.Fatalf("Qwen Code registry entry = %#v", entry)
	}
	plugin, ok := NewPlugin(qwenAgentKey).(*QwenPlugin)
	if !ok {
		t.Fatal("Qwen Code plugin is not registered")
	}
	if !plugin.ManagesGuardRuntime() {
		t.Fatal("Qwen Code plugin must own its runtime transaction")
	}
	if _, ok := any(plugin).(InstallPreflighter); !ok {
		t.Fatal("Qwen Code plugin must preflight before CLI writes")
	}
}

func TestQwenInstallPreservesSourcesAndIsIdempotent(t *testing.T) {
	settings := "{\r\n" +
		"  // Keep this user preference.\r\n" +
		"  \"theme\": \"GitHub\",\r\n" +
		"  \"hooks\": {\r\n" +
		"    \"FutureEvent\": { \"opaque\": true },\r\n" +
		"    \"SessionStart\": [{ \"hooks\": [{ \"type\": \"command\", \"command\": \"session-hook\" }] }],\r\n" +
		"    \"PreToolUse\": [{ \"matcher\": \"read_file\", \"hooks\": [{ \"type\": \"command\", \"command\": \"user-hook\" }] }]\r\n" +
		"  }\r\n" +
		"}\r\n"
	fixture := prepareQwenFixture(t, qwenFixtureOptions{settings: qwenString(settings)})
	writeOptionalQwenFile(
		t,
		fixture.workspaceConfig,
		qwenString("{ \"owner\": \"workspace\" }\n"),
	)
	workspaceBefore := readQwenTestFile(t, fixture.workspaceConfig)
	installQwenFixture(t, fixture)
	firstSettings := readQwenTestFile(t, fixture.configPath)
	firstGuard := readQwenTestFile(t, fixture.guardPath)
	firstAudit := readQwenTestFile(t, fixture.hookPath)
	installQwenFixture(t, fixture)
	if readQwenTestFile(t, fixture.configPath) != firstSettings ||
		readQwenTestFile(t, fixture.guardPath) != firstGuard ||
		readQwenTestFile(t, fixture.hookPath) != firstAudit {
		t.Fatal("idempotent install changed Qwen Code installation")
	}
	if !strings.Contains(firstSettings, "Keep this user preference") ||
		!strings.Contains(firstSettings, "\r\n") ||
		!strings.Contains(firstSettings, "FutureEvent") {
		t.Fatalf("settings source fidelity was lost:\n%s", firstSettings)
	}
	root := readQwenTestObject(t, fixture.configPath)
	if root["theme"] != "GitHub" {
		t.Fatalf("theme = %#v", root["theme"])
	}
	hooks := requireQwenObject(t, root["hooks"])
	if len(requireQwenArray(t, hooks["SessionStart"])) != 1 ||
		len(requireQwenArray(t, hooks["PreToolUse"])) != 2 ||
		len(requireQwenArray(t, hooks["PostToolUse"])) != 1 ||
		len(requireQwenArray(t, hooks["PostToolUseFailure"])) != 1 {
		t.Fatalf("Qwen hooks = %#v", hooks)
	}
	for _, item := range []struct {
		event, path, name string
	}{
		{"PreToolUse", fixture.guardPath, qwenGuardHookName},
		{"PostToolUse", fixture.hookPath, qwenAuditHookName},
		{"PostToolUseFailure", fixture.hookPath, qwenAuditHookName},
	} {
		group := qwenManagedGroup(t, root, item.event, item.path)
		if len(group) != 1 {
			t.Fatalf("managed group = %#v", group)
		}
		handler := qwenManagedHandler(t, root, item.event, item.path)
		if len(handler) != 5 || handler["type"] != "command" ||
			handler["name"] != item.name || handler["shell"] != qwenExpectedShell() ||
			handler["timeout"] != qwenHookTimeout {
			t.Fatalf("managed handler = %#v", handler)
		}
	}
	if readQwenTestFile(t, fixture.workspaceConfig) != workspaceBefore {
		t.Fatal("workspace settings changed")
	}
	if firstGuard != generateGuardScript(qwenAgentKey, qwenTestAgentID, "", false, "") {
		t.Fatal("guard runtime differs from the canonical generator")
	}
	if firstAudit != buildHookScriptWithOutput(
		qwenAgentKey,
		qwenTestAgentID,
		"",
		false,
		true,
	) {
		t.Fatal("audit runtime differs from the native-payload generator")
	}
	runtimeConfig := readQwenTestObject(t, fixture.runtimeConfig)
	if runtimeConfig["agent_name"] != qwenAgentKey ||
		runtimeConfig["agent_id"] != qwenTestAgentID ||
		runtimeConfig["base_url"] != fixture.config.BaseURL {
		t.Fatalf("runtime config = %#v", runtimeConfig)
	}
}

func TestQwenInstallReportsOfficialReviewCommand(t *testing.T) {
	fixture := prepareQwenFixture(t, qwenFixtureOptions{})
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
		t.Fatalf(
			"capture install output: install=%v close=%v read=%v",
			installErr,
			closeErr,
			readErr,
		)
	}
	text := string(output)
	for _, marker := range []string{fixture.configPath, "/hooks"} {
		if !strings.Contains(text, marker) {
			t.Fatalf("install output is missing %q: %s", marker, text)
		}
	}
}

func legacyQwenGroupForTest(
	t *testing.T,
	event, scriptPath string,
) map[string]any {
	t.Helper()
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		t.Fatalf("resolve Node.js: %v", err)
	}
	return map[string]any{
		"matcher": "*",
		"hooks": []any{map[string]any{
			"type":    "command",
			"command": buildQwenCommand(nodePath, scriptPath),
			"shell":   qwenExpectedShell(),
			"timeout": qwenHookTimeout,
		}},
		"event": event,
	}
}

func legacyQwenGroupValue(t *testing.T, scriptPath string) map[string]any {
	t.Helper()
	group := legacyQwenGroupForTest(t, "", scriptPath)
	delete(group, "event")
	return group
}

func TestQwenInstallMigratesLegacyPairToCurrentTriple(t *testing.T) {
	fixture := prepareQwenFixture(t, qwenFixtureOptions{})
	lookalike := legacyQwenGroupValue(t, fixture.guardPath)
	requireQwenObject(t, requireQwenArray(t, lookalike["hooks"])[0])["timeout"] = float64(9_000)
	settings := map[string]any{
		"owner": "user",
		"hooks": map[string]any{
			"PreToolUse": []any{
				legacyQwenGroupValue(t, fixture.guardPath),
				lookalike,
			},
			"PostToolUse": []any{legacyQwenGroupValue(t, fixture.hookPath)},
		},
	}
	writeQwenTestObject(t, fixture.configPath, settings)
	installQwenFixture(t, fixture)
	root := readQwenTestObject(t, fixture.configPath)
	for _, item := range []struct{ event, path string }{
		{"PreToolUse", fixture.guardPath},
		{"PostToolUse", fixture.hookPath},
		{"PostToolUseFailure", fixture.hookPath},
	} {
		group := qwenManagedGroup(t, root, item.event, item.path)
		if len(group) != 1 {
			t.Fatalf("legacy group remains for %s: %#v", item.event, group)
		}
	}
	preserved := false
	for _, groupValue := range requireQwenArray(t, requireQwenObject(t, root["hooks"])["PreToolUse"]) {
		for _, handlerValue := range requireQwenArray(t, requireQwenObject(t, groupValue)["hooks"]) {
			preserved = preserved || requireQwenObject(t, handlerValue)["timeout"] == float64(9_000)
		}
	}
	if !preserved {
		t.Fatal("legacy ownership lookalike was removed")
	}
}

func TestQwenUninstallPreservesExternalMutationsAndExactOwnership(t *testing.T) {
	fixture := prepareQwenFixture(t, qwenFixtureOptions{
		settings: qwenString(`{"$version":4,"owner":"user"}`),
	})
	installQwenFixture(t, fixture)
	settings := readQwenTestObject(t, fixture.configPath)
	group := qwenManagedGroup(t, settings, "PreToolUse", fixture.guardPath)
	group["hooks"] = append(requireQwenArray(t, group["hooks"]), map[string]any{
		"type": "command", "command": "user-command",
	})
	otherAgent := filepath.Join(filepath.Dir(fixture.agentDir), "agent-10")
	hooks := requireQwenObject(t, settings["hooks"])
	hooks["PreToolUse"] = append(
		requireQwenArray(t, hooks["PreToolUse"]),
		legacyQwenGroupValue(t, filepath.Join(otherAgent, qwenGuardScript)),
	)
	writeQwenTestObject(t, fixture.configPath, settings)
	before := readQwenTestFile(t, fixture.configPath)
	if err := fixture.plugin.Uninstall("other-agent"); err != nil {
		t.Fatalf("uninstall other agent: %v", err)
	}
	if readQwenTestFile(t, fixture.configPath) != before {
		t.Fatal("uninstall changed an unrelated installation")
	}
	agentID := qwenTestAgentID
	if runtime.GOOS == "windows" {
		agentID = "AGENT-1"
	}
	if err := fixture.plugin.Uninstall(agentID); err != nil {
		t.Fatalf("uninstall Qwen Code hooks: %v", err)
	}
	remaining := readQwenTestObject(t, fixture.configPath)
	if remaining["$version"] != float64(4) || remaining["owner"] != "user" {
		t.Fatalf("root metadata = %#v", remaining)
	}
	remainingRaw := readQwenTestFile(t, fixture.configPath)
	for _, marker := range []string{"user-command", "agent-10"} {
		if !strings.Contains(remainingRaw, marker) {
			t.Fatalf("uninstall removed %q", marker)
		}
	}
	remainingHooks := requireQwenObject(t, remaining["hooks"])
	if _, exists := remainingHooks["PostToolUse"]; exists {
		t.Fatalf("PostToolUse remains: %#v", remainingHooks)
	}
	if _, exists := remainingHooks["PostToolUseFailure"]; exists {
		t.Fatalf("PostToolUseFailure remains: %#v", remainingHooks)
	}
}

func TestQwenUninstallDeletesOwnedEmptySettings(t *testing.T) {
	fixture := prepareQwenFixture(t, qwenFixtureOptions{})
	installQwenFixture(t, fixture)
	if !strings.HasPrefix(readQwenTestFile(t, fixture.configPath), qwenOwnedFileMarker) {
		t.Fatal("created settings are missing the ownership marker")
	}
	if err := fixture.plugin.Uninstall(qwenTestAgentID); err != nil {
		t.Fatalf("uninstall Qwen Code hooks: %v", err)
	}
	requireMissingQwenFile(t, fixture.configPath)
}
