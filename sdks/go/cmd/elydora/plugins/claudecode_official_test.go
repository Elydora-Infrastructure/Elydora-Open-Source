package plugins

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestClaudeOfficialBinaryAcceptsInstalledSettings(t *testing.T) {
	binary := os.Getenv("ELYDORA_CLAUDE_BINARY")
	if binary == "" {
		t.Skip("ELYDORA_CLAUDE_BINARY is unset")
	}
	source := `{"hooks":{
  "Stop":[{"hooks":[{
    "type":"command",
    "command":"node",
    "args":["--version"],
    "asyncRewake":true,
    "rewakeMessage":"Background validation failed",
    "rewakeSummary":"Validation feedback"
  }]}]
}}`
	fixture := prepareClaudeFixture(
		t,
		claudeFixtureOptions{existingRaw: &source},
	)
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Claude hooks: %v", err)
	}
	environment := append(
		os.Environ(),
		"HOME="+fixture.homeDir,
		"USERPROFILE="+fixture.homeDir,
		"CLAUDE_CONFIG_DIR="+fixture.configDir,
		"DISABLE_AUTOUPDATER=1",
		"DISABLE_TELEMETRY=1",
	)
	version := exec.Command(binary, "--version")
	version.Dir = fixture.projectDir
	version.Env = environment
	versionOutput, err := version.CombinedOutput()
	if err != nil || !strings.Contains(string(versionOutput), "Claude Code") {
		t.Fatalf("official Claude version: %v\n%s", err, versionOutput)
	}
	doctor := exec.Command(binary, "doctor")
	doctor.Dir = fixture.projectDir
	doctor.Env = environment
	doctorOutput, err := doctor.CombinedOutput()
	if err != nil {
		t.Fatalf("official Claude doctor: %v\n%s", err, doctorOutput)
	}
	output := strings.ToLower(string(doctorOutput))
	for _, problem := range []string{
		"invalid settings",
		"settings validation",
		"failed to parse",
	} {
		if strings.Contains(output, problem) {
			t.Fatalf("official Claude doctor reported %q:\n%s", problem, doctorOutput)
		}
	}
}
