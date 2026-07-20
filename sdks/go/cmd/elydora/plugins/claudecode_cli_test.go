package plugins

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func claudeRunCLI(
	t *testing.T,
	binary string,
	fixture *claudeFixture,
	args ...string,
) (string, string, int) {
	t.Helper()
	process := exec.Command(binary, args...)
	process.Dir = fixture.projectDir
	process.Env = append(
		os.Environ(),
		"HOME="+fixture.homeDir,
		"USERPROFILE="+fixture.homeDir,
		"CLAUDE_CONFIG_DIR="+fixture.configDir,
	)
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
	return stdout.String(), stderr.String(), -1
}

func TestClaudeCLIInstallStatusAndUninstallEndToEnd(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	repository := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", "..", ".."))
	binary := filepath.Join(t.TempDir(), "elydora")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, "./cmd/elydora")
	build.Dir = repository
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build Elydora CLI: %v\n%s", err, output)
	}
	fixture := prepareClaudeFixture(
		t,
		claudeFixtureOptions{
			configEnvPresent:  true,
			configEnvOverride: filepath.Join(t.TempDir(), "claude config"),
		},
	)
	privateKeyFile := filepath.Join(t.TempDir(), "private.key")
	tokenFile := filepath.Join(t.TempDir(), "api.token")
	if err := os.WriteFile(privateKeyFile, []byte(claudePrivateKey+"\n"), 0600); err != nil {
		t.Fatalf("write private key input: %v", err)
	}
	if err := os.WriteFile(tokenFile, []byte("token-1\n"), 0600); err != nil {
		t.Fatalf("write token input: %v", err)
	}
	stdout, stderr, exit := claudeRunCLI(
		t,
		binary,
		fixture,
		"install",
		"--agent", claudeAgentKey,
		"--org-id", "org-1",
		"--agent-id", claudeTestAgentID,
		"--private-key-file", privateKeyFile,
		"--token-file", tokenFile,
		"--kid", "kid-1",
		"--base-url", "http://127.0.0.1:9",
	)
	if exit != 0 || stderr != "" || !strings.Contains(stdout, "claude doctor") {
		t.Fatalf("CLI install = %d, %q, %q", exit, stdout, stderr)
	}
	requireStrictClaudeTriple(t, fixture)
	stdout, stderr, exit = claudeRunCLI(
		t,
		binary,
		fixture,
		"status",
		"--agent", claudeAgentKey,
	)
	if exit != 0 || stderr != "" || !strings.Contains(stdout, "installed") {
		t.Fatalf("CLI status = %d, %q, %q", exit, stdout, stderr)
	}
	stdout, stderr, exit = claudeRunCLI(
		t,
		binary,
		fixture,
		"uninstall",
		"--agent", claudeAgentKey,
		"--agent-id", claudeTestAgentID,
	)
	if exit != 0 || stderr != "" || !strings.Contains(stdout, "Removed agent directory") {
		t.Fatalf("CLI uninstall = %d, %q, %q", exit, stdout, stderr)
	}
	if _, err := os.Lstat(fixture.configPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed settings remain after CLI uninstall: %v", err)
	}
	if _, err := os.Lstat(fixture.agentDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("runtime remains after CLI uninstall: %v", err)
	}
}
