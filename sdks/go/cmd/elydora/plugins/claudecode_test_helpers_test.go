package plugins

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

const (
	claudeTestAgentID = "agent-1"
	claudePrivateKey  = "BwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwc"
)

type claudeFixtureOptions struct {
	existingRaw       *string
	configEnvPresent  bool
	configEnvOverride string
}

type claudeFixture struct {
	plugin        *ClaudeCodePlugin
	config        InstallConfig
	homeDir       string
	projectDir    string
	configDir     string
	agentDir      string
	configPath    string
	guardPath     string
	hookPath      string
	runtimeConfig string
	privateKey    string
}

func claudeString(value string) *string {
	return &value
}

func unsetClaudeConfigDir(t *testing.T) {
	t.Helper()
	value, existed := os.LookupEnv("CLAUDE_CONFIG_DIR")
	if err := os.Unsetenv("CLAUDE_CONFIG_DIR"); err != nil {
		t.Fatalf("unset CLAUDE_CONFIG_DIR: %v", err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv("CLAUDE_CONFIG_DIR", value)
		} else {
			_ = os.Unsetenv("CLAUDE_CONFIG_DIR")
		}
	})
}

func prepareClaudeFixture(t *testing.T, options claudeFixtureOptions) *claudeFixture {
	t.Helper()
	root := t.TempDir()
	homeDir := filepath.Join(root, "home with 'quote %CLAUDE_HOOK_EVENT%")
	projectDir := filepath.Join(root, "project with spaces")
	if err := os.MkdirAll(projectDir, 0700); err != nil {
		t.Fatalf("create project directory: %v", err)
	}
	previousDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("read current directory: %v", err)
	}
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("enter project directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousDirectory) })
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	var configDir string
	if options.configEnvPresent {
		t.Setenv("CLAUDE_CONFIG_DIR", options.configEnvOverride)
		configDir, err = filepath.Abs(options.configEnvOverride)
		if err != nil {
			t.Fatalf("resolve fixture config directory: %v", err)
		}
	} else {
		unsetClaudeConfigDir(t)
		configDir = filepath.Join(homeDir, ".claude")
	}
	agentDir := filepath.Join(homeDir, ".elydora", claudeTestAgentID)
	configPath := filepath.Join(configDir, claudeConfigFile)
	if options.existingRaw != nil {
		if err := os.MkdirAll(configDir, 0700); err != nil {
			t.Fatalf("create Claude config directory: %v", err)
		}
		if err := os.WriteFile(configPath, []byte(*options.existingRaw), 0600); err != nil {
			t.Fatalf("write Claude settings: %v", err)
		}
	}
	guardPath := filepath.Join(agentDir, claudeGuardScript)
	return &claudeFixture{
		plugin: &ClaudeCodePlugin{},
		config: InstallConfig{
			AgentName: claudeAgentKey, OrgID: "org-1", AgentID: claudeTestAgentID,
			PrivateKey: claudePrivateKey, KID: "kid-1", Token: "token-1",
			BaseURL: "https://api.elydora.test", GuardScriptPath: guardPath,
		},
		homeDir: homeDir, projectDir: projectDir, configDir: configDir,
		agentDir: agentDir, configPath: configPath, guardPath: guardPath,
		hookPath:      filepath.Join(agentDir, claudeAuditScript),
		runtimeConfig: filepath.Join(agentDir, "config.json"),
		privateKey:    filepath.Join(agentDir, "private.key"),
	}
}

func readClaudeTestObject(t *testing.T, path string) map[string]any {
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

func writeClaudeTestObject(t *testing.T, path string, value map[string]any) {
	t.Helper()
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("marshal Claude settings: %v", err)
	}
	encoded = append(encoded, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("create settings directory: %v", err)
	}
	if err := os.WriteFile(path, encoded, 0600); err != nil {
		t.Fatalf("write Claude settings: %v", err)
	}
}

func claudeTestManagedHandler(
	t *testing.T,
	settings map[string]any,
	event string,
	scriptName string,
	statusMessage string,
) map[string]any {
	t.Helper()
	hooks := requireObject(t, settings["hooks"])
	for _, groupValue := range requireArray(t, hooks[event]) {
		group := requireObject(t, groupValue)
		if len(group) != 1 {
			continue
		}
		for _, handlerValue := range requireArray(t, group["hooks"]) {
			handler := requireObject(t, handlerValue)
			reference, err := managedClaudeReference(
				handler,
				scriptName,
				statusMessage,
				false,
			)
			if err != nil {
				t.Fatalf("inspect managed %s handler: %v", event, err)
			}
			if reference != nil {
				return handler
			}
		}
	}
	t.Fatalf("managed %s handler for %s not found", event, scriptName)
	return nil
}

func requireStrictClaudeTriple(t *testing.T, fixture *claudeFixture) {
	t.Helper()
	settings := readClaudeTestObject(t, fixture.configPath)
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		t.Fatalf("resolve Node.js runtime: %v", err)
	}
	for _, item := range []struct{ event, script, path, status string }{
		{"PreToolUse", claudeGuardScript, fixture.guardPath, claudeGuardStatusMessage},
		{"PostToolUse", claudeAuditScript, fixture.hookPath, claudeAuditStatusMessage},
		{"PostToolUseFailure", claudeAuditScript, fixture.hookPath, claudeAuditStatusMessage},
	} {
		handler := claudeTestManagedHandler(
			t,
			settings,
			item.event,
			item.script,
			item.status,
		)
		want := map[string]any{
			"type": "command", "command": nodePath, "args": []any{item.path},
			"timeout": float64(10), "statusMessage": item.status,
		}
		if !reflect.DeepEqual(handler, want) {
			t.Fatalf("%s handler = %#v, want %#v", item.event, handler, want)
		}
	}
}

func claudeOfficialPayload(event string) map[string]any {
	payload := map[string]any{
		"session_id": "session-1", "transcript_path": "C:/transcript.jsonl",
		"cwd": "C:/project", "permission_mode": "default",
		"hook_event_name": event, "tool_name": "Bash",
		"tool_input":  map[string]any{"command": "echo test"},
		"tool_use_id": "call-1",
	}
	if event == "PostToolUse" {
		payload["tool_response"] = map[string]any{"output": "test", "success": true}
	}
	if event == "PostToolUseFailure" {
		payload["error"] = "command failed"
		payload["is_interrupt"] = false
	}
	return payload
}

func runClaudeHandler(
	t *testing.T,
	handler map[string]any,
	fixture *claudeFixture,
	payload map[string]any,
) (int, string, string) {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal Claude payload: %v", err)
	}
	return runClaudeRawHandler(t, handler, fixture, encoded)
}

func runClaudeRawHandler(
	t *testing.T,
	handler map[string]any,
	fixture *claudeFixture,
	input []byte,
) (int, string, string) {
	t.Helper()
	command, ok := handler["command"].(string)
	if !ok {
		t.Fatalf("Claude command = %#v", handler["command"])
	}
	values := requireArray(t, handler["args"])
	args := make([]string, 0, len(values))
	for _, value := range values {
		argument, ok := value.(string)
		if !ok {
			t.Fatalf("Claude argument = %#v", value)
		}
		args = append(args, argument)
	}
	process := exec.Command(command, args...)
	process.Dir = fixture.projectDir
	process.Env = append(
		os.Environ(),
		"HOME="+fixture.homeDir,
		"USERPROFILE="+fixture.homeDir,
	)
	process.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	process.Stdout = &stdout
	process.Stderr = &stderr
	err := process.Run()
	if err == nil {
		return 0, stdout.String(), stderr.String()
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitError.ExitCode(), stdout.String(), stderr.String()
	}
	t.Fatalf("run Claude handler: %v", err)
	return -1, stdout.String(), stderr.String()
}

func assertNoClaudeRuntimeWrites(t *testing.T, fixture *claudeFixture) {
	t.Helper()
	for _, path := range []string{
		fixture.runtimeConfig, fixture.privateKey, fixture.guardPath, fixture.hookPath,
	} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("runtime file exists at %s: %v", path, err)
		}
	}
}

func assertNoClaudeTransactionArtifacts(t *testing.T, root string) {
	t.Helper()
	if _, err := os.Lstat(root); errors.Is(err, os.ErrNotExist) {
		return
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := entry.Name()
		if filepath.Ext(name) == ".tmp" || filepath.Ext(name) == ".rollback" {
			t.Errorf("transaction artifact remains at %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk Claude fixture: %v", err)
	}
}

func claudeSymlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
}
