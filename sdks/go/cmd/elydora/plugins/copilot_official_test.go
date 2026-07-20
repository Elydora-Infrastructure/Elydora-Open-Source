package plugins

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestOfficialCopilot1071LoadsAllManagedHooks(t *testing.T) {
	entry := os.Getenv("ELYDORA_COPILOT_ENTRY")
	runtimeEntry := os.Getenv("ELYDORA_COPILOT_RUNTIME_ENTRY")
	nodePath, nodeErr := exec.LookPath("node")
	if entry == "" || runtimeEntry == "" || nodeErr != nil {
		t.Skip(
			"set ELYDORA_COPILOT_ENTRY and ELYDORA_COPILOT_RUNTIME_ENTRY to official package files",
		)
	}
	fixture := prepareCopilotFixture(t, copilotFixtureOptions{})
	installCopilotFixture(t, fixture)
	environment := append(os.Environ(),
		"HOME="+fixture.homeDir,
		"USERPROFILE="+fixture.homeDir,
		"COPILOT_HOME="+fixture.copilotHome,
	)
	version := exec.Command(nodePath, entry, "--version") // #nosec G204 -- test paths are explicit environment inputs.
	version.Dir = fixture.projectDir
	version.Env = environment
	versionOutput, err := version.CombinedOutput()
	if err != nil || !strings.Contains(string(versionOutput), "GitHub Copilot CLI 1.0.71.") {
		t.Fatalf("official Copilot version = %q, %v", versionOutput, err)
	}

	source := `
import { createRequire } from 'node:module';
const require = createRequire(import.meta.url);
const runtime = require(process.env.ELYDORA_COPILOT_RUNTIME_ENTRY);
const session = await runtime.hookSessionCreate({
  cwd: process.env.ELYDORA_PROJECT,
  repoRoot: process.env.ELYDORA_PROJECT,
  sessionId: 'elydora-go-official-test',
  settingsJson: '{}',
  userHooksDir: process.env.ELYDORA_HOOKS,
  allowLocalhost: false,
  allowHttpAuthHooks: false,
  discoverPolicies: false,
});
try {
  const snapshot = JSON.parse(await runtime.hookSessionSnapshot(session.handle));
  console.log(JSON.stringify({ load: session.load, snapshot }));
} finally {
  runtime.hookSessionDispose(session.handle);
}
`
	load := exec.Command( // #nosec G204 -- nodePath is resolved through exec.LookPath.
		nodePath,
		"--input-type=module",
		"--eval",
		source,
	)
	load.Dir = fixture.projectDir
	load.Env = append(environment,
		"ELYDORA_COPILOT_RUNTIME_ENTRY="+runtimeEntry,
		"ELYDORA_HOOKS="+filepath.Dir(fixture.configPath),
		"ELYDORA_PROJECT="+fixture.projectDir,
	)
	loaded, err := load.CombinedOutput()
	if err != nil {
		t.Fatalf("load official Copilot hooks: %s: %v", loaded, err)
	}
	var result map[string]any
	if err := json.Unmarshal(loaded, &result); err != nil {
		t.Fatalf("decode official Copilot loader output %q: %v", loaded, err)
	}
	loadResult := requireCopilotObject(t, result["load"])
	if loadResult["hookCount"] != float64(3) ||
		len(requireCopilotArray(t, loadResult["errors"])) != 0 ||
		len(requireCopilotArray(t, loadResult["warnings"])) != 0 {
		t.Fatalf("official Copilot load result = %#v", loadResult)
	}
	snapshot := requireCopilotObject(t, result["snapshot"])
	hooks := requireCopilotArray(t, snapshot["hooks"])
	events := make([]string, 0, len(hooks))
	for _, value := range hooks {
		hook := requireCopilotObject(t, value)
		events = append(events, stringValue(hook["eventName"]))
		if filepath.Base(stringValue(hook["source"])) != copilotConfigFile {
			t.Fatalf("official Copilot hook source = %#v", hook["source"])
		}
		var spec map[string]any
		if err := json.Unmarshal([]byte(stringValue(hook["specJson"])), &spec); err != nil {
			t.Fatalf("decode official hook spec: %v", err)
		}
		config := requireCopilotObject(t, spec["config"])
		if config["type"] != "command" || config["timeoutSec"] != copilotHookTimeout ||
			!strings.Contains(strings.ToLower(stringValue(config["bash"])), "node") ||
			!strings.HasPrefix(stringValue(config["powershell"]), "& ") {
			t.Fatalf("official Copilot hook spec = %#v", spec)
		}
	}
	sort.Strings(events)
	want := []string{"postToolUse", "postToolUseFailure", "preToolUse"}
	if strings.Join(events, ",") != strings.Join(want, ",") {
		t.Fatalf("official Copilot events = %#v, want %#v", events, want)
	}
}
