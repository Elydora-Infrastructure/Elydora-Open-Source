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

const kiroTestAgentID = "agent-1"

type kiroFixture struct {
	plugin        *KiroCliPlugin
	config        InstallConfig
	homeDir       string
	guardPath     string
	hookPath      string
	runtimeConfig string
	v2Path        string
	v3Path        string
}

func prepareKiroFixture(t *testing.T, v2Raw, v3Raw string) *kiroFixture {
	t.Helper()
	homeDir := filepath.Join(t.TempDir(), "home with spaces")
	agentDir := filepath.Join(homeDir, ".elydora", kiroTestAgentID)
	guardPath := filepath.Join(agentDir, "guard.js")
	v2Path := filepath.Join(homeDir, ".kiro", "agents", "elydora-audit.json")
	v3Path := filepath.Join(homeDir, ".kiro", "hooks", "elydora-audit.json")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("create agent directory: %v", err)
	}
	if err := os.WriteFile(
		guardPath,
		[]byte("process.stderr.write('Agent is frozen by Elydora.'); process.exit(2);\n"),
		0600,
	); err != nil {
		t.Fatalf("write guard: %v", err)
	}
	writeOptionalKiroConfig(t, v2Path, v2Raw)
	writeOptionalKiroConfig(t, v3Path, v3Raw)
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)

	return &kiroFixture{
		plugin:  &KiroCliPlugin{},
		homeDir: homeDir,
		config: InstallConfig{
			AgentName:       "kirocli",
			OrgID:           "org-1",
			AgentID:         kiroTestAgentID,
			PrivateKey:      "test-key",
			KID:             "kid-1",
			BaseURL:         "https://api.elydora.test",
			GuardScriptPath: guardPath,
		},
		guardPath:     guardPath,
		hookPath:      filepath.Join(agentDir, "hook.js"),
		runtimeConfig: filepath.Join(agentDir, "config.json"),
		v2Path:        v2Path,
		v3Path:        v3Path,
	}
}

func writeOptionalKiroConfig(t *testing.T, path, raw string) {
	t.Helper()
	if raw == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("create config directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func readKiroTestObject(t *testing.T, path string) map[string]any {
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

func requireObject(t *testing.T, value any) map[string]any {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("value = %#v, want object", value)
	}
	return object
}

func requireArray(t *testing.T, value any) []any {
	t.Helper()
	array, ok := value.([]any)
	if !ok {
		t.Fatalf("value = %#v, want array", value)
	}
	return array
}

func findKiroV3Hook(t *testing.T, config map[string]any, name string) map[string]any {
	t.Helper()
	for _, value := range requireArray(t, config["hooks"]) {
		hook := requireObject(t, value)
		if hook["name"] == name {
			return hook
		}
	}
	t.Fatalf("hook %q not found", name)
	return nil
}

func kiroHookCommand(t *testing.T, hook map[string]any) string {
	t.Helper()
	action := requireObject(t, hook["action"])
	command, ok := action["command"].(string)
	if !ok {
		t.Fatalf("hook command = %#v, want string", action["command"])
	}
	return command
}

func marshalKiroPayload(t *testing.T, payload map[string]any) []byte {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal Kiro hook payload: %v", err)
	}
	return encoded
}

func runKiroCommand(t *testing.T, command, homeDir string, payload []byte) (int, string) {
	t.Helper()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		commandFile := filepath.Join(t.TempDir(), "run-kiro-hook.cmd")
		if err := os.WriteFile(commandFile, []byte("@echo off\r\n"+command+"\r\n"), 0600); err != nil {
			t.Fatalf("write hook command file: %v", err)
		}
		cmd = exec.Command("cmd.exe", "/d", "/c", commandFile)
	} else {
		cmd = exec.Command("sh", "-c", command)
	}
	cmd.Env = append(os.Environ(), "HOME="+homeDir, "USERPROFILE="+homeDir)
	cmd.Stdin = bytes.NewReader(payload)
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
	t.Fatalf("run hook command: %v", err)
	return -1, stderr.String()
}

func TestKiroCLIRegistryPointsAtV3GlobalHookContract(t *testing.T) {
	entry := SupportedAgents["kirocli"]
	if entry.Name != "Kiro CLI" || entry.ConfigDir != "~/.kiro/hooks" || entry.ConfigFile != "elydora-audit.json" {
		t.Fatalf("Kiro CLI registry entry = %#v", entry)
	}
}

func TestKiroCLIInstallPreservesHooksAndWritesIdempotentV2V3Contracts(t *testing.T) {
	fixture := prepareKiroFixture(t, `{
  "description": "User Kiro agent",
  "tools": ["read"],
  "hooks": {
    "agentSpawn": [{"command": "existing-spawn"}],
    "preToolUse": [{"matcher": "read", "command": "existing-v2"}]
  }
}`, `{
  "version": "v1",
  "hooks": [{
    "name": "existing-v3",
    "trigger": "SessionStart",
    "action": {"type": "command", "command": "existing-command"}
  }]
}`)
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Kiro CLI hooks: %v", err)
	}
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("repeat Kiro CLI install: %v", err)
	}

	v2 := readKiroTestObject(t, fixture.v2Path)
	if v2["description"] != "User Kiro agent" || !reflect.DeepEqual(v2["tools"], []any{"read"}) {
		t.Fatalf("v2 user fields changed: %#v", v2)
	}
	v2Hooks := requireObject(t, v2["hooks"])
	v2Pre := requireArray(t, v2Hooks["preToolUse"])
	if len(v2Pre) != 2 || len(requireArray(t, v2Hooks["postToolUse"])) != 1 {
		t.Fatalf("unexpected v2 hooks: %#v", v2Hooks)
	}
	v2Guard := requireObject(t, v2Pre[1])
	if v2Guard["matcher"] != "*" || v2Guard["timeout_ms"] != float64(5000) {
		t.Fatalf("unexpected v2 guard: %#v", v2Guard)
	}
	if _, ok := v2Guard["command"].(string); !ok {
		t.Fatalf("v2 guard command = %#v, want string", v2Guard["command"])
	}
	if !reflect.DeepEqual(v2Hooks["agentSpawn"], []any{map[string]any{"command": "existing-spawn"}}) {
		t.Fatalf("v2 unrelated hook changed: %#v", v2Hooks["agentSpawn"])
	}

	v3 := readKiroTestObject(t, fixture.v3Path)
	if v3["version"] != "v1" || len(requireArray(t, v3["hooks"])) != 3 {
		t.Fatalf("unexpected v3 config: %#v", v3)
	}
	guard := findKiroV3Hook(t, v3, "elydora-guard")
	if guard["trigger"] != "PreToolUse" || guard["matcher"] != ".*" || guard["timeout"] != float64(5) || guard["enabled"] != true {
		t.Fatalf("unexpected v3 guard: %#v", guard)
	}
	if requireObject(t, guard["action"])["type"] != "command" {
		t.Fatalf("unexpected v3 guard action: %#v", guard["action"])
	}
	if findKiroV3Hook(t, v3, "elydora-audit")["trigger"] != "PostToolUse" {
		t.Fatalf("unexpected v3 audit hook: %#v", v3)
	}
}

func TestKiroCLICommandsBlockFrozenAgentAndForwardOfficialPayload(t *testing.T) {
	fixture := prepareKiroFixture(t, "", "")
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Kiro CLI hooks: %v", err)
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

	v3 := readKiroTestObject(t, fixture.v3Path)
	payload := map[string]any{
		"hook_event_name": "PreToolUse",
		"cwd":             fixture.homeDir,
		"session_id":      "session-1",
		"tool_name":       "execute_bash",
		"tool_input":      map[string]any{"command": "echo test"},
	}
	encoded := marshalKiroPayload(t, payload)
	exitCode, stderr := runKiroCommand(t, kiroHookCommand(t, findKiroV3Hook(t, v3, "elydora-guard")), fixture.homeDir, encoded)
	if exitCode != 2 || !strings.Contains(stderr, "Agent is frozen by Elydora") {
		t.Fatalf("guard exit = %d, stderr = %q", exitCode, stderr)
	}

	payload["hook_event_name"] = "PostToolUse"
	payload["tool_response"] = map[string]any{"success": true, "result": "test"}
	encoded = marshalKiroPayload(t, payload)
	exitCode, stderr = runKiroCommand(t, kiroHookCommand(t, findKiroV3Hook(t, v3, "elydora-audit")), fixture.homeDir, encoded)
	if exitCode != 0 {
		t.Fatalf("audit exit = %d, stderr = %q", exitCode, stderr)
	}
	var captured map[string]any
	if raw, err := os.ReadFile(capturePath); err != nil {
		t.Fatalf("read captured event: %v", err)
	} else if err := json.Unmarshal(raw, &captured); err != nil {
		t.Fatalf("decode captured event: %v", err)
	}
	if !reflect.DeepEqual(captured, payload) {
		t.Fatalf("captured event = %#v, want %#v", captured, payload)
	}
}

func TestKiroCLIStatusAcceptsEitherContractAndRequiresBothRuntimes(t *testing.T) {
	fixture := prepareKiroFixture(t, "", "")
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Kiro CLI hooks: %v", err)
	}
	assertKiroStatus(t, fixture.plugin, true, fixture.v3Path)

	if err := os.Remove(fixture.v3Path); err != nil {
		t.Fatalf("remove v3 config: %v", err)
	}
	assertKiroStatus(t, fixture.plugin, true, fixture.v2Path)

	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("reinstall Kiro CLI hooks: %v", err)
	}
	if err := os.Remove(fixture.v2Path); err != nil {
		t.Fatalf("remove v2 config: %v", err)
	}
	assertKiroStatus(t, fixture.plugin, true, fixture.v3Path)

	if err := os.Remove(fixture.guardPath); err != nil {
		t.Fatalf("remove guard: %v", err)
	}
	status, err := fixture.plugin.Status()
	if err != nil {
		t.Fatalf("read degraded status: %v", err)
	}
	if status.Installed || !status.HookConfigured || status.HookScriptExists {
		t.Fatalf("unexpected degraded status: %#v", status)
	}
}

func assertKiroStatus(t *testing.T, plugin *KiroCliPlugin, installed bool, configPath string) {
	t.Helper()
	status, err := plugin.Status()
	if err != nil {
		t.Fatalf("read Kiro CLI status: %v", err)
	}
	if status.Installed != installed || status.ConfigPath != configPath {
		t.Fatalf("status = %#v, want installed=%v config=%q", status, installed, configPath)
	}
}

func TestKiroCLIStatusSurfacesMalformedReferencedRuntimeMetadata(t *testing.T) {
	fixture := prepareKiroFixture(t, "", "")
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Kiro CLI hooks: %v", err)
	}
	if err := os.WriteFile(fixture.runtimeConfig, []byte("{ malformed"), 0600); err != nil {
		t.Fatalf("corrupt runtime config: %v", err)
	}
	if _, err := fixture.plugin.Status(); err == nil || !strings.Contains(err.Error(), "parse Elydora runtime config") {
		t.Fatalf("status error = %v", err)
	}
}

func TestKiroCLIUninstallPreservesUnrelatedHooks(t *testing.T) {
	fixture := prepareKiroFixture(t, `{"hooks":{"preToolUse":[{"matcher":"read","command":"existing-v2"}]}}`, `{
  "version":"v1",
  "hooks":[{"name":"existing-v3","trigger":"SessionStart","action":{"type":"command","command":"existing-command"}}]
}`)
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Kiro CLI hooks: %v", err)
	}
	if err := fixture.plugin.Uninstall(kiroTestAgentID); err != nil {
		t.Fatalf("uninstall Kiro CLI hooks: %v", err)
	}

	v2Hooks := requireObject(t, readKiroTestObject(t, fixture.v2Path)["hooks"])
	if !reflect.DeepEqual(v2Hooks["preToolUse"], []any{map[string]any{"matcher": "read", "command": "existing-v2"}}) {
		t.Fatalf("unexpected v2 hooks after uninstall: %#v", v2Hooks)
	}
	if len(requireArray(t, v2Hooks["postToolUse"])) != 0 {
		t.Fatalf("managed v2 audit hook remains: %#v", v2Hooks)
	}
	v3Hooks := requireArray(t, readKiroTestObject(t, fixture.v3Path)["hooks"])
	if len(v3Hooks) != 1 || requireObject(t, v3Hooks[0])["name"] != "existing-v3" {
		t.Fatalf("unexpected v3 hooks after uninstall: %#v", v3Hooks)
	}
}

func TestKiroCLIUninstallRemovesElydoraOwnedConfigs(t *testing.T) {
	fixture := prepareKiroFixture(t, "", "")
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Kiro CLI hooks: %v", err)
	}
	if err := fixture.plugin.Uninstall(kiroTestAgentID); err != nil {
		t.Fatalf("uninstall Kiro CLI hooks: %v", err)
	}
	for _, path := range []string{fixture.v2Path, fixture.v3Path} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("owned config still exists at %s: %v", path, err)
		}
	}
}

func TestKiroCLIInstallPreservesMalformedConfigsBeforeRuntimeWrite(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		v2Raw   string
		v3Raw   string
		badPath func(*kiroFixture) string
	}{
		{name: "v2", v2Raw: "{ malformed", badPath: func(f *kiroFixture) string { return f.v2Path }},
		{name: "v3", v3Raw: "{ malformed", badPath: func(f *kiroFixture) string { return f.v3Path }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := prepareKiroFixture(t, testCase.v2Raw, testCase.v3Raw)
			if err := fixture.plugin.Install(fixture.config); err == nil || !strings.Contains(err.Error(), "parse Kiro CLI "+testCase.name) {
				t.Fatalf("install error = %v", err)
			}
			raw, err := os.ReadFile(testCase.badPath(fixture))
			if err != nil || string(raw) != "{ malformed" {
				t.Fatalf("malformed config changed: %q, %v", raw, err)
			}
			for _, path := range []string{fixture.hookPath, fixture.runtimeConfig} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("runtime write occurred at %s: %v", path, err)
				}
			}
		})
	}
}

func TestKiroCLIInstallRejectsInvalidContractShapesBeforeRuntimeWrite(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		v2Raw string
		v3Raw string
		want  string
	}{
		{name: "v2-null-hooks", v2Raw: `{"hooks":null}`, want: `field "hooks" must be an object`},
		{name: "v3-null-version", v3Raw: `{"version":null,"hooks":[]}`, want: `field "version" must be "v1"`},
		{name: "v3-null-hooks", v3Raw: `{"version":"v1","hooks":null}`, want: `field "hooks" must be an array`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := prepareKiroFixture(t, testCase.v2Raw, testCase.v3Raw)
			if err := fixture.plugin.Install(fixture.config); err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("install error = %v, want %q", err, testCase.want)
			}
			for _, path := range []string{fixture.hookPath, fixture.runtimeConfig} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("runtime write occurred at %s: %v", path, err)
				}
			}
		})
	}
}
