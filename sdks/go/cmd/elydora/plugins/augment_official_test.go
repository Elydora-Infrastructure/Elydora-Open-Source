package plugins

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestAugmentOfficialBinaryAcceptsInstalledUserHooks(t *testing.T) {
	entry := os.Getenv("ELYDORA_AUGGIE_ENTRY")
	if entry == "" {
		t.Skip("ELYDORA_AUGGIE_ENTRY is unset")
	}
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		t.Fatalf("resolve Node.js runtime: %v", err)
	}
	fixture := prepareAugmentFixture(t, augmentFixtureOptions{})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Auggie hooks: %v", err)
	}
	environment := append(
		os.Environ(),
		"HOME="+fixture.homeDir,
		"USERPROFILE="+fixture.homeDir,
	)
	version := exec.Command(nodePath, entry, "--version")
	version.Dir = fixture.projectDir
	version.Env = environment
	versionOutput, err := version.CombinedOutput()
	if err != nil || !strings.Contains(string(versionOutput), "0.33.0") {
		t.Fatalf("official Auggie version: %v\n%s", err, versionOutput)
	}
	load := exec.Command(nodePath, entry, "tools", "list")
	load.Dir = fixture.projectDir
	load.Env = environment
	loadOutput, err := load.CombinedOutput()
	if err != nil {
		t.Fatalf("official Auggie settings load: %v\n%s", err, loadOutput)
	}
	output := strings.ToLower(string(loadOutput))
	for _, problem := range []string{
		"invalid settings",
		"settings validation",
		"failed to parse",
		"hook configuration error",
	} {
		if strings.Contains(output, problem) {
			t.Fatalf("official Auggie loader reported %q:\n%s", problem, loadOutput)
		}
	}
}
