package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const clineCLIPrivateKey = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE"

func clineCLIRepository(t *testing.T) string {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve Cline CLI test source path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", ".."))
}

func buildClineCLIBinary(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "elydora")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, "./cmd/elydora")
	build.Dir = clineCLIRepository(t)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build Elydora CLI: %v\n%s", err, output)
	}
	return binary
}

func runClineCLI(
	t *testing.T,
	binary string,
	workingDirectory string,
	environment []string,
	args ...string,
) (string, string, int) {
	t.Helper()
	process := exec.Command(binary, args...)
	process.Dir = workingDirectory
	process.Env = append(os.Environ(), environment...)
	var stdout, stderr strings.Builder
	process.Stdout = &stdout
	process.Stderr = &stderr
	err := process.Run()
	if err == nil {
		return stdout.String(), stderr.String(), 0
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return stdout.String(), stderr.String(), exitError.ExitCode()
	}
	t.Fatalf("run Elydora CLI: %v", err)
	return "", "", -1
}

func prepareClineCLIEnvironment(t *testing.T) (string, string, string, []string) {
	t.Helper()
	root := t.TempDir()
	homeDir := filepath.Join(root, "home")
	clineDir := filepath.Join(root, "cline-home")
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatalf("create Cline CLI workspace: %v", err)
	}
	environment := []string{
		"HOME=" + homeDir,
		"USERPROFILE=" + homeDir,
		"CLINE_DIR=" + clineDir,
	}
	return homeDir, clineDir, workspace, environment
}

func writeClineCLISecret(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value+"\n"), 0600); err != nil {
		t.Fatalf("write Cline CLI secret: %v", err)
	}
}

func TestClineCLIPreflightPreservesCollisionBeforeRuntimeCreation(t *testing.T) {
	binary := buildClineCLIBinary(t)
	homeDir, clineDir, workspace, environment := prepareClineCLIEnvironment(t)
	hooksDir := filepath.Join(clineDir, "hooks")
	auditPath := filepath.Join(hooksDir, "PostToolUse.mjs")
	if err := os.MkdirAll(hooksDir, 0700); err != nil {
		t.Fatalf("create Cline hooks directory: %v", err)
	}
	original := "// user PostToolUse hook\n"
	if err := os.WriteFile(auditPath, []byte(original), 0600); err != nil {
		t.Fatalf("write user Cline hook: %v", err)
	}
	keyPath := filepath.Join(t.TempDir(), "private.key")
	writeClineCLISecret(t, keyPath, clineCLIPrivateKey)
	_, stderr, exit := runClineCLI(
		t,
		binary,
		workspace,
		environment,
		"install",
		"--agent", "cline",
		"--org-id", "org-1",
		"--agent-id", "agent-1",
		"--private-key-file", keyPath,
		"--kid", "kid-1",
		"--base-url", "https://api.elydora.test",
	)
	if exit == 0 || !strings.Contains(stderr, "owned by another integration") {
		t.Fatalf("CLI preflight = %d, %q", exit, stderr)
	}
	raw, err := os.ReadFile(auditPath)
	if err != nil || string(raw) != original {
		t.Fatalf("user Cline hook changed: %q, %v", raw, err)
	}
	if _, err := os.Lstat(filepath.Join(homeDir, ".elydora")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("runtime directory created during failed preflight: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(hooksDir, "PreToolUse.mjs")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("guard hook created during failed preflight: %v", err)
	}
}

func TestClineCLIInstallStatusAndUninstallEndToEnd(t *testing.T) {
	binary := buildClineCLIBinary(t)
	homeDir, clineDir, workspace, environment := prepareClineCLIEnvironment(t)
	keyPath := filepath.Join(t.TempDir(), "private.key")
	tokenPath := filepath.Join(t.TempDir(), "token")
	writeClineCLISecret(t, keyPath, clineCLIPrivateKey)
	writeClineCLISecret(t, tokenPath, "token-1")
	stdout, stderr, exit := runClineCLI(
		t,
		binary,
		workspace,
		environment,
		"install",
		"--agent", "cline",
		"--org-id", "org-1",
		"--agent-id", "agent-1",
		"--private-key-file", keyPath,
		"--token-file", tokenPath,
		"--kid", "kid-1",
		"--base-url", "http://127.0.0.1:9",
	)
	if exit != 0 || stderr != "" || !strings.Contains(stdout, "Cline") {
		t.Fatalf("CLI install = %d, %q, %q", exit, stdout, stderr)
	}
	agentDir := filepath.Join(homeDir, ".elydora", "agent-1")
	for _, path := range []string{
		filepath.Join(agentDir, "guard.js"),
		filepath.Join(agentDir, "config.json"),
		filepath.Join(agentDir, "private.key"),
		filepath.Join(agentDir, "hook.js"),
		filepath.Join(clineDir, "hooks", "PreToolUse.mjs"),
		filepath.Join(clineDir, "hooks", "PostToolUse.mjs"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("installed file missing at %s: %v", path, err)
		}
	}
	stdout, stderr, exit = runClineCLI(
		t, binary, workspace, environment, "status", "--agent", "cline",
	)
	if exit != 0 || stderr != "" || !strings.Contains(stdout, "installed") {
		t.Fatalf("CLI status = %d, %q, %q", exit, stdout, stderr)
	}
	stdout, stderr, exit = runClineCLI(
		t,
		binary,
		workspace,
		environment,
		"uninstall",
		"--agent", "cline",
		"--agent-id", "agent-1",
	)
	if exit != 0 || stderr != "" || !strings.Contains(stdout, "Removed agent directory") {
		t.Fatalf("CLI uninstall = %d, %q, %q", exit, stdout, stderr)
	}
	for _, path := range []string{
		agentDir,
		filepath.Join(clineDir, "hooks", "PreToolUse.mjs"),
		filepath.Join(clineDir, "hooks", "PostToolUse.mjs"),
	} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("managed path remains at %s: %v", path, err)
		}
	}
}
