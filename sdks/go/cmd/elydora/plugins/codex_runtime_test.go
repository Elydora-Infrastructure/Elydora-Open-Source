package plugins

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

type codexTestAPI struct {
	mu         sync.Mutex
	status     string
	postStatus int
	getAuth    string
	postAuth   string
	postBody   string
	operations []map[string]any
}

func (api *codexTestAPI) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	api.mu.Lock()
	defer api.mu.Unlock()
	response.Header().Set("Content-Type", "application/json")
	switch {
	case request.Method == http.MethodGet &&
		request.URL.EscapedPath() == "/v1/agents/agent-1":
		api.getAuth = request.Header.Get("Authorization")
		_ = json.NewEncoder(response).Encode(map[string]any{
			"agent": map[string]any{"status": api.status},
		})
	case request.Method == http.MethodPost && request.URL.Path == "/v1/operations":
		api.postAuth = request.Header.Get("Authorization")
		var operation map[string]any
		if err := json.NewDecoder(request.Body).Decode(&operation); err != nil {
			http.Error(response, err.Error(), http.StatusBadRequest)
			return
		}
		api.operations = append(api.operations, operation)
		status := api.postStatus
		if status == 0 {
			status = http.StatusCreated
		}
		response.WriteHeader(status)
		body := api.postBody
		if body == "" {
			body = `{"operation":{"accepted":true}}`
		}
		_, _ = response.Write([]byte(body))
	default:
		http.NotFound(response, request)
	}
}

func (api *codexTestAPI) snapshot() (string, string, []map[string]any) {
	api.mu.Lock()
	defer api.mu.Unlock()
	return api.getAuth, api.postAuth, append([]map[string]any(nil), api.operations...)
}

func codexCommandKey() string {
	if os.PathSeparator == '\\' {
		return "commandWindows"
	}
	return "command"
}

func codexOfficialPayload(event string) map[string]any {
	payload := map[string]any{
		"hook_event_name": event,
		"session_id":      "session-1",
		"turn_id":         "turn-1",
		"transcript_path": nil,
		"cwd":             "C:/workspace",
		"model":           "gpt-5",
		"permission_mode": "default",
		"tool_name":       "Bash",
		"tool_use_id":     "call-1",
		"tool_input":      map[string]any{"command": "echo test"},
	}
	if event == "PostToolUse" {
		payload["tool_response"] = map[string]any{
			"output":  "test",
			"success": true,
		}
	}
	return payload
}

func installCodexRuntimeFixture(
	t *testing.T,
	status string,
) (*codexFixture, *codexTestAPI, *httptest.Server) {
	t.Helper()
	api := &codexTestAPI{status: status}
	server := httptest.NewServer(api)
	fixture := prepareCodexFixture(t, "")
	fixture.config.BaseURL = server.URL
	fixture.config.Token = "ely_test_token"
	if err := fixture.plugin.Install(fixture.config); err != nil {
		server.Close()
		t.Fatalf("install Codex hooks: %v", err)
	}
	return fixture, api, server
}

func TestCodexGeneratedRuntimePreservesNativePayloadAuthAndChain(t *testing.T) {
	fixture, api, server := installCodexRuntimeFixture(t, "active")
	defer server.Close()
	settings := readCodexTestObject(t, fixture.configPath)
	key := codexCommandKey()
	guard := requireCodexHandler(t, settings, "PreToolUse", codexGuardStatus)
	audit := requireCodexHandler(t, settings, "PostToolUse", codexAuditStatus)

	if exitCode, stderr := runCodexCommand(
		t, guard[key].(string), fixture.homeDir, codexOfficialPayload("PreToolUse"),
	); exitCode != 0 || stderr != "" {
		t.Fatalf("guard exit = %d, stderr = %q", exitCode, stderr)
	}
	first := codexOfficialPayload("PostToolUse")
	second := codexOfficialPayload("PostToolUse")
	second["tool_use_id"] = "call-2"
	second["tool_response"] = map[string]any{"error": "command failed", "success": false}
	for _, payload := range []map[string]any{first, second} {
		if exitCode, stderr := runCodexCommand(
			t, audit[key].(string), fixture.homeDir, payload,
		); exitCode != 0 || stderr != "" {
			t.Fatalf("audit exit = %d, stderr = %q", exitCode, stderr)
		}
	}

	getAuth, postAuth, operations := api.snapshot()
	if getAuth != "Bearer ely_test_token" || postAuth != "Bearer ely_test_token" {
		t.Fatalf("authorization headers = %q, %q", getAuth, postAuth)
	}
	if len(operations) != 2 || !reflect.DeepEqual(operations[0]["payload"], first) ||
		!reflect.DeepEqual(operations[1]["payload"], second) {
		t.Fatalf("native operations = %#v", operations)
	}
	if operations[0]["prev_chain_hash"] != "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" ||
		operations[1]["prev_chain_hash"] != operations[0]["chain_hash"] {
		t.Fatalf("operation chain = %#v", operations)
	}
}

func TestCodexGeneratedGuardBlocksFrozenAndRevokedAgents(t *testing.T) {
	for _, status := range []string{"frozen", "revoked"} {
		t.Run(status, func(t *testing.T) {
			fixture, _, server := installCodexRuntimeFixture(t, status)
			defer server.Close()
			guard := requireCodexHandler(
				t, readCodexTestObject(t, fixture.configPath), "PreToolUse", codexGuardStatus,
			)
			exitCode, stderr := runCodexCommand(
				t, guard[codexCommandKey()].(string), fixture.homeDir,
				codexOfficialPayload("PreToolUse"),
			)
			if exitCode != 2 || !strings.Contains(stderr, "Tool execution blocked") {
				t.Fatalf("guard exit = %d, stderr = %q", exitCode, stderr)
			}
		})
	}
}

func TestCodexFailOpenGuardReportsInputConfigStatusAndAPIFailures(t *testing.T) {
	fixture, api, server := installCodexRuntimeFixture(t, "active")
	settings := readCodexTestObject(t, fixture.configPath)
	guard := requireCodexHandler(t, settings, "PreToolUse", codexGuardStatus)
	command := guard[codexCommandKey()].(string)
	runtimeConfig, err := os.ReadFile(fixture.runtimeConfig)
	if err != nil {
		t.Fatalf("read runtime config: %v", err)
	}

	malformedExit, malformedError := runCodexRawCommand(
		t, command, fixture.homeDir, []byte("{ malformed"),
	)
	if err := os.WriteFile(fixture.runtimeConfig, []byte("{ malformed"), 0600); err != nil {
		t.Fatalf("corrupt runtime config: %v", err)
	}
	configExit, configError := runCodexCommand(
		t, command, fixture.homeDir, codexOfficialPayload("PreToolUse"),
	)
	if err := os.WriteFile(fixture.runtimeConfig, runtimeConfig, 0600); err != nil {
		t.Fatalf("restore runtime config: %v", err)
	}
	_ = os.Remove(filepath.Join(fixture.agentDir, "status-cache.json"))
	api.mu.Lock()
	api.status = "unknown"
	api.mu.Unlock()
	statusExit, statusError := runCodexCommand(
		t, command, fixture.homeDir, codexOfficialPayload("PreToolUse"),
	)
	_ = os.Remove(filepath.Join(fixture.agentDir, "status-cache.json"))
	server.Close()
	apiExit, apiError := runCodexCommand(
		t, command, fixture.homeDir, codexOfficialPayload("PreToolUse"),
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

func TestCodexFailOpenAuditRecordsRuntimeAndAPIFailures(t *testing.T) {
	for _, testCase := range []struct {
		name, target, source, want string
	}{
		{"input", "input", "{ malformed", "invalid JSON"},
		{"config", "config.json", "{ malformed", "agent config"},
		{"key", "private.key", "invalid", "Private key"},
		{"chain", "chain-state.json", "{ malformed", "Chain state"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture, _, server := installCodexRuntimeFixture(t, "active")
			defer server.Close()
			audit := requireCodexHandler(
				t, readCodexTestObject(t, fixture.configPath), "PostToolUse", codexAuditStatus,
			)
			input := []byte(testCase.source)
			if testCase.target != "input" {
				if err := os.WriteFile(
					filepath.Join(fixture.agentDir, testCase.target),
					[]byte(testCase.source),
					0600,
				); err != nil {
					t.Fatalf("write invalid runtime source: %v", err)
				}
				input, _ = json.Marshal(codexOfficialPayload("PostToolUse"))
			}
			exitCode, _ := runCodexRawCommand(
				t, audit[codexCommandKey()].(string), fixture.homeDir, input,
			)
			log, err := os.ReadFile(filepath.Join(fixture.agentDir, "error.log"))
			if exitCode != 0 || err != nil ||
				!strings.Contains(strings.ToLower(string(log)), strings.ToLower(testCase.want)) {
				t.Fatalf("audit failure = %d, %q, %v", exitCode, log, err)
			}
		})
	}

	fixture, api, server := installCodexRuntimeFixture(t, "active")
	defer server.Close()
	api.mu.Lock()
	api.postStatus = http.StatusInternalServerError
	api.mu.Unlock()
	audit := requireCodexHandler(
		t, readCodexTestObject(t, fixture.configPath), "PostToolUse", codexAuditStatus,
	)
	exitCode, _ := runCodexCommand(
		t, audit[codexCommandKey()].(string), fixture.homeDir,
		codexOfficialPayload("PostToolUse"),
	)
	log, err := os.ReadFile(filepath.Join(fixture.agentDir, "error.log"))
	if exitCode != 0 || err != nil || !strings.Contains(string(log), "HTTP 500") {
		t.Fatalf("audit API failure = %d, %q, %v", exitCode, log, err)
	}
}

func TestCodexAuditRejectsInvalidServerChainHash(t *testing.T) {
	fixture, api, server := installCodexRuntimeFixture(t, "active")
	defer server.Close()
	api.mu.Lock()
	api.postStatus = http.StatusBadRequest
	api.postBody = `{"error":{"code":"PREV_HASH_MISMATCH","message":"Expected prev_chain_hash \"invalid\""}}`
	api.mu.Unlock()
	audit := requireCodexHandler(
		t, readCodexTestObject(t, fixture.configPath), "PostToolUse", codexAuditStatus,
	)
	exitCode, _ := runCodexCommand(
		t, audit[codexCommandKey()].(string), fixture.homeDir,
		codexOfficialPayload("PostToolUse"),
	)
	if exitCode != 0 {
		t.Fatalf("audit exit = %d", exitCode)
	}
	chainPath := filepath.Join(fixture.agentDir, "chain-state.json")
	if _, err := os.Stat(chainPath); !os.IsNotExist(err) {
		t.Fatalf("invalid server hash created chain state: %v", err)
	}
	log, err := os.ReadFile(filepath.Join(fixture.agentDir, "error.log"))
	if err != nil || !strings.Contains(string(log), "invalid chain hash") {
		t.Fatalf("audit error log = %q, %v", log, err)
	}
}

func TestCodexGeneratedRuntimePreservesLinkedStateTargets(t *testing.T) {
	for _, targetName := range []string{"status-cache.json", "chain-state.json", "error.log"} {
		t.Run(targetName, func(t *testing.T) {
			fixture, _, server := installCodexRuntimeFixture(t, "active")
			defer server.Close()
			target := filepath.Join(t.TempDir(), targetName+".target")
			source := []byte("external")
			if err := os.WriteFile(target, source, 0600); err != nil {
				t.Fatalf("write state target: %v", err)
			}
			codexSymlinkOrSkip(t, target, filepath.Join(fixture.agentDir, targetName))
			settings := readCodexTestObject(t, fixture.configPath)
			var command string
			var input []byte
			if targetName == "status-cache.json" {
				command = requireCodexHandler(
					t, settings, "PreToolUse", codexGuardStatus,
				)[codexCommandKey()].(string)
				input, _ = json.Marshal(codexOfficialPayload("PreToolUse"))
			} else {
				command = requireCodexHandler(
					t, settings, "PostToolUse", codexAuditStatus,
				)[codexCommandKey()].(string)
				if targetName == "error.log" {
					input = []byte("{ malformed")
				} else {
					input, _ = json.Marshal(codexOfficialPayload("PostToolUse"))
				}
			}
			if exitCode, _ := runCodexRawCommand(
				t, command, fixture.homeDir, input,
			); exitCode != 0 {
				t.Fatalf("runtime exit = %d", exitCode)
			}
			actual, err := os.ReadFile(target)
			if err != nil || string(actual) != string(source) {
				t.Fatalf("linked state target changed: %q, %v", actual, err)
			}
		})
	}
}

func TestCodexGeneratedHooksReportUnsafeRuntimeOrigin(t *testing.T) {
	fixture, _, server := installCodexRuntimeFixture(t, "active")
	defer server.Close()
	config := readCodexTestObject(t, fixture.runtimeConfig)
	config["base_url"] = `https://api.elydora.com\evil`
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("encode unsafe runtime config: %v", err)
	}
	if err := os.WriteFile(fixture.runtimeConfig, encoded, 0600); err != nil {
		t.Fatalf("write unsafe runtime config: %v", err)
	}
	_ = os.Remove(filepath.Join(fixture.agentDir, "status-cache.json"))
	settings := readCodexTestObject(t, fixture.configPath)
	guard := requireCodexHandler(t, settings, "PreToolUse", codexGuardStatus)
	audit := requireCodexHandler(t, settings, "PostToolUse", codexAuditStatus)
	guardExit, guardError := runCodexCommand(
		t, guard[codexCommandKey()].(string), fixture.homeDir,
		codexOfficialPayload("PreToolUse"),
	)
	auditExit, _ := runCodexCommand(
		t, audit[codexCommandKey()].(string), fixture.homeDir,
		codexOfficialPayload("PostToolUse"),
	)
	log, readErr := os.ReadFile(filepath.Join(fixture.agentDir, "error.log"))
	if guardExit != 0 || !strings.Contains(guardError, "absolute HTTP or HTTPS URL") ||
		auditExit != 0 || readErr != nil ||
		!strings.Contains(string(log), "absolute HTTP or HTTPS URL") {
		t.Fatalf("unsafe origin results = %d, %q, %d, %q, %v", guardExit, guardError, auditExit, log, readErr)
	}
}
