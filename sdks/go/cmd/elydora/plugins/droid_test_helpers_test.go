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

const droidTestAgentID = "agent-1"

type droidFixtureOptions struct {
	hooks       *string
	legacyHooks *string
	settings    *string
	skipGuard   bool
}

type droidFixture struct {
	plugin        *DroidPlugin
	config        InstallConfig
	homeDir       string
	workspaceDir  string
	factoryDir    string
	configPath    string
	legacyPath    string
	settingsPath  string
	agentDir      string
	guardPath     string
	hookPath      string
	runtimeConfig string
	privateKey    string
}

type droidCommandResult struct {
	exitCode int
	stderr   string
}

func droidString(value string) *string {
	return &value
}

func droidJSON(value any) *string {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		panic(err)
	}
	return droidString(string(encoded) + "\n")
}

func prepareDroidFixture(t *testing.T, options droidFixtureOptions) *droidFixture {
	t.Helper()
	rootDir := t.TempDir()
	homeDir := filepath.Join(rootDir, "home with spaces and 'quote")
	workspaceDir := filepath.Join(rootDir, "workspace")
	factoryDir := filepath.Join(homeDir, ".factory")
	configPath := filepath.Join(factoryDir, "hooks.json")
	legacyPath := filepath.Join(factoryDir, "hooks", "hooks.json")
	settingsPath := filepath.Join(factoryDir, "settings.json")
	agentDir := filepath.Join(homeDir, ".elydora", droidTestAgentID)
	guardPath := filepath.Join(agentDir, droidGuardScript)
	hookPath := filepath.Join(agentDir, droidAuditScript)
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
	writeOptionalDroidFile(t, configPath, options.hooks)
	writeOptionalDroidFile(t, legacyPath, options.legacyHooks)
	writeOptionalDroidFile(t, settingsPath, options.settings)
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	return &droidFixture{
		plugin:        &DroidPlugin{},
		homeDir:       homeDir,
		workspaceDir:  workspaceDir,
		factoryDir:    factoryDir,
		configPath:    configPath,
		legacyPath:    legacyPath,
		settingsPath:  settingsPath,
		agentDir:      agentDir,
		guardPath:     guardPath,
		hookPath:      hookPath,
		runtimeConfig: filepath.Join(agentDir, "config.json"),
		privateKey:    filepath.Join(agentDir, "private.key"),
		config: InstallConfig{
			AgentName:       droidAgentKey,
			OrgID:           "org-1",
			AgentID:         droidTestAgentID,
			PrivateKey:      "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE",
			KID:             "kid-1",
			BaseURL:         "https://api.elydora.test",
			GuardScriptPath: guardPath,
		},
	}
}

func writeOptionalDroidFile(t *testing.T, path string, source *string) {
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

func installDroidFixture(t *testing.T, fixture *droidFixture) {
	t.Helper()
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Factory Droid hooks: %v", err)
	}
}

func readDroidTestFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

func requireMissingDroidFile(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be missing, got %v", path, err)
	}
}

func readDroidTestObject(t *testing.T, path string) map[string]any {
	t.Helper()
	standard, err := standardizeJSONC(
		[]byte(readDroidTestFile(t, path)), "Factory Droid test source", true,
	)
	if err != nil {
		t.Fatalf("standardize %s: %v", path, err)
	}
	var object map[string]any
	if err := json.Unmarshal(standard, &object); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return object
}

func requireDroidObject(t *testing.T, value any) map[string]any {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("value is not an object: %#v", value)
	}
	return object
}

func requireDroidArray(t *testing.T, value any) []any {
	t.Helper()
	array, ok := value.([]any)
	if !ok {
		t.Fatalf("value is not an array: %#v", value)
	}
	return array
}

func droidManagedHandler(
	t *testing.T,
	groupsValue any,
	scriptPath string,
) map[string]any {
	t.Helper()
	group := droidManagedGroup(t, groupsValue, scriptPath)
	for _, handlerValue := range requireDroidArray(t, group["hooks"]) {
		handler := requireDroidObject(t, handlerValue)
		command, _ := handler["command"].(string)
		if strings.Contains(command, scriptPath) {
			return handler
		}
	}
	t.Fatalf("managed handler for %s not found", scriptPath)
	return nil
}

func droidManagedGroup(t *testing.T, groupsValue any, scriptPath string) map[string]any {
	t.Helper()
	for _, groupValue := range requireDroidArray(t, groupsValue) {
		group := requireDroidObject(t, groupValue)
		for _, handlerValue := range requireDroidArray(t, group["hooks"]) {
			handler := requireDroidObject(t, handlerValue)
			command, _ := handler["command"].(string)
			if strings.Contains(command, scriptPath) {
				return group
			}
		}
	}
	t.Fatalf("managed group for %s not found", scriptPath)
	return nil
}

func runDroidCommand(t *testing.T, command, homeDir, payload string) droidCommandResult {
	t.Helper()
	var process *exec.Cmd
	if runtime.GOOS == "windows" {
		commandFile := filepath.Join(t.TempDir(), "run-droid-hook.cmd")
		if err := os.WriteFile(commandFile, []byte("@echo off\r\n"+command+"\r\n"), 0600); err != nil {
			t.Fatalf("write command file: %v", err)
		}
		process = exec.Command("cmd.exe", "/d", "/c", commandFile)
	} else {
		process = exec.Command("sh", "-c", command)
	}
	process.Env = append(os.Environ(), "HOME="+homeDir, "USERPROFILE="+homeDir)
	process.Stdin = bytes.NewBufferString(payload)
	var stderr bytes.Buffer
	process.Stderr = &stderr
	err := process.Run()
	if err == nil {
		return droidCommandResult{stderr: stderr.String()}
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return droidCommandResult{exitCode: exitError.ExitCode(), stderr: stderr.String()}
	}
	t.Fatalf("run Factory Droid hook command: %v", err)
	return droidCommandResult{}
}
