package plugins

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

const (
	codexTestAgentID = "agent-1"
	codexGuardStatus = "Checking Elydora agent state"
	codexAuditStatus = "Recording Elydora tool use"
	codexPrivateKey  = "BwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwc"
)

type codexFixture struct {
	plugin        *CodexPlugin
	config        InstallConfig
	homeDir       string
	agentDir      string
	configPath    string
	guardPath     string
	hookPath      string
	runtimeConfig string
	privateKey    string
}

func prepareCodexFixture(t *testing.T, existingRaw string) *codexFixture {
	t.Helper()
	homeDir := filepath.Join(t.TempDir(), "home with 'quote %ELYDORA_HOOK_PATH%")
	agentDir := filepath.Join(homeDir, ".elydora", codexTestAgentID)
	configPath := filepath.Join(homeDir, ".codex", "hooks.json")
	guardPath := filepath.Join(agentDir, "guard.js")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("create agent directory: %v", err)
	}
	if existingRaw != "" {
		if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
			t.Fatalf("create Codex config directory: %v", err)
		}
		if err := os.WriteFile(configPath, []byte(existingRaw), 0600); err != nil {
			t.Fatalf("write Codex config: %v", err)
		}
	}
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	t.Setenv("CODEX_HOME", "")

	return &codexFixture{
		plugin:   &CodexPlugin{},
		homeDir:  homeDir,
		agentDir: agentDir,
		config: InstallConfig{
			AgentName:       "codex",
			OrgID:           "org-1",
			AgentID:         codexTestAgentID,
			PrivateKey:      codexPrivateKey,
			KID:             "kid-1",
			BaseURL:         "https://api.elydora.test",
			GuardScriptPath: guardPath,
		},
		configPath:    configPath,
		guardPath:     guardPath,
		hookPath:      filepath.Join(agentDir, "hook.js"),
		runtimeConfig: filepath.Join(agentDir, "config.json"),
		privateKey:    filepath.Join(agentDir, "private.key"),
	}
}

func readCodexTestObject(t *testing.T, path string) map[string]any {
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

func requireCodexHandler(t *testing.T, settings map[string]any, event, status string) map[string]any {
	t.Helper()
	hooks := requireObject(t, settings["hooks"])
	for _, groupValue := range requireArray(t, hooks[event]) {
		group := requireObject(t, groupValue)
		for _, handlerValue := range requireArray(t, group["hooks"]) {
			handler := requireObject(t, handlerValue)
			if handler["statusMessage"] == status {
				return handler
			}
		}
	}
	t.Fatalf("handler %q not found", status)
	return nil
}

func runCodexCommand(t *testing.T, command, homeDir string, payload map[string]any) (int, string) {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal Codex payload: %v", err)
	}
	return runCodexRawCommand(t, command, homeDir, encoded)
}

func runCodexRawCommand(t *testing.T, command, homeDir string, encoded []byte) (int, string) {
	t.Helper()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		commandFile := filepath.Join(t.TempDir(), "run-codex-hook.cmd")
		if err := os.WriteFile(commandFile, []byte("@echo off\r\n"+command+"\r\n"), 0600); err != nil {
			t.Fatalf("write hook command file: %v", err)
		}
		cmd = exec.Command("cmd.exe", "/d", "/c", commandFile)
	} else {
		cmd = exec.Command("sh", "-c", command)
	}
	cmd.Env = append(os.Environ(), "HOME="+homeDir, "USERPROFILE="+homeDir)
	cmd.Stdin = bytes.NewReader(encoded)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return 0, stderr.String()
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitError.ExitCode(), stderr.String()
	}
	t.Fatalf("run Codex hook command: %v", err)
	return -1, stderr.String()
}

func TestCodexRegistryPointsAtGlobalHooksContract(t *testing.T) {
	entry := SupportedAgents["codex"]
	if entry.Name != "OpenAI Codex" || entry.ConfigDir != "~/.codex" || entry.ConfigFile != "hooks.json" {
		t.Fatalf("Codex registry entry = %#v", entry)
	}
}

func TestCodexInstallPreservesHooksAndIsIdempotent(t *testing.T) {
	fixture := prepareCodexFixture(t, `{
  "description": "Workspace hooks",
	  "hooks": {
	    "SessionStart": [{"hooks": [{"type": "command", "command": "existing-command"}]}],
	    "PreToolUse": [{"matcher": "Read", "hooks": []}]
	  }
}`)
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Codex hooks: %v", err)
	}
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("repeat Codex install: %v", err)
	}

	settings := readCodexTestObject(t, fixture.configPath)
	if settings["description"] != "Workspace hooks" {
		t.Fatalf("description changed: %#v", settings["description"])
	}
	hooks := requireObject(t, settings["hooks"])
	if len(requireArray(t, hooks["PreToolUse"])) != 2 || len(requireArray(t, hooks["PostToolUse"])) != 1 {
		t.Fatalf("managed hooks are not idempotent: %#v", hooks)
	}
	if len(requireArray(t, hooks["SessionStart"])) != 1 {
		t.Fatalf("unrelated hooks changed: %#v", hooks)
	}
	guard := requireCodexHandler(t, settings, "PreToolUse", codexGuardStatus)
	if guard["type"] != "command" || guard["timeout"] != float64(10) {
		t.Fatalf("unexpected guard handler: %#v", guard)
	}
	if _, ok := guard["command"].(string); !ok {
		t.Fatalf("POSIX command = %#v, want string", guard["command"])
	}
	if _, ok := guard["commandWindows"].(string); !ok {
		t.Fatalf("Windows command = %#v, want string", guard["commandWindows"])
	}
}

func TestCodexCommandsBlockAndForwardOfficialPayload(t *testing.T) {
	fixture := prepareCodexFixture(t, "")
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Codex hooks: %v", err)
	}
	guardScript := "process.stderr.write('Agent is frozen by Elydora.'); process.exit(2);\n"
	if err := os.WriteFile(fixture.guardPath, []byte(guardScript), 0700); err != nil {
		t.Fatalf("write blocking guard: %v", err)
	}
	capturePath := filepath.Join(t.TempDir(), "captured-event.json")
	captureJSON, err := json.Marshal(capturePath)
	if err != nil {
		t.Fatalf("marshal capture path: %v", err)
	}
	captureScript := "const fs = require('node:fs'); const chunks = []; " +
		"process.stdin.on('data', chunk => chunks.push(chunk)); " +
		"process.stdin.on('end', () => fs.writeFileSync(" + string(captureJSON) + ", Buffer.concat(chunks)));\n"
	if err := os.WriteFile(fixture.hookPath, []byte(captureScript), 0600); err != nil {
		t.Fatalf("write capture hook: %v", err)
	}

	settings := readCodexTestObject(t, fixture.configPath)
	commandKey := "command"
	if runtime.GOOS == "windows" {
		commandKey = "commandWindows"
	}
	payload := map[string]any{
		"hook_event_name": "PreToolUse",
		"session_id":      "session-1",
		"turn_id":         "turn-1",
		"transcript_path": nil,
		"cwd":             fixture.homeDir,
		"model":           "gpt-5",
		"permission_mode": "default",
		"tool_name":       "Bash",
		"tool_use_id":     "call-1",
		"tool_input":      map[string]any{"command": "echo test"},
	}
	guard := requireCodexHandler(t, settings, "PreToolUse", codexGuardStatus)
	if strings.Contains(guard["commandWindows"].(string), "%ELYDORA_HOOK_PATH%") {
		t.Fatalf("Windows command exposes an expandable path: %q", guard["commandWindows"])
	}
	exitCode, stderr := runCodexCommand(t, guard[commandKey].(string), fixture.homeDir, payload)
	if exitCode != 2 || !strings.Contains(stderr, "Agent is frozen by Elydora") {
		t.Fatalf("guard exit = %d, stderr = %q", exitCode, stderr)
	}

	payload["hook_event_name"] = "PostToolUse"
	payload["tool_response"] = map[string]any{"output": "test"}
	audit := requireCodexHandler(t, settings, "PostToolUse", codexAuditStatus)
	exitCode, stderr = runCodexCommand(t, audit[commandKey].(string), fixture.homeDir, payload)
	if exitCode != 0 {
		t.Fatalf("audit exit = %d, stderr = %q", exitCode, stderr)
	}
	captured := readCodexTestObject(t, capturePath)
	if !reflect.DeepEqual(captured, payload) {
		t.Fatalf("captured event = %#v, want %#v", captured, payload)
	}
}

func TestCodexStatusRequiresBothRuntimes(t *testing.T) {
	fixture := prepareCodexFixture(t, "")
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Codex hooks: %v", err)
	}
	status, err := fixture.plugin.Status()
	if err != nil || !status.Installed || !status.HookConfigured || !status.HookScriptExists {
		t.Fatalf("installed status = %#v, %v", status, err)
	}
	if err := os.Remove(fixture.guardPath); err != nil {
		t.Fatalf("remove guard: %v", err)
	}
	status, err = fixture.plugin.Status()
	if err != nil || status.Installed || !status.HookConfigured || status.HookScriptExists {
		t.Fatalf("degraded status = %#v, %v", status, err)
	}
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("repair Codex hooks: %v", err)
	}
	if err := os.Remove(fixture.privateKey); err != nil {
		t.Fatalf("remove private key: %v", err)
	}
	status, err = fixture.plugin.Status()
	if err != nil || status.Installed || !status.HookConfigured || status.HookScriptExists {
		t.Fatalf("missing key status = %#v, %v", status, err)
	}
}

func TestCodexInstallCommitsAllManagedRuntimeFiles(t *testing.T) {
	fixture := prepareCodexFixture(t, "")
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Codex hooks: %v", err)
	}
	for _, path := range []string{
		fixture.configPath,
		fixture.guardPath,
		fixture.hookPath,
		fixture.runtimeConfig,
		fixture.privateKey,
	} {
		if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
			t.Fatalf("managed file %s = %v, %v", path, info, err)
		}
	}
}

func TestCodexStatusSurfacesMalformedReferencedRuntimeMetadata(t *testing.T) {
	fixture := prepareCodexFixture(t, "")
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Codex hooks: %v", err)
	}
	if err := os.WriteFile(fixture.runtimeConfig, []byte("{ malformed"), 0600); err != nil {
		t.Fatalf("corrupt runtime config: %v", err)
	}
	if _, err := fixture.plugin.Status(); err == nil || !strings.Contains(err.Error(), "parse Elydora runtime config") {
		t.Fatalf("status error = %v", err)
	}
}

func TestCodexInstallPreservesMatchingUserStatusText(t *testing.T) {
	fixture := prepareCodexFixture(t, `{"hooks":{
	  "PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"node ~/.elydora/user/guard.js.backup","statusMessage":"Checking Elydora agent state"}]}],
	  "PostToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"node ~/.elydora/user/hook.js.backup","statusMessage":"Recording Elydora tool use"}]}]
	}}`)
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Codex hooks: %v", err)
	}
	hooks := requireObject(t, readCodexTestObject(t, fixture.configPath)["hooks"])
	if len(requireArray(t, hooks["PreToolUse"])) != 2 || len(requireArray(t, hooks["PostToolUse"])) != 2 {
		t.Fatalf("user handlers were removed: %#v", hooks)
	}
}

func TestCodexUninstallPreservesUnrelatedHooksAndExactAgent(t *testing.T) {
	fixture := prepareCodexFixture(t, `{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"existing-command"}]}]}}`)
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Codex hooks: %v", err)
	}
	settings := readCodexTestObject(t, fixture.configPath)
	otherDir := filepath.Join(filepath.Dir(fixture.agentDir), "agent-10")
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		t.Fatalf("resolve Node.js: %v", err)
	}
	for _, contract := range []struct{ event, status, script string }{
		{"PreToolUse", codexGuardStatus, codexGuardScript},
		{"PostToolUse", codexAuditStatus, codexAuditScript},
	} {
		other := codexHandler(nodePath, filepath.Join(otherDir, contract.script), contract.status)
		hooks := requireObject(t, settings["hooks"])
		hooks[contract.event] = append(requireArray(t, hooks[contract.event]), map[string]any{
			"matcher": "*", "hooks": []any{other},
		})
	}
	encoded, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("marshal augmented config: %v", err)
	}
	if err := os.WriteFile(fixture.configPath, encoded, 0600); err != nil {
		t.Fatalf("write augmented config: %v", err)
	}
	if err := fixture.plugin.Uninstall(codexTestAgentID); err != nil {
		t.Fatalf("uninstall Codex hooks: %v", err)
	}
	remaining := readCodexTestObject(t, fixture.configPath)
	hooks := requireObject(t, remaining["hooks"])
	if len(requireArray(t, hooks["PreToolUse"])) != 2 || len(requireArray(t, hooks["PostToolUse"])) != 1 {
		t.Fatalf("unexpected remaining hooks: %#v", hooks)
	}
	_, remainingScript, parsed := parseCodexPOSIXCommand(
		requireCodexHandler(t, remaining, "PostToolUse", codexAuditStatus)["command"],
	)
	if !parsed || !sameCodexPath(remainingScript, filepath.Join(otherDir, codexAuditScript)) {
		t.Fatalf("other agent handler was removed: %#v", hooks)
	}
}

func TestCodexUninstallRemovesOwnedConfig(t *testing.T) {
	fixture := prepareCodexFixture(t, "")
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Codex hooks: %v", err)
	}
	if err := fixture.plugin.Uninstall(codexTestAgentID); err != nil {
		t.Fatalf("uninstall Codex hooks: %v", err)
	}
	if _, err := os.Stat(fixture.configPath); !os.IsNotExist(err) {
		t.Fatalf("owned config still exists: %v", err)
	}
}

func TestCodexMalformedConfigAndShapesPreventRuntimeWrites(t *testing.T) {
	for _, testCase := range []struct{ name, raw, want string }{
		{"malformed", "{ malformed", "parse Codex user hooks"},
		{"comments", `{"hooks":{} // comment\n}`, "invalid character"},
		{"trailing comma", `{"hooks":{},}`, "invalid character"},
		{"duplicate root", `{"hooks":{},"hooks":{}}`, "duplicate"},
		{"nested duplicate", `{"hooks":{"PreToolUse":[{"hooks":[],"hooks":[]}]}}`, "duplicate"},
		{"null-hooks", `{"hooks":null}`, `field "hooks" must be an object`},
		{"null-event", `{"hooks":{"PreToolUse":null}}`, `field "hooks.PreToolUse" must be an array`},
		{"null-handlers", `{"hooks":{"PreToolUse":[{"hooks":null}]}}`, "must contain a hooks array"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := prepareCodexFixture(t, testCase.raw)
			err := fixture.plugin.Install(fixture.config)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("install error = %v, want %q", err, testCase.want)
			}
			raw, readErr := os.ReadFile(fixture.configPath)
			if readErr != nil || string(raw) != testCase.raw {
				t.Fatalf("original config changed: %q, %v", raw, readErr)
			}
			for _, path := range []string{fixture.hookPath, fixture.runtimeConfig, fixture.privateKey} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("runtime write occurred at %s: %v", path, err)
				}
			}
		})
	}
}
