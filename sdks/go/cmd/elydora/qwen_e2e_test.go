package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type qwenCLIResult struct {
	stdout string
	stderr string
	err    error
}

func runQwenCLI(
	t *testing.T,
	binary, directory string,
	environment []string,
	arguments ...string,
) qwenCLIResult {
	t.Helper()
	command := exec.Command(binary, arguments...)
	command.Dir = directory
	command.Env = environment
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return qwenCLIResult{stdout: stdout.String(), stderr: stderr.String(), err: err}
}

func TestQwenCLIInstallStatusAndUninstallEndToEnd(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "elydora")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build Elydora CLI: %v\n%s", err, output)
	}
	home := filepath.Join(root, "home")
	workspace := filepath.Join(home, "workspace")
	if err := os.MkdirAll(workspace, 0755); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	privateKeyPath := filepath.Join(root, "private.key")
	tokenPath := filepath.Join(root, "token.txt")
	if err := os.WriteFile(
		privateKeyPath,
		[]byte("AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE\n"),
		0600,
	); err != nil {
		t.Fatalf("write private key: %v", err)
	}
	if err := os.WriteFile(tokenPath, []byte("token-1\n"), 0600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	environment := append(
		os.Environ(),
		"HOME="+home,
		"USERPROFILE="+home,
		"QWEN_HOME=",
		"QWEN_RUNTIME_DIR=",
		"QWEN_CODE_SYSTEM_SETTINGS_PATH="+filepath.Join(root, "system.json"),
		"QWEN_CODE_SYSTEM_DEFAULTS_PATH="+filepath.Join(root, "defaults.json"),
		"QWEN_CODE_TRUSTED_FOLDERS_PATH="+filepath.Join(root, "trusted.json"),
	)
	install := runQwenCLI(
		t,
		binary,
		workspace,
		environment,
		"install",
		"--agent", "qwen",
		"--org-id", "org-1",
		"--agent-id", qwenTestCLIAgentID,
		"--kid", "kid-1",
		"--private-key-file", privateKeyPath,
		"--token-file", tokenPath,
		"--base-url", "https://api.elydora.test",
	)
	if install.err != nil {
		t.Fatalf("install Qwen CLI hooks: %v\n%s", install.err, install.stderr)
	}
	settingsPath := filepath.Join(home, ".qwen", "settings.json")
	settings, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read Qwen settings: %v", err)
	}
	for _, marker := range []string{
		"PreToolUse",
		"PostToolUse",
		"PostToolUseFailure",
		"elydora-guard",
		"elydora-audit",
	} {
		if !strings.Contains(string(settings), marker) {
			t.Fatalf("installed settings are missing %q", marker)
		}
	}
	status := runQwenCLI(t, binary, workspace, environment, "status", "--agent", "qwen")
	if status.err != nil || !strings.Contains(status.stdout, "installed") {
		t.Fatalf("Qwen status: %v\n%s\n%s", status.err, status.stdout, status.stderr)
	}
	uninstall := runQwenCLI(
		t,
		binary,
		workspace,
		environment,
		"uninstall",
		"--agent", "qwen",
		"--agent-id", qwenTestCLIAgentID,
	)
	if uninstall.err != nil {
		t.Fatalf("uninstall Qwen CLI hooks: %v\n%s", uninstall.err, uninstall.stderr)
	}
	for _, path := range []string{
		settingsPath,
		filepath.Join(home, ".elydora", qwenTestCLIAgentID),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("uninstall left %s: %v", path, err)
		}
	}
}

const qwenTestCLIAgentID = "qwen-e2e-agent"
