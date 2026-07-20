package plugins

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func snapshotGeminiFiles(t *testing.T, paths ...string) map[string][]byte {
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

func requireGeminiSnapshot(t *testing.T, snapshot map[string][]byte) {
	t.Helper()
	for path, want := range snapshot {
		actual, err := os.ReadFile(path)
		if err != nil || string(actual) != string(want) {
			t.Fatalf("snapshot changed at %s: %q, %v", path, actual, err)
		}
	}
}

func prepareGeminiTestChanges(
	t *testing.T,
	fixture *geminiFixture,
	document *geminiDocument,
) ([]*fileChange, *geminiRuntimePaths) {
	t.Helper()
	paths, nodePath, err := preflightGeminiInstallation(fixture.config, document)
	if err != nil {
		t.Fatalf("preflight Gemini installation: %v", err)
	}
	guard, err := buildGeminiGroup(
		nodePath,
		paths.guardPath,
		geminiGuardHookName,
	)
	if err != nil {
		t.Fatalf("build Gemini guard group: %v", err)
	}
	audit, err := buildGeminiGroup(
		nodePath,
		paths.auditPath,
		geminiAuditHookName,
	)
	if err != nil {
		t.Fatalf("build Gemini audit group: %v", err)
	}
	rendered, err := renderGeminiDocument(
		document,
		"",
		map[string]map[string]any{
			"BeforeTool": guard,
			"AfterTool":  audit,
		},
	)
	if err != nil {
		t.Fatalf("render Gemini document: %v", err)
	}
	changes, err := prepareGeminiInstallationChanges(
		fixture.config,
		paths,
		rendered,
	)
	if err != nil {
		t.Fatalf("prepare Gemini installation: %v", err)
	}
	return changes, paths
}

func TestGeminiInstallRollsBackAllFiveFilesAfterSettingsFailure(t *testing.T) {
	fixture := prepareGeminiFixture(t, geminiFixtureOptions{})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Gemini hooks: %v", err)
	}
	settings := readGeminiTestObject(t, fixture.settingsPath)
	settings["owner"] = "user"
	delete(requireObject(t, settings["hooks"]), "AfterTool")
	writeGeminiTestObject(t, fixture.settingsPath, settings)
	staleRuntime := `{"org_id":"old","agent_id":"agent-1","kid":"old","base_url":"https://old.test","agent_name":"gemini"}`
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
		fixture.settingsPath,
	}
	before := snapshotGeminiFiles(t, paths...)
	fixture.config.OrgID = "org-updated"
	fixture.config.Token = "token-updated"
	fixture.plugin.rename = func(source, destination string) error {
		if sameGeminiPath(destination, fixture.settingsPath) &&
			strings.HasSuffix(source, ".tmp") {
			return errors.New("injected Gemini settings failure")
		}
		return os.Rename(source, destination)
	}
	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "injected Gemini settings failure") {
		t.Fatalf("install error = %v", err)
	}
	requireGeminiSnapshot(t, before)
	assertNoGeminiTransactionArtifacts(t, fixture.homeDir)
}

func TestPreparedGeminiInstallRejectsConcurrentSettingsChange(t *testing.T) {
	source := `{"owner":"original"}`
	fixture := prepareGeminiFixture(
		t,
		geminiFixtureOptions{existingRaw: &source},
	)
	document, err := readGeminiDocument()
	if err != nil {
		t.Fatalf("read Gemini document: %v", err)
	}
	changes, paths := prepareGeminiTestChanges(t, fixture, document)
	concurrent := []byte(`{"owner":"concurrent"}`)
	if err := os.WriteFile(fixture.settingsPath, concurrent, 0600); err != nil {
		t.Fatalf("write concurrent settings: %v", err)
	}
	err = writeGeminiChanges(
		changes,
		"Install Gemini CLI hooks",
		nil,
		paths.runtimeRoot,
		paths.agentDirectory,
		filepath.Dir(document.filePath),
	)
	if err == nil || !strings.Contains(err.Error(), "changed during installation") {
		t.Fatalf("prepared install error = %v", err)
	}
	actual, readErr := os.ReadFile(fixture.settingsPath)
	if readErr != nil || string(actual) != string(concurrent) {
		t.Fatalf("concurrent settings changed: %q, %v", actual, readErr)
	}
	assertNoGeminiRuntimeWrites(t, fixture)
	assertNoGeminiTransactionArtifacts(t, fixture.homeDir)
}

func TestGeminiInstallDetectsConcurrentSettingsIdentityReplacement(t *testing.T) {
	source := []byte(`{"owner":"original"}`)
	fixture := prepareGeminiFixture(
		t,
		geminiFixtureOptions{existingRaw: geminiString(string(source))},
	)
	mutated := false
	fixture.plugin.rename = func(stagedPath, destination string) error {
		if !mutated && strings.HasSuffix(stagedPath, ".tmp") {
			mutated = true
			external := fixture.settingsPath + ".external"
			if err := os.Rename(fixture.settingsPath, external); err != nil {
				return err
			}
			if err := os.WriteFile(fixture.settingsPath, source, 0600); err != nil {
				return err
			}
		}
		return os.Rename(stagedPath, destination)
	}
	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "changed during installation") {
		t.Fatalf("concurrent identity error = %v", err)
	}
	actual, readErr := os.ReadFile(fixture.settingsPath)
	if readErr != nil || string(actual) != string(source) {
		t.Fatalf("replacement settings changed: %q, %v", actual, readErr)
	}
	assertNoGeminiRuntimeWrites(t, fixture)
	assertNoGeminiTransactionArtifacts(t, fixture.homeDir)
}

func TestGeminiUninstallPreservesSettingsAfterCommitFailure(t *testing.T) {
	source := `{"owner":"user"}`
	fixture := prepareGeminiFixture(
		t,
		geminiFixtureOptions{existingRaw: &source},
	)
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Gemini hooks: %v", err)
	}
	before, err := os.ReadFile(fixture.settingsPath)
	if err != nil {
		t.Fatalf("read installed Gemini settings: %v", err)
	}
	fixture.plugin.rename = func(source, destination string) error {
		if sameGeminiPath(destination, fixture.settingsPath) &&
			strings.HasSuffix(source, ".tmp") {
			return errors.New("injected Gemini uninstall failure")
		}
		return os.Rename(source, destination)
	}
	err = fixture.plugin.Uninstall(geminiTestAgentID)
	if err == nil || !strings.Contains(err.Error(), "injected Gemini uninstall failure") {
		t.Fatalf("uninstall error = %v", err)
	}
	after, readErr := os.ReadFile(fixture.settingsPath)
	if readErr != nil || string(after) != string(before) {
		t.Fatalf("Gemini settings changed: %q, %v", after, readErr)
	}
	assertNoGeminiTransactionArtifacts(t, fixture.homeDir)
}
