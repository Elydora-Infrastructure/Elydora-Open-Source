package plugins

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const cursorTestAgentID = "agent-1"

type cursorFixture struct {
	plugin        *CursorPlugin
	config        InstallConfig
	homeDir       string
	agentDir      string
	configPath    string
	guardPath     string
	hookPath      string
	runtimeConfig string
	privateKey    string
}

type cursorCommandResult struct {
	exitCode int
	stdout   string
	stderr   string
}

func cursorString(value string) *string {
	return &value
}

func cursorJSON(value any) *string {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		panic(err)
	}
	return cursorString(string(encoded) + "\n")
}

func prepareCursorFixture(t *testing.T, existingRaw *string, createGuard bool) *cursorFixture {
	t.Helper()
	rootDir := t.TempDir()
	homeDir := filepath.Join(rootDir, "home with spaces and 'quote")
	agentDir := filepath.Join(homeDir, ".elydora", cursorTestAgentID)
	configPath := filepath.Join(homeDir, ".cursor", "hooks.json")
	guardPath := filepath.Join(agentDir, "guard.js")
	hookPath := filepath.Join(agentDir, "hook.js")
	if err := os.MkdirAll(agentDir, 0700); err != nil {
		t.Fatalf("create Cursor fixture: %v", err)
	}
	if createGuard {
		if err := os.WriteFile(
			guardPath,
			[]byte(GenerateGuardScript("cursor", cursorTestAgentID)),
			0700,
		); err != nil {
			t.Fatalf("write guard runtime: %v", err)
		}
	}
	writeOptionalCursorFile(t, configPath, existingRaw)
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	return &cursorFixture{
		plugin:        &CursorPlugin{},
		homeDir:       homeDir,
		agentDir:      agentDir,
		configPath:    configPath,
		guardPath:     guardPath,
		hookPath:      hookPath,
		runtimeConfig: filepath.Join(agentDir, "config.json"),
		privateKey:    filepath.Join(agentDir, "private.key"),
		config: InstallConfig{
			AgentName: "cursor", OrgID: "org-1", AgentID: cursorTestAgentID,
			PrivateKey: "test-key", KID: "kid-1", Token: "token-1",
			BaseURL: "https://api.elydora.test", GuardScriptPath: guardPath,
		},
	}
}

func writeOptionalCursorFile(t *testing.T, path string, source *string) {
	t.Helper()
	if source == nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("create directory for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(*source), 0600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func writeCursorObject(t *testing.T, path string, value map[string]any) {
	t.Helper()
	writeOptionalCursorFile(t, path, cursorJSON(value))
}

func readCursorObject(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return value
}

func cursorObject(t *testing.T, value any) map[string]any {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("value is not an object: %#v", value)
	}
	return object
}

func cursorArray(t *testing.T, value any) []any {
	t.Helper()
	array, ok := value.([]any)
	if !ok {
		t.Fatalf("value is not an array: %#v", value)
	}
	return array
}

func managedCursorHandler(t *testing.T, settings map[string]any, event, script string) map[string]any {
	t.Helper()
	hooks := cursorObject(t, settings["hooks"])
	for _, value := range cursorArray(t, hooks[event]) {
		handler := cursorObject(t, value)
		if strings.Contains(cursorStringValue(handler["command"]), script) {
			return handler
		}
	}
	t.Fatalf("managed Cursor handler for %s was not found", event)
	return nil
}

func cursorStringValue(value any) string {
	result, _ := value.(string)
	return result
}

func assertNativeCursorHandler(t *testing.T, handler map[string]any) {
	t.Helper()
	if len(handler) != 3 || handler["timeout"] != float64(10) || handler["failClosed"] != true {
		t.Fatalf("Cursor handler = %#v", handler)
	}
	command := cursorStringValue(handler["command"])
	if !strings.Contains(strings.ToLower(command), "node") {
		t.Fatalf("Cursor command = %q", command)
	}
	if runtime.GOOS == "windows" &&
		(!strings.HasPrefix(command, "& '") || !strings.HasSuffix(command, "; exit $LASTEXITCODE")) {
		t.Fatalf("Cursor PowerShell command = %q", command)
	}
	if runtime.GOOS != "windows" && !strings.HasPrefix(command, "'") {
		t.Fatalf("Cursor POSIX command = %q", command)
	}
}

func runCursorHandler(t *testing.T, handler map[string]any, payload string, environment ...string) cursorCommandResult {
	t.Helper()
	var process *exec.Cmd
	if runtime.GOOS == "windows" {
		process = exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", cursorStringValue(handler["command"]))
	} else {
		process = exec.Command("sh", "-c", cursorStringValue(handler["command"]))
	}
	process.Env = append(os.Environ(), environment...)
	process.Stdin = strings.NewReader(payload)
	var stdout, stderr bytes.Buffer
	process.Stdout, process.Stderr = &stdout, &stderr
	err := process.Run()
	if err == nil {
		return cursorCommandResult{stdout: stdout.String(), stderr: stderr.String()}
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return cursorCommandResult{exitCode: exitError.ExitCode(), stdout: stdout.String(), stderr: stderr.String()}
	}
	t.Fatalf("run Cursor hook: %v", err)
	return cursorCommandResult{exitCode: -1}
}

func installCursorFixture(t *testing.T, fixture *cursorFixture) {
	t.Helper()
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Cursor hooks: %v", err)
	}
}
