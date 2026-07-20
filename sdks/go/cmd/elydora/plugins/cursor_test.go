package plugins

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCursorRegistryUsesNativeUserHooks(t *testing.T) {
	want := AgentRegistryEntry{Name: "Cursor", ConfigDir: "~/.cursor", ConfigFile: "hooks.json"}
	if got := SupportedAgents["cursor"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("Cursor registry = %#v, want %#v", got, want)
	}
}

func TestCursorInstallPreservesUserHooksMigratesLegacyAndIsIdempotent(t *testing.T) {
	fixture := prepareCursorFixture(t, nil, false)
	writeCursorObject(t, fixture.configPath, map[string]any{
		"description": "user-owned",
		"hooks": map[string]any{
			"sessionStart": []any{map[string]any{"command": "user-session"}},
			"preToolUse": []any{
				map[string]any{"command": "user-pre"},
				map[string]any{"command": "node " + fixture.guardPath},
			},
			"postToolUse": []any{map[string]any{"command": "node " + fixture.hookPath}},
		},
	})

	installCursorFixture(t, fixture)
	installCursorFixture(t, fixture)

	settings := readCursorObject(t, fixture.configPath)
	if settings["version"] != float64(1) || settings["description"] != "user-owned" {
		t.Fatalf("Cursor settings = %#v", settings)
	}
	hooks := cursorObject(t, settings["hooks"])
	if len(cursorArray(t, hooks["sessionStart"])) != 1 ||
		len(cursorArray(t, hooks["preToolUse"])) != 2 ||
		len(cursorArray(t, hooks["postToolUse"])) != 1 ||
		len(cursorArray(t, hooks["postToolUseFailure"])) != 1 {
		t.Fatalf("Cursor hooks = %#v", hooks)
	}
	if cursorObject(t, cursorArray(t, hooks["preToolUse"])[0])["command"] != "user-pre" {
		t.Fatalf("first preToolUse handler = %#v", cursorArray(t, hooks["preToolUse"])[0])
	}
	assertNativeCursorHandler(t, managedCursorHandler(t, settings, "preToolUse", "guard.js"))
	assertNativeCursorHandler(t, managedCursorHandler(t, settings, "postToolUse", "hook.js"))
	assertNativeCursorHandler(t, managedCursorHandler(t, settings, "postToolUseFailure", "hook.js"))
	runtimeConfig := readCursorObject(t, fixture.runtimeConfig)
	if runtimeConfig["agent_name"] != "cursor" || runtimeConfig["agent_id"] != cursorTestAgentID {
		t.Fatalf("runtime config = %#v", runtimeConfig)
	}
}

func TestCursorCommandsBlockAndForwardOfficialPayloadByteForByte(t *testing.T) {
	fixture := prepareCursorFixture(t, nil, false)
	installCursorFixture(t, fixture)
	settings := readCursorObject(t, fixture.configPath)
	guard := managedCursorHandler(t, settings, "preToolUse", "guard.js")
	writeCursorObject(t, filepath.Join(fixture.agentDir, "status-cache.json"), map[string]any{
		"status": "active", "cached_at": time.Now().UnixMilli(),
	})
	active := runCursorHandler(t, guard, "{}\n")
	var allow map[string]any
	if err := json.Unmarshal([]byte(active.stdout), &allow); err != nil || allow["permission"] != "allow" {
		t.Fatalf("active guard output = %q, %v", active.stdout, err)
	}
	writeCursorObject(t, filepath.Join(fixture.agentDir, "status-cache.json"), map[string]any{
		"status": "frozen", "cached_at": time.Now().UnixMilli(),
	})
	prePayload := `{"conversation_id":"conversation-1","generation_id":"generation-1","hook_event_name":"preToolUse","tool_name":"Shell","tool_input":{"command":"Get-ChildItem"},"tool_use_id":"call-1","cwd":"project"}` + "\n"
	frozen := runCursorHandler(t, guard, prePayload)
	if frozen.exitCode != 2 || !strings.Contains(frozen.stderr, "frozen by Elydora") {
		t.Fatalf("frozen guard result = %#v", frozen)
	}
	writeCursorObject(t, filepath.Join(fixture.agentDir, "status-cache.json"), map[string]any{
		"status": "revoked", "cached_at": time.Now().UnixMilli(),
	})
	revoked := runCursorHandler(t, guard, prePayload)
	if revoked.exitCode != 2 || !strings.Contains(revoked.stderr, "revoked in Elydora") {
		t.Fatalf("revoked guard result = %#v", revoked)
	}

	capturePath := filepath.Join(t.TempDir(), "captured.json")
	captureScript := "const fs = require('node:fs');\n" +
		"fs.writeFileSync(process.env.ELYDORA_CAPTURE, fs.readFileSync(0));\n" +
		"process.stdout.write('{}\\n');\n"
	if err := os.WriteFile(fixture.hookPath, []byte(captureScript), 0700); err != nil {
		t.Fatalf("write capture runtime: %v", err)
	}
	postPayload := `{"conversation_id":"conversation-1","generation_id":"generation-1","hook_event_name":"postToolUse","tool_name":"Shell","tool_input":{"command":"Get-ChildItem"},"tool_output":"{\"exitCode\":0,\"stdout\":\"ok\"}","tool_use_id":"call-1","cwd":"project","duration":42}` + "\n"
	audit := managedCursorHandler(t, settings, "postToolUse", "hook.js")
	result := runCursorHandler(t, audit, postPayload, "ELYDORA_CAPTURE="+capturePath)
	captured, err := os.ReadFile(capturePath)
	if err != nil || result.exitCode != 0 || string(captured) != postPayload {
		t.Fatalf("audit result = %#v, captured = %q, %v", result, captured, err)
	}
	failurePayload := `{"conversation_id":"conversation-1","generation_id":"generation-1","hook_event_name":"postToolUseFailure","tool_name":"Shell","tool_input":{"command":"exit 1"},"tool_use_id":"call-2","cwd":"project","error_message":"command failed","failure_type":"error","duration":21,"is_interrupt":false}` + "\n"
	failureAudit := managedCursorHandler(t, settings, "postToolUseFailure", "hook.js")
	failureResult := runCursorHandler(t, failureAudit, failurePayload, "ELYDORA_CAPTURE="+capturePath)
	captured, err = os.ReadFile(capturePath)
	if err != nil || failureResult.exitCode != 0 || string(captured) != failurePayload {
		t.Fatalf("failure audit result = %#v, captured = %q, %v", failureResult, captured, err)
	}
}

func TestCursorAuditPreservesNativeFailurePayload(t *testing.T) {
	received := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		var operation map[string]any
		if err := json.NewDecoder(request.Body).Decode(&operation); err != nil {
			t.Errorf("decode operation: %v", err)
		}
		received <- operation
		response.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	fixture := prepareCursorFixture(t, nil, false)
	fixture.config.BaseURL = server.URL
	fixture.config.PrivateKey = base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	installCursorFixture(t, fixture)
	payload := `{"conversation_id":"conversation-1","generation_id":"generation-1","hook_event_name":"postToolUseFailure","tool_name":"Shell","tool_input":{"command":"exit 1"},"tool_use_id":"call-1","cwd":"project","error_message":"command failed","failure_type":"error","duration":42,"is_interrupt":false}` + "\n"
	handler := managedCursorHandler(
		t,
		readCursorObject(t, fixture.configPath),
		"postToolUseFailure",
		cursorAuditScript,
	)
	result := runCursorHandler(t, handler, payload)
	if result.exitCode != 0 || result.stdout != "{}\n" || result.stderr != "" {
		t.Fatalf("Cursor audit result = %#v", result)
	}
	operation := <-received
	var expected map[string]any
	if err := json.Unmarshal([]byte(payload), &expected); err != nil {
		t.Fatalf("decode expected payload: %v", err)
	}
	if !reflect.DeepEqual(operation["payload"], expected) {
		t.Fatalf("operation payload = %#v, want %#v", operation["payload"], expected)
	}
	subject := cursorObject(t, operation["subject"])
	if subject["session_id"] != "conversation-1" {
		t.Fatalf("operation subject = %#v", subject)
	}
}

func TestCursorGeneratedRuntimesFailClosed(t *testing.T) {
	fixture := prepareCursorFixture(t, nil, false)
	installCursorFixture(t, fixture)
	settings := readCursorObject(t, fixture.configPath)
	if err := os.Remove(fixture.runtimeConfig); err != nil {
		t.Fatalf("remove runtime config: %v", err)
	}
	guard := runCursorHandler(
		t,
		managedCursorHandler(t, settings, "preToolUse", cursorGuardScript),
		"{}\n",
	)
	if guard.exitCode != 1 || guard.stdout != "" || !strings.Contains(guard.stderr, "Failed to read agent config") {
		t.Fatalf("guard failure = %#v", guard)
	}

	auditFixture := prepareCursorFixture(t, nil, false)
	installCursorFixture(t, auditFixture)
	auditSettings := readCursorObject(t, auditFixture.configPath)
	if err := os.Remove(auditFixture.privateKey); err != nil {
		t.Fatalf("remove private key: %v", err)
	}
	audit := runCursorHandler(
		t,
		managedCursorHandler(t, auditSettings, "postToolUse", cursorAuditScript),
		`{"conversation_id":"conversation-1","tool_name":"Shell","tool_input":{}}`+"\n",
	)
	if audit.exitCode != 1 || audit.stdout != "" || !strings.Contains(audit.stderr, "Failed to read agent config/key") {
		t.Fatalf("audit failure = %#v", audit)
	}
}

func TestCursorStatusRequiresExactPairIdentityAndRuntimeFiles(t *testing.T) {
	fixture := prepareCursorFixture(t, nil, false)
	installCursorFixture(t, fixture)
	status, err := fixture.plugin.Status()
	if err != nil || !status.Installed || !status.HookConfigured || !status.HookScriptExists {
		t.Fatalf("installed status = %#v, %v", status, err)
	}

	settings := readCursorObject(t, fixture.configPath)
	managedCursorHandler(t, settings, "preToolUse", "guard.js")["failClosed"] = false
	writeCursorObject(t, fixture.configPath, settings)
	status, err = fixture.plugin.Status()
	if err != nil || status.HookConfigured || status.Installed {
		t.Fatalf("modified handler status = %#v, %v", status, err)
	}

	installCursorFixture(t, fixture)
	settings = readCursorObject(t, fixture.configPath)
	delete(cursorObject(t, settings["hooks"]), "postToolUseFailure")
	writeCursorObject(t, fixture.configPath, settings)
	status, err = fixture.plugin.Status()
	if err != nil || status.HookConfigured || status.Installed {
		t.Fatalf("missing failure audit status = %#v, %v", status, err)
	}

	installCursorFixture(t, fixture)
	if err := os.Remove(fixture.hookPath); err != nil {
		t.Fatalf("remove audit runtime: %v", err)
	}
	status, err = fixture.plugin.Status()
	if err != nil || !status.HookConfigured || status.HookScriptExists || status.Installed {
		t.Fatalf("missing runtime status = %#v, %v", status, err)
	}

	installCursorFixture(t, fixture)
	runtimeConfig := readCursorObject(t, fixture.runtimeConfig)
	runtimeConfig["agent_id"] = "another-agent"
	writeCursorObject(t, fixture.runtimeConfig, runtimeConfig)
	status, err = fixture.plugin.Status()
	if err != nil || status.HookScriptExists || status.Installed {
		t.Fatalf("identity mismatch status = %#v, %v", status, err)
	}
	if err := os.WriteFile(fixture.runtimeConfig, []byte("{ malformed"), 0600); err != nil {
		t.Fatalf("write malformed runtime config: %v", err)
	}
	if _, err := fixture.plugin.Status(); err == nil || !strings.Contains(err.Error(), "parse Elydora runtime config") {
		t.Fatalf("malformed runtime status error = %v", err)
	}
}

func TestCursorUninstallRemovesExactOwnershipAndPreservesUserEntries(t *testing.T) {
	fixture := prepareCursorFixture(t, cursorJSON(map[string]any{
		"version": float64(1),
		"hooks":   map[string]any{"sessionStart": []any{map[string]any{"command": "keep"}}},
	}), false)
	installCursorFixture(t, fixture)
	settings := readCursorObject(t, fixture.configPath)
	hooks := cursorObject(t, settings["hooks"])
	pre := managedCursorHandler(t, settings, "preToolUse", "guard.js")
	post := managedCursorHandler(t, settings, "postToolUse", "hook.js")
	otherPre := map[string]any{}
	otherPost := map[string]any{}
	otherFailure := map[string]any{}
	for key, value := range pre {
		otherPre[key] = value
	}
	for key, value := range post {
		otherPost[key] = value
		otherFailure[key] = value
	}
	otherPre["command"] = strings.ReplaceAll(cursorStringValue(otherPre["command"]), "agent-1", "agent-10")
	otherPost["command"] = strings.ReplaceAll(cursorStringValue(otherPost["command"]), "agent-1", "agent-10")
	hooks["preToolUse"] = append(cursorArray(t, hooks["preToolUse"]), otherPre, map[string]any{
		"command": "echo elydora agent-1 guard.js", "timeout": float64(10), "failClosed": true,
	})
	hooks["postToolUse"] = append(cursorArray(t, hooks["postToolUse"]), otherPost)
	otherFailure["command"] = strings.ReplaceAll(
		cursorStringValue(otherFailure["command"]),
		"agent-1",
		"agent-10",
	)
	hooks["postToolUseFailure"] = append(
		cursorArray(t, hooks["postToolUseFailure"]),
		otherFailure,
	)
	writeCursorObject(t, fixture.configPath, settings)
	uninstallID := cursorTestAgentID
	if runtime.GOOS == "windows" {
		uninstallID = strings.ToUpper(uninstallID)
	}
	if err := fixture.plugin.Uninstall(uninstallID); err != nil {
		t.Fatalf("uninstall Cursor hooks: %v", err)
	}
	remaining := readCursorObject(t, fixture.configPath)
	remainingHooks := cursorObject(t, remaining["hooks"])
	if len(cursorArray(t, remainingHooks["preToolUse"])) != 2 ||
		len(cursorArray(t, remainingHooks["postToolUse"])) != 1 ||
		len(cursorArray(t, remainingHooks["postToolUseFailure"])) != 1 ||
		len(cursorArray(t, remainingHooks["sessionStart"])) != 1 {
		t.Fatalf("remaining hooks = %#v", remainingHooks)
	}
}

func TestCursorRejectsInvalidConfigBeforeRuntimeWrites(t *testing.T) {
	cases := []string{
		"{ malformed", "[]\n", `{"hooks":{}}`, `{"version":2,"hooks":{}}`,
		`{"version":1,"hooks":null}`, `{"version":1,"hooks":{"preToolUse":null}}`,
		`{"version":1,"hooks":{"preToolUse":[null]}}`,
		`{"version":1,"version":1,"hooks":{}}`, `{"version":1,"hooks":{},}`,
		`{"version":1,"hooks":{/* comments are outside Cursor's JSON contract */}}`,
	}
	for index, source := range cases {
		t.Run(strings.ReplaceAll(source, "/", "_"), func(t *testing.T) {
			fixture := prepareCursorFixture(t, cursorString(source), true)
			originalGuard, readErr := os.ReadFile(fixture.guardPath)
			if readErr != nil {
				t.Fatalf("read original guard: %v", readErr)
			}
			if err := fixture.plugin.Install(fixture.config); err == nil {
				t.Fatalf("invalid config %d was accepted: %s", index, source)
			}
			raw, err := os.ReadFile(fixture.configPath)
			if err != nil || string(raw) != source {
				t.Fatalf("config changed to %q, %v", raw, err)
			}
			guard, err := os.ReadFile(fixture.guardPath)
			if err != nil || string(guard) != string(originalGuard) {
				t.Fatalf("guard changed, %v", err)
			}
			for _, path := range []string{fixture.hookPath, fixture.runtimeConfig, fixture.privateKey} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("runtime write exists at %s: %v", path, err)
				}
			}
		})
	}
}

func TestCursorCreatesMissingGuardAndRejectsUnmanagedRuntimePaths(t *testing.T) {
	fixture := prepareCursorFixture(t, nil, false)
	installCursorFixture(t, fixture)
	if exists, err := regularFileExists(fixture.guardPath, "Cursor guard"); err != nil || !exists {
		t.Fatalf("guard exists = %v, %v", exists, err)
	}

	for _, field := range []string{"guard", "audit"} {
		t.Run(field, func(t *testing.T) {
			fixture := prepareCursorFixture(t, nil, false)
			unmanaged := filepath.Join(fixture.homeDir, "unmanaged-"+field+".js")
			if field == "guard" {
				fixture.config.GuardScriptPath = unmanaged
			} else {
				fixture.config.HookScript = unmanaged
			}
			if err := fixture.plugin.Install(fixture.config); err == nil ||
				!strings.Contains(err.Error(), "managed agent directory") {
				t.Fatalf("unmanaged %s error = %v", field, err)
			}
			if _, err := os.Stat(fixture.configPath); !os.IsNotExist(err) {
				t.Fatalf("Cursor config exists after rejected path: %v", err)
			}
		})
	}
}

func TestCursorUninstallRemovesOwnedConfigAndPreservesUserEmptyConfig(t *testing.T) {
	fixture := prepareCursorFixture(t, nil, false)
	installCursorFixture(t, fixture)
	if err := fixture.plugin.Uninstall(cursorTestAgentID); err != nil {
		t.Fatalf("uninstall Cursor hooks: %v", err)
	}
	if _, err := os.Stat(fixture.configPath); !os.IsNotExist(err) {
		t.Fatalf("owned Cursor config remains: %v", err)
	}
	if err := fixture.plugin.Uninstall(cursorTestAgentID); err != nil {
		t.Fatalf("uninstall absent Cursor hooks: %v", err)
	}
	userSource := `{"version":1,"hooks":{}}` + "\n"
	writeOptionalCursorFile(t, fixture.configPath, cursorString(userSource))
	if err := fixture.plugin.Uninstall(cursorTestAgentID); err != nil {
		t.Fatalf("uninstall user Cursor config: %v", err)
	}
	raw, err := os.ReadFile(fixture.configPath)
	if err != nil || string(raw) != userSource {
		t.Fatalf("user config = %q, %v", raw, err)
	}
}
