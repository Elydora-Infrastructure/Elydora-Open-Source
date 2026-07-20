package plugins

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func runOfficialGemini(
	t *testing.T,
	fixture *geminiFixture,
	entry string,
	args ...string,
) (string, error) {
	t.Helper()
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		t.Fatalf("resolve Node.js runtime: %v", err)
	}
	process := exec.Command(nodePath, append([]string{entry}, args...)...)
	process.Dir = fixture.projectDir
	process.Env = append(
		os.Environ(),
		"HOME="+fixture.homeDir,
		"USERPROFILE="+fixture.homeDir,
		"GEMINI_CLI_HOME="+fixture.geminiHome,
		"GEMINI_API_KEY=official-loader-test-key",
		"GEMINI_TELEMETRY_ENABLED=false",
		"OTEL_SDK_DISABLED=true",
	)
	output, runErr := process.CombinedOutput()
	return string(output), runErr
}

func TestGeminiOfficialCLIReadsManagedPair(t *testing.T) {
	entry := os.Getenv("ELYDORA_GEMINI_ENTRY")
	if entry == "" {
		t.Skip("ELYDORA_GEMINI_ENTRY is unset")
	}
	fixture := prepareGeminiFixture(t, geminiFixtureOptions{})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Gemini hooks: %v", err)
	}
	version, err := runOfficialGemini(t, fixture, entry, "--version")
	if err != nil || !strings.Contains(version, "0.51.0") {
		t.Fatalf("official Gemini version = %q, %v", version, err)
	}
	output, err := runOfficialGemini(
		t,
		fixture,
		entry,
		"--skip-trust",
		"--list-extensions",
	)
	if err != nil {
		t.Fatalf("load official Gemini settings: %v\n%s", err, output)
	}
	lower := strings.ToLower(output)
	for _, failure := range []string{
		"invalid settings",
		"settings validation",
		"failed to parse",
		"hook configuration error",
	} {
		if strings.Contains(lower, failure) {
			t.Fatalf("official Gemini rejected settings: %s", output)
		}
	}
}
