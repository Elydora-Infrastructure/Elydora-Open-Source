package plugins

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestKiroIdeRollbackRestoresRuntimeWorkspaceAndLegacySources(t *testing.T) {
	original := `{"version":"v1","owner":"workspace","hooks":[]}`
	fixture := prepareKiroIdeFixture(t, &original, nil)
	writeKiroIdeJSON(
		t,
		fixture.legacyPath,
		legacyKiroIdeGoDocument(fixture, kiroIdeTestAgentID),
	)
	fixture.plugin.rename = func(source, destination string) error {
		if sameKiroIdePath(source, fixture.legacyPath) &&
			strings.HasSuffix(destination, ".rollback") {
			return errors.New("injected Kiro IDE legacy cleanup failure")
		}
		return os.Rename(source, destination)
	}
	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "injected Kiro IDE legacy cleanup failure") {
		t.Fatalf("install error = %v", err)
	}
	actual, readErr := os.ReadFile(fixture.configPath)
	if readErr != nil || string(actual) != original {
		t.Fatalf("workspace source after rollback = %q, %v", actual, readErr)
	}
	if _, err := os.Lstat(fixture.legacyPath); err != nil {
		t.Fatalf("legacy source was not restored: %v", err)
	}
	assertNoKiroIdeRuntimeFiles(t, fixture)
	assertNoKiroIdeTransactionArtifacts(t, fixture.homeDir)
	assertNoKiroIdeTransactionArtifacts(t, fixture.workspace)
}

func TestKiroIdePreparedInstallProtectsUnchangedSources(t *testing.T) {
	fixture := prepareKiroIdeFixture(t, nil, nil)
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Kiro IDE hooks: %v", err)
	}
	if err := os.WriteFile(fixture.guardPath, []byte("repair me\n"), 0700); err != nil {
		t.Fatalf("damage guard runtime: %v", err)
	}
	prepared := prepareKiroIdeInstallForTest(t, fixture)
	concurrent := `{"version":"v1","owner":"concurrent","hooks":[]}`
	if err := os.WriteFile(fixture.configPath, []byte(concurrent), 0600); err != nil {
		t.Fatalf("write concurrent workspace source: %v", err)
	}
	err := commitKiroIdeInstallation(prepared, nil)
	if err == nil || !strings.Contains(err.Error(), "changed during Kiro IDE installation") {
		t.Fatalf("prepared install error = %v", err)
	}
	actual, readErr := os.ReadFile(fixture.configPath)
	if readErr != nil || string(actual) != concurrent {
		t.Fatalf("concurrent workspace source changed: %q, %v", actual, readErr)
	}
	guard, readErr := os.ReadFile(fixture.guardPath)
	if readErr != nil || string(guard) != "repair me\n" {
		t.Fatalf("runtime changed after source race: %q, %v", guard, readErr)
	}
	assertNoKiroIdeTransactionArtifacts(t, fixture.homeDir)
	assertNoKiroIdeTransactionArtifacts(t, fixture.workspace)
}

func TestKiroIdeInstallRepairsRuntimeModesOnPOSIX(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes are unavailable on Windows")
	}
	fixture := prepareKiroIdeFixture(t, nil, nil)
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Kiro IDE hooks: %v", err)
	}
	files := []struct {
		path  string
		drift os.FileMode
		want  os.FileMode
	}{
		{fixture.runtimeConfig, 0644, 0600},
		{fixture.privateKey, 0644, 0600},
		{fixture.guardPath, 0600, 0700},
		{fixture.auditPath, 0600, 0700},
	}
	for _, file := range files {
		if err := os.Chmod(file.path, file.drift); err != nil {
			t.Fatalf("drift runtime mode at %s: %v", file.path, err)
		}
	}
	status, err := fixture.plugin.Status()
	if err != nil || status.Installed || !status.HookConfigured {
		t.Fatalf("mode-drifted Kiro IDE status = %#v, %v", status, err)
	}
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("repair Kiro IDE runtime modes: %v", err)
	}
	for _, file := range files {
		info, err := os.Stat(file.path)
		if err != nil || info.Mode().Perm() != file.want {
			t.Fatalf("runtime mode at %s = %v, %v", file.path, infoMode(info), err)
		}
	}
	status, err = fixture.plugin.Status()
	if err != nil || !status.Installed || !status.HookConfigured {
		t.Fatalf("repaired Kiro IDE status = %#v, %v", status, err)
	}
}

func TestKiroIdePreparedInstallDetectsConcurrentModeChangesOnPOSIX(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes are unavailable on Windows")
	}
	t.Run("changed private key", func(t *testing.T) {
		fixture := prepareKiroIdeFixture(t, nil, nil)
		if err := fixture.plugin.Install(fixture.config); err != nil {
			t.Fatalf("install Kiro IDE hooks: %v", err)
		}
		fixture.config.PrivateKey = base64.RawURLEncoding.EncodeToString(
			bytes.Repeat([]byte{12}, 32),
		)
		prepared := prepareKiroIdeInstallForTest(t, fixture)
		if err := os.Chmod(fixture.privateKey, 0644); err != nil {
			t.Fatalf("concurrently chmod private key: %v", err)
		}
		err := commitKiroIdeInstallation(prepared, nil)
		if err == nil || !strings.Contains(err.Error(), "private key changed during installation") {
			t.Fatalf("concurrent private key mode error = %v", err)
		}
		key, readErr := os.ReadFile(fixture.privateKey)
		if readErr != nil || string(key) != kiroIdePrivateKey {
			t.Fatalf("private key changed after mode race: %q, %v", key, readErr)
		}
		info, statErr := os.Stat(fixture.privateKey)
		if statErr != nil || info.Mode().Perm() != 0644 {
			t.Fatalf("private key mode after race = %v, %v", infoMode(info), statErr)
		}
		assertNoKiroIdeTransactionArtifacts(t, fixture.homeDir)
	})
	t.Run("unchanged runtime config", func(t *testing.T) {
		fixture := prepareKiroIdeFixture(t, nil, nil)
		if err := fixture.plugin.Install(fixture.config); err != nil {
			t.Fatalf("install Kiro IDE hooks: %v", err)
		}
		if err := os.WriteFile(fixture.guardPath, []byte("repair me\n"), 0700); err != nil {
			t.Fatalf("damage guard runtime: %v", err)
		}
		prepared := prepareKiroIdeInstallForTest(t, fixture)
		if err := os.Chmod(fixture.runtimeConfig, 0644); err != nil {
			t.Fatalf("concurrently chmod runtime config: %v", err)
		}
		err := commitKiroIdeInstallation(prepared, nil)
		if err == nil || !strings.Contains(err.Error(), "runtime config changed during Kiro IDE installation") {
			t.Fatalf("concurrent runtime config mode error = %v", err)
		}
		guard, readErr := os.ReadFile(fixture.guardPath)
		if readErr != nil || string(guard) != "repair me\n" {
			t.Fatalf("guard changed after mode race: %q, %v", guard, readErr)
		}
		assertNoKiroIdeTransactionArtifacts(t, fixture.homeDir)
	})
}

func infoMode(info os.FileInfo) os.FileMode {
	if info == nil {
		return 0
	}
	return info.Mode().Perm()
}

func TestKiroIdePreparedInstallRejectsFreshWorkspaceDirectoryReplacement(t *testing.T) {
	fixture := prepareKiroIdeFixture(t, nil, nil)
	prepared := prepareKiroIdeInstallForTest(t, fixture)
	external := filepath.Join(filepath.Dir(fixture.homeDir), "external-workspace")
	if err := os.MkdirAll(external, 0700); err != nil {
		t.Fatalf("create external workspace directory: %v", err)
	}
	if err := os.Symlink(external, filepath.Join(fixture.workspace, ".kiro")); err != nil {
		t.Skipf("directory symbolic links unavailable: %v", err)
	}
	err := commitKiroIdeInstallation(prepared, nil)
	if err == nil || !strings.Contains(err.Error(), "changed during Kiro IDE installation") {
		t.Fatalf("workspace replacement error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(external, "hooks", kiroIdeConfigFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("external workspace received hooks: %v", err)
	}
	assertNoKiroIdeRuntimeFiles(t, fixture)
}

func TestKiroIdePreparedInstallRejectsFreshRuntimeDirectoryReplacements(t *testing.T) {
	for _, target := range []string{"root", "agent"} {
		t.Run(target, func(t *testing.T) {
			fixture := prepareKiroIdeFixture(t, nil, nil)
			runtimeRoot := filepath.Join(fixture.homeDir, ".elydora")
			if target == "agent" {
				if err := os.Mkdir(runtimeRoot, 0700); err != nil {
					t.Fatalf("create physical runtime root: %v", err)
				}
			}
			prepared := prepareKiroIdeInstallForTest(t, fixture)
			external := filepath.Join(filepath.Dir(fixture.homeDir), "external-runtime-"+target)
			if err := os.MkdirAll(external, 0700); err != nil {
				t.Fatalf("create external runtime directory: %v", err)
			}
			linkPath := runtimeRoot
			if target == "agent" {
				linkPath = fixture.agentDir
			}
			if err := os.Symlink(external, linkPath); err != nil {
				t.Skipf("directory symbolic links unavailable: %v", err)
			}
			err := commitKiroIdeInstallation(prepared, nil)
			if err == nil || !strings.Contains(err.Error(), "changed during Kiro IDE installation") {
				t.Fatalf("runtime %s replacement error = %v", target, err)
			}
			if _, err := os.Lstat(filepath.Join(external, "private.key")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("external runtime received private key: %v", err)
			}
			if _, err := os.Lstat(filepath.Join(external, kiroIdeGuardScript)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("external runtime received guard: %v", err)
			}
		})
	}
}

func TestKiroIdePreparedInstallRejectsExistingWorkspaceIdentityReplacement(t *testing.T) {
	source := `{"version":"v1","owner":"workspace","hooks":[]}`
	fixture := prepareKiroIdeFixture(t, &source, nil)
	prepared := prepareKiroIdeInstallForTest(t, fixture)
	kiroDirectory := filepath.Join(fixture.workspace, ".kiro")
	backup := kiroDirectory + ".original"
	external := filepath.Join(filepath.Dir(fixture.homeDir), "external-existing-workspace")
	if err := os.MkdirAll(filepath.Join(external, "hooks"), 0700); err != nil {
		t.Fatalf("create external hooks directory: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(external, "hooks", kiroIdeConfigFile),
		[]byte(source),
		0600,
	); err != nil {
		t.Fatalf("write external matching source: %v", err)
	}
	if err := os.Rename(kiroDirectory, backup); err != nil {
		t.Fatalf("move physical Kiro directory: %v", err)
	}
	if err := os.Symlink(external, kiroDirectory); err != nil {
		_ = os.Rename(backup, kiroDirectory)
		t.Skipf("directory symbolic links unavailable: %v", err)
	}
	err := commitKiroIdeInstallation(prepared, nil)
	if err == nil || !strings.Contains(err.Error(), "changed during Kiro IDE installation") {
		t.Fatalf("existing workspace replacement error = %v", err)
	}
	actual, readErr := os.ReadFile(filepath.Join(external, "hooks", kiroIdeConfigFile))
	if readErr != nil || string(actual) != source {
		t.Fatalf("external matching source changed: %q, %v", actual, readErr)
	}
	assertNoKiroIdeRuntimeFiles(t, fixture)
}

func TestKiroIdeRejectsFileAndDirectoryLinksBeforeRuntimeWrites(t *testing.T) {
	for _, kind := range []string{"workspace-config", "workspace-directory", "runtime-directory"} {
		t.Run(kind, func(t *testing.T) {
			fixture := prepareKiroIdeFixture(t, nil, nil)
			external := filepath.Join(filepath.Dir(fixture.homeDir), "external-"+kind)
			if kind == "workspace-config" {
				if err := os.MkdirAll(filepath.Dir(fixture.configPath), 0700); err != nil {
					t.Fatalf("create hooks directory: %v", err)
				}
				if err := os.WriteFile(external, []byte(`{"version":"v1","hooks":[]}`), 0600); err != nil {
					t.Fatalf("write external config: %v", err)
				}
				if err := os.Symlink(external, fixture.configPath); err != nil {
					t.Skipf("file symbolic links unavailable: %v", err)
				}
			} else {
				if err := os.MkdirAll(external, 0700); err != nil {
					t.Fatalf("create external directory: %v", err)
				}
				linkPath := filepath.Join(fixture.workspace, ".kiro")
				if kind == "runtime-directory" {
					linkPath = filepath.Join(fixture.homeDir, ".elydora")
				}
				if err := os.MkdirAll(filepath.Dir(linkPath), 0700); err != nil {
					t.Fatalf("create link parent: %v", err)
				}
				if err := os.Symlink(external, linkPath); err != nil {
					t.Skipf("directory symbolic links unavailable: %v", err)
				}
			}
			err := fixture.plugin.Install(fixture.config)
			if err == nil || !strings.Contains(err.Error(), "physical") {
				t.Fatalf("linked %s error = %v", kind, err)
			}
			assertNoKiroIdeRuntimeFiles(t, fixture)
		})
	}
}

func TestKiroIdeInstallSurvivesAdversarialWorkspaceLinkSwap(t *testing.T) {
	fixture := prepareKiroIdeFixture(t, nil, nil)
	kiroDirectory := filepath.Join(fixture.workspace, ".kiro")
	external := filepath.Join(filepath.Dir(fixture.homeDir), "external-link-swap")
	probe := filepath.Join(filepath.Dir(fixture.homeDir), "link-probe")
	if err := os.MkdirAll(external, 0700); err != nil {
		t.Fatalf("create external directory: %v", err)
	}
	if err := os.Symlink(external, probe); err != nil {
		t.Skipf("directory symbolic links unavailable: %v", err)
	}
	if err := os.Remove(probe); err != nil {
		t.Fatalf("remove link probe: %v", err)
	}
	swapped := false
	fixture.plugin.rename = func(source, destination string) error {
		if !swapped && sameKiroIdePath(destination, fixture.configPath) &&
			strings.HasSuffix(source, ".tmp") {
			swapped = true
			backup := kiroDirectory + ".original"
			if err := os.Rename(kiroDirectory, backup); err != nil {
				return err
			}
			if err := os.Symlink(external, kiroDirectory); err != nil {
				_ = os.Rename(backup, kiroDirectory)
				return err
			}
			renameErr := os.Rename(source, destination)
			_ = os.Remove(kiroDirectory)
			restoreErr := os.Rename(backup, kiroDirectory)
			return errors.Join(renameErr, restoreErr)
		}
		return os.Rename(source, destination)
	}
	err := fixture.plugin.Install(fixture.config)
	if err == nil || !swapped {
		t.Fatalf("link-swap install error = %v, swapped=%v", err, swapped)
	}
	if _, err := os.Lstat(filepath.Join(external, "hooks", kiroIdeConfigFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("external hook was written during link swap: %v", err)
	}
	assertNoKiroIdeRuntimeFiles(t, fixture)
	assertNoKiroIdeTransactionArtifacts(t, fixture.homeDir)
	assertNoKiroIdeTransactionArtifacts(t, fixture.workspace)
}

func TestKiroIdeInstallDetectsConcurrentSourceIdentityReplacement(t *testing.T) {
	source := `{"version":"v1","owner":"original","hooks":[]}`
	fixture := prepareKiroIdeFixture(t, &source, nil)
	mutated := false
	fixture.plugin.rename = func(stagedPath, destination string) error {
		if !mutated && strings.HasSuffix(stagedPath, ".tmp") {
			mutated = true
			external := fixture.configPath + ".external"
			if err := os.Rename(fixture.configPath, external); err != nil {
				return err
			}
			if err := os.WriteFile(fixture.configPath, []byte(source), 0600); err != nil {
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
	if readErr != nil || string(actual) != source {
		t.Fatalf("replacement source changed: %q, %v", actual, readErr)
	}
	assertNoKiroIdeRuntimeFiles(t, fixture)
	assertNoKiroIdeTransactionArtifacts(t, fixture.homeDir)
}
