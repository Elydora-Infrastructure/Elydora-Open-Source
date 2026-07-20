package plugins

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func installClaudeRuntimeFixture(
	t *testing.T,
	status string,
) (*claudeFixture, *codexTestAPI, *httptest.Server) {
	t.Helper()
	api := &codexTestAPI{status: status}
	server := httptest.NewServer(api)
	fixture := prepareClaudeFixture(t, claudeFixtureOptions{})
	fixture.config.BaseURL = server.URL
	fixture.config.Token = "ely_test_token"
	if err := fixture.plugin.Install(fixture.config); err != nil {
		server.Close()
		t.Fatalf("install Claude hooks: %v", err)
	}
	return fixture, api, server
}

func TestClaudeGeneratedRuntimePreservesNativePayloadAuthAndChain(t *testing.T) {
	fixture, api, server := installClaudeRuntimeFixture(t, "active")
	defer server.Close()
	settings := readClaudeTestObject(t, fixture.configPath)
	guard := claudeTestManagedHandler(
		t,
		settings,
		"PreToolUse",
		claudeGuardScript,
		claudeGuardStatusMessage,
	)
	success := claudeTestManagedHandler(
		t,
		settings,
		"PostToolUse",
		claudeAuditScript,
		claudeAuditStatusMessage,
	)
	failure := claudeTestManagedHandler(
		t,
		settings,
		"PostToolUseFailure",
		claudeAuditScript,
		claudeAuditStatusMessage,
	)
	if exit, stdout, stderr := runClaudeHandler(
		t,
		guard,
		fixture,
		claudeOfficialPayload("PreToolUse"),
	); exit != 0 || stdout != "" || stderr != "" {
		t.Fatalf("guard result = %d, %q, %q", exit, stdout, stderr)
	}
	successPayload := claudeOfficialPayload("PostToolUse")
	failurePayload := claudeOfficialPayload("PostToolUseFailure")
	for _, item := range []struct {
		handler map[string]any
		payload map[string]any
	}{
		{success, successPayload},
		{failure, failurePayload},
	} {
		if exit, stdout, stderr := runClaudeHandler(
			t,
			item.handler,
			fixture,
			item.payload,
		); exit != 0 || stdout != "" || stderr != "" {
			t.Fatalf("audit result = %d, %q, %q", exit, stdout, stderr)
		}
	}
	getAuth, postAuth, operations := api.snapshot()
	if getAuth != "Bearer ely_test_token" || postAuth != "Bearer ely_test_token" {
		t.Fatalf("authorization headers = %q, %q", getAuth, postAuth)
	}
	if len(operations) != 2 ||
		!reflect.DeepEqual(operations[0]["payload"], successPayload) ||
		!reflect.DeepEqual(operations[1]["payload"], failurePayload) {
		t.Fatalf("Claude native operations = %#v", operations)
	}
	if operations[0]["prev_chain_hash"] !=
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" ||
		operations[1]["prev_chain_hash"] != operations[0]["chain_hash"] {
		t.Fatalf("Claude operation chain = %#v", operations)
	}
}

func TestClaudeGuardUsesOfficialBlockingExitCode(t *testing.T) {
	for _, status := range []string{"frozen", "revoked"} {
		t.Run(status, func(t *testing.T) {
			fixture, _, server := installClaudeRuntimeFixture(t, status)
			defer server.Close()
			guard := claudeTestManagedHandler(
				t,
				readClaudeTestObject(t, fixture.configPath),
				"PreToolUse",
				claudeGuardScript,
				claudeGuardStatusMessage,
			)
			exit, stdout, stderr := runClaudeHandler(
				t,
				guard,
				fixture,
				claudeOfficialPayload("PreToolUse"),
			)
			if exit != 2 || stdout != "" ||
				!strings.Contains(strings.ToLower(stderr), status) {
				t.Fatalf("guard result = %d, %q, %q", exit, stdout, stderr)
			}
		})
	}
}

func TestClaudeFailOpenGuardReportsInputConfigStatusAndAPIFailures(t *testing.T) {
	fixture, api, server := installClaudeRuntimeFixture(t, "active")
	guard := claudeTestManagedHandler(
		t,
		readClaudeTestObject(t, fixture.configPath),
		"PreToolUse",
		claudeGuardScript,
		claudeGuardStatusMessage,
	)
	runtimeConfig, err := os.ReadFile(fixture.runtimeConfig)
	if err != nil {
		t.Fatalf("read runtime config: %v", err)
	}
	malformedExit, _, malformedError := runClaudeRawHandler(
		t,
		guard,
		fixture,
		[]byte("{ malformed"),
	)
	if err := os.WriteFile(fixture.runtimeConfig, []byte("{ malformed"), 0600); err != nil {
		t.Fatalf("corrupt runtime config: %v", err)
	}
	configExit, _, configError := runClaudeHandler(
		t,
		guard,
		fixture,
		claudeOfficialPayload("PreToolUse"),
	)
	if err := os.WriteFile(fixture.runtimeConfig, runtimeConfig, 0600); err != nil {
		t.Fatalf("restore runtime config: %v", err)
	}
	_ = os.Remove(filepath.Join(fixture.agentDir, "status-cache.json"))
	api.mu.Lock()
	api.status = "unknown"
	api.mu.Unlock()
	statusExit, _, statusError := runClaudeHandler(
		t,
		guard,
		fixture,
		claudeOfficialPayload("PreToolUse"),
	)
	_ = os.Remove(filepath.Join(fixture.agentDir, "status-cache.json"))
	server.Close()
	apiExit, _, apiError := runClaudeHandler(
		t,
		guard,
		fixture,
		claudeOfficialPayload("PreToolUse"),
	)
	for _, result := range []struct {
		exit int
		err  string
		want string
	}{
		{malformedExit, malformedError, "invalid JSON"},
		{configExit, configError, "Failed to read agent config"},
		{statusExit, statusError, "invalid agent status"},
		{apiExit, apiError, "Failed to resolve agent status"},
	} {
		if result.exit != 0 || !strings.Contains(result.err, result.want) {
			t.Fatalf("fail-open result = %d, %q; want %q", result.exit, result.err, result.want)
		}
	}
}

func TestClaudeAuditRecordsMalformedInputAndAPIFailures(t *testing.T) {
	fixture, api, server := installClaudeRuntimeFixture(t, "active")
	defer server.Close()
	handler := claudeTestManagedHandler(
		t,
		readClaudeTestObject(t, fixture.configPath),
		"PostToolUseFailure",
		claudeAuditScript,
		claudeAuditStatusMessage,
	)
	malformedExit, malformedOutput, malformedError := runClaudeRawHandler(
		t,
		handler,
		fixture,
		[]byte("{ malformed"),
	)
	api.mu.Lock()
	api.postStatus = http.StatusServiceUnavailable
	api.mu.Unlock()
	upstreamExit, upstreamOutput, upstreamError := runClaudeHandler(
		t,
		handler,
		fixture,
		claudeOfficialPayload("PostToolUseFailure"),
	)
	log, err := os.ReadFile(filepath.Join(fixture.agentDir, "error.log"))
	if malformedExit != 0 || malformedOutput != "" || malformedError != "" ||
		upstreamExit != 0 || upstreamOutput != "" || upstreamError != "" ||
		err != nil || !strings.Contains(string(log), "invalid JSON") ||
		!strings.Contains(string(log), "HTTP 503") {
		t.Fatalf(
			"audit failures = (%d, %q, %q), (%d, %q, %q), log=%q, err=%v",
			malformedExit,
			malformedOutput,
			malformedError,
			upstreamExit,
			upstreamOutput,
			upstreamError,
			log,
			err,
		)
	}
}

func TestClaudeStatusRequiresStrictRuntimeFiles(t *testing.T) {
	for _, name := range []string{"config.json", "private.key", "guard.js", "hook.js"} {
		t.Run("missing "+name, func(t *testing.T) {
			fixture := prepareClaudeFixture(t, claudeFixtureOptions{})
			if err := fixture.plugin.Install(fixture.config); err != nil {
				t.Fatalf("install Claude hooks: %v", err)
			}
			if err := os.Remove(filepath.Join(fixture.agentDir, name)); err != nil {
				t.Fatalf("remove %s: %v", name, err)
			}
			status, err := fixture.plugin.Status()
			if err != nil || status.Installed || !status.HookConfigured ||
				status.HookScriptExists {
				t.Fatalf("missing %s status = %#v, %v", name, status, err)
			}
		})
	}
}

func TestClaudeStatusSurfacesInvalidRuntimeMetadata(t *testing.T) {
	tests := []struct{ name, source, want string }{
		{"malformed", "{ malformed", "parse Elydora runtime config"},
		{"unsupported", `{"org_id":"o","agent_id":"agent-1","kid":"k","base_url":"https://api.test","agent_name":"claudecode","extra":true}`, "unsupported field"},
		{"identity", `{"org_id":"o","agent_id":"other","kid":"k","base_url":"https://api.test","agent_name":"claudecode"}`, "identity does not match"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := prepareClaudeFixture(t, claudeFixtureOptions{})
			if err := fixture.plugin.Install(fixture.config); err != nil {
				t.Fatalf("install Claude hooks: %v", err)
			}
			if err := os.WriteFile(
				fixture.runtimeConfig,
				[]byte(testCase.source),
				0600,
			); err != nil {
				t.Fatalf("replace runtime config: %v", err)
			}
			_, err := fixture.plugin.Status()
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("status error = %v, want %q", err, testCase.want)
			}
		})
	}
}

func TestClaudeRuntimeConfigOmitsEmptyToken(t *testing.T) {
	fixture := prepareClaudeFixture(t, claudeFixtureOptions{})
	fixture.config.Token = ""
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Claude hooks: %v", err)
	}
	config := readClaudeTestObject(t, fixture.runtimeConfig)
	if _, exists := config["token"]; exists {
		t.Fatalf("empty token persisted: %#v", config)
	}
	raw, err := json.Marshal(config)
	if err != nil || len(raw) == 0 {
		t.Fatalf("runtime config encoding = %q, %v", raw, err)
	}
}
