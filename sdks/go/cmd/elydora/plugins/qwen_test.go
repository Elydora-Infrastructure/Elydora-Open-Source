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

func TestQwenRegistryUsesOfficialUserSettings(t *testing.T) {
	entry := SupportedAgents[qwenAgentKey]
	if entry.Name != "Qwen Code" || entry.ConfigDir != "~/.qwen" || entry.ConfigFile != "settings.json" {
		t.Fatalf("Qwen Code registry entry = %#v", entry)
	}
	if _, ok := NewPlugin(qwenAgentKey).(*QwenPlugin); !ok {
		t.Fatal("Qwen Code plugin is not registered")
	}
}

func TestQwenInstallPreservesJSONCommentsAndIsIdempotent(t *testing.T) {
	settings := "{\r\n" +
		"  // Keep this user preference.\r\n" +
		"  \"theme\": \"GitHub\",\r\n" +
		"  \"hooks\": {\r\n" +
		"    \"SessionStart\": [{ \"hooks\": [{ \"type\": \"command\", \"command\": \"session-hook\" }] }],\r\n" +
		"    \"PreToolUse\": [{ \"matcher\": \"read_file\", \"hooks\": [{ \"type\": \"command\", \"command\": \"user-hook\" }] }]\r\n" +
		"  }\r\n" +
		"}\r\n"
	fixture := prepareQwenFixture(t, qwenFixtureOptions{settings: qwenString(settings)})
	workspaceSettings := filepath.Join(fixture.workspaceDir, ".qwen", "settings.json")
	writeOptionalQwenFile(t, workspaceSettings, qwenString("{ \"owner\": \"workspace\" }\n"))
	installQwenFixture(t, fixture)
	first := readQwenTestFile(t, fixture.configPath)
	installQwenFixture(t, fixture)
	if readQwenTestFile(t, fixture.configPath) != first {
		t.Fatal("idempotent install changed Qwen Code settings")
	}
	if !strings.Contains(first, "Keep this user preference") || !strings.Contains(first, "\r\n") {
		t.Fatalf("settings comments or line endings were lost:\n%s", first)
	}
	root := readQwenTestObject(t, fixture.configPath)
	if root["theme"] != "GitHub" {
		t.Fatalf("theme = %#v", root["theme"])
	}
	hooks := requireQwenObject(t, root["hooks"])
	if len(requireQwenArray(t, hooks["SessionStart"])) != 1 ||
		len(requireQwenArray(t, hooks["PreToolUse"])) != 2 ||
		len(requireQwenArray(t, hooks["PostToolUse"])) != 1 {
		t.Fatalf("Qwen hooks = %#v", hooks)
	}
	for _, item := range []struct{ event, path string }{
		{"PreToolUse", fixture.guardPath}, {"PostToolUse", fixture.hookPath},
	} {
		handler := qwenManagedHandler(t, root, item.event, item.path)
		if len(handler) != 4 || handler["type"] != "command" ||
			handler["shell"] != expectedQwenShell() || handler["timeout"] != float64(10_000) {
			t.Fatalf("managed handler = %#v", handler)
		}
	}
	if readQwenTestObject(t, workspaceSettings)["owner"] != "workspace" {
		t.Fatal("workspace settings changed")
	}
	runtimeConfig := readQwenTestObject(t, fixture.runtimeConfig)
	if runtimeConfig["agent_name"] != qwenAgentKey {
		t.Fatalf("runtime config = %#v", runtimeConfig)
	}
}

func TestQwenInstallReportsReviewCommand(t *testing.T) {
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
		t.Fatalf("capture install output: install=%v close=%v read=%v", installErr, closeErr, readErr)
	}
	text := string(output)
	for _, marker := range []string{fixture.configPath, "/hooks"} {
		if !strings.Contains(text, marker) {
			t.Fatalf("install output is missing %q: %s", marker, text)
		}
	}
}

func TestQwenHomeUsesOfficialUserEnvironmentPrecedence(t *testing.T) {
	fixture := prepareQwenFixture(t, qwenFixtureOptions{})
	firstHome := filepath.Join(filepath.Dir(fixture.homeDir), "first # qwen home")
	secondHome := filepath.Join(filepath.Dir(fixture.homeDir), "second qwen home")
	writeOptionalQwenFile(
		t,
		filepath.Join(fixture.qwenDir, ".env"),
		qwenString("export QWEN_HOME = \""+firstHome+"\" # selected by Qwen\n"),
	)
	writeOptionalQwenFile(t, filepath.Join(fixture.homeDir, ".env"), qwenString("QWEN_HOME="+secondHome+"\n"))
	installQwenFixture(t, fixture)
	selected := filepath.Join(firstHome, "settings.json")
	qwenManagedHandler(t, readQwenTestObject(t, selected), "PreToolUse", fixture.guardPath)
	requireMissingQwenFile(t, filepath.Join(secondHome, "settings.json"))
	requireMissingQwenFile(t, fixture.configPath)
	status, err := fixture.plugin.Status()
	if err != nil || status.ConfigPath != selected || !status.Installed {
		t.Fatalf("Qwen status = %#v, %v", status, err)
	}
}

func TestQwenExplicitHomeSupportsRelativeTildeAndEmptyValues(t *testing.T) {
	tests := []struct {
		name, value string
		expected    func(*qwenFixture) string
	}{
		{"relative", "relative-qwen", func(f *qwenFixture) string {
			return filepath.Join(f.workspaceDir, "relative-qwen", "settings.json")
		}},
		{"tilde", "~/custom-qwen", func(f *qwenFixture) string {
			return filepath.Join(f.homeDir, "custom-qwen", "settings.json")
		}},
		{"empty", "", func(f *qwenFixture) string { return f.configPath }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := prepareQwenFixture(t, qwenFixtureOptions{})
			ignored := filepath.Join(filepath.Dir(fixture.homeDir), "ignored-qwen-home")
			writeOptionalQwenFile(
				t, filepath.Join(fixture.qwenDir, ".env"), qwenString("QWEN_HOME="+ignored+"\n"),
			)
			t.Setenv("QWEN_HOME", test.value)
			installQwenFixture(t, fixture)
			qwenManagedHandler(t, readQwenTestObject(t, test.expected(fixture)), "PreToolUse", fixture.guardPath)
			requireMissingQwenFile(t, filepath.Join(ignored, "settings.json"))
		})
	}
}

func TestQwenCommandsBlockAndForwardOfficialInputByteForByte(t *testing.T) {
	fixture := prepareQwenFixture(t, qwenFixtureOptions{})
	installQwenFixture(t, fixture)
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
	settings := readQwenTestObject(t, fixture.configPath)
	guard := qwenManagedHandler(t, settings, "PreToolUse", fixture.guardPath)
	audit := qwenManagedHandler(t, settings, "PostToolUse", fixture.hookPath)
	prePayload := `{"session_id":"session-1","cwd":"C:/workspace","hook_event_name":"PreToolUse","timestamp":"2026-07-19T00:00:00.000Z","tool_name":"run_shell_command","tool_input":{"command":"echo test"}}`
	guardResult := runQwenHandler(t, guard, fixture.homeDir, prePayload)
	if guardResult.exitCode != 2 || !strings.Contains(guardResult.stderr, "Agent is frozen by Elydora") {
		t.Fatalf("guard result = %#v", guardResult)
	}
	postPayload := strings.Replace(prePayload, "PreToolUse", "PostToolUse", 1)
	auditResult := runQwenHandler(t, audit, fixture.homeDir, postPayload)
	if auditResult.exitCode != 0 {
		t.Fatalf("audit result = %#v", auditResult)
	}
	if readQwenTestFile(t, capturePath) != postPayload {
		t.Fatal("audit hook changed the official Qwen Code payload")
	}
}

func TestQwenStatusRequiresEnabledPairAndRuntimeFiles(t *testing.T) {
	fixture := prepareQwenFixture(t, qwenFixtureOptions{})
	installQwenFixture(t, fixture)
	status, err := fixture.plugin.Status()
	if err != nil || !status.Installed || !status.HookConfigured || !status.HookScriptExists {
		t.Fatalf("installed status = %#v, %v", status, err)
	}
	settings := readQwenTestObject(t, fixture.configPath)
	settings["disableAllHooks"] = true
	writeQwenTestObject(t, fixture.configPath, settings)
	status, err = fixture.plugin.Status()
	if err != nil || status.Installed || status.HookConfigured {
		t.Fatalf("disabled status = %#v, %v", status, err)
	}
	settings["disableAllHooks"] = false
	writeQwenTestObject(t, fixture.configPath, settings)
	if err := os.Remove(fixture.guardPath); err != nil {
		t.Fatalf("remove guard runtime: %v", err)
	}
	status, err = fixture.plugin.Status()
	if err != nil || status.Installed || !status.HookConfigured || status.HookScriptExists {
		t.Fatalf("missing-runtime status = %#v, %v", status, err)
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
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		t.Fatalf("resolve Node.js: %v", err)
	}
	hooks := requireQwenObject(t, settings["hooks"])
	hooks["PreToolUse"] = append(requireQwenArray(t, hooks["PreToolUse"]), map[string]any{
		"matcher": "*",
		"hooks": []any{map[string]any{
			"type": "command", "command": buildQwenCommand(nodePath, filepath.Join(filepath.Dir(fixture.agentDir), "agent-10", qwenGuardScript)),
			"shell": expectedQwenShell(), "timeout": float64(10_000),
		}},
	})
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

func TestQwenDotenvParserMatchesOfficialGrammar(t *testing.T) {
	tests := []struct {
		source string
		want   map[string]string
	}{
		{"QWEN_HOME=plain\n", map[string]string{"QWEN_HOME": "plain"}},
		{"export QWEN_HOME = \"path # one\" # comment\n", map[string]string{"QWEN_HOME": "path # one"}},
		{"QWEN_HOME: path with spaces\n", map[string]string{"QWEN_HOME": "path with spaces"}},
		{"QWEN_HOME=\n", map[string]string{"QWEN_HOME": ""}},
		{"QWEN_HOME='single quoted'\r\n", map[string]string{"QWEN_HOME": "single quoted"}},
		{"QWEN_HOME=`backtick value`\n", map[string]string{"QWEN_HOME": "backtick value"}},
		{"QWEN_HOME=first\nQWEN_HOME=second\n", map[string]string{"QWEN_HOME": "second"}},
		{"OTHER=x\nQWEN_HOME=\"line\\nnext\"\n", map[string]string{"OTHER": "x", "QWEN_HOME": "line\nnext"}},
	}
	for _, test := range tests {
		got := parseDotenv([]byte(test.source))
		encodedGot, _ := json.Marshal(got)
		encodedWant, _ := json.Marshal(test.want)
		if string(encodedGot) != string(encodedWant) {
			t.Fatalf("parseDotenv(%q) = %s, want %s", test.source, encodedGot, encodedWant)
		}
	}
}

func writeQwenTestObject(t *testing.T, path string, value map[string]any) {
	t.Helper()
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("encode %s: %v", path, err)
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
