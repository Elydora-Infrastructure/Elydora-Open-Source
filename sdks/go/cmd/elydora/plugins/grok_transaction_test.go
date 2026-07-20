package plugins

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func snapshotGrokFiles(t *testing.T, paths ...string) map[string][]byte {
	t.Helper()
	result := make(map[string][]byte, len(paths))
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read snapshot %s: %v", path, err)
		}
		result[path] = raw
	}
	return result
}

func requireGrokSnapshot(t *testing.T, snapshot map[string][]byte) {
	t.Helper()
	for path, want := range snapshot {
		actual, err := os.ReadFile(path)
		if err != nil || string(actual) != string(want) {
			t.Fatalf("snapshot changed at %s: %q, %v", path, actual, err)
		}
	}
}

func TestGrokInstallRollsBackFourRuntimesAfterConfigCommitFailure(t *testing.T) {
	fixture := prepareGrokFixture(t, grokFixtureOptions{})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Grok hooks: %v", err)
	}
	settings := readGrokTestObject(t, fixture.configPath)
	settings["owner"] = "user"
	writeGrokTestObject(t, fixture.configPath, settings)
	staleRuntime := `{"org_id":"old","agent_id":"agent-1","kid":"old","base_url":"https://old.test","agent_name":"grok"}`
	for _, item := range []struct{ path, source string }{
		{fixture.guardPath, "stale guard\n"},
		{fixture.runtimeConfig, staleRuntime},
		{fixture.privateKey, "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
		{fixture.hookPath, "stale audit\n"},
	} {
		if err := os.WriteFile(item.path, []byte(item.source), 0600); err != nil {
			t.Fatalf("write stale runtime %s: %v", item.path, err)
		}
	}
	paths := []string{
		fixture.guardPath,
		fixture.runtimeConfig,
		fixture.privateKey,
		fixture.hookPath,
		fixture.configPath,
	}
	before := snapshotGrokFiles(t, paths...)
	fixture.plugin.rename = func(source, destination string) error {
		if sameGrokPath(destination, fixture.configPath) &&
			strings.HasSuffix(source, ".tmp") {
			return errors.New("injected Grok config failure")
		}
		return os.Rename(source, destination)
	}

	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "injected Grok config failure") {
		t.Fatalf("install error = %v", err)
	}
	requireGrokSnapshot(t, before)
	assertNoGrokTransactionArtifacts(t, fixture.homeDir)
}

func TestPreparedGrokInstallRejectsConcurrentConfigChange(t *testing.T) {
	source := `{"owner":"original"}`
	fixture := prepareGrokFixture(t, grokFixtureOptions{existingRaw: &source})
	document, err := readGrokDocument()
	if err != nil {
		t.Fatalf("read Grok document: %v", err)
	}
	paths, nodePath, err := preflightGrokInstallation(fixture.config, document)
	if err != nil {
		t.Fatalf("preflight Grok installation: %v", err)
	}
	guardCommand, err := buildGrokCommand(nodePath, paths.guardPath)
	if err != nil {
		t.Fatalf("build guard command: %v", err)
	}
	auditCommand, err := buildGrokCommand(nodePath, paths.auditPath)
	if err != nil {
		t.Fatalf("build audit command: %v", err)
	}
	hooks, err := removeManagedGrokHooks(document.hooks, "")
	if err != nil {
		t.Fatalf("remove managed Grok hooks: %v", err)
	}
	for _, item := range []struct{ event, command string }{
		{"PreToolUse", guardCommand},
		{"PostToolUse", auditCommand},
		{"PostToolUseFailure", auditCommand},
	} {
		hooks[item.event] = append(hooks[item.event], buildGrokGroup(item.command))
	}
	rendered, err := renderGrokDocument(document, hooks)
	if err != nil {
		t.Fatalf("render Grok document: %v", err)
	}
	changes, err := prepareGrokInstallationChanges(fixture.config, paths, rendered)
	if err != nil {
		t.Fatalf("prepare Grok installation: %v", err)
	}
	concurrent := []byte(`{"owner":"concurrent"}`)
	if err := os.WriteFile(fixture.configPath, concurrent, 0600); err != nil {
		t.Fatalf("write concurrent config: %v", err)
	}
	err = writeGrokChanges(
		changes,
		"Install Grok hooks",
		nil,
		paths.runtimeRoot,
		paths.agentDirectory,
		filepath.Dir(document.configPath),
	)
	if err == nil || !strings.Contains(err.Error(), "changed during installation") {
		t.Fatalf("prepared install error = %v", err)
	}
	actual, readErr := os.ReadFile(fixture.configPath)
	if readErr != nil || string(actual) != string(concurrent) {
		t.Fatalf("concurrent config changed: %q, %v", actual, readErr)
	}
	assertNoGrokRuntimeWrites(t, fixture)
	assertNoGrokTransactionArtifacts(t, fixture.homeDir)
}

func TestGrokInstallDetectsConcurrentConfigIdentityReplacement(t *testing.T) {
	source := []byte(`{"owner":"original"}`)
	fixture := prepareGrokFixture(
		t,
		grokFixtureOptions{existingRaw: grokString(string(source))},
	)
	mutated := false
	fixture.plugin.rename = func(stagedPath, destination string) error {
		if !mutated && strings.HasSuffix(stagedPath, ".tmp") {
			mutated = true
			external := fixture.configPath + ".external"
			if err := os.Rename(fixture.configPath, external); err != nil {
				return err
			}
			if err := os.WriteFile(fixture.configPath, source, 0600); err != nil {
				return err
			}
		}
		return os.Rename(stagedPath, destination)
	}

	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "changed during installation") {
		t.Fatalf("concurrent identity error = %v", err)
	}
	actual, readErr := os.ReadFile(fixture.configPath)
	if readErr != nil || string(actual) != string(source) {
		t.Fatalf("replacement config changed: %q, %v", actual, readErr)
	}
	assertNoGrokRuntimeWrites(t, fixture)
	assertNoGrokTransactionArtifacts(t, fixture.homeDir)
}

func TestGrokUninstallPreservesConfigAfterCommitFailure(t *testing.T) {
	source := `{"owner":"user"}`
	fixture := prepareGrokFixture(t, grokFixtureOptions{existingRaw: &source})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Grok hooks: %v", err)
	}
	before, err := os.ReadFile(fixture.configPath)
	if err != nil {
		t.Fatalf("read installed Grok config: %v", err)
	}
	fixture.plugin.rename = func(source, destination string) error {
		if sameGrokPath(destination, fixture.configPath) &&
			strings.HasSuffix(source, ".tmp") {
			return errors.New("injected Grok uninstall failure")
		}
		return os.Rename(source, destination)
	}
	err = fixture.plugin.Uninstall(grokTestAgentID)
	if err == nil || !strings.Contains(err.Error(), "injected Grok uninstall failure") {
		t.Fatalf("uninstall error = %v", err)
	}
	after, readErr := os.ReadFile(fixture.configPath)
	if readErr != nil || string(after) != string(before) {
		t.Fatalf("Grok config changed: %q, %v", after, readErr)
	}
	assertNoGrokTransactionArtifacts(t, fixture.homeDir)
}
