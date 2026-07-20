package plugins

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func prepareDroidTestInstallation(
	t *testing.T,
	fixture *droidFixture,
	config InstallConfig,
) *preparedDroidInstallation {
	t.Helper()
	sources, err := readDroidSources()
	if err != nil {
		t.Fatalf("read Factory Droid sources: %v", err)
	}
	paths, nodePath, err := preflightDroidInstallation(config, sources)
	if err != nil {
		t.Fatalf("preflight Factory Droid installation: %v", err)
	}
	rendered, err := renderDroidInstallation(
		sources,
		paths.guardPath,
		paths.auditPath,
		nodePath,
		paths.runtimeRoot,
	)
	if err != nil {
		t.Fatalf("render Factory Droid installation: %v", err)
	}
	prepared, err := prepareDroidInstallation(config, sources, rendered)
	if err != nil {
		t.Fatalf("prepare Factory Droid installation: %v", err)
	}
	return prepared
}

func droidManagedPaths(fixture *droidFixture) []string {
	return []string{
		fixture.guardPath,
		fixture.runtimeConfig,
		fixture.privateKey,
		fixture.hookPath,
		fixture.configPath,
	}
}

func TestDroidInstallRollsBackRuntimeAfterLateHookCommitFailure(t *testing.T) {
	fixture := prepareDroidFixture(t, droidFixtureOptions{})
	prepared := prepareDroidTestInstallation(t, fixture, fixture.config)
	failed := false
	rename := func(source, destination string) error {
		if !failed && sameDroidPath(destination, fixture.configPath) {
			failed = true
			return errors.New("injected Droid hook commit failure")
		}
		return os.Rename(source, destination)
	}
	err := commitDroidInstallation(prepared, rename)
	if err == nil || !strings.Contains(err.Error(), "injected Droid hook commit failure") || !failed {
		t.Fatalf("transaction error = %v, failed = %v", err, failed)
	}
	for _, path := range droidManagedPaths(fixture) {
		requireMissingDroidFile(t, path)
	}
	requireNoDroidStagingFiles(t, fixture.homeDir)
}

func TestDroidConcurrentActiveHookReplacementPreventsRuntimeCommit(t *testing.T) {
	fixture := prepareDroidFixture(t, droidFixtureOptions{})
	installDroidFixture(t, fixture)
	before := snapshotDroidFiles(t, droidManagedPaths(fixture)...)
	updated := fixture.config
	updated.OrgID = "org-updated"
	prepared := prepareDroidTestInstallation(t, fixture, updated)
	concurrent := "{\"hooks\":{\"PreToolUse\":[]},\"owner\":\"concurrent\"}\n"
	if err := os.WriteFile(fixture.configPath, []byte(concurrent), 0600); err != nil {
		t.Fatalf("replace active hook source: %v", err)
	}
	err := commitDroidInstallation(prepared, nil)
	if err == nil || !strings.Contains(err.Error(), "changed during Install Factory Droid") {
		t.Fatalf("concurrent source error = %v", err)
	}
	for path, source := range before {
		if sameDroidPath(path, fixture.configPath) {
			continue
		}
		if readDroidTestFile(t, path) != source {
			t.Fatalf("runtime changed after concurrent source replacement: %s", path)
		}
	}
	if readDroidTestFile(t, fixture.configPath) != concurrent {
		t.Fatal("concurrent source was overwritten")
	}
	requireNoDroidStagingFiles(t, fixture.homeDir)
}

func TestDroidConcurrentInactiveLocalSourceCreationIsDetected(t *testing.T) {
	fixture := prepareDroidFixture(t, droidFixtureOptions{})
	installDroidFixture(t, fixture)
	before := snapshotDroidFiles(t, droidManagedPaths(fixture)...)
	updated := fixture.config
	updated.OrgID = "org-updated"
	prepared := prepareDroidTestInstallation(t, fixture, updated)
	concurrent := "{\"hooksDisabled\":true}\n"
	writeOptionalDroidFile(t, fixture.localSettingsPath, droidString(concurrent))
	err := commitDroidInstallation(prepared, nil)
	if err == nil || !strings.Contains(err.Error(), "local settings changed during Install") {
		t.Fatalf("concurrent local source error = %v", err)
	}
	requireDroidSnapshot(t, before)
	if readDroidTestFile(t, fixture.localSettingsPath) != concurrent {
		t.Fatal("concurrent local settings source was overwritten")
	}
	requireNoDroidStagingFiles(t, fixture.homeDir)
}

func TestDroidConcurrentProjectPolicyChangeIsDetected(t *testing.T) {
	fixture := prepareDroidFixture(t, droidFixtureOptions{
		projectSettings: droidJSON(map[string]any{"hooksDisabled": false}),
	})
	installDroidFixture(t, fixture)
	before := snapshotDroidFiles(t, droidManagedPaths(fixture)...)
	updated := fixture.config
	updated.OrgID = "org-updated"
	prepared := prepareDroidTestInstallation(t, fixture, updated)
	concurrent := "{\"hooksDisabled\":true}\n"
	if err := os.WriteFile(fixture.projectSettingsPath, []byte(concurrent), 0600); err != nil {
		t.Fatalf("change project policy: %v", err)
	}
	err := commitDroidInstallation(prepared, nil)
	if err == nil || !strings.Contains(err.Error(), "project settings changed during Install") {
		t.Fatalf("concurrent project policy error = %v", err)
	}
	requireDroidSnapshot(t, before)
	if readDroidTestFile(t, fixture.projectSettingsPath) != concurrent {
		t.Fatal("concurrent project policy was overwritten")
	}
	requireNoDroidStagingFiles(t, fixture.homeDir)
}

func TestDroidConcurrentPolicyChangePreventsRuntimeDirectoryCreation(t *testing.T) {
	fixture := prepareDroidFixture(t, droidFixtureOptions{
		projectSettings: droidJSON(map[string]any{"hooksDisabled": false}),
	})
	prepared := prepareDroidTestInstallation(t, fixture, fixture.config)
	if err := os.WriteFile(
		fixture.projectSettingsPath,
		[]byte("{\"hooksDisabled\":true}\n"),
		0600,
	); err != nil {
		t.Fatalf("change project policy: %v", err)
	}
	err := commitDroidInstallation(prepared, nil)
	if err == nil || !strings.Contains(err.Error(), "project settings changed during Install") {
		t.Fatalf("concurrent project policy error = %v", err)
	}
	requireMissingDroidFile(t, fixture.agentDir)
	requireMissingDroidFile(t, fixture.configPath)
}

func TestDroidSameContentReplacementIsRejectedByPhysicalIdentity(t *testing.T) {
	fixture := prepareDroidFixture(t, droidFixtureOptions{})
	installDroidFixture(t, fixture)
	before := snapshotDroidFiles(t, droidManagedPaths(fixture)...)
	sources, err := readDroidSources()
	if err != nil {
		t.Fatalf("read Factory Droid sources: %v", err)
	}
	original := readDroidTestFile(t, fixture.configPath)
	if err := os.Remove(fixture.configPath); err != nil {
		t.Fatalf("remove hook source: %v", err)
	}
	if err := os.WriteFile(fixture.configPath, []byte(original), 0600); err != nil {
		t.Fatalf("replace hook source: %v", err)
	}
	updated := fixture.config
	updated.OrgID = "org-updated"
	paths, nodePath, err := preflightDroidInstallation(updated, sources)
	if err != nil {
		t.Fatalf("preflight stale sources: %v", err)
	}
	rendered, err := renderDroidInstallation(
		sources,
		paths.guardPath,
		paths.auditPath,
		nodePath,
		paths.runtimeRoot,
	)
	if err != nil {
		t.Fatalf("render stale sources: %v", err)
	}
	prepared, err := prepareDroidInstallation(updated, sources, rendered)
	if err != nil {
		t.Fatalf("prepare stale sources: %v", err)
	}
	err = commitDroidInstallation(prepared, nil)
	if err == nil || !strings.Contains(err.Error(), "changed during Install") {
		t.Fatalf("same-content replacement error = %v", err)
	}
	if readDroidTestFile(t, fixture.configPath) != original {
		t.Fatal("same-content replacement changed")
	}
	for path, source := range before {
		if !sameDroidPath(path, fixture.configPath) && readDroidTestFile(t, path) != source {
			t.Fatalf("runtime changed after inode replacement: %s", path)
		}
	}
	requireNoDroidStagingFiles(t, fixture.homeDir)
}

func TestDroidUninstallRollsBackAllHookDocuments(t *testing.T) {
	fixture := prepareDroidFixture(t, droidFixtureOptions{})
	installDroidFixture(t, fixture)
	installed := droidCurrentHooks(t, fixture.configPath)
	writeDroidTestObject(t, fixture.settingsPath, map[string]any{
		"hooks": installed,
		"owner": "user",
	})
	paths := append(droidManagedPaths(fixture), fixture.settingsPath)
	before := snapshotDroidFiles(t, paths...)
	sources, err := readDroidSources()
	if err != nil {
		t.Fatalf("read uninstall sources: %v", err)
	}
	runtimeRoot, err := droidRuntimeRoot()
	if err != nil {
		t.Fatalf("resolve runtime root: %v", err)
	}
	rendered, err := renderDroidUninstall(sources, droidTestAgentID, runtimeRoot)
	if err != nil {
		t.Fatalf("render uninstall: %v", err)
	}
	prepared, err := prepareDroidUninstall(rendered)
	if err != nil {
		t.Fatalf("prepare uninstall: %v", err)
	}
	failed := false
	rename := func(source, destination string) error {
		if !failed && sameDroidPath(destination, fixture.settingsPath) {
			failed = true
			return errors.New("injected Droid uninstall failure")
		}
		return os.Rename(source, destination)
	}
	err = commitDroidUninstall(prepared, rename)
	if err == nil || !strings.Contains(err.Error(), "injected Droid uninstall failure") || !failed {
		t.Fatalf("uninstall rollback error = %v, failed = %v", err, failed)
	}
	requireDroidSnapshot(t, before)
	requireNoDroidStagingFiles(t, fixture.homeDir)
}

func TestDroidPreflightBlocksDisabledHooksBeforeRuntimeCreation(t *testing.T) {
	fixture := prepareDroidFixture(t, droidFixtureOptions{
		settings: droidJSON(map[string]any{"hooksDisabled": true}),
	})
	preflighter, ok := any(fixture.plugin).(InstallPreflighter)
	if !ok {
		t.Fatal("Factory Droid plugin does not implement InstallPreflighter")
	}
	err := preflighter.PreflightInstall(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "hooksDisabled") {
		t.Fatalf("preflight error = %v", err)
	}
	requireMissingDroidFile(t, fixture.agentDir)
}
