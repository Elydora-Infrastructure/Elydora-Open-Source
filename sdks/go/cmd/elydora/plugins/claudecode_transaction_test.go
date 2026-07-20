package plugins

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func snapshotClaudeFiles(t *testing.T, paths ...string) map[string][]byte {
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

func requireClaudeSnapshot(t *testing.T, snapshot map[string][]byte) {
	t.Helper()
	for path, want := range snapshot {
		actual, err := os.ReadFile(path)
		if err != nil || string(actual) != string(want) {
			t.Fatalf("snapshot changed at %s: %q, %v", path, actual, err)
		}
	}
}

func TestClaudeInstallRollsBackAllRuntimesAfterSettingsFailure(t *testing.T) {
	fixture := prepareClaudeFixture(t, claudeFixtureOptions{})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Claude hooks: %v", err)
	}
	settings := readClaudeTestObject(t, fixture.configPath)
	settings["owner"] = "user"
	delete(requireObject(t, settings["hooks"]), "PostToolUseFailure")
	writeClaudeTestObject(t, fixture.configPath, settings)
	staleConfig := `{"org_id":"old","agent_id":"agent-1","kid":"old","base_url":"https://old.test","agent_name":"claudecode"}`
	for _, item := range []struct{ path, source string }{
		{fixture.guardPath, "stale guard\n"},
		{fixture.runtimeConfig, staleConfig},
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
	before := snapshotClaudeFiles(t, paths...)
	fixture.plugin.rename = func(source, destination string) error {
		if sameClaudePath(destination, fixture.configPath) &&
			strings.HasSuffix(source, ".tmp") {
			return errors.New("injected Claude settings failure")
		}
		return os.Rename(source, destination)
	}
	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "injected Claude settings failure") {
		t.Fatalf("install error = %v", err)
	}
	requireClaudeSnapshot(t, before)
	assertNoClaudeTransactionArtifacts(t, fixture.homeDir)
}

func TestPreparedClaudeInstallRejectsConcurrentSettingsChange(t *testing.T) {
	source := `{"owner":"original"}`
	fixture := prepareClaudeFixture(
		t,
		claudeFixtureOptions{existingRaw: &source},
	)
	document, err := readClaudeDocument()
	if err != nil {
		t.Fatalf("read Claude document: %v", err)
	}
	paths, nodePath, err := preflightClaudeInstallation(fixture.config, document)
	if err != nil {
		t.Fatalf("preflight Claude installation: %v", err)
	}
	hooks, err := removeManagedClaudeHooks(document.hooks, "")
	if err != nil {
		t.Fatalf("remove managed Claude hooks: %v", err)
	}
	for _, item := range []struct{ event, script, status string }{
		{"PreToolUse", paths.guardPath, claudeGuardStatusMessage},
		{"PostToolUse", paths.auditPath, claudeAuditStatusMessage},
		{"PostToolUseFailure", paths.auditPath, claudeAuditStatusMessage},
	} {
		hooks[item.event] = append(
			hooks[item.event],
			buildClaudeGroup(nodePath, item.script, item.status),
		)
	}
	rendered, err := renderClaudeDocument(document, hooks)
	if err != nil {
		t.Fatalf("render Claude document: %v", err)
	}
	changes, err := prepareClaudeInstallationChanges(fixture.config, paths, rendered)
	if err != nil {
		t.Fatalf("prepare Claude installation: %v", err)
	}
	concurrent := []byte(`{"owner":"concurrent"}`)
	if err := os.WriteFile(fixture.configPath, concurrent, 0600); err != nil {
		t.Fatalf("write concurrent settings: %v", err)
	}
	err = writeClaudeChanges(
		changes,
		"Install Claude Code hooks",
		nil,
		paths.runtimeRoot,
		paths.agentDirectory,
		filepath.Dir(document.filePath),
	)
	if err == nil || !strings.Contains(err.Error(), "changed during installation") {
		t.Fatalf("prepared install error = %v", err)
	}
	actual, readErr := os.ReadFile(fixture.configPath)
	if readErr != nil || string(actual) != string(concurrent) {
		t.Fatalf("concurrent settings changed: %q, %v", actual, readErr)
	}
	assertNoClaudeRuntimeWrites(t, fixture)
	assertNoClaudeTransactionArtifacts(t, fixture.homeDir)
}

func TestClaudeInstallDetectsConcurrentSettingsIdentityReplacement(t *testing.T) {
	source := []byte(`{"owner":"original"}`)
	fixture := prepareClaudeFixture(
		t,
		claudeFixtureOptions{existingRaw: claudeString(string(source))},
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
		t.Fatalf("replacement settings changed: %q, %v", actual, readErr)
	}
	assertNoClaudeRuntimeWrites(t, fixture)
	assertNoClaudeTransactionArtifacts(t, fixture.homeDir)
}

func TestClaudeUninstallPreservesSettingsAfterCommitFailure(t *testing.T) {
	source := `{"owner":"user"}`
	fixture := prepareClaudeFixture(t, claudeFixtureOptions{existingRaw: &source})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Claude hooks: %v", err)
	}
	before, err := os.ReadFile(fixture.configPath)
	if err != nil {
		t.Fatalf("read installed settings: %v", err)
	}
	fixture.plugin.rename = func(source, destination string) error {
		if sameClaudePath(destination, fixture.configPath) &&
			strings.HasSuffix(source, ".tmp") {
			return errors.New("injected Claude uninstall failure")
		}
		return os.Rename(source, destination)
	}
	err = fixture.plugin.Uninstall(claudeTestAgentID)
	if err == nil || !strings.Contains(err.Error(), "injected Claude uninstall failure") {
		t.Fatalf("uninstall error = %v", err)
	}
	after, readErr := os.ReadFile(fixture.configPath)
	if readErr != nil || string(after) != string(before) {
		t.Fatalf("Claude settings changed: %q, %v", after, readErr)
	}
	assertNoClaudeTransactionArtifacts(t, fixture.homeDir)
}
