package plugins

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

const augmentTestAgentID = "agent-1"

type augmentFixtureOptions struct {
	existingRaw  *string
	withoutGuard bool
}

type augmentFixture struct {
	plugin        *AugmentPlugin
	config        InstallConfig
	homeDir       string
	workspaceDir  string
	agentDir      string
	configPath    string
	guardPath     string
	hookPath      string
	guardWrapper  string
	auditWrapper  string
	runtimeConfig string
	privateKey    string
}

func augmentString(value string) *string {
	return &value
}

func prepareAugmentFixture(
	t *testing.T,
	options augmentFixtureOptions,
) *augmentFixture {
	t.Helper()
	homeDir := filepath.Join(t.TempDir(), "home with spaces and 'quote")
	workspaceDir := filepath.Join(homeDir, "workspace")
	agentDir := filepath.Join(homeDir, ".elydora", augmentTestAgentID)
	configPath := filepath.Join(homeDir, ".augment", "settings.json")
	guardPath := filepath.Join(agentDir, augmentGuardScript)
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
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
			t.Fatalf("create Auggie config directory: %v", err)
		}
		if err := os.WriteFile(configPath, []byte(*options.existingRaw), 0600); err != nil {
			t.Fatalf("write Auggie settings: %v", err)
		}
	}
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	return &augmentFixture{
		plugin:       &AugmentPlugin{},
		homeDir:      homeDir,
		workspaceDir: workspaceDir,
		agentDir:     agentDir,
		config: InstallConfig{
			AgentName:       augmentAgentKey,
			OrgID:           "org-1",
			AgentID:         augmentTestAgentID,
			PrivateKey:      "test-key",
			KID:             "kid-1",
			BaseURL:         "https://api.elydora.test",
			GuardScriptPath: guardPath,
		},
		configPath:    configPath,
		guardPath:     guardPath,
		hookPath:      filepath.Join(agentDir, augmentAuditScript),
		guardWrapper:  filepath.Join(agentDir, augmentGuardWrapperName()),
		auditWrapper:  filepath.Join(agentDir, augmentAuditWrapperName()),
		runtimeConfig: filepath.Join(agentDir, "config.json"),
		privateKey:    filepath.Join(agentDir, "private.key"),
	}
}

func readAugmentTestObject(t *testing.T, path string) map[string]any {
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

func writeAugmentTestObject(t *testing.T, path string, value map[string]any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal Auggie settings: %v", err)
	}
	if err := os.WriteFile(path, encoded, 0600); err != nil {
		t.Fatalf("write Auggie settings: %v", err)
	}
}

func augmentTestManagedHandler(
	t *testing.T,
	settings map[string]any,
	event, wrapperPath string,
) map[string]any {
	t.Helper()
	expected := buildAugmentCommand(wrapperPath)
	hooks := requireObject(t, settings["hooks"])
	for _, groupValue := range requireArray(t, hooks[event]) {
		group := requireObject(t, groupValue)
		for _, handlerValue := range requireArray(t, group["hooks"]) {
			handler := requireObject(t, handlerValue)
			if handler["command"] == expected {
				return handler
			}
		}
	}
	t.Fatalf("%s handler for %s not found", event, wrapperPath)
	return nil
}

func runAugmentCommand(
	t *testing.T,
	command, homeDir, payload string,
) (int, string) {
	t.Helper()
	var process *exec.Cmd
	if runtime.GOOS == "windows" {
		commandFile := filepath.Join(t.TempDir(), "run-auggie-hook.cmd")
		contents := []byte("@echo off\r\n" + command + "\r\n")
		if err := os.WriteFile(commandFile, contents, 0600); err != nil {
			t.Fatalf("write Auggie command file: %v", err)
		}
		process = exec.Command("cmd.exe", "/d", "/c", commandFile)
	} else {
		process = exec.Command("sh", "-c", command)
	}
	process.Env = append(
		os.Environ(), "HOME="+homeDir, "USERPROFILE="+homeDir,
	)
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
	t.Fatalf("run Auggie hook command: %v", err)
	return -1, stderr.String()
}
