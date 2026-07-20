package plugins

import (
	"encoding/json"
	"os"
	"os/exec"
	"reflect"
	"testing"
)

func TestGrokOfficialBinaryReadsManagedTriple(t *testing.T) {
	binary := os.Getenv("ELYDORA_GROK_BINARY")
	if binary == "" {
		t.Skip("ELYDORA_GROK_BINARY is unset")
	}
	fixture := prepareGrokFixture(t, grokFixtureOptions{})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Grok hooks: %v", err)
	}
	command := exec.Command(binary, "inspect", "--json")
	command.Dir = fixture.projectDir
	command.Env = append(
		os.Environ(),
		"HOME="+fixture.homeDir,
		"USERPROFILE="+fixture.homeDir,
		"GROK_HOME="+fixture.grokHome,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("inspect official Grok config: %v\n%s", err, output)
	}
	var report struct {
		Hooks []struct {
			Event    string `json:"event"`
			HookType string `json:"hookType"`
			Target   string `json:"target"`
			Matcher  any    `json:"matcher"`
			Source   struct {
				Type string `json:"type"`
			} `json:"source"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(output, &report); err != nil {
		t.Fatalf("decode official Grok inspection: %v\n%s", err, output)
	}
	if len(report.Hooks) != 3 {
		t.Fatalf("official Grok hook count = %d\n%s", len(report.Hooks), output)
	}
	wantTargets := map[string]string{}
	settings := readGrokTestObject(t, fixture.configPath)
	for _, item := range []struct{ event, script string }{
		{"PreToolUse", grokGuardScript},
		{"PostToolUse", grokAuditScript},
		{"PostToolUseFailure", grokAuditScript},
	} {
		wantTargets[item.event] = grokTestManagedHandler(
			t,
			settings,
			item.event,
			item.script,
		)["command"].(string)
	}
	actual := map[string]string{}
	for _, hook := range report.Hooks {
		if hook.HookType != "command" || hook.Matcher != nil ||
			hook.Source.Type != "user" {
			t.Fatalf("official Grok hook shape = %#v", hook)
		}
		actual[hook.Event] = hook.Target
	}
	if !reflect.DeepEqual(actual, wantTargets) {
		t.Fatalf("official Grok hooks = %#v, want %#v", actual, wantTargets)
	}
}
