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
	grokTestAgentID = "agent-1"
	grokPrivateKey  = "BwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwc"
)

type grokFixtureOptions struct {
	existingRaw    *string
	useDefaultHome bool
}

type grokFixture struct {
	plugin        *GrokPlugin
	config        InstallConfig
	homeDir       string
	projectDir    string
	grokHome      string
	agentDir      string
	configPath    string
	guardPath     string
	hookPath      string
	runtimeConfig string
	privateKey    string
}

func grokString(value string) *string {
	return &value
}

func prepareGrokFixture(t *testing.T, options grokFixtureOptions) *grokFixture {
	t.Helper()
	root := t.TempDir()
	homeDir := filepath.Join(root, "home with 'quote %ELYDORA_HOOK_PATH%")
	projectDir := filepath.Join(root, "project with spaces")
	grokHome := filepath.Join(homeDir, "custom grok")
	if options.useDefaultHome {
		grokHome = filepath.Join(homeDir, ".grok")
	}
	agentDir := filepath.Join(homeDir, ".elydora", grokTestAgentID)
	configPath := filepath.Join(grokHome, "hooks", grokConfigFile)
	if err := os.MkdirAll(projectDir, 0700); err != nil {
		t.Fatalf("create project directory: %v", err)
	}
	if options.existingRaw != nil {
		if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
			t.Fatalf("create Grok config directory: %v", err)
		}
		if err := os.WriteFile(configPath, []byte(*options.existingRaw), 0600); err != nil {
			t.Fatalf("write Grok config: %v", err)
		}
	}
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	t.Setenv("ELYDORA_HOOK_PATH", filepath.Join(root, "expanded-command-fragment"))
	if options.useDefaultHome {
		t.Setenv("GROK_HOME", "")
	} else {
		t.Setenv("GROK_HOME", grokHome)
	}
	guardPath := filepath.Join(agentDir, grokGuardScript)
	return &grokFixture{
		plugin: &GrokPlugin{}, config: InstallConfig{
			AgentName: grokAgentKey, OrgID: "org-1", AgentID: grokTestAgentID,
			PrivateKey: grokPrivateKey, KID: "kid-1", Token: "token-1",
			BaseURL: "https://api.elydora.test", GuardScriptPath: guardPath,
		},
		homeDir: homeDir, projectDir: projectDir, grokHome: grokHome,
		agentDir: agentDir, configPath: configPath, guardPath: guardPath,
		hookPath:      filepath.Join(agentDir, grokAuditScript),
		runtimeConfig: filepath.Join(agentDir, "config.json"),
		privateKey:    filepath.Join(agentDir, "private.key"),
	}
}

func readGrokTestObject(t *testing.T, path string) map[string]any {
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

func grokTestManagedHandler(
	t *testing.T,
	settings map[string]any,
	event string,
	scriptName string,
) map[string]any {
	t.Helper()
	hooks := requireObject(t, settings["hooks"])
	for _, groupValue := range requireArray(t, hooks[event]) {
		group := requireObject(t, groupValue)
		for _, handlerValue := range requireArray(t, group["hooks"]) {
			handler := requireObject(t, handlerValue)
			reference, err := managedGrokReference(handler, scriptName)
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

func requireStrictGrokTriple(t *testing.T, settings map[string]any) {
	t.Helper()
	hooks := requireObject(t, settings["hooks"])
	commands := map[string]string{}
	for _, item := range []struct{ event, script string }{
		{"PreToolUse", grokGuardScript},
		{"PostToolUse", grokAuditScript},
		{"PostToolUseFailure", grokAuditScript},
	} {
		groups := requireArray(t, hooks[item.event])
		handler := grokTestManagedHandler(t, settings, item.event, item.script)
		wantHandler := map[string]any{
			"type": "command", "command": handler["command"], "timeout": float64(10),
		}
		if !reflect.DeepEqual(handler, wantHandler) {
			t.Fatalf("%s contract = %#v", item.event, groups)
		}
		managedGroups := 0
		for _, groupValue := range groups {
			group := requireObject(t, groupValue)
			for _, handlerValue := range requireArray(t, group["hooks"]) {
				candidate := requireObject(t, handlerValue)
				reference, err := managedGrokReference(candidate, item.script)
				if err != nil {
					t.Fatalf("inspect %s contract: %v", item.event, err)
				}
				if reference != nil {
					managedGroups++
					if len(group) != 1 || len(requireArray(t, group["hooks"])) != 1 {
						t.Fatalf("%s managed group has extra fields: %#v", item.event, group)
					}
				}
			}
		}
		if managedGroups != 1 {
			t.Fatalf("%s managed group count = %d", item.event, managedGroups)
		}
		command := handler["command"].(string)
		if runtime.GOOS == "windows" && !strings.Contains(command, " -EncodedCommand ") {
			t.Fatalf("Windows Grok command is not encoded PowerShell: %q", command)
		}
		commands[item.event] = command
	}
	if commands["PostToolUse"] != commands["PostToolUseFailure"] {
		t.Fatal("Grok success and failure audit commands differ")
	}
}

func grokOfficialPayload(event string) map[string]any {
	nativeName := map[string]string{
		"PreToolUse": "pre_tool_use", "PostToolUse": "post_tool_use",
		"PostToolUseFailure": "post_tool_use_failure",
	}[event]
	payload := map[string]any{
		"hookEventName":      nativeName,
		"sessionId":          "session-1",
		"cwd":                "C:/project",
		"workspaceRoot":      "C:/project",
		"toolName":           "Bash",
		"toolInput":          map[string]any{"command": "echo test"},
		"toolUseId":          "call-1",
		"toolInputTruncated": false,
		"timestamp":          "2026-07-19T12:00:00.000Z",
	}
	if event != "PreToolUse" {
		payload["toolResult"] = map[string]any{
			"output": "test", "success": event == "PostToolUse",
		}
		payload["toolResultTruncated"] = false
		payload["durationMs"] = float64(12)
	}
	return payload
}

func runGrokCommand(
	t *testing.T,
	command string,
	fixture *grokFixture,
	payload map[string]any,
) (int, string, string) {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal Grok payload: %v", err)
	}
	return runGrokRawCommand(t, command, fixture, encoded)
}

func runGrokRawCommand(
	t *testing.T,
	command string,
	fixture *grokFixture,
	encoded []byte,
) (int, string, string) {
	t.Helper()
	var process *exec.Cmd
	if runtime.GOOS == "windows" {
		commandFile := filepath.Join(t.TempDir(), "run-grok-hook.cmd")
		if err := os.WriteFile(
			commandFile,
			[]byte("@echo off\r\n"+command+"\r\n"),
			0600,
		); err != nil {
			t.Fatalf("write Grok command file: %v", err)
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
	t.Fatalf("run Grok hook command: %v", err)
	return -1, stdout.String(), stderr.String()
}

func writeGrokTestObject(t *testing.T, path string, value map[string]any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal Grok config: %v", err)
	}
	if err := os.WriteFile(path, encoded, 0600); err != nil {
		t.Fatalf("write Grok config: %v", err)
	}
}

func legacyGrokCommand(t *testing.T, scriptPath string) string {
	t.Helper()
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		t.Fatalf("resolve Node.js runtime: %v", err)
	}
	if runtime.GOOS == "windows" {
		return quoteGrokLegacyWindowsArgument(nodePath) + " " +
			quoteGrokLegacyWindowsArgument(scriptPath)
	}
	return quotePOSIXArgument(nodePath) + " " + quotePOSIXArgument(scriptPath)
}

func assertNoGrokRuntimeWrites(t *testing.T, fixture *grokFixture) {
	t.Helper()
	for _, path := range []string{
		fixture.runtimeConfig, fixture.privateKey, fixture.guardPath, fixture.hookPath,
	} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("runtime file exists at %s: %v", path, err)
		}
	}
}

func assertNoGrokTransactionArtifacts(t *testing.T, root string) {
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
		t.Fatalf("walk Grok fixture: %v", err)
	}
}

func grokSymlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
}
