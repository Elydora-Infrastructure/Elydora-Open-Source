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

func geminiRunCLI(
	t *testing.T,
	binary string,
	fixture *geminiFixture,
	args ...string,
) (string, string, int) {
	t.Helper()
	process := exec.Command(binary, args...)
	process.Dir = fixture.projectDir
	process.Env = append(
		os.Environ(),
		"HOME="+fixture.homeDir,
		"USERPROFILE="+fixture.homeDir,
		"GEMINI_CLI_HOME="+fixture.geminiHome,
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

func TestGeminiCLIInstallStatusAndUninstallEndToEnd(t *testing.T) {
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
	fixture := prepareGeminiFixture(t, geminiFixtureOptions{})
	privateKeyFile := filepath.Join(t.TempDir(), "private.key")
	tokenFile := filepath.Join(t.TempDir(), "api.token")
	if err := os.WriteFile(privateKeyFile, []byte(geminiPrivateKey+"\n"), 0600); err != nil {
		t.Fatalf("write private key input: %v", err)
	}
	if err := os.WriteFile(tokenFile, []byte("token-1\n"), 0600); err != nil {
		t.Fatalf("write token input: %v", err)
	}
	stdout, stderr, exit := geminiRunCLI(
		t,
		binary,
		fixture,
		"install",
		"--agent", geminiAgentKey,
		"--org-id", "org-1",
		"--agent-id", geminiTestAgentID,
		"--private-key-file", privateKeyFile,
		"--token-file", tokenFile,
		"--kid", "kid-1",
		"--base-url", "http://127.0.0.1:9",
	)
	if exit != 0 || stderr != "" || !strings.Contains(stdout, "/hooks list") {
		t.Fatalf("CLI install = %d, %q, %q", exit, stdout, stderr)
	}
	requireStrictGeminiPair(t, readGeminiTestObject(t, fixture.settingsPath))
	stdout, stderr, exit = geminiRunCLI(
		t,
		binary,
		fixture,
		"status",
		"--agent", geminiAgentKey,
	)
	if exit != 0 || stderr != "" || !strings.Contains(stdout, "installed") {
		t.Fatalf("CLI status = %d, %q, %q", exit, stdout, stderr)
	}
	stdout, stderr, exit = geminiRunCLI(
		t,
		binary,
		fixture,
		"uninstall",
		"--agent", geminiAgentKey,
		"--agent-id", geminiTestAgentID,
	)
	if exit != 0 || stderr != "" || !strings.Contains(stdout, "Removed agent directory") {
		t.Fatalf("CLI uninstall = %d, %q, %q", exit, stdout, stderr)
	}
	if _, err := os.Lstat(fixture.settingsPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed settings remain after CLI uninstall: %v", err)
	}
	if _, err := os.Lstat(fixture.agentDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("runtime remains after CLI uninstall: %v", err)
	}
}
