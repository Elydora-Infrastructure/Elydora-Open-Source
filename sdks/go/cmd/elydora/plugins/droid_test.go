package plugins

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDroidRegistryUsesOfficialGlobalHookSource(t *testing.T) {
	entry := SupportedAgents[droidAgentKey]
	if entry.Name != "Factory Droid" || entry.ConfigDir != "~/.factory" || entry.ConfigFile != "hooks.json" {
		t.Fatalf("Factory Droid registry entry = %#v", entry)
	}
	plugin, ok := NewPlugin(droidAgentKey).(*DroidPlugin)
	if !ok || !plugin.ManagesGuardRuntime() {
		t.Fatal("Factory Droid plugin is not registered as a guard runtime manager")
	}
}

func TestDroidInstallWritesCurrentContainerAndCompleteRuntime(t *testing.T) {
	fixture := prepareDroidFixture(t, droidFixtureOptions{})
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
	if !strings.Contains(string(output), fixture.configPath) || !strings.Contains(string(output), "run /hooks") {
		t.Fatalf("install output = %s", output)
	}
	source := readDroidTestFile(t, fixture.configPath)
	if !strings.HasPrefix(source, droidOwnedFileMarker+"\n") {
		t.Fatalf("managed source header is missing: %s", source)
	}
	hooks := droidCurrentHooks(t, fixture.configPath)
	guardGroup := droidManagedGroup(t, hooks["PreToolUse"], fixture.guardPath)
	auditGroup := droidManagedGroup(t, hooks["PostToolUse"], fixture.hookPath)
	requireDroidNativeGroup(t, guardGroup)
	requireDroidNativeGroup(t, auditGroup)
	guardCommand := droidManagedHandler(t, hooks["PreToolUse"], fixture.guardPath)["command"].(string)
	if runtime.GOOS == "windows" &&
		(!strings.HasPrefix(guardCommand, "& '") || !strings.HasSuffix(guardCommand, droidWindowsExitSuffix)) {
		t.Fatalf("Windows Factory command = %q", guardCommand)
	}
	runtimeConfig := readDroidTestObject(t, fixture.runtimeConfig)
	expectedConfig := map[string]any{
		"org_id":     "org-1",
		"agent_id":   droidTestAgentID,
		"kid":        "kid-1",
		"base_url":   "http://127.0.0.1:9",
		"agent_name": droidAgentKey,
		"token":      "token-1",
	}
	if !reflect.DeepEqual(runtimeConfig, expectedConfig) {
		t.Fatalf("runtime config = %#v", runtimeConfig)
	}
	if readDroidTestFile(t, fixture.privateKey) != droidTestPrivateKey {
		t.Fatal("private key content changed")
	}
	if !strings.Contains(readDroidTestFile(t, fixture.guardPath), `const AGENT_NAME = "droid"`) {
		t.Fatal("guard runtime identity is missing")
	}
	if !strings.Contains(readDroidTestFile(t, fixture.hookPath), "const NATIVE_PAYLOAD = true") {
		t.Fatal("audit runtime is missing native payload mode")
	}
	paths := []string{
		fixture.configPath,
		fixture.guardPath,
		fixture.hookPath,
		fixture.runtimeConfig,
		fixture.privateKey,
	}
	before := snapshotDroidFiles(t, paths...)
	installDroidFixture(t, fixture)
	requireDroidSnapshot(t, before)
	requireNoDroidStagingFiles(t, fixture.homeDir)
}

func TestDroidInstallMigratesLegacyWindowsCommands(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows migration contract")
	}
	fixture := prepareDroidFixture(t, droidFixtureOptions{root: droidJSON(map[string]any{"hooks": map[string]any{}})})
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		t.Fatalf("resolve Node.js: %v", err)
	}
	group := func(script string) map[string]any {
		return map[string]any{
			"matcher": "*",
			"hooks": []any{map[string]any{
				"type":    "command",
				"command": fmt.Sprintf(`"%s" "%s"`, nodePath, script),
				"timeout": 10,
			}},
		}
	}
	writeDroidTestObject(t, fixture.configPath, map[string]any{"hooks": map[string]any{
		"PreToolUse":  []any{group(fixture.guardPath)},
		"PostToolUse": []any{group(fixture.hookPath)},
	}})
	installDroidFixture(t, fixture)
	hooks := droidCurrentHooks(t, fixture.configPath)
	for _, event := range droidToolEvents {
		groups := requireDroidArray(t, hooks[event])
		if len(groups) != 1 {
			t.Fatalf("%s groups = %#v", event, groups)
		}
		command := requireDroidObject(t, requireDroidArray(t, requireDroidObject(t, groups[0])["hooks"])[0])["command"].(string)
		if !strings.HasPrefix(command, "& '") {
			t.Fatalf("migrated command = %q", command)
		}
	}
}

func TestDroidRootHookFileHasWholeSourcePrecedence(t *testing.T) {
	root := `{
  // active root source
  "hooks": {
    "PreToolUse": [
      { "matcher": "Read", "hooks": [{ "type": "command", "command": "root-user" }] }
    ]
  }
}
`
	settings := `{
  // inactive settings source
  "theme": "dark",
  "hooks": {
    "PostToolUse": [
      { "matcher": "Edit", "hooks": [{ "type": "command", "command": "settings-user" }] }
    ]
  }
}
`
	fixture := prepareDroidFixture(t, droidFixtureOptions{
		root:     droidString(root),
		settings: droidString(settings),
	})
	installDroidFixture(t, fixture)
	if !strings.Contains(readDroidTestFile(t, fixture.configPath), "active root source") {
		t.Fatal("root JSONC comment was lost")
	}
	hooks := droidCurrentHooks(t, fixture.configPath)
	pre := requireDroidArray(t, hooks["PreToolUse"])
	userCommand := requireDroidObject(t, requireDroidArray(t, requireDroidObject(t, pre[0])["hooks"])[0])["command"]
	if userCommand != "root-user" {
		t.Fatalf("root user command = %#v", userCommand)
	}
	requireDroidNativeGroup(t, droidManagedGroup(t, hooks["PreToolUse"], fixture.guardPath))
	requireDroidNativeGroup(t, droidManagedGroup(t, hooks["PostToolUse"], fixture.hookPath))
	if readDroidTestFile(t, fixture.settingsPath) != settings {
		t.Fatal("inactive settings source changed")
	}
}

func TestDroidSettingsSourcePreservesFormatting(t *testing.T) {
	settings := "{\r\n\t\"theme\": \"dark\",\r\n\t\"hooks\": {}\r\n}\r\n"
	fixture := prepareDroidFixture(t, droidFixtureOptions{settings: droidString(settings)})
	installDroidFixture(t, fixture)
	requireMissingDroidFile(t, fixture.configPath)
	raw := readDroidTestFile(t, fixture.settingsPath)
	if !strings.Contains(raw, "\r\n\t\t\"PreToolUse\"") ||
		!strings.Contains(raw, "\r\n\t\t\"PostToolUse\"") {
		t.Fatalf("settings formatting changed:\n%s", raw)
	}
	hooks := requireDroidObject(t, readDroidTestObject(t, fixture.settingsPath)["hooks"])
	requireDroidNativeGroup(t, droidManagedGroup(t, hooks["PreToolUse"], fixture.guardPath))
	requireDroidNativeGroup(t, droidManagedGroup(t, hooks["PostToolUse"], fixture.hookPath))
}

func TestDroidLocalSettingsContainerHasWholeSourcePrecedence(t *testing.T) {
	settings := droidJSON(map[string]any{"hooks": map[string]any{"Notification": []any{}}})
	fixture := prepareDroidFixture(t, droidFixtureOptions{
		settings:      settings,
		localSettings: droidJSON(map[string]any{"hooks": map[string]any{"SessionStart": []any{}}}),
	})
	baseBefore := readDroidTestFile(t, fixture.settingsPath)
	installDroidFixture(t, fixture)
	if readDroidTestFile(t, fixture.settingsPath) != baseBefore {
		t.Fatal("base settings changed while local settings were active")
	}
	hooks := requireDroidObject(t, readDroidTestObject(t, fixture.localSettingsPath)["hooks"])
	requireDroidNativeGroup(t, droidManagedGroup(t, hooks["PreToolUse"], fixture.guardPath))
	requireDroidNativeGroup(t, droidManagedGroup(t, hooks["PostToolUse"], fixture.hookPath))
}

func TestDroidLegacyHookFileStaysActiveUntilFactoryMigratesIt(t *testing.T) {
	legacy := droidJSON(map[string]any{"PreToolUse": []any{map[string]any{
		"matcher": "Read",
		"hooks":   []any{map[string]any{"type": "command", "command": "legacy-user"}},
	}}})
	settings := droidJSON(map[string]any{"hooks": map[string]any{"PostToolUse": []any{}}})
	fixture := prepareDroidFixture(t, droidFixtureOptions{legacy: legacy, settings: settings})
	settingsBefore := readDroidTestFile(t, fixture.settingsPath)
	installDroidFixture(t, fixture)
	requireMissingDroidFile(t, fixture.configPath)
	legacyHooks := readDroidTestObject(t, fixture.legacyPath)
	requireDroidNativeGroup(t, droidManagedGroup(t, legacyHooks["PreToolUse"], fixture.guardPath))
	requireDroidNativeGroup(t, droidManagedGroup(t, legacyHooks["PostToolUse"], fixture.hookPath))
	if readDroidTestFile(t, fixture.settingsPath) != settingsBefore {
		t.Fatal("inactive settings source changed")
	}
}

func TestDroidInstallCleansExactManagedHooksFromInactiveSources(t *testing.T) {
	fixture := prepareDroidFixture(t, droidFixtureOptions{})
	installDroidFixture(t, fixture)
	writeDroidTestObject(t, fixture.settingsPath, map[string]any{
		"hooks": droidCurrentHooks(t, fixture.configPath),
		"owner": "user",
	})
	installDroidFixture(t, fixture)
	settings := readDroidTestObject(t, fixture.settingsPath)
	if settings["owner"] != "user" {
		t.Fatalf("inactive settings owner = %#v", settings["owner"])
	}
	hooks := requireDroidObject(t, settings["hooks"])
	if _, exists := hooks["PreToolUse"]; exists {
		t.Fatalf("inactive PreToolUse remains: %#v", hooks)
	}
	if _, exists := hooks["PostToolUse"]; exists {
		t.Fatalf("inactive PostToolUse remains: %#v", hooks)
	}
}

type droidAPIRequest struct {
	method string
	path   string
	body   []byte
}

func TestDroidGuardBlocksAndAuditPreservesNativePayload(t *testing.T) {
	requests := make([]droidAPIRequest, 0)
	var requestLock sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read API request: %v", err)
		}
		requestLock.Lock()
		requests = append(requests, droidAPIRequest{request.Method, request.URL.Path, body})
		requestLock.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		if request.Method == http.MethodPost {
			writer.WriteHeader(http.StatusCreated)
			_, _ = writer.Write([]byte(`{"operation":{"accepted":true}}`))
			return
		}
		_, _ = writer.Write([]byte(`{"agent":{"status":"active"}}`))
	}))
	defer server.Close()
	fixture := prepareDroidFixture(t, droidFixtureOptions{baseURL: server.URL})
	installDroidFixture(t, fixture)
	hooks := droidCurrentHooks(t, fixture.configPath)
	guard := droidManagedHandler(t, hooks["PreToolUse"], fixture.guardPath)
	audit := droidManagedHandler(t, hooks["PostToolUse"], fixture.hookPath)
	writeDroidTestObject(t, filepath.Join(fixture.agentDir, "status-cache.json"), map[string]any{
		"status":    "frozen",
		"cached_at": time.Now().UnixMilli(),
	})
	prePayload := map[string]any{
		"session_id":      "session-1",
		"transcript_path": filepath.Join(fixture.homeDir, "transcript.jsonl"),
		"cwd":             fixture.workspaceDir,
		"permission_mode": "auto-high",
		"hook_event_name": "PreToolUse",
		"tool_name":       "Execute",
		"tool_input":      map[string]any{"command": "echo test"},
	}
	preRaw, _ := json.Marshal(prePayload)
	guardResult := runDroidCommand(t, guard["command"].(string), fixture.homeDir, string(preRaw))
	if guardResult.exitCode != 2 || !strings.Contains(guardResult.stderr, `Agent "droid" is frozen`) {
		t.Fatalf("guard result = %#v", guardResult)
	}
	postPayload := make(map[string]any, len(prePayload)+1)
	for key, value := range prePayload {
		postPayload[key] = value
	}
	postPayload["hook_event_name"] = "PostToolUse"
	postPayload["tool_response"] = map[string]any{"output": "test", "success": true}
	postRaw, _ := json.Marshal(postPayload)
	auditResult := runDroidCommand(t, audit["command"].(string), fixture.homeDir, string(postRaw))
	if auditResult.exitCode != 0 {
		t.Fatalf("audit result = %#v", auditResult)
	}
	requestLock.Lock()
	defer requestLock.Unlock()
	var operation map[string]any
	for _, request := range requests {
		if request.method == http.MethodPost {
			if err := json.Unmarshal(request.body, &operation); err != nil {
				t.Fatalf("decode audit request: %v", err)
			}
			break
		}
	}
	if operation == nil || !reflect.DeepEqual(operation["payload"], postPayload) {
		t.Fatalf("native operation = %#v", operation)
	}
	if !reflect.DeepEqual(operation["subject"], map[string]any{"session_id": "session-1"}) ||
		!reflect.DeepEqual(operation["action"], map[string]any{"tool": "Execute"}) {
		t.Fatalf("native operation identity = %#v", operation)
	}
}

func TestDroidStatusRequiresExactHooksAndRuntimeSources(t *testing.T) {
	fixture := prepareDroidFixture(t, droidFixtureOptions{})
	installDroidFixture(t, fixture)
	status, err := fixture.plugin.Status()
	if err != nil || !status.Installed || !status.HookConfigured || !status.HookScriptExists {
		t.Fatalf("installed status = %#v, %v", status, err)
	}
	if err := os.WriteFile(fixture.hookPath, []byte("tampered\n"), 0700); err != nil {
		t.Fatalf("tamper audit runtime: %v", err)
	}
	status, err = fixture.plugin.Status()
	if err != nil || status.Installed || !status.HookConfigured || status.HookScriptExists {
		t.Fatalf("tampered status = %#v, %v", status, err)
	}
}

func TestDroidStatusUsesOneWholeActiveSource(t *testing.T) {
	fixture := prepareDroidFixture(t, droidFixtureOptions{
		settings: droidJSON(map[string]any{"hooks": map[string]any{}}),
	})
	installDroidFixture(t, fixture)
	settingsHooks := requireDroidObject(t, readDroidTestObject(t, fixture.settingsPath)["hooks"])
	writeDroidTestObject(t, fixture.configPath, map[string]any{"hooks": map[string]any{
		"PreToolUse": settingsHooks["PreToolUse"],
	}})
	status, err := fixture.plugin.Status()
	if err != nil || status.HookConfigured || status.Installed {
		t.Fatalf("split-source status = %#v, %v", status, err)
	}
}

func TestDroidStatusAndUninstallDoNotRequireNodeResolution(t *testing.T) {
	fixture := prepareDroidFixture(t, droidFixtureOptions{})
	installDroidFixture(t, fixture)
	t.Setenv("PATH", "")
	status, err := fixture.plugin.Status()
	if err != nil || !status.Installed {
		t.Fatalf("status without PATH = %#v, %v", status, err)
	}
	if err := fixture.plugin.Uninstall(droidTestAgentID); err != nil {
		t.Fatalf("uninstall without PATH: %v", err)
	}
	requireMissingDroidFile(t, fixture.configPath)
}
