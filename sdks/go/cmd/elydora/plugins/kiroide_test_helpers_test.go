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

const (
	kiroIdeTestAgentID = "agent-1"
	kiroIdePrivateKey  = "CwsLCwsLCwsLCwsLCwsLCwsLCwsLCwsLCwsLCwsLCws"
)

type kiroIdeFixture struct {
	plugin        *KiroIdePlugin
	config        InstallConfig
	homeDir       string
	workspace     string
	configPath    string
	legacyPath    string
	agentDir      string
	guardPath     string
	auditPath     string
	runtimeConfig string
	privateKey    string
}

func prepareKiroIdeFixture(t *testing.T, workspaceSource, legacySource *string) *kiroIdeFixture {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home with spaces and 'quote %KIRO%")
	preparePrivateTransactionTestDirectory(t, home)
	workspace := filepath.Join(root, "workspace with spaces")
	for _, directory := range []string{home, workspace} {
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatalf("create Kiro IDE fixture directory: %v", err)
		}
	}
	configPath := filepath.Join(workspace, ".kiro", "hooks", kiroIdeConfigFile)
	legacyPath := filepath.Join(home, ".kiro", "hooks", kiroIdeLegacyFile)
	writeOptionalKiroIdeSource(t, configPath, workspaceSource)
	writeOptionalKiroIdeSource(t, legacyPath, legacySource)
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("read current directory: %v", err)
	}
	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("enter Kiro IDE workspace: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Errorf("restore current directory: %v", err)
		}
	})
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	agentDir := filepath.Join(home, ".elydora", kiroIdeTestAgentID)
	guardPath := filepath.Join(agentDir, kiroIdeGuardScript)
	return &kiroIdeFixture{
		plugin: &KiroIdePlugin{},
		config: InstallConfig{
			AgentName: kiroIdeAgentKey, OrgID: "org-1", AgentID: kiroIdeTestAgentID,
			PrivateKey: kiroIdePrivateKey, KID: "kid-1", Token: "token-1",
			BaseURL: "https://api.elydora.test", GuardScriptPath: guardPath,
		},
		homeDir: home, workspace: workspace, configPath: configPath,
		legacyPath: legacyPath, agentDir: agentDir, guardPath: guardPath,
		auditPath:     filepath.Join(agentDir, kiroIdeAuditScript),
		runtimeConfig: filepath.Join(agentDir, "config.json"),
		privateKey:    filepath.Join(agentDir, "private.key"),
	}
}

func kiroIdeString(value string) *string {
	return &value
}

func writeOptionalKiroIdeSource(t *testing.T, path string, source *string) {
	t.Helper()
	if source == nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("create source directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(*source), 0600); err != nil {
		t.Fatalf("write Kiro IDE source: %v", err)
	}
}

func writeKiroIdeJSON(t *testing.T, path string, value any) {
	t.Helper()
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("encode Kiro IDE JSON: %v", err)
	}
	encoded = append(encoded, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("create Kiro IDE JSON directory: %v", err)
	}
	if err := os.WriteFile(path, encoded, 0600); err != nil {
		t.Fatalf("write Kiro IDE JSON: %v", err)
	}
}

func readKiroIdeObject(t *testing.T, path string) map[string]any {
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

func findKiroIdeHook(t *testing.T, document map[string]any, name string) map[string]any {
	t.Helper()
	for _, value := range requireArray(t, document["hooks"]) {
		hook := requireObject(t, value)
		if hook["name"] == name {
			return hook
		}
	}
	t.Fatalf("Kiro IDE hook %q is missing", name)
	return nil
}

func kiroIdeHookCommand(t *testing.T, hook map[string]any) string {
	t.Helper()
	action := requireObject(t, hook["action"])
	command, ok := action["command"].(string)
	if !ok {
		t.Fatalf("Kiro IDE command = %#v", action["command"])
	}
	return command
}

func legacyKiroIdeGoDocument(fixture *kiroIdeFixture, agentID string) map[string]any {
	agentDirectory := filepath.Join(fixture.homeDir, ".elydora", agentID)
	return map[string]any{
		"name": "Elydora Audit",
		"hooks": map[string]any{
			"pre_tool_use": map[string]any{
				"command":    "node " + filepath.Join(agentDirectory, kiroIdeGuardScript),
				"timeout_ms": 5000,
			},
			"post_tool_use": map[string]any{
				"command":    "node " + filepath.Join(agentDirectory, kiroIdeAuditScript),
				"timeout_ms": 5000,
			},
		},
	}
}

func runKiroIdeCommand(
	t *testing.T,
	command string,
	fixture *kiroIdeFixture,
	input []byte,
) (int, string, string) {
	t.Helper()
	var process *exec.Cmd
	if runtime.GOOS == "windows" {
		commandFile := filepath.Join(t.TempDir(), "run-kiroide-hook.cmd")
		if err := os.WriteFile(
			commandFile,
			[]byte("@echo off\r\n"+command+"\r\n"),
			0600,
		); err != nil {
			t.Fatalf("write Kiro IDE command file: %v", err)
		}
		process = exec.Command("cmd.exe", "/d", "/c", commandFile)
	} else {
		process = exec.Command("sh", "-c", command)
	}
	process.Dir = fixture.workspace
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
	t.Fatalf("run Kiro IDE command: %v", err)
	return -1, stdout.String(), stderr.String()
}

func assertNoKiroIdeRuntimeFiles(t *testing.T, fixture *kiroIdeFixture) {
	t.Helper()
	for _, path := range []string{
		fixture.runtimeConfig, fixture.privateKey, fixture.guardPath, fixture.auditPath,
	} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("runtime file exists at %s: %v", path, err)
		}
	}
}

func assertNoKiroIdeTransactionArtifacts(t *testing.T, root string) {
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
			t.Errorf("Kiro IDE transaction artifact remains at %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk Kiro IDE fixture: %v", err)
	}
}

func prepareKiroIdeInstallForTest(
	t *testing.T,
	fixture *kiroIdeFixture,
) *kiroIdePreparedTransaction {
	t.Helper()
	sources, err := readKiroIdeSources()
	if err != nil {
		t.Fatalf("read Kiro IDE sources: %v", err)
	}
	paths, nodePath, err := preflightKiroIdeInstallation(fixture.config)
	if err != nil {
		t.Fatalf("preflight Kiro IDE installation: %v", err)
	}
	guard, err := buildKiroIdeHook(kiroIdeGuardName, nodePath, paths.guardPath)
	if err != nil {
		t.Fatalf("build Kiro IDE guard: %v", err)
	}
	audit, err := buildKiroIdeHook(kiroIdeAuditName, nodePath, paths.auditPath)
	if err != nil {
		t.Fatalf("build Kiro IDE audit: %v", err)
	}
	rendered, err := renderKiroIdeDocument(
		sources.document,
		append(withoutManagedKiroIdeHooks(sources.document.hooks, ""), guard, audit),
	)
	if err != nil {
		t.Fatalf("render Kiro IDE hooks: %v", err)
	}
	prepared, err := prepareKiroIdeInstallation(fixture.config, sources, paths, rendered)
	if err != nil {
		t.Fatalf("prepare Kiro IDE installation: %v", err)
	}
	return prepared
}
