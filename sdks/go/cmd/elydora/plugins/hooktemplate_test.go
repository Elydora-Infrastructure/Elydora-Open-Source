package plugins

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const guardTestAgentID = "agent 1"

func writeGuardFixture(t *testing.T, status string) (string, string, *httptest.Server) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/v1/agents/agent%201" {
			t.Errorf("request path = %q, want encoded agent path", r.URL.EscapedPath())
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"agent": map[string]string{"status": status},
		})
	}))

	homeDir := t.TempDir()
	agentDir := filepath.Join(homeDir, ".elydora", guardTestAgentID)
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("create agent directory: %v", err)
	}
	config, err := json.Marshal(map[string]string{
		"agent_id": guardTestAgentID,
		"base_url": server.URL,
	})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "config.json"), config, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	scriptPath := filepath.Join(homeDir, "guard.js")
	if err := os.WriteFile(scriptPath, []byte(GenerateGuardScript("claudecode", guardTestAgentID)), 0755); err != nil {
		t.Fatalf("write guard script: %v", err)
	}

	return scriptPath, homeDir, server
}

func runGuard(t *testing.T, scriptPath string, homeDir string) (int, string) {
	t.Helper()

	nodeBinary, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is required to execute generated hook scripts")
	}
	cmd := exec.Command(nodeBinary, scriptPath)
	cmd.Env = append(os.Environ(), "HOME="+homeDir, "USERPROFILE="+homeDir)
	cmd.Stdin = strings.NewReader("{}")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err == nil {
		return 0, stderr.String()
	}
	exitError, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("run guard: %v", err)
	}
	return exitError.ExitCode(), stderr.String()
}

func TestGuardBlocksRemoteFrozenAgent(t *testing.T) {
	scriptPath, homeDir, server := writeGuardFixture(t, "frozen")
	defer server.Close()

	exitCode, stderr := runGuard(t, scriptPath, homeDir)
	if exitCode != 2 {
		t.Fatalf("exit code = %d, want 2; stderr = %q", exitCode, stderr)
	}
	if !strings.Contains(stderr, "Tool execution blocked") {
		t.Fatalf("stderr = %q, want blocking message", stderr)
	}
}

func TestGuardAllowsActiveAgent(t *testing.T) {
	scriptPath, homeDir, server := writeGuardFixture(t, "active")
	defer server.Close()

	exitCode, stderr := runGuard(t, scriptPath, homeDir)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", exitCode, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty output", stderr)
	}
}

func TestGuardRejectsFutureCachedStatus(t *testing.T) {
	scriptPath, homeDir, server := writeGuardFixture(t, "active")
	defer server.Close()

	cache := map[string]any{
		"status":    "frozen",
		"cached_at": time.Now().Add(time.Minute).UnixMilli(),
	}
	cacheJSON, err := json.Marshal(cache)
	if err != nil {
		t.Fatalf("marshal cache: %v", err)
	}
	cachePath := filepath.Join(homeDir, ".elydora", guardTestAgentID, "status-cache.json")
	if err := os.WriteFile(cachePath, cacheJSON, 0600); err != nil {
		t.Fatalf("write cache: %v", err)
	}

	exitCode, stderr := runGuard(t, scriptPath, homeDir)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", exitCode, stderr)
	}
	if !strings.Contains(stderr, "timestamp is in the future") {
		t.Fatalf("stderr = %q, want future timestamp diagnostic", stderr)
	}
}
