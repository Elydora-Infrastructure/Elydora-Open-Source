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

const (
	augmentTestAgentID = "agent-1"
	augmentPrivateKey  = "BwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwc"
)

type augmentFixtureOptions struct {
	existingRaw *string
}

type augmentFixture struct {
	plugin        *AugmentPlugin
	config        InstallConfig
	homeDir       string
	projectDir    string
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
	root := t.TempDir()
	homeDir := filepath.Join(root, "home with spaces and 'quote")
	projectDir := filepath.Join(root, "project with spaces")
	if err := os.MkdirAll(projectDir, 0700); err != nil {
		t.Fatalf("create project directory: %v", err)
	}
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	agentDir := filepath.Join(homeDir, ".elydora", augmentTestAgentID)
	configPath := filepath.Join(homeDir, ".augment", "settings.json")
	if options.existingRaw != nil {
		if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
			t.Fatalf("create Auggie config directory: %v", err)
		}
		if err := os.WriteFile(configPath, []byte(*options.existingRaw), 0600); err != nil {
			t.Fatalf("write Auggie settings: %v", err)
		}
	}
	guardPath := filepath.Join(agentDir, augmentGuardScript)
	return &augmentFixture{
		plugin: &AugmentPlugin{},
		config: InstallConfig{
			AgentName: augmentAgentKey, OrgID: "org-1", AgentID: augmentTestAgentID,
			PrivateKey: augmentPrivateKey, KID: "kid-1", Token: "token-1",
			BaseURL: "https://api.elydora.test", GuardScriptPath: guardPath,
		},
		homeDir: homeDir, projectDir: projectDir, agentDir: agentDir,
		configPath: configPath, guardPath: guardPath,
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
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("marshal Auggie settings: %v", err)
	}
	encoded = append(encoded, '\n')
	writeAugmentTestFile(t, path, encoded, 0600)
}

func writeAugmentTestFile(
	t *testing.T,
	path string,
	contents []byte,
	mode os.FileMode,
) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("create parent directory for %s: %v", path, err)
	}
	if err := os.WriteFile(path, contents, mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func augmentTestManagedHandler(
	t *testing.T,
	settings map[string]any,
	event string,
	wrapperPath string,
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
	command string,
	fixture *augmentFixture,
	payload []byte,
) (int, string, string) {
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
	process.Dir = fixture.projectDir
	process.Env = append(
		os.Environ(),
		"HOME="+fixture.homeDir,
		"USERPROFILE="+fixture.homeDir,
	)
	process.Stdin = bytes.NewReader(payload)
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
	t.Fatalf("run Auggie hook command: %v", err)
	return -1, stdout.String(), stderr.String()
}

func assertNoAugmentRuntimeWrites(t *testing.T, fixture *augmentFixture) {
	t.Helper()
	for _, path := range []string{
		fixture.runtimeConfig,
		fixture.privateKey,
		fixture.guardPath,
		fixture.hookPath,
		fixture.guardWrapper,
		fixture.auditWrapper,
	} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("runtime file exists at %s: %v", path, err)
		}
	}
}

func assertNoAugmentTransactionArtifacts(t *testing.T, root string) {
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
		t.Fatalf("walk Auggie fixture: %v", err)
	}
}

func augmentSymlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
}
