package plugins

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func installAugmentRuntimeFixture(
	t *testing.T,
	status string,
) (*augmentFixture, *codexTestAPI, *httptest.Server) {
	t.Helper()
	api := &codexTestAPI{status: status}
	server := httptest.NewServer(api)
	fixture := prepareAugmentFixture(t, augmentFixtureOptions{})
	fixture.config.BaseURL = server.URL
	if err := fixture.plugin.Install(fixture.config); err != nil {
		server.Close()
		t.Fatalf("install Auggie hooks: %v", err)
	}
	return fixture, api, server
}

func augmentOfficialPayload(event string) map[string]any {
	value := map[string]any{
		"hook_event_name": event,
		"conversation_id": "conversation-1",
		"workspace_roots": []any{"C:/workspace"},
		"tool_name":       "launch-process",
		"tool_input": map[string]any{
			"command": "go test ./...",
			"nested":  map[string]any{"preserve": true},
		},
		"is_mcp_tool": false,
		"conversation_data": []any{
			map[string]any{"role": "user", "content": "preserve this"},
		},
		"mcp_metadata": map[string]any{
			"server": "local", "transport": "stdio",
		},
		"user_context": map[string]any{"account": "user-1"},
		"future_field": map[string]any{"survives": []any{"exactly", float64(2)}},
	}
	if event == "PostToolUse" {
		value["tool_output"] = map[string]any{
			"stdout": "passed", "stderr": "", "exit_code": float64(0),
		}
	}
	return value
}

func marshalAugmentPayload(t *testing.T, payload map[string]any) []byte {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal Auggie payload: %v", err)
	}
	return encoded
}

func TestAugmentGuardPropagatesOfficialBlockingExitCode(t *testing.T) {
	for _, status := range []string{"frozen", "revoked"} {
		t.Run(status, func(t *testing.T) {
			fixture, _, server := installAugmentRuntimeFixture(t, status)
			defer server.Close()
			handler := augmentTestManagedHandler(
				t,
				readAugmentTestObject(t, fixture.configPath),
				"PreToolUse",
				fixture.guardWrapper,
			)
			exit, stdout, stderr := runAugmentCommand(
				t,
				handler["command"].(string),
				fixture,
				marshalAugmentPayload(t, augmentOfficialPayload("PreToolUse")),
			)
			if exit != 2 || stdout != "" ||
				!strings.Contains(strings.ToLower(stderr), status) ||
				!strings.Contains(stderr, "Tool execution blocked") {
				t.Fatalf("guard result = %d, %q, %q", exit, stdout, stderr)
			}
		})
	}
}

func TestAugmentAuditPreservesCompleteNativePayload(t *testing.T) {
	fixture, api, server := installAugmentRuntimeFixture(t, "active")
	defer server.Close()
	payload := augmentOfficialPayload("PostToolUse")
	handler := augmentTestManagedHandler(
		t,
		readAugmentTestObject(t, fixture.configPath),
		"PostToolUse",
		fixture.auditWrapper,
	)
	exit, stdout, stderr := runAugmentCommand(
		t,
		handler["command"].(string),
		fixture,
		marshalAugmentPayload(t, payload),
	)
	if exit != 0 || stdout != "" || stderr != "" {
		t.Fatalf("audit result = %d, %q, %q", exit, stdout, stderr)
	}
	_, postAuth, operations := api.snapshot()
	if postAuth != "Bearer token-1" ||
		len(operations) != 1 ||
		!reflect.DeepEqual(operations[0]["payload"], payload) {
		t.Fatalf("Auggie native operation = auth %q, %#v", postAuth, operations)
	}
	if operations[0]["prev_chain_hash"] !=
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" {
		t.Fatalf("Auggie operation chain = %#v", operations[0])
	}
}

func TestAugmentRuntimeFailuresRemainObservableAndFailOpen(t *testing.T) {
	fixture, _, server := installAugmentRuntimeFixture(t, "active")
	settings := readAugmentTestObject(t, fixture.configPath)
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
	server.Close()
	guardExit, _, guardError := runAugmentCommand(
		t,
		guard["command"].(string),
		fixture,
		marshalAugmentPayload(t, augmentOfficialPayload("PreToolUse")),
	)
	auditExit, _, _ := runAugmentCommand(
		t,
		audit["command"].(string),
		fixture,
		marshalAugmentPayload(t, augmentOfficialPayload("PostToolUse")),
	)
	malformedExit, _, _ := runAugmentCommand(
		t,
		audit["command"].(string),
		fixture,
		[]byte("{ malformed"),
	)
	log, err := os.ReadFile(filepath.Join(fixture.agentDir, "error.log"))
	if guardExit != 0 ||
		!strings.Contains(guardError, "Failed to resolve agent status") ||
		auditExit != 0 ||
		malformedExit != 0 ||
		err != nil ||
		!strings.Contains(string(log), "invalid JSON") ||
		!strings.Contains(strings.ToLower(string(log)), "fetch failed") {
		t.Fatalf(
			"fail-open results = (%d, %q), %d, %d, log=%q, err=%v",
			guardExit,
			guardError,
			auditExit,
			malformedExit,
			log,
			err,
		)
	}
}

func TestAugmentStatusRequiresExactRuntimeFiles(t *testing.T) {
	for _, name := range []string{
		"config.json",
		"private.key",
		augmentGuardScript,
		augmentAuditScript,
		augmentGuardWrapperName(),
		augmentAuditWrapperName(),
	} {
		t.Run("missing "+name, func(t *testing.T) {
			fixture := prepareAugmentFixture(t, augmentFixtureOptions{})
			if err := fixture.plugin.Install(fixture.config); err != nil {
				t.Fatalf("install Auggie hooks: %v", err)
			}
			status, err := fixture.plugin.Status()
			if err != nil || !status.Installed || !status.HookConfigured ||
				!status.HookScriptExists {
				t.Fatalf("installed status = %#v, %v", status, err)
			}
			path := filepath.Join(fixture.agentDir, name)
			if err := os.Remove(path); err != nil {
				t.Fatalf("remove %s: %v", path, err)
			}
			status, err = fixture.plugin.Status()
			if err != nil || status.Installed || !status.HookConfigured ||
				status.HookScriptExists {
				t.Fatalf("missing %s status = %#v, %v", path, status, err)
			}
		})
	}
	fixture := prepareAugmentFixture(t, augmentFixtureOptions{})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Auggie hooks for wrapper check: %v", err)
	}
	writeAugmentTestFile(
		t,
		fixture.auditWrapper,
		[]byte("tampered wrapper\n"),
		0700,
	)
	status, err := fixture.plugin.Status()
	if err != nil || status.Installed || status.HookScriptExists {
		t.Fatalf("tampered-wrapper status = %#v, %v", status, err)
	}
	fixture = prepareAugmentFixture(t, augmentFixtureOptions{})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Auggie hooks for key check: %v", err)
	}
	writeAugmentTestFile(t, fixture.privateKey, []byte("invalid"), 0600)
	if _, err := fixture.plugin.Status(); err == nil ||
		!strings.Contains(err.Error(), "canonical 32-byte") {
		t.Fatalf("invalid-key status error = %v", err)
	}
}

func TestAugmentStatusSurfacesInvalidRuntimeMetadata(t *testing.T) {
	for _, testCase := range []struct{ name, source, want string }{
		{"malformed", "{ malformed", "parse Elydora runtime config"},
		{"duplicate", `{"agent_name":"augment","agent_name":"augment"}`, "duplicate key"},
		{
			"unsupported",
			`{"org_id":"o","agent_id":"agent-1","kid":"k","base_url":"https://api.test","agent_name":"augment","extra":true}`,
			"unsupported field",
		},
		{
			"identity",
			`{"org_id":"o","agent_id":"other","kid":"k","base_url":"https://api.test","agent_name":"augment"}`,
			"identity does not match",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := prepareAugmentFixture(t, augmentFixtureOptions{})
			if err := fixture.plugin.Install(fixture.config); err != nil {
				t.Fatalf("install Auggie hooks: %v", err)
			}
			writeAugmentTestFile(
				t,
				fixture.runtimeConfig,
				[]byte(testCase.source),
				0600,
			)
			_, err := fixture.plugin.Status()
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("status error = %v, want %q", err, testCase.want)
			}
		})
	}
}

func TestAugmentStatusIgnoresIncompleteManagedHookPairs(t *testing.T) {
	fixture := prepareAugmentFixture(t, augmentFixtureOptions{})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Auggie hooks: %v", err)
	}
	settings := readAugmentTestObject(t, fixture.configPath)
	delete(requireObject(t, settings["hooks"]), "PostToolUse")
	writeAugmentTestObject(t, fixture.configPath, settings)
	status, err := fixture.plugin.Status()
	if err != nil || status.Installed || status.HookConfigured {
		t.Fatalf("incomplete-pair status = %#v, %v", status, err)
	}
}

func TestAugmentRuntimeConfigOmitsEmptyToken(t *testing.T) {
	fixture := prepareAugmentFixture(t, augmentFixtureOptions{})
	fixture.config.Token = ""
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Auggie hooks: %v", err)
	}
	config := readAugmentTestObject(t, fixture.runtimeConfig)
	if _, exists := config["token"]; exists {
		t.Fatalf("empty token persisted: %#v", config)
	}
}
