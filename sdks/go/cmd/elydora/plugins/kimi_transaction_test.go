package plugins

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func snapshotKimiFiles(t *testing.T, paths ...string) map[string][]byte {
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

func requireKimiSnapshot(t *testing.T, snapshot map[string][]byte) {
	t.Helper()
	for path, want := range snapshot {
		actual, err := os.ReadFile(path)
		if err != nil || string(actual) != string(want) {
			t.Fatalf("snapshot changed at %s: %q, %v", path, actual, err)
		}
	}
}

func TestKimiInstallRollsBackRuntimeAndBothConfigs(t *testing.T) {
	stable := []byte("# stable owner config\ndefault_model = \"kimi-code/k3\"\n")
	legacy := []byte("# legacy owner config\ntelemetry = false\n")
	fixture := prepareKimiFixture(t, kimiFixtureOptions{
		modernConfig: kimiString(string(stable)), legacyConfig: kimiString(string(legacy)),
	})
	fixture.plugin.rename = func(source, destination string) error {
		if sameKimiPath(destination, fixture.legacyPath) {
			return errors.New("injected legacy config failure")
		}
		return os.Rename(source, destination)
	}

	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "injected legacy config failure") {
		t.Fatalf("install error = %v", err)
	}
	for path, want := range map[string][]byte{
		fixture.modernPath: stable, fixture.legacyPath: legacy,
	} {
		actual, readErr := os.ReadFile(path)
		if readErr != nil || string(actual) != string(want) {
			t.Fatalf("config changed at %s: %q, %v", path, actual, readErr)
		}
	}
	assertNoKimiRuntimeWrites(t, fixture)
	assertNoKimiTransactionArtifacts(t, fixture.homeDir)
}

func TestKimiInstallPreservesConcurrentConfigMutation(t *testing.T) {
	original := "# original owner config\ndefault_model = \"kimi-code/k3\"\n"
	concurrent := []byte("# concurrent owner change\ntelemetry = false\n")
	fixture := prepareKimiFixture(t, kimiFixtureOptions{
		modernConfig: kimiString(original), withoutLegacyEvidence: true,
	})
	mutated := false
	fixture.plugin.rename = func(source, destination string) error {
		if !mutated && strings.HasSuffix(source, ".tmp") {
			mutated = true
			if err := os.WriteFile(fixture.modernPath, concurrent, 0600); err != nil {
				return err
			}
		}
		return os.Rename(source, destination)
	}

	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "changed during installation") {
		t.Fatalf("concurrent install error = %v", err)
	}
	actual, readErr := os.ReadFile(fixture.modernPath)
	if readErr != nil || string(actual) != string(concurrent) {
		t.Fatalf("concurrent config changed: %q, %v", actual, readErr)
	}
	assertNoKimiRuntimeWrites(t, fixture)
	assertNoKimiTransactionArtifacts(t, fixture.homeDir)
}

func TestKimiInstallDetectsConcurrentConfigIdentityReplacement(t *testing.T) {
	original := []byte("# original owner config\n")
	fixture := prepareKimiFixture(t, kimiFixtureOptions{
		modernConfig: kimiString(string(original)), withoutLegacyEvidence: true,
	})
	mutated := false
	fixture.plugin.rename = func(source, destination string) error {
		if !mutated && strings.HasSuffix(source, ".tmp") {
			mutated = true
			external := fixture.modernPath + ".external"
			if err := os.Rename(fixture.modernPath, external); err != nil {
				return err
			}
			if err := os.WriteFile(fixture.modernPath, original, 0600); err != nil {
				return err
			}
		}
		return os.Rename(source, destination)
	}

	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "changed during installation") {
		t.Fatalf("concurrent identity error = %v", err)
	}
	actual, readErr := os.ReadFile(fixture.modernPath)
	if readErr != nil || string(actual) != string(original) {
		t.Fatalf("replacement config changed: %q, %v", actual, readErr)
	}
	assertNoKimiRuntimeWrites(t, fixture)
	assertNoKimiTransactionArtifacts(t, fixture.homeDir)
}

func TestKimiUninstallRollsBackBothConfigRemovals(t *testing.T) {
	fixture := prepareKimiFixture(t, kimiFixtureOptions{})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Kimi hooks: %v", err)
	}
	paths := []string{
		fixture.guardPath, fixture.runtimeConfig, fixture.privateKey, fixture.hookPath,
		fixture.modernPath, fixture.legacyPath,
	}
	before := snapshotKimiFiles(t, paths...)
	removals := 0
	fixture.plugin.rename = func(source, destination string) error {
		if strings.HasSuffix(destination, ".rollback") {
			removals++
			if removals == 2 {
				return errors.New("injected uninstall failure")
			}
		}
		return os.Rename(source, destination)
	}

	err := fixture.plugin.Uninstall(kimiTestAgentID)
	if err == nil || !strings.Contains(err.Error(), "injected uninstall failure") {
		t.Fatalf("uninstall error = %v", err)
	}
	requireKimiSnapshot(t, before)
	assertNoKimiTransactionArtifacts(t, fixture.homeDir)
}

func TestPreparedKimiInstallDetectsSourceChangeBeforeCommit(t *testing.T) {
	fixture := prepareKimiFixture(t, kimiFixtureOptions{withoutLegacyEvidence: true})
	documents, err := readAllKimiConfigs()
	if err != nil {
		t.Fatalf("read Kimi configs: %v", err)
	}
	paths, nodePath, err := preflightKimiInstallation(fixture.config, documents)
	if err != nil {
		t.Fatalf("preflight Kimi installation: %v", err)
	}
	guardCommand, err := buildKimiCommand(nodePath, paths.guardPath)
	if err != nil {
		t.Fatalf("build guard command: %v", err)
	}
	auditCommand, err := buildKimiCommand(nodePath, paths.auditPath)
	if err != nil {
		t.Fatalf("build audit command: %v", err)
	}
	additions := make([]kimiHook, 0, 3)
	for _, item := range []struct{ event, command string }{
		{"PreToolUse", guardCommand},
		{"PostToolUse", auditCommand},
		{"PostToolUseFailure", auditCommand},
	} {
		hook, hookErr := buildKimiHook(item.event, item.command)
		if hookErr != nil {
			t.Fatalf("build Kimi hook: %v", hookErr)
		}
		additions = append(additions, hook)
	}
	rendered := make([]kimiRenderedDocument, 0, len(documents))
	for _, document := range documents {
		keep, keepErr := keptKimiHookIndices(document.hooks, "")
		if keepErr != nil {
			t.Fatalf("select Kimi hooks: %v", keepErr)
		}
		change, renderErr := renderKimiChange(document, keep, additions)
		if renderErr != nil {
			t.Fatalf("render Kimi hooks: %v", renderErr)
		}
		rendered = append(rendered, change)
	}
	changes, err := prepareKimiInstallationChanges(fixture.config, paths, rendered)
	if err != nil {
		t.Fatalf("prepare Kimi installation: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(fixture.modernPath), 0700); err != nil {
		t.Fatalf("create Kimi home: %v", err)
	}
	concurrent := []byte("# changed after preparation\n")
	if err := os.WriteFile(fixture.modernPath, concurrent, 0600); err != nil {
		t.Fatalf("write concurrent source: %v", err)
	}

	err = writeKimiChanges(
		changes,
		"Install Kimi hooks",
		nil,
		paths.runtimeRoot,
		paths.agentDirectory,
	)
	if err == nil || !strings.Contains(err.Error(), "changed during installation") {
		t.Fatalf("prepared install error = %v", err)
	}
	actual, readErr := os.ReadFile(fixture.modernPath)
	if readErr != nil || string(actual) != string(concurrent) {
		t.Fatalf("prepared source changed: %q, %v", actual, readErr)
	}
	assertNoKimiRuntimeWrites(t, fixture)
	assertNoKimiTransactionArtifacts(t, fixture.homeDir)
}
