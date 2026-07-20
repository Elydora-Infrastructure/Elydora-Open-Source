package plugins

import (
	"encoding/json"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func runOfficialCline(
	t *testing.T,
	fixture *clineFixture,
	nodePath string,
	args ...string,
) (string, error) {
	t.Helper()
	process := exec.Command(nodePath, args...)
	process.Dir = fixture.workspaceDir
	process.Env = append(
		os.Environ(),
		"HOME="+fixture.homeDir,
		"USERPROFILE="+fixture.homeDir,
		"CLINE_DIR="+fixture.clineDir,
	)
	output, err := process.CombinedOutput()
	return string(output), err
}

func TestClineOfficialLoaderDiscoversBothManagedFileHooks(t *testing.T) {
	coreEntry := os.Getenv("ELYDORA_CLINE_CORE_ENTRY")
	clineEntry := os.Getenv("ELYDORA_CLINE_ENTRY")
	if coreEntry == "" || clineEntry == "" {
		t.Skip("set ELYDORA_CLINE_CORE_ENTRY and ELYDORA_CLINE_ENTRY")
	}
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		t.Fatalf("resolve Node.js runtime: %v", err)
	}
	fixture := prepareClineFixture(t, clineFixtureOptions{})
	installClineFixture(t, fixture)
	version, err := runOfficialCline(t, fixture, nodePath, clineEntry, "--version")
	foundVersion := false
	for _, line := range strings.Split(strings.ReplaceAll(version, "\r\n", "\n"), "\n") {
		foundVersion = foundVersion || strings.TrimSpace(line) == "3.0.46"
	}
	if err != nil || !foundVersion {
		t.Fatalf("official Cline version = %q, %v", version, err)
	}
	source := `
import { pathToFileURL } from 'node:url';
const { listHookConfigFiles } = await import(pathToFileURL(process.env.ELYDORA_CLINE_CORE_ENTRY).href);
console.log(JSON.stringify(listHookConfigFiles(process.env.ELYDORA_WORKSPACE)));
`
	process := exec.Command(nodePath, "--input-type=module", "--eval", source)
	process.Dir = fixture.workspaceDir
	process.Env = append(
		os.Environ(),
		"HOME="+fixture.homeDir,
		"USERPROFILE="+fixture.homeDir,
		"CLINE_DIR="+fixture.clineDir,
		"ELYDORA_CLINE_CORE_ENTRY="+coreEntry,
		"ELYDORA_WORKSPACE="+fixture.workspaceDir,
	)
	output, err := process.CombinedOutput()
	if err != nil {
		t.Fatalf("official Cline hook discovery: %v\n%s", err, output)
	}
	var actual []map[string]any
	if err := json.Unmarshal(output, &actual); err != nil {
		t.Fatalf("decode official Cline hook discovery: %v\n%s", err, output)
	}
	expected := []map[string]any{
		{"fileName": "PostToolUse", "hookEventName": "tool_result", "path": fixture.auditWrapper},
		{"fileName": "PreToolUse", "hookEventName": "tool_call", "path": fixture.guardWrapper},
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("official Cline hooks = %#v", actual)
	}
}
