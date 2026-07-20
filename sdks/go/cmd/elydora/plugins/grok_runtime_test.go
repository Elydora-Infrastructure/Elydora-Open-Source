package plugins

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func installGrokRuntimeFixture(
	t *testing.T,
	status string,
) (*grokFixture, *codexTestAPI, *httptest.Server) {
	t.Helper()
	api := &codexTestAPI{status: status}
	server := httptest.NewServer(api)
	fixture := prepareGrokFixture(t, grokFixtureOptions{})
	fixture.config.BaseURL = server.URL
	fixture.config.Token = "ely_test_token"
	if err := fixture.plugin.Install(fixture.config); err != nil {
		server.Close()
		t.Fatalf("install Grok hooks: %v", err)
	}
	return fixture, api, server
}

func TestGrokGeneratedRuntimePreservesNativePayloadAuthAndChain(t *testing.T) {
	fixture, api, server := installGrokRuntimeFixture(t, "active")
	defer server.Close()
	settings := readGrokTestObject(t, fixture.configPath)
	guard := grokTestManagedHandler(t, settings, "PreToolUse", grokGuardScript)
	success := grokTestManagedHandler(t, settings, "PostToolUse", grokAuditScript)
	failure := grokTestManagedHandler(t, settings, "PostToolUseFailure", grokAuditScript)

	if exitCode, stdout, stderr := runGrokCommand(
		t,
		guard["command"].(string),
		fixture,
		grokOfficialPayload("PreToolUse"),
	); exitCode != 0 || stdout != "" || stderr != "" {
		t.Fatalf("guard result = %d, %q, %q", exitCode, stdout, stderr)
	}
	successPayload := grokOfficialPayload("PostToolUse")
	failurePayload := grokOfficialPayload("PostToolUseFailure")
	for _, item := range []struct {
		command string
		payload map[string]any
	}{
		{success["command"].(string), successPayload},
		{failure["command"].(string), failurePayload},
	} {
		if exitCode, stdout, stderr := runGrokCommand(
			t,
			item.command,
			fixture,
			item.payload,
		); exitCode != 0 || stdout != "" || stderr != "" {
			t.Fatalf("audit result = %d, %q, %q", exitCode, stdout, stderr)
		}
	}

	getAuth, postAuth, operations := api.snapshot()
	if getAuth != "Bearer ely_test_token" || postAuth != "Bearer ely_test_token" {
		t.Fatalf("authorization headers = %q, %q", getAuth, postAuth)
	}
	if len(operations) != 2 ||
		!reflect.DeepEqual(operations[0]["payload"], successPayload) ||
		!reflect.DeepEqual(operations[1]["payload"], failurePayload) {
		t.Fatalf("Grok native operations = %#v", operations)
	}
	if operations[0]["prev_chain_hash"] !=
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" ||
		operations[1]["prev_chain_hash"] != operations[0]["chain_hash"] {
		t.Fatalf("Grok operation chain = %#v", operations)
	}
}

func TestGrokGuardEmitsOfficialDenyJSONAndExitCode(t *testing.T) {
	for _, status := range []string{"frozen", "revoked"} {
		t.Run(status, func(t *testing.T) {
			fixture, _, server := installGrokRuntimeFixture(t, status)
			defer server.Close()
			guard := grokTestManagedHandler(
				t,
				readGrokTestObject(t, fixture.configPath),
				"PreToolUse",
				grokGuardScript,
			)
			exitCode, stdout, stderr := runGrokCommand(
				t,
				guard["command"].(string),
				fixture,
				grokOfficialPayload("PreToolUse"),
			)
			if exitCode != 2 || !strings.Contains(strings.ToLower(stderr), status) {
				t.Fatalf("guard result = %d, %q, %q", exitCode, stdout, stderr)
			}
			decision := map[string]any{}
			if err := json.Unmarshal([]byte(stdout), &decision); err != nil {
				t.Fatalf("decode Grok denial: %v", err)
			}
			keys := make([]string, 0, len(decision))
			for key := range decision {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			if !reflect.DeepEqual(keys, []string{"decision", "reason"}) ||
				decision["decision"] != "deny" ||
				!strings.Contains(strings.ToLower(decision["reason"].(string)), status) {
				t.Fatalf("Grok denial = %#v", decision)
			}
		})
	}
}

func TestGrokFailOpenGuardReportsInputConfigStatusAndAPIFailures(t *testing.T) {
	fixture, api, server := installGrokRuntimeFixture(t, "active")
	guard := grokTestManagedHandler(
		t,
		readGrokTestObject(t, fixture.configPath),
		"PreToolUse",
		grokGuardScript,
	)
	command := guard["command"].(string)
	runtimeConfig, err := os.ReadFile(fixture.runtimeConfig)
	if err != nil {
		t.Fatalf("read runtime config: %v", err)
	}

	malformedExit, _, malformedError := runGrokRawCommand(
		t,
		command,
		fixture,
		[]byte("{ malformed"),
	)
	if err := os.WriteFile(fixture.runtimeConfig, []byte("{ malformed"), 0600); err != nil {
		t.Fatalf("corrupt runtime config: %v", err)
	}
	configExit, _, configError := runGrokCommand(
		t,
		command,
		fixture,
		grokOfficialPayload("PreToolUse"),
	)
	if err := os.WriteFile(fixture.runtimeConfig, runtimeConfig, 0600); err != nil {
		t.Fatalf("restore runtime config: %v", err)
	}
	_ = os.Remove(filepath.Join(fixture.agentDir, "status-cache.json"))
	api.mu.Lock()
	api.status = "unknown"
	api.mu.Unlock()
	statusExit, _, statusError := runGrokCommand(
		t,
		command,
		fixture,
		grokOfficialPayload("PreToolUse"),
	)
	_ = os.Remove(filepath.Join(fixture.agentDir, "status-cache.json"))
	server.Close()
	apiExit, _, apiError := runGrokCommand(
		t,
		command,
		fixture,
		grokOfficialPayload("PreToolUse"),
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

func TestGrokAuditRecordsMalformedInputAndAPIFailures(t *testing.T) {
	fixture, api, server := installGrokRuntimeFixture(t, "active")
	defer server.Close()
	command := grokTestManagedHandler(
		t,
		readGrokTestObject(t, fixture.configPath),
		"PostToolUseFailure",
		grokAuditScript,
	)["command"].(string)

	malformedExit, malformedOutput, malformedError := runGrokRawCommand(
		t,
		command,
		fixture,
		[]byte("{ malformed"),
	)
	api.mu.Lock()
	api.postStatus = http.StatusServiceUnavailable
	api.mu.Unlock()
	upstreamExit, upstreamOutput, upstreamError := runGrokCommand(
		t,
		command,
		fixture,
		grokOfficialPayload("PostToolUseFailure"),
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
