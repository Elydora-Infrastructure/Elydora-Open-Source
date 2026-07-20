package plugins

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func clineOfficialPayload(event string) map[string]any {
	payload := map[string]any{
		"clineVersion": "3.0.46",
		"hookName":     event,
		"timestamp":    "2026-07-19T12:00:00.000Z",
		"taskId":       "task-1",
		"workspaceRoots": []any{
			"C:/workspace",
		},
		"userId":          "user-1",
		"agent_id":        "cline-agent",
		"parent_agent_id": nil,
	}
	if event == "tool_call" {
		payload["tool_call"] = map[string]any{
			"id": "call-1", "name": "read_file",
			"input": map[string]any{"path": "README.md"},
		}
		payload["preToolUse"] = map[string]any{
			"toolName":   "read_file",
			"parameters": map[string]any{"path": "README.md"},
		}
	}
	return payload
}

func marshalClinePayload(t *testing.T, payload map[string]any) []byte {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal Cline payload: %v", err)
	}
	return encoded
}

func installClineRuntimeFixture(
	t *testing.T,
	status string,
) (*clineFixture, *codexTestAPI, *httptest.Server) {
	t.Helper()
	api := &codexTestAPI{status: status}
	server := httptest.NewServer(api)
	fixture := prepareClineFixture(t, clineFixtureOptions{})
	fixture.config.BaseURL = server.URL
	if err := fixture.plugin.Install(fixture.config); err != nil {
		server.Close()
		t.Fatalf("install Cline hooks: %v", err)
	}
	return fixture, api, server
}

func TestClineGeneratedGuardEmitsPureCancellationForFrozenAndRevokedAgents(t *testing.T) {
	for _, status := range []string{"frozen", "revoked"} {
		t.Run(status, func(t *testing.T) {
			fixture, _, server := installClineRuntimeFixture(t, status)
			defer server.Close()
			result := runClineWrapper(
				t,
				fixture,
				fixture.guardWrapper,
				marshalClinePayload(t, clineOfficialPayload("tool_call")),
			)
			control := decodeClineControl(t, result.stdout)
			if result.exitCode != 0 || control["cancel"] != true ||
				!strings.Contains(strings.ToLower(result.stderr), status) ||
				!strings.Contains(result.stderr, "Tool execution blocked") {
				t.Fatalf("guard result = %#v, %#v", result, control)
			}
		})
	}
}

func TestClineGeneratedRuntimesKeepFailuresObservableAndFailOpen(t *testing.T) {
	fixture, _, server := installClineRuntimeFixture(t, "active")
	payload := marshalClinePayload(t, clineOfficialPayload("tool_call"))
	server.Close()
	guard := runClineWrapper(t, fixture, fixture.guardWrapper, payload)
	audit := runClineWrapper(t, fixture, fixture.auditWrapper, []byte("{ malformed"))
	log, err := os.ReadFile(filepath.Join(fixture.agentDir, "error.log"))
	if guard.exitCode != 0 || !strings.Contains(guard.stderr, "Failed to resolve agent status") ||
		audit.exitCode != 0 || err != nil ||
		!strings.Contains(strings.ToLower(string(log)), "invalid json") {
		t.Fatalf("fail-open results = %#v, %#v, log=%q, err=%v", guard, audit, log, err)
	}
}
