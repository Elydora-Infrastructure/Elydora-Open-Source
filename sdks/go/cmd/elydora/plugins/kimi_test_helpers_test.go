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

const kimiTestAgentID = "agent-1"

type kimiFixtureOptions struct {
	modernConfig     *string
	legacyConfig     *string
	useDefaultHome   bool
	withoutLegacyCLI bool
	withoutGuard     bool
}

type kimiFixture struct {
	plugin        *KimiPlugin
	config        InstallConfig
	homeDir       string
	binDir        string
	kimiHome      string
	modernPath    string
	legacyPath    string
	agentDir      string
	guardPath     string
	hookPath      string
	runtimeConfig string
	privateKey    string
	legacyCLIPath string
}

func kimiString(value string) *string {
	return &value
}

func prepareKimiFixture(t *testing.T, options kimiFixtureOptions) *kimiFixture {
	t.Helper()
	homeDir := filepath.Join(t.TempDir(), "home with spaces")
	binDir := filepath.Join(homeDir, "bin")
	kimiHome := filepath.Join(homeDir, "custom kimi code")
	if options.useDefaultHome {
		kimiHome = filepath.Join(homeDir, ".kimi-code")
	}
	modernPath := filepath.Join(kimiHome, "config.toml")
	legacyPath := filepath.Join(homeDir, ".kimi", "config.toml")
	agentDir := filepath.Join(homeDir, ".elydora", kimiTestAgentID)
	guardPath := filepath.Join(agentDir, kimiGuardScript)
	hookPath := filepath.Join(agentDir, kimiAuditScript)
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("create agent directory: %v", err)
	}
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("create binary directory: %v", err)
	}
	if !options.withoutGuard {
		source := "process.stdin.resume(); process.stderr.write('Agent is frozen by Elydora.'); process.exit(2);\n"
		if err := os.WriteFile(guardPath, []byte(source), 0700); err != nil {
			t.Fatalf("write guard runtime: %v", err)
		}
	}
	writeOptionalKimiConfig(t, modernPath, options.modernConfig)
	writeOptionalKimiConfig(t, legacyPath, options.legacyConfig)

	legacyCLIPath := filepath.Join(binDir, "kimi-cli")
	if runtime.GOOS == "windows" {
		legacyCLIPath += ".cmd"
	}
	if !options.withoutLegacyCLI {
		if err := os.WriteFile(legacyCLIPath, []byte(""), 0700); err != nil {
			t.Fatalf("write legacy CLI marker: %v", err)
		}
	}
	originalPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+originalPath)
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	if options.useDefaultHome {
		t.Setenv("KIMI_CODE_HOME", "")
	} else {
		t.Setenv("KIMI_CODE_HOME", kimiHome)
	}

	return &kimiFixture{
		plugin:   &KimiPlugin{},
		homeDir:  homeDir,
		binDir:   binDir,
		kimiHome: kimiHome,
		config: InstallConfig{
			AgentName:       kimiAgentKey,
			OrgID:           "org-1",
			AgentID:         kimiTestAgentID,
			PrivateKey:      "test-key",
			KID:             "kid-1",
			BaseURL:         "https://api.elydora.test",
			GuardScriptPath: guardPath,
		},
		modernPath:    modernPath,
		legacyPath:    legacyPath,
		agentDir:      agentDir,
		guardPath:     guardPath,
		hookPath:      hookPath,
		runtimeConfig: filepath.Join(agentDir, "config.json"),
		privateKey:    filepath.Join(agentDir, "private.key"),
		legacyCLIPath: legacyCLIPath,
	}
}

func writeOptionalKimiConfig(t *testing.T, path string, raw *string) {
	t.Helper()
	if raw == nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
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

func findKimiTestHook(t *testing.T, hooks []map[string]any, event, script string) map[string]any {
	t.Helper()
	for _, hook := range hooks {
		command, _ := hook["command"].(string)
		if hook["event"] == event && strings.Contains(command, script) {
			return hook
		}
	}
	t.Fatalf("%s hook for %s not found", event, script)
	return nil
}

func requireStrictKimiHook(t *testing.T, hook map[string]any) {
	t.Helper()
	want := map[string]any{
		"event":   hook["event"],
		"command": hook["command"],
		"timeout": int64(10),
	}
	if !reflect.DeepEqual(hook, want) {
		t.Fatalf("hook = %#v, want strict contract %#v", hook, want)
	}
}

func runKimiCommand(t *testing.T, command, homeDir string, payload map[string]any) (int, string) {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal Kimi payload: %v", err)
	}
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
	process.Env = append(os.Environ(), "HOME="+homeDir, "USERPROFILE="+homeDir)
	process.Stdin = bytes.NewReader(encoded)
	var stderr bytes.Buffer
	process.Stderr = &stderr
	err = process.Run()
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
