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

	"github.com/pelletier/go-toml/v2"
)

const (
	kimiTestAgentID = "agent-1"
	kimiPrivateKey  = "BwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwc"
)

type kimiFixtureOptions struct {
	modernConfig          *string
	legacyConfig          *string
	useDefaultHome        bool
	withoutModernEvidence bool
	withoutLegacyEvidence bool
}

type kimiFixture struct {
	plugin        *KimiPlugin
	config        InstallConfig
	homeDir       string
	projectDir    string
	kimiHome      string
	modernPath    string
	legacyHome    string
	legacyPath    string
	agentDir      string
	guardPath     string
	hookPath      string
	runtimeConfig string
	privateKey    string
}

func kimiString(value string) *string {
	return &value
}

func prepareKimiFixture(t *testing.T, options kimiFixtureOptions) *kimiFixture {
	t.Helper()
	root := t.TempDir()
	homeDir := filepath.Join(root, "home with spaces and 'quote %ELYDORA_HOOK_PATH%")
	projectDir := filepath.Join(root, "project with spaces")
	kimiHome := filepath.Join(homeDir, "custom kimi-code")
	if options.useDefaultHome {
		kimiHome = filepath.Join(homeDir, ".kimi-code")
	}
	modernPath := filepath.Join(kimiHome, "config.toml")
	legacyHome := filepath.Join(homeDir, ".kimi")
	legacyPath := filepath.Join(legacyHome, "config.toml")
	agentDir := filepath.Join(homeDir, ".elydora", kimiTestAgentID)
	guardPath := filepath.Join(agentDir, kimiGuardScript)
	hookPath := filepath.Join(agentDir, kimiAuditScript)
	if err := os.MkdirAll(projectDir, 0700); err != nil {
		t.Fatalf("create project directory: %v", err)
	}
	if options.useDefaultHome && !options.withoutModernEvidence {
		if err := os.MkdirAll(kimiHome, 0700); err != nil {
			t.Fatalf("create Kimi Code home: %v", err)
		}
	}
	if !options.withoutLegacyEvidence {
		if err := os.MkdirAll(legacyHome, 0700); err != nil {
			t.Fatalf("create legacy Kimi home: %v", err)
		}
	}
	writeOptionalKimiConfig(t, modernPath, options.modernConfig)
	writeOptionalKimiConfig(t, legacyPath, options.legacyConfig)

	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	t.Setenv("ELYDORA_HOOK_PATH", filepath.Join(root, "expanded-command-fragment"))
	if options.useDefaultHome {
		t.Setenv("KIMI_CODE_HOME", "")
	} else {
		t.Setenv("KIMI_CODE_HOME", kimiHome)
	}

	return &kimiFixture{
		plugin: &KimiPlugin{}, homeDir: homeDir, projectDir: projectDir,
		kimiHome: kimiHome, modernPath: modernPath,
		legacyHome: legacyHome, legacyPath: legacyPath,
		agentDir: agentDir, guardPath: guardPath, hookPath: hookPath,
		runtimeConfig: filepath.Join(agentDir, "config.json"),
		privateKey:    filepath.Join(agentDir, "private.key"),
		config: InstallConfig{
			AgentName: kimiAgentKey, OrgID: "org-1", AgentID: kimiTestAgentID,
			PrivateKey: kimiPrivateKey, KID: "kid-1", Token: "token-1",
			BaseURL: "https://api.elydora.test", GuardScriptPath: guardPath,
		},
	}
}

func writeOptionalKimiConfig(t *testing.T, path string, raw *string) {
	t.Helper()
	if raw == nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("create Kimi config directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(*raw), 0600); err != nil {
		t.Fatalf("write Kimi config: %v", err)
	}
}

func readKimiTestHooks(t *testing.T, path string) []map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	root := map[string]any{}
	if err := toml.Unmarshal(raw, &root); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	values, ok := root["hooks"].([]any)
	if !ok {
		t.Fatalf("hooks = %#v, want array", root["hooks"])
	}
	hooks := make([]map[string]any, 0, len(values))
	for _, value := range values {
		hook, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("hook = %#v, want table", value)
		}
		hooks = append(hooks, hook)
	}
	return hooks
}

func managedKimiTestHook(t *testing.T, hooks []map[string]any, event string) map[string]any {
	t.Helper()
	for index := len(hooks) - 1; index >= 0; index-- {
		hook := hooks[index]
		if hook["event"] == event && hook["timeout"] == int64(10) {
			return hook
		}
	}
	t.Fatalf("managed %s hook not found", event)
	return nil
}

func requireStrictKimiHook(t *testing.T, hook map[string]any, event string) {
	t.Helper()
	want := map[string]any{
		"event": event, "command": hook["command"], "timeout": int64(10),
	}
	if !reflect.DeepEqual(hook, want) {
		t.Fatalf("hook = %#v, want strict contract %#v", hook, want)
	}
	command, ok := hook["command"].(string)
	if !ok || command == "" {
		t.Fatalf("Kimi command = %#v", hook["command"])
	}
	if runtime.GOOS == "windows" && !strings.Contains(command, " -EncodedCommand ") {
		t.Fatalf("Windows Kimi command is not encoded PowerShell: %q", command)
	}
}

func requireKimiManagedTriple(t *testing.T, hooks []map[string]any) {
	t.Helper()
	for _, event := range []string{"PreToolUse", "PostToolUse", "PostToolUseFailure"} {
		requireStrictKimiHook(t, managedKimiTestHook(t, hooks, event), event)
	}
	if managedKimiTestHook(t, hooks, "PostToolUse")["command"] !=
		managedKimiTestHook(t, hooks, "PostToolUseFailure")["command"] {
		t.Fatal("Kimi success and failure audit commands differ")
	}
}

func kimiOfficialPayload(event string) map[string]any {
	payload := map[string]any{
		"hook_event_name": event,
		"session_id":      "session-1",
		"cwd":             "C:/project",
		"tool_name":       "Bash",
		"tool_input":      map[string]any{"command": "echo test"},
		"tool_call_id":    "call-1",
	}
	switch event {
	case "PostToolUse":
		payload["tool_output"] = map[string]any{"output": "test", "success": true}
	case "PostToolUseFailure":
		payload["error"] = map[string]any{
			"name": "ToolError", "message": "command failed", "code": "tool.failed",
		}
	}
	return payload
}

func runKimiCommand(
	t *testing.T,
	command string,
	fixture *kimiFixture,
	payload map[string]any,
) (int, string) {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal Kimi payload: %v", err)
	}
	return runKimiRawCommand(t, command, fixture, encoded)
}

func runKimiRawCommand(
	t *testing.T,
	command string,
	fixture *kimiFixture,
	encoded []byte,
) (int, string) {
	t.Helper()
	var process *exec.Cmd
	if runtime.GOOS == "windows" {
		commandFile := filepath.Join(t.TempDir(), "run-kimi-hook.cmd")
		if err := os.WriteFile(commandFile, []byte("@echo off\r\n"+command+"\r\n"), 0600); err != nil {
			t.Fatalf("write Kimi command file: %v", err)
		}
		process = exec.Command("cmd.exe", "/d", "/c", commandFile)
	} else {
		process = exec.Command("sh", "-c", command)
	}
	process.Dir = fixture.projectDir
	process.Env = append(
		os.Environ(),
		"HOME="+fixture.homeDir,
		"USERPROFILE="+fixture.homeDir,
		"ELYDORA_HOOK_PATH=injected-command-fragment",
	)
	process.Stdin = bytes.NewReader(encoded)
	var stderr bytes.Buffer
	process.Stderr = &stderr
	err := process.Run()
	if err == nil {
		return 0, stderr.String()
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitError.ExitCode(), stderr.String()
	}
	t.Fatalf("run Kimi hook command: %v", err)
	return -1, stderr.String()
}

func legacyKimiCommand(t *testing.T, scriptPath string) string {
	t.Helper()
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		t.Fatalf("resolve Node.js runtime: %v", err)
	}
	if runtime.GOOS == "windows" {
		return quoteWindowsArgument(nodePath) + " " + quoteWindowsArgument(scriptPath)
	}
	return quotePOSIXArgument(nodePath) + " " + quotePOSIXArgument(scriptPath)
}

func assertNoKimiRuntimeWrites(t *testing.T, fixture *kimiFixture) {
	t.Helper()
	for _, path := range []string{
		fixture.runtimeConfig, fixture.privateKey, fixture.guardPath, fixture.hookPath,
	} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("runtime file exists at %s: %v", path, err)
		}
	}
}

func assertNoKimiTransactionArtifacts(t *testing.T, root string) {
	t.Helper()
	if _, err := os.Lstat(root); errors.Is(err, os.ErrNotExist) {
		return
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasSuffix(entry.Name(), ".tmp") ||
			strings.HasSuffix(entry.Name(), ".rollback") {
			t.Errorf("transaction artifact remains at %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk Kimi fixture: %v", err)
	}
}

func kimiSymlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
}
