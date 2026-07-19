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
	"time"
)

func TestClineRegistryUsesNativeGlobalHookDirectory(t *testing.T) {
	entry := SupportedAgents["cline"]
	if entry.Name != "Cline" || entry.ConfigDir != "~/.cline/hooks" || entry.ConfigFile != "PreToolUse.mjs" {
		t.Fatalf("Cline registry entry = %#v", entry)
	}
	if _, ok := NewPlugin("cline").(*ClinePlugin); !ok {
		t.Fatalf("Cline plugin is not registered")
	}
}

func TestClineInstallWritesOnlyNativeGlobalHooksAndIsIdempotent(t *testing.T) {
	fixture := prepareClineFixture(t, clineFixtureOptions{})
	installClineFixture(t, fixture)
	originalGuard := readClineTestFile(t, fixture.guardWrapper)
	originalAudit := readClineTestFile(t, fixture.auditWrapper)

	installClineFixture(t, fixture)

	if readClineTestFile(t, fixture.guardWrapper) != originalGuard {
		t.Fatalf("guard wrapper changed during idempotent install")
	}
	if readClineTestFile(t, fixture.auditWrapper) != originalAudit {
		t.Fatalf("audit wrapper changed during idempotent install")
	}
	if !strings.HasPrefix(originalGuard, "#!/usr/bin/env node\n// @elydora-cline-hook ") {
		t.Fatalf("guard wrapper has unexpected header: %q", originalGuard)
	}
	entries, err := os.ReadDir(fixture.hooksDir)
	if err != nil {
		t.Fatalf("read Cline hook directory: %v", err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Fatalf("temporary hook file remains: %s", entry.Name())
		}
	}
	requireMissingClineTestFile(t, filepath.Join(fixture.homeDir, "Documents", "Cline", "Hooks", "PreToolUse.mjs"))
	requireMissingClineTestFile(t, filepath.Join(fixture.workspaceDir, ".cline", "hooks", "PreToolUse.mjs"))
	requireMissingClineTestFile(t, filepath.Join(fixture.workspaceDir, ".clinerules", "hooks", "PreToolUse.mjs"))
}

func TestClineInstallUsesOfficialDefaultWhenClineDirIsEmpty(t *testing.T) {
	fixture := prepareClineFixture(t, clineFixtureOptions{})
	t.Setenv("CLINE_DIR", "")
	installClineFixture(t, fixture)

	defaultHooks := filepath.Join(fixture.homeDir, ".cline", "hooks")
	readClineTestFile(t, filepath.Join(defaultHooks, "PreToolUse.mjs"))
	readClineTestFile(t, filepath.Join(defaultHooks, "PostToolUse.mjs"))
	requireMissingClineTestFile(t, fixture.guardWrapper)
}

func TestClineWrappersTranslateFreezeAndForwardPayloadByteForByte(t *testing.T) {
	fixture := prepareClineFixture(t, clineFixtureOptions{})
	installClineFixture(t, fixture)
	capturePath := filepath.Join(t.TempDir(), "captured-event.json")
	captureJSON, err := json.Marshal(capturePath)
	if err != nil {
		t.Fatalf("marshal capture path: %v", err)
	}
	captureScript := "const fs = require('node:fs'); const chunks = []; " +
		"process.stdin.on('data', chunk => chunks.push(chunk)); " +
		"process.stdin.on('end', () => fs.writeFileSync(" + string(captureJSON) + ", JSON.stringify({" +
		"cwd: process.cwd(), input: Buffer.concat(chunks).toString('utf-8')})));\n"
	if err := os.WriteFile(fixture.hookPath, []byte(captureScript), 0700); err != nil {
		t.Fatalf("write capture hook: %v", err)
	}

	prePayload := []byte(`{"clineVersion":"3.0.46","hookName":"tool_call","taskId":"task-1","tool_call":{"id":"call-1","name":"read_file","input":{"path":"README.md"}}}`)
	guard := runClineWrapper(t, fixture, fixture.guardWrapper, prePayload)
	if guard.exitCode != 0 || !strings.Contains(guard.stderr, "Agent is frozen by Elydora") {
		t.Fatalf("guard result = %#v", guard)
	}
	control := decodeClineControl(t, guard.stdout)
	if control["cancel"] != true || control["errorMessage"] != "Agent is frozen by Elydora." {
		t.Fatalf("guard control = %#v", control)
	}

	postPayload := []byte("{\n  \"clineVersion\": \"3.0.46\",\n  \"hookName\": \"tool_result\",\n  \"taskId\": \"task-1\",\n  \"tool_result\": {\"name\": \"read_file\", \"input\": {\"path\": \"README.md\"}}\n}")
	audit := runClineWrapper(t, fixture, fixture.auditWrapper, postPayload)
	if audit.exitCode != 0 || audit.stdout != "" {
		t.Fatalf("audit result = %#v", audit)
	}
	var captured map[string]any
	if err := json.Unmarshal([]byte(readClineTestFile(t, capturePath)), &captured); err != nil {
		t.Fatalf("decode captured payload: %v", err)
	}
	if captured["cwd"] != fixture.workspaceDir || captured["input"] != string(postPayload) {
		t.Fatalf("captured payload = %#v", captured)
	}
}

func TestClineAuditRuntimeMapsOfficialNestedFields(t *testing.T) {
	operations := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		var operation map[string]any
		if err := json.NewDecoder(request.Body).Decode(&operation); err != nil {
			http.Error(response, err.Error(), http.StatusBadRequest)
			return
		}
		operations <- operation
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusCreated)
		_, _ = response.Write([]byte(`{}`))
	}))
	defer server.Close()

	fixture := prepareClineFixture(t, clineFixtureOptions{})
	fixture.config.BaseURL = server.URL
	installClineFixture(t, fixture)
	payload := []byte(`{"hookName":"tool_result","taskId":"task-1","tool_result":{"name":"read_file","input":{"path":"README.md"},"output":"ok"}}`)
	result := runClineWrapper(t, fixture, fixture.auditWrapper, payload)
	if result.exitCode != 0 {
		t.Fatalf("audit wrapper result = %#v", result)
	}

	var operation map[string]any
	select {
	case operation = <-operations:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for Elydora operation")
	}
	wantPayload := map[string]any{
		"tool_name":  "read_file",
		"tool_input": map[string]any{"path": "README.md"},
		"session_id": "task-1",
	}
	if !reflect.DeepEqual(requireClineTestObject(t, operation["payload"]), wantPayload) {
		t.Fatalf("operation payload = %#v", operation["payload"])
	}
	if !reflect.DeepEqual(requireClineTestObject(t, operation["action"]), map[string]any{"tool": "read_file"}) {
		t.Fatalf("operation action = %#v", operation["action"])
	}
	if !reflect.DeepEqual(requireClineTestObject(t, operation["subject"]), map[string]any{"session_id": "task-1"}) {
		t.Fatalf("operation subject = %#v", operation["subject"])
	}
}

func TestClineWrappersKeepPassesQuietAndSurfaceRuntimeFailures(t *testing.T) {
	passing := prepareClineFixture(t, clineFixtureOptions{
		GuardSource: clineString("process.stdin.resume();\n"),
	})
	installClineFixture(t, passing)
	passResult := runClineWrapper(t, passing, passing.guardWrapper, []byte(`{}`))
	if passResult.exitCode != 0 || passResult.stdout != "" {
		t.Fatalf("passing guard result = %#v", passResult)
	}

	failing := prepareClineFixture(t, clineFixtureOptions{})
	installClineFixture(t, failing)
	if err := os.WriteFile(
		failing.hookPath,
		[]byte("process.stderr.write('audit failed\\n'); process.exit(7);\n"),
		0700,
	); err != nil {
		t.Fatalf("write failing audit runtime: %v", err)
	}
	failResult := runClineWrapper(t, failing, failing.auditWrapper, []byte(`{}`))
	if failResult.exitCode != 1 || !strings.Contains(failResult.stderr, "audit failed") ||
		!strings.Contains(failResult.stderr, "exited with code 7") {
		t.Fatalf("failing audit result = %#v", failResult)
	}
}
