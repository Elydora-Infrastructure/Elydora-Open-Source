package plugins

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type mismatchAPI struct {
	mu         sync.Mutex
	rejections int
	delay      time.Duration
	staleHead  bool
	staleLock  bool
	liveLock   bool
	statePath  string
	operations []map[string]any
}

const mismatchExpected = "Rxlf4j36C3KvIQ3hWuOkX698BR5iDypUFuB70JjEuvM"

func (api *mismatchAPI) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	api.mu.Lock()
	defer api.mu.Unlock()
	response.Header().Set("Content-Type", "application/json")
	if request.Method != http.MethodPost || request.URL.Path != "/v1/operations" {
		_ = json.NewEncoder(response).Encode(map[string]any{"agent": map[string]any{"status": "active"}})
		return
	}
	var operation map[string]any
	if err := json.NewDecoder(request.Body).Decode(&operation); err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}
	api.operations = append(api.operations, operation)
	count := len(api.operations)
	if api.delay > 0 {
		api.mu.Unlock()
		time.Sleep(api.delay)
		api.mu.Lock()
	}
	if api.statePath != "" {
		_ = os.WriteFile(api.statePath, []byte(`{"prev_chain_hash":"`+strings.Repeat("B", 43)+`"}`), 0o600)
	}
	if count > api.rejections {
		response.WriteHeader(http.StatusAccepted)
		_, _ = fmt.Fprintf(response, `{"receipt":{"seq_no":%d}}`, count)
		return
	}
	expected := mismatchExpected
	if api.rejections != 1 {
		expected = strings.Repeat("A", 42) + fmt.Sprint(count)
	}
	response.WriteHeader(http.StatusBadRequest)
	_, _ = fmt.Fprintf(
		response,
		`{"error":{"code":"PREV_HASH_MISMATCH","message":"Expected prev_chain_hash \"%s\", got \"x\"."}}`,
		expected,
	)
}

func runClaudeAuditAgainst(t *testing.T, api *mismatchAPI) (*claudeFixture, []map[string]any, time.Duration) {
	t.Helper()
	server := httptest.NewServer(api)
	defer server.Close()
	fixture := prepareClaudeFixture(t, claudeFixtureOptions{})
	fixture.config.BaseURL = server.URL
	fixture.config.Token = "ely_test_token"
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Claude hooks: %v", err)
	}
	settings := readClaudeTestObject(t, fixture.configPath)
	audit := claudeTestManagedHandler(t, settings, "PostToolUse", claudeAuditScript, claudeAuditStatusMessage)
	if api.staleHead {
		api.mu.Lock()
		api.statePath = filepath.Join(fixture.agentDir, "chain-state.json")
		api.mu.Unlock()
	}
	if api.staleLock || api.liveLock {
		lockPath := filepath.Join(fixture.agentDir, "chain-state.json.lock")
		var owner []byte
		if api.liveLock {
			owner = []byte(fmt.Sprint(os.Getpid()))
		}
		if err := os.WriteFile(lockPath, owner, 0o600); err != nil {
			t.Fatalf("write stale lock: %v", err)
		}
		stale := time.Now().Add(-10 * time.Second)
		if err := os.Chtimes(lockPath, stale, stale); err != nil {
			t.Fatalf("age stale lock: %v", err)
		}
	}
	started := time.Now()
	exit, stdout, stderr := runClaudeHandler(t, audit, fixture, claudeOfficialPayload("PostToolUse"))
	elapsed := time.Since(started)
	if exit != 0 || stdout != "" || stderr != "" {
		t.Fatalf("audit result = %d, %q, %q", exit, stdout, stderr)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	return fixture, api.operations, elapsed
}

func TestClaudeAuditRetriesRejectedChainHash(t *testing.T) {
	fixture, operations, _ := runClaudeAuditAgainst(t, &mismatchAPI{rejections: 1})
	if len(operations) != 2 || operations[1]["prev_chain_hash"] != mismatchExpected {
		t.Fatalf("operations = %#v", operations)
	}
	if operations[0]["operation_id"] == operations[1]["operation_id"] || operations[0]["nonce"] == operations[1]["nonce"] {
		t.Fatalf("retry reused identifiers: %#v", operations)
	}
	state, err := os.ReadFile(filepath.Join(fixture.agentDir, "chain-state.json"))
	if err != nil {
		t.Fatalf("read chain state: %v", err)
	}
	if !strings.Contains(string(state), fmt.Sprint(operations[1]["chain_hash"])) {
		t.Fatalf("chain state %s does not hold %v", state, operations[1]["chain_hash"])
	}
	log, err := os.ReadFile(filepath.Join(fixture.agentDir, "error.log"))
	if err != nil || !strings.Contains(string(log), "resynced to server: Rxlf") {
		t.Fatalf("error log = %q, %v", log, err)
	}
}

func TestClaudeAuditStopsAfterFiveRejections(t *testing.T) {
	fixture, operations, _ := runClaudeAuditAgainst(t, &mismatchAPI{rejections: 99})
	if len(operations) != 5 || operations[4]["prev_chain_hash"] != strings.Repeat("A", 42)+"4" {
		t.Fatalf("operations = %#v", operations)
	}
	log, err := os.ReadFile(filepath.Join(fixture.agentDir, "error.log"))
	if err != nil || !strings.Contains(string(log), "rejected prev_chain_hash 5 times") {
		t.Fatalf("error log = %q, %v", log, err)
	}
}

func TestClaudeAuditStopsRetryingWhenBudgetSpent(t *testing.T) {
	fixture, operations, elapsed := runClaudeAuditAgainst(t, &mismatchAPI{rejections: 99, delay: 2600 * time.Millisecond})
	if elapsed > 7500*time.Millisecond {
		t.Fatalf("hook ran %s", elapsed)
	}
	if len(operations) < 2 || len(operations) > 3 {
		t.Fatalf("submissions = %d", len(operations))
	}
	log, err := os.ReadFile(filepath.Join(fixture.agentDir, "error.log"))
	if err != nil || !(strings.Contains(string(log), "aborted") || strings.Contains(string(log), "retry budget exhausted")) {
		t.Fatalf("error log = %q, %v", log, err)
	}
}

func TestClaudeAuditDoesNotRegressAConcurrentlyAdvancedChainHead(t *testing.T) {
	fixture, operations, _ := runClaudeAuditAgainst(t, &mismatchAPI{staleHead: true})
	if len(operations) != 1 || operations[0]["prev_chain_hash"] != strings.Repeat("A", 43) {
		t.Fatalf("operations = %#v", operations)
	}
	state, err := os.ReadFile(filepath.Join(fixture.agentDir, "chain-state.json"))
	if err != nil || !strings.Contains(string(state), strings.Repeat("B", 43)) {
		t.Fatalf("chain state = %s, %v", state, err)
	}
}

func TestClaudeAuditClearsAStaleChainStateLock(t *testing.T) {
	fixture, operations, _ := runClaudeAuditAgainst(t, &mismatchAPI{staleLock: true})
	if len(operations) != 1 {
		t.Fatalf("operations = %#v", operations)
	}
	if _, err := os.Stat(filepath.Join(fixture.agentDir, "chain-state.json.lock")); !os.IsNotExist(err) {
		t.Fatalf("stale lock still present: %v", err)
	}
	state, err := os.ReadFile(filepath.Join(fixture.agentDir, "chain-state.json"))
	if err != nil || !strings.Contains(string(state), fmt.Sprint(operations[0]["chain_hash"])) {
		t.Fatalf("chain state = %s, %v", state, err)
	}
}

func TestClaudeAuditDoesNotReclaimALiveOwnersLock(t *testing.T) {
	fixture, operations, _ := runClaudeAuditAgainst(t, &mismatchAPI{liveLock: true})
	if len(operations) != 1 {
		t.Fatalf("operations = %#v", operations)
	}
	if _, err := os.Stat(filepath.Join(fixture.agentDir, "chain-state.json.lock")); err != nil {
		t.Fatalf("live lock was removed: %v", err)
	}
	log, err := os.ReadFile(filepath.Join(fixture.agentDir, "error.log"))
	if err != nil || !strings.Contains(string(log), "lock timed out") {
		t.Fatalf("error log = %q, %v", log, err)
	}
}
