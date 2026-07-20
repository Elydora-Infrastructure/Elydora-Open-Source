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

func installKimiRuntimeFixture(
	t *testing.T,
	status string,
) (*kimiFixture, *codexTestAPI, *httptest.Server) {
	t.Helper()
	api := &codexTestAPI{status: status}
	server := httptest.NewServer(api)
	fixture := prepareKimiFixture(t, kimiFixtureOptions{withoutLegacyEvidence: true})
	fixture.config.BaseURL = server.URL
	fixture.config.Token = "ely_test_token"
	if err := fixture.plugin.Install(fixture.config); err != nil {
		server.Close()
		t.Fatalf("install Kimi hooks: %v", err)
	}
	return fixture, api, server
}

func TestKimiGeneratedRuntimePreservesSuccessAndFailurePayloads(t *testing.T) {
	fixture, api, server := installKimiRuntimeFixture(t, "active")
	defer server.Close()
	hooks := readKimiTestHooks(t, fixture.modernPath)
	guard := managedKimiTestHook(t, hooks, "PreToolUse")["command"].(string)
	success := managedKimiTestHook(t, hooks, "PostToolUse")["command"].(string)
	failure := managedKimiTestHook(t, hooks, "PostToolUseFailure")["command"].(string)

	if exitCode, stderr := runKimiCommand(
		t, guard, fixture, kimiOfficialPayload("PreToolUse"),
	); exitCode != 0 || stderr != "" {
		t.Fatalf("guard exit = %d, stderr = %q", exitCode, stderr)
	}
	successPayload := kimiOfficialPayload("PostToolUse")
	failurePayload := kimiOfficialPayload("PostToolUseFailure")
	for _, item := range []struct {
		command string
		payload map[string]any
	}{{success, successPayload}, {failure, failurePayload}} {
		if exitCode, stderr := runKimiCommand(
			t, item.command, fixture, item.payload,
		); exitCode != 0 || stderr != "" {
			t.Fatalf("audit exit = %d, stderr = %q", exitCode, stderr)
		}
	}

	getAuth, postAuth, operations := api.snapshot()
	if getAuth != "Bearer ely_test_token" || postAuth != "Bearer ely_test_token" {
		t.Fatalf("authorization headers = %q, %q", getAuth, postAuth)
	}
	if len(operations) != 2 ||
		!reflect.DeepEqual(operations[0]["payload"], successPayload) ||
		!reflect.DeepEqual(operations[1]["payload"], failurePayload) {
		t.Fatalf("Kimi native operations = %#v", operations)
	}
	if operations[0]["prev_chain_hash"] != "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" ||
		operations[1]["prev_chain_hash"] != operations[0]["chain_hash"] {
		t.Fatalf("Kimi operation chain = %#v", operations)
	}
}

func TestKimiGuardPropagatesOfficialBlockExitCode(t *testing.T) {
	for _, status := range []string{"frozen", "revoked"} {
		t.Run(status, func(t *testing.T) {
			fixture, _, server := installKimiRuntimeFixture(t, status)
			defer server.Close()
			command := managedKimiTestHook(
				t, readKimiTestHooks(t, fixture.modernPath), "PreToolUse",
			)["command"].(string)
			exitCode, stderr := runKimiCommand(
				t, command, fixture, kimiOfficialPayload("PreToolUse"),
			)
			if exitCode != 2 || !strings.Contains(stderr, "Tool execution blocked") {
				t.Fatalf("guard exit = %d, stderr = %q", exitCode, stderr)
			}
		})
	}
}

func TestKimiFailOpenGuardReportsInputConfigStatusAndAPIFailures(t *testing.T) {
	fixture, api, server := installKimiRuntimeFixture(t, "active")
	command := managedKimiTestHook(
		t, readKimiTestHooks(t, fixture.modernPath), "PreToolUse",
	)["command"].(string)
	runtimeConfig, err := os.ReadFile(fixture.runtimeConfig)
	if err != nil {
		t.Fatalf("read runtime config: %v", err)
	}

	malformedExit, malformedError := runKimiRawCommand(
		t, command, fixture, []byte("{ malformed"),
	)
	if err := os.WriteFile(fixture.runtimeConfig, []byte("{ malformed"), 0600); err != nil {
		t.Fatalf("corrupt runtime config: %v", err)
	}
	configExit, configError := runKimiCommand(
		t, command, fixture, kimiOfficialPayload("PreToolUse"),
	)
	if err := os.WriteFile(fixture.runtimeConfig, runtimeConfig, 0600); err != nil {
		t.Fatalf("restore runtime config: %v", err)
	}
	_ = os.Remove(filepath.Join(fixture.agentDir, "status-cache.json"))
	api.mu.Lock()
	api.status = "unknown"
	api.mu.Unlock()
	statusExit, statusError := runKimiCommand(
		t, command, fixture, kimiOfficialPayload("PreToolUse"),
	)
	_ = os.Remove(filepath.Join(fixture.agentDir, "status-cache.json"))
	server.Close()
	apiExit, apiError := runKimiCommand(
		t, command, fixture, kimiOfficialPayload("PreToolUse"),
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

func TestKimiAuditRecordsMalformedInputAndAPIFailures(t *testing.T) {
	fixture, api, server := installKimiRuntimeFixture(t, "active")
	defer server.Close()
	command := managedKimiTestHook(
		t, readKimiTestHooks(t, fixture.modernPath), "PostToolUseFailure",
	)["command"].(string)

	malformedExit, malformedError := runKimiRawCommand(
		t, command, fixture, []byte("{ malformed"),
	)
	api.mu.Lock()
	api.postStatus = http.StatusServiceUnavailable
	api.mu.Unlock()
	upstreamExit, upstreamError := runKimiCommand(
		t, command, fixture, kimiOfficialPayload("PostToolUseFailure"),
	)
	log, err := os.ReadFile(filepath.Join(fixture.agentDir, "error.log"))
	if malformedExit != 0 || malformedError != "" || upstreamExit != 0 || upstreamError != "" ||
		err != nil || !strings.Contains(string(log), "invalid JSON") ||
		!strings.Contains(string(log), "HTTP 503") {
		t.Fatalf(
			"audit failures = (%d, %q), (%d, %q), log=%q, err=%v",
			malformedExit, malformedError, upstreamExit, upstreamError, log, err,
		)
	}
}

func TestKimiFailurePayloadIncludesOfficialErrorShape(t *testing.T) {
	payload := kimiOfficialPayload("PostToolUseFailure")
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if decoded["hook_event_name"] != "PostToolUseFailure" || decoded["error"] == nil {
		t.Fatalf("failure payload = %#v", decoded)
	}
}
