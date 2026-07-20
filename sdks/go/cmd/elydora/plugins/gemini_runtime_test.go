package plugins

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func installGeminiRuntimeFixture(
	t *testing.T,
	status string,
) (*geminiFixture, *codexTestAPI, *httptest.Server) {
	t.Helper()
	api := &codexTestAPI{status: status}
	server := httptest.NewServer(api)
	fixture := prepareGeminiFixture(t, geminiFixtureOptions{})
	fixture.config.BaseURL = server.URL
	fixture.config.Token = "ely_test_token"
	if err := fixture.plugin.Install(fixture.config); err != nil {
		server.Close()
		t.Fatalf("install Gemini hooks: %v", err)
	}
	return fixture, api, server
}

func TestGeminiGeneratedRuntimePreservesNativePayloadAuthAndChain(t *testing.T) {
	fixture, api, server := installGeminiRuntimeFixture(t, "active")
	defer server.Close()
	settings := readGeminiTestObject(t, fixture.settingsPath)
	guard := geminiTestManagedHandler(
		t,
		settings,
		"BeforeTool",
		geminiGuardScript,
		geminiGuardHookName,
	)
	audit := geminiTestManagedHandler(
		t,
		settings,
		"AfterTool",
		geminiAuditScript,
		geminiAuditHookName,
	)
	guardPayload := geminiOfficialPayload(fixture, "BeforeTool")
	if exit, stdout, stderr := runGeminiCommand(
		t,
		guard["command"].(string),
		fixture,
		guardPayload,
	); exit != 0 || stdout != "{}\n" || stderr != "" {
		t.Fatalf("guard result = %d, %q, %q", exit, stdout, stderr)
	}
	firstPayload := geminiOfficialPayload(fixture, "AfterTool")
	firstPayload["mcp_context"] = map[string]any{
		"server_name": "filesystem",
		"tool_name":   "write_file",
	}
	firstPayload["original_request_name"] = "write_file"
	firstPayload["future_provider_field"] = map[string]any{"preserved": true}
	secondPayload := geminiOfficialPayload(fixture, "AfterTool")
	secondPayload["tool_response"] = map[string]any{
		"output": "second",
		"error":  nil,
	}
	for _, payload := range []map[string]any{firstPayload, secondPayload} {
		if exit, stdout, stderr := runGeminiCommand(
			t,
			audit["command"].(string),
			fixture,
			payload,
		); exit != 0 || stdout != "{}\n" || stderr != "" {
			t.Fatalf("audit result = %d, %q, %q", exit, stdout, stderr)
		}
	}
	getAuth, postAuth, operations := api.snapshot()
	if getAuth != "Bearer ely_test_token" || postAuth != "Bearer ely_test_token" {
		t.Fatalf("authorization headers = %q, %q", getAuth, postAuth)
	}
	if len(operations) != 2 ||
		!reflect.DeepEqual(operations[0]["payload"], firstPayload) ||
		!reflect.DeepEqual(operations[1]["payload"], secondPayload) {
		t.Fatalf("Gemini native operations = %#v", operations)
	}
	if operations[0]["prev_chain_hash"] !=
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" ||
		operations[1]["prev_chain_hash"] != operations[0]["chain_hash"] {
		t.Fatalf("Gemini operation chain = %#v", operations)
	}
}

func TestGeminiGuardUsesOfficialBlockingExitCode(t *testing.T) {
	for _, status := range []string{"frozen", "revoked"} {
		t.Run(status, func(t *testing.T) {
			fixture, _, server := installGeminiRuntimeFixture(t, status)
			defer server.Close()
			guard := geminiTestManagedHandler(
				t,
				readGeminiTestObject(t, fixture.settingsPath),
				"BeforeTool",
				geminiGuardScript,
				geminiGuardHookName,
			)
			exit, stdout, stderr := runGeminiCommand(
				t,
				guard["command"].(string),
				fixture,
				geminiOfficialPayload(fixture, "BeforeTool"),
			)
			if exit != 2 || stdout != "" ||
				!strings.Contains(strings.ToLower(stderr), status) {
				t.Fatalf("guard result = %d, %q, %q", exit, stdout, stderr)
			}
		})
	}
}

func TestGeminiRuntimesKeepFailOpenErrorsObservable(t *testing.T) {
	fixture, api, server := installGeminiRuntimeFixture(t, "active")
	settings := readGeminiTestObject(t, fixture.settingsPath)
	guardCommand := geminiTestManagedHandler(
		t,
		settings,
		"BeforeTool",
		geminiGuardScript,
		geminiGuardHookName,
	)["command"].(string)
	auditCommand := geminiTestManagedHandler(
		t,
		settings,
		"AfterTool",
		geminiAuditScript,
		geminiAuditHookName,
	)["command"].(string)
	invalidExit, invalidOutput, invalidError := runGeminiRawCommand(
		t,
		guardCommand,
		fixture,
		[]byte("{ malformed"),
	)
	if invalidExit != 0 || invalidOutput != "{}\n" ||
		!strings.Contains(invalidError, "invalid JSON") {
		t.Fatalf(
			"invalid guard result = %d, %q, %q",
			invalidExit,
			invalidOutput,
			invalidError,
		)
	}
	api.mu.Lock()
	api.postStatus = http.StatusServiceUnavailable
	api.mu.Unlock()
	auditExit, auditOutput, auditError := runGeminiCommand(
		t,
		auditCommand,
		fixture,
		geminiOfficialPayload(fixture, "AfterTool"),
	)
	server.Close()
	log, err := os.ReadFile(filepath.Join(fixture.agentDir, "error.log"))
	if auditExit != 0 || auditOutput != "{}\n" || auditError != "" ||
		err != nil || !strings.Contains(string(log), "HTTP 503") {
		t.Fatalf(
			"audit failure = %d, %q, %q, log=%q, err=%v",
			auditExit,
			auditOutput,
			auditError,
			log,
			err,
		)
	}
}

func TestGeminiRuntimeArtifactsUsePrivateModesOnPOSIX(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode bits are not authoritative on Windows")
	}
	fixture := prepareGeminiFixture(t, geminiFixtureOptions{})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Gemini hooks: %v", err)
	}
	for _, item := range []struct {
		path string
		mode os.FileMode
	}{
		{fixture.runtimeConfig, 0600},
		{fixture.privateKey, 0600},
		{fixture.guardPath, 0700},
		{fixture.hookPath, 0700},
		{fixture.settingsPath, 0600},
	} {
		info, err := os.Stat(item.path)
		if err != nil || info.Mode().Perm() != item.mode {
			t.Fatalf("mode for %s = %v, %v", item.path, info, err)
		}
	}
}

func TestGeminiRuntimeOutputIsValidJSON(t *testing.T) {
	fixture, _, server := installGeminiRuntimeFixture(t, "active")
	defer server.Close()
	guard := geminiTestManagedHandler(
		t,
		readGeminiTestObject(t, fixture.settingsPath),
		"BeforeTool",
		geminiGuardScript,
		geminiGuardHookName,
	)
	_, stdout, _ := runGeminiCommand(
		t,
		guard["command"].(string),
		fixture,
		geminiOfficialPayload(fixture, "BeforeTool"),
	)
	var response map[string]any
	if err := json.Unmarshal([]byte(stdout), &response); err != nil || len(response) != 0 {
		t.Fatalf("Gemini response = %q, %v", stdout, err)
	}
}
