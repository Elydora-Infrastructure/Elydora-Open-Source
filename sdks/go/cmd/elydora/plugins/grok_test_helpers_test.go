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

const grokTestAgentID = "agent-1"

type grokFixtureOptions struct {
	existingRaw    *string
	useDefaultHome bool
	withoutGuard   bool
}

type grokFixture struct {
	plugin        *GrokPlugin
	config        InstallConfig
	homeDir       string
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
	homeDir := filepath.Join(t.TempDir(), "home with spaces")
	grokHome := filepath.Join(homeDir, "custom grok")
	if options.useDefaultHome {
		grokHome = filepath.Join(homeDir, ".grok")
	}
	agentDir := filepath.Join(homeDir, ".elydora", grokTestAgentID)
	guardPath := filepath.Join(agentDir, grokGuardScript)
	configPath := filepath.Join(grokHome, "hooks", grokConfigFile)
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("create agent directory: %v", err)
	}
	if !options.withoutGuard {
		source := "process.stdin.resume(); process.stderr.write('Agent is frozen by Elydora.'); process.exit(2);\n"
		if err := os.WriteFile(guardPath, []byte(source), 0700); err != nil {
			t.Fatalf("write guard runtime: %v", err)
		}
	}
	if options.existingRaw != nil {
		if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
			t.Fatalf("create Grok config directory: %v", err)
		}
		if err := os.WriteFile(configPath, []byte(*options.existingRaw), 0600); err != nil {
			t.Fatalf("write Grok config: %v", err)
		}
	}
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	if options.useDefaultHome {
		t.Setenv("GROK_HOME", "")
	} else {
		t.Setenv("GROK_HOME", grokHome)
	}
	return &grokFixture{
		plugin:   &GrokPlugin{},
		homeDir:  homeDir,
		grokHome: grokHome,
		agentDir: agentDir,
		config: InstallConfig{
			AgentName:       grokAgentKey,
			OrgID:           "org-1",
			AgentID:         grokTestAgentID,
			PrivateKey:      "test-key",
			KID:             "kid-1",
			BaseURL:         "https://api.elydora.test",
			GuardScriptPath: guardPath,
		},
		configPath:    configPath,
		guardPath:     guardPath,
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

func grokTestManagedHandler(t *testing.T, settings map[string]any, event, scriptName string) map[string]any {
	t.Helper()
	hooks := requireObject(t, settings["hooks"])
	for _, groupValue := range requireArray(t, hooks[event]) {
		group := requireObject(t, groupValue)
		if _, hasMatcher := group["matcher"]; hasMatcher {
			continue
		}
		for _, handlerValue := range requireArray(t, group["hooks"]) {
			handler := requireObject(t, handlerValue)
			command, _ := handler["command"].(string)
			if strings.Contains(command, scriptName) {
				return handler
			}
		}
	}
	t.Fatalf("%s handler for %s not found", event, scriptName)
	return nil
}

func runGrokCommand(t *testing.T, command, homeDir, payload string) (int, string) {
	t.Helper()
	var process *exec.Cmd
	if runtime.GOOS == "windows" {
		commandFile := filepath.Join(t.TempDir(), "run-grok-hook.cmd")
		if err := os.WriteFile(commandFile, []byte("@echo off\r\n"+command+"\r\n"), 0600); err != nil {
			t.Fatalf("write Grok command file: %v", err)
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
		return 0, stderr.String()
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitError.ExitCode(), stderr.String()
	}
	t.Fatalf("run Grok hook command: %v", err)
	return -1, stderr.String()
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
