package plugins

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const qwenTestAgentID = "agent-1"

type qwenFixtureOptions struct {
	settings  *string
	skipGuard bool
}

type qwenFixture struct {
	plugin        *QwenPlugin
	config        InstallConfig
	homeDir       string
	workspaceDir  string
	qwenDir       string
	configPath    string
	agentDir      string
	guardPath     string
	hookPath      string
	runtimeConfig string
	privateKey    string
}

type qwenCommandResult struct {
	exitCode int
	stderr   string
}

func qwenString(value string) *string {
	return &value
}

func qwenJSON(value any) *string {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		panic(err)
	}
	return qwenString(string(encoded) + "\n")
}

func prepareQwenFixture(t *testing.T, options qwenFixtureOptions) *qwenFixture {
	t.Helper()
	rootDir := t.TempDir()
	homeDir := filepath.Join(rootDir, "home with spaces and 'quote")
	workspaceDir := filepath.Join(homeDir, "workspace")
	qwenDir := filepath.Join(homeDir, ".qwen")
	configPath := filepath.Join(qwenDir, "settings.json")
	agentDir := filepath.Join(homeDir, ".elydora", qwenTestAgentID)
	guardPath := filepath.Join(agentDir, qwenGuardScript)
	hookPath := filepath.Join(agentDir, qwenAuditScript)
	for _, directory := range []string{workspaceDir, agentDir} {
		if err := os.MkdirAll(directory, 0755); err != nil {
			t.Fatalf("create fixture directory %s: %v", directory, err)
		}
	}
	if !options.skipGuard {
		guard := "process.stdin.resume(); process.stderr.write('Agent is frozen by Elydora.\\n'); process.exit(2);\n"
		if err := os.WriteFile(guardPath, []byte(guard), 0700); err != nil {
			t.Fatalf("write guard runtime: %v", err)
		}
	}
	writeOptionalQwenFile(t, configPath, options.settings)
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	unsetQwenTestEnv(t, "QWEN_HOME")
	previousDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("read current directory: %v", err)
	}
	if err := os.Chdir(workspaceDir); err != nil {
		t.Fatalf("enter workspace directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousDirectory); err != nil {
			t.Errorf("restore current directory: %v", err)
		}
	})
	return &qwenFixture{
		plugin:        &QwenPlugin{},
		homeDir:       homeDir,
		workspaceDir:  workspaceDir,
		qwenDir:       qwenDir,
		configPath:    configPath,
		agentDir:      agentDir,
		guardPath:     guardPath,
		hookPath:      hookPath,
		runtimeConfig: filepath.Join(agentDir, "config.json"),
		privateKey:    filepath.Join(agentDir, "private.key"),
		config: InstallConfig{
			AgentName:       qwenAgentKey,
			OrgID:           "org-1",
			AgentID:         qwenTestAgentID,
			PrivateKey:      "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE",
			KID:             "kid-1",
			BaseURL:         "https://api.elydora.test",
			GuardScriptPath: guardPath,
		},
	}
}

func unsetQwenTestEnv(t *testing.T, name string) {
	t.Helper()
	value, existed := os.LookupEnv(name)
	if err := os.Unsetenv(name); err != nil {
		t.Fatalf("unset %s: %v", name, err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(name, value)
		} else {
			_ = os.Unsetenv(name)
		}
	})
}

func writeOptionalQwenFile(t *testing.T, path string, source *string) {
	t.Helper()
	if source == nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("create directory for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(*source), 0600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func installQwenFixture(t *testing.T, fixture *qwenFixture) {
	t.Helper()
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Qwen Code hooks: %v", err)
	}
}

func readQwenTestFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

func readQwenTestObject(t *testing.T, path string) map[string]any {
	t.Helper()
	object, err := decodeJSONCObject(
		[]byte(readQwenTestFile(t, path)), "Qwen test settings", false,
	)
	if err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return object
}

func requireQwenObject(t *testing.T, value any) map[string]any {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("value is not an object: %#v", value)
	}
	return object
}

func requireQwenArray(t *testing.T, value any) []any {
	t.Helper()
	array, ok := value.([]any)
	if !ok {
		t.Fatalf("value is not an array: %#v", value)
	}
	return array
}

func qwenManagedGroup(t *testing.T, settings map[string]any, event, scriptPath string) map[string]any {
	t.Helper()
	hooks := requireQwenObject(t, settings["hooks"])
	for _, groupValue := range requireQwenArray(t, hooks[event]) {
		group := requireQwenObject(t, groupValue)
		for _, handlerValue := range requireQwenArray(t, group["hooks"]) {
			handler := requireQwenObject(t, handlerValue)
			command, _ := handler["command"].(string)
			_, candidate, managed := parseQwenCommand(command)
			if managed && sameQwenPath(candidate, scriptPath) {
				return group
			}
		}
	}
	t.Fatalf("managed group for %s not found", scriptPath)
	return nil
}

func qwenManagedHandler(t *testing.T, settings map[string]any, event, scriptPath string) map[string]any {
	t.Helper()
	group := qwenManagedGroup(t, settings, event, scriptPath)
	for _, handlerValue := range requireQwenArray(t, group["hooks"]) {
		handler := requireQwenObject(t, handlerValue)
		command, _ := handler["command"].(string)
		_, candidate, managed := parseQwenCommand(command)
		if managed && sameQwenPath(candidate, scriptPath) {
			return handler
		}
	}
	t.Fatalf("managed handler for %s not found", scriptPath)
	return nil
}

func runQwenHandler(t *testing.T, handler map[string]any, homeDir, payload string) qwenCommandResult {
	t.Helper()
	command := handler["command"].(string)
	var process *exec.Cmd
	if handler["shell"] == "powershell" {
		process = exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", command)
	} else {
		process = exec.Command("bash", "-c", command)
	}
	process.Env = append(os.Environ(), "HOME="+homeDir, "USERPROFILE="+homeDir)
	process.Stdin = bytes.NewBufferString(payload)
	var stderr bytes.Buffer
	process.Stderr = &stderr
	err := process.Run()
	if err == nil {
		return qwenCommandResult{stderr: stderr.String()}
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return qwenCommandResult{exitCode: exitError.ExitCode(), stderr: stderr.String()}
	}
	t.Fatalf("run Qwen Code hook command: %v", err)
	return qwenCommandResult{}
}

func requireMissingQwenFile(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be missing, got %v", path, err)
	}
}

func requireNoQwenStagingFiles(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasSuffix(entry.Name(), ".tmp") || strings.HasSuffix(entry.Name(), ".rollback") {
			t.Errorf("staging file remains: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk staging files: %v", err)
	}
}

func expectedQwenShell() string {
	if runtime.GOOS == "windows" {
		return "powershell"
	}
	return "bash"
}
