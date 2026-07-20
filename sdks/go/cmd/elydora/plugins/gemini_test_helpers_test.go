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
	geminiTestAgentID = "agent-1"
	geminiPrivateKey  = "BwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwc"
)

type geminiFixtureOptions struct {
	existingRaw    *string
	useDefaultHome bool
}

type geminiFixture struct {
	plugin        *GeminiPlugin
	config        InstallConfig
	homeDir       string
	projectDir    string
	geminiHome    string
	agentDir      string
	settingsPath  string
	guardPath     string
	hookPath      string
	runtimeConfig string
	privateKey    string
}

func geminiString(value string) *string {
	return &value
}

func prepareGeminiFixture(
	t *testing.T,
	options geminiFixtureOptions,
) *geminiFixture {
	t.Helper()
	root := t.TempDir()
	homeDir := filepath.Join(
		root,
		"home with 'quote $GEMINI_CWD %GEMINI_CWD%",
	)
	projectDir := filepath.Join(root, "project with spaces")
	geminiHome := filepath.Join(homeDir, "custom gemini home")
	if options.useDefaultHome {
		geminiHome = homeDir
	}
	settingsPath := filepath.Join(geminiHome, ".gemini", geminiConfigFile)
	agentDir := filepath.Join(homeDir, ".elydora", geminiTestAgentID)
	if err := os.MkdirAll(projectDir, 0700); err != nil {
		t.Fatalf("create project directory: %v", err)
	}
	if options.existingRaw != nil {
		if err := os.MkdirAll(filepath.Dir(settingsPath), 0700); err != nil {
			t.Fatalf("create Gemini settings directory: %v", err)
		}
		if err := os.WriteFile(
			settingsPath,
			[]byte(*options.existingRaw),
			0600,
		); err != nil {
			t.Fatalf("write Gemini settings: %v", err)
		}
	}
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	t.Setenv("ELYDORA_HOOK_PATH", filepath.Join(root, "expanded-fragment"))
	if options.useDefaultHome {
		t.Setenv("GEMINI_CLI_HOME", "")
	} else {
		t.Setenv("GEMINI_CLI_HOME", geminiHome)
	}
	guardPath := filepath.Join(agentDir, geminiGuardScript)
	return &geminiFixture{
		plugin: &GeminiPlugin{},
		config: InstallConfig{
			AgentName: geminiAgentKey, OrgID: "org-1",
			AgentID: geminiTestAgentID, PrivateKey: geminiPrivateKey,
			KID: "kid-1", Token: "token-1",
			BaseURL: "http://127.0.0.1:9", GuardScriptPath: guardPath,
		},
		homeDir: homeDir, projectDir: projectDir, geminiHome: geminiHome,
		agentDir: agentDir, settingsPath: settingsPath, guardPath: guardPath,
		hookPath:      filepath.Join(agentDir, geminiAuditScript),
		runtimeConfig: filepath.Join(agentDir, "config.json"),
		privateKey:    filepath.Join(agentDir, "private.key"),
	}
}

func readGeminiTestObject(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	value, err := decodeJSONCObject(raw, "Gemini test settings", false)
	if err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return value
}

func writeGeminiTestObject(t *testing.T, path string, value map[string]any) {
	t.Helper()
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("marshal Gemini settings: %v", err)
	}
	encoded = append(encoded, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("create Gemini settings directory: %v", err)
	}
	if err := os.WriteFile(path, encoded, 0600); err != nil {
		t.Fatalf("write Gemini settings: %v", err)
	}
}

func geminiTestManagedHandler(
	t *testing.T,
	settings map[string]any,
	event string,
	scriptName string,
	hookName string,
) map[string]any {
	t.Helper()
	hooks := requireObject(t, settings["hooks"])
	for _, groupValue := range requireArray(t, hooks[event]) {
		group := requireObject(t, groupValue)
		for _, handlerValue := range requireArray(t, group["hooks"]) {
			handler := requireObject(t, handlerValue)
			reference, err := currentManagedGeminiReference(
				handler,
				scriptName,
				hookName,
			)
			if err != nil {
				t.Fatalf("inspect managed %s handler: %v", event, err)
			}
			if reference != nil {
				return handler
			}
		}
	}
	t.Fatalf("%s handler for %s not found", event, scriptName)
	return nil
}

func requireStrictGeminiPair(t *testing.T, settings map[string]any) {
	t.Helper()
	hooks := requireObject(t, settings["hooks"])
	for _, item := range []struct {
		event, script, name string
	}{
		{"BeforeTool", geminiGuardScript, geminiGuardHookName},
		{"AfterTool", geminiAuditScript, geminiAuditHookName},
	} {
		handler := geminiTestManagedHandler(
			t,
			settings,
			item.event,
			item.script,
			item.name,
		)
		want := map[string]any{
			"type": "command", "name": item.name,
			"command": handler["command"], "timeout": float64(10_000),
		}
		if !reflect.DeepEqual(handler, want) {
			t.Fatalf("%s handler = %#v", item.event, handler)
		}
		managed := 0
		for _, groupValue := range requireArray(t, hooks[item.event]) {
			group := requireObject(t, groupValue)
			for _, handlerValue := range requireArray(t, group["hooks"]) {
				reference, err := currentManagedGeminiReference(
					requireObject(t, handlerValue),
					item.script,
					item.name,
				)
				if err != nil {
					t.Fatalf("inspect %s contract: %v", item.event, err)
				}
				if reference != nil {
					managed++
					if len(group) != 1 || len(requireArray(t, group["hooks"])) != 1 {
						t.Fatalf("%s managed group has extra fields: %#v", item.event, group)
					}
				}
			}
		}
		if managed != 1 {
			t.Fatalf("%s managed handler count = %d", item.event, managed)
		}
		command := handler["command"].(string)
		if runtime.GOOS == "windows" && !strings.Contains(command, " -EncodedCommand ") {
			t.Fatalf("Windows Gemini command is not encoded PowerShell: %q", command)
		}
	}
}

func geminiOfficialPayload(fixture *geminiFixture, event string) map[string]any {
	payload := map[string]any{
		"session_id": "session-1", "transcript_path": filepath.Join(
			fixture.projectDir,
			"transcript.jsonl",
		),
		"cwd": fixture.projectDir, "hook_event_name": event,
		"timestamp":  "2026-07-19T00:00:00.000Z",
		"tool_name":  "run_shell_command",
		"tool_input": map[string]any{"command": "echo test"},
	}
	if event == "AfterTool" {
		payload["tool_response"] = map[string]any{"output": "test", "error": nil}
	}
	return payload
}

func runGeminiCommand(
	t *testing.T,
	command string,
	fixture *geminiFixture,
	payload map[string]any,
) (int, string, string) {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal Gemini payload: %v", err)
	}
	return runGeminiRawCommand(t, command, fixture, encoded)
}

func runGeminiRawCommand(
	t *testing.T,
	command string,
	fixture *geminiFixture,
	encoded []byte,
) (int, string, string) {
	t.Helper()
	var process *exec.Cmd
	if runtime.GOOS == "windows" {
		process = exec.Command(
			codexPowerShellPath(),
			"-NoLogo",
			"-NoProfile",
			"-NonInteractive",
			"-Command",
			command+"; if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }",
		)
	} else {
		process = exec.Command("sh", "-c", command)
	}
	process.Dir = fixture.projectDir
	process.Env = append(
		os.Environ(),
		"HOME="+fixture.homeDir,
		"USERPROFILE="+fixture.homeDir,
		"GEMINI_CLI_HOME="+fixture.geminiHome,
		"ELYDORA_HOOK_PATH=injected-command-fragment",
	)
	process.Stdin = bytes.NewReader(encoded)
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
	t.Fatalf("run Gemini hook command: %v", err)
	return -1, stdout.String(), stderr.String()
}

func legacyGeminiCommand(scriptPath string) string {
	return "node " + scriptPath
}

func assertNoGeminiRuntimeWrites(t *testing.T, fixture *geminiFixture) {
	t.Helper()
	for _, path := range []string{
		fixture.runtimeConfig,
		fixture.privateKey,
		fixture.guardPath,
		fixture.hookPath,
	} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("runtime file exists at %s: %v", path, err)
		}
	}
}

func assertNoGeminiTransactionArtifacts(t *testing.T, root string) {
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
		t.Fatalf("walk Gemini fixture: %v", err)
	}
}

func geminiSymlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
}
