package plugins

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func prepareAugmentTestInstallation(
	t *testing.T,
	fixture *augmentFixture,
) ([]*fileChange, *augmentRuntimePaths, *augmentDocument) {
	t.Helper()
	document, err := readAugmentDocument()
	if err != nil {
		t.Fatalf("read Auggie document: %v", err)
	}
	paths, nodePath, err := preflightAugmentInstallation(fixture.config, document)
	if err != nil {
		t.Fatalf("preflight Auggie installation: %v", err)
	}
	hooks, _ := removeManagedAugmentHooks(document.hooks, "", paths.runtimeRoot)
	hooks["PreToolUse"] = append(
		hooks["PreToolUse"],
		buildAugmentGroup(buildAugmentHandler(paths.guardWrapperPath)),
	)
	hooks["PostToolUse"] = append(
		hooks["PostToolUse"],
		buildAugmentGroup(buildAugmentHandler(paths.auditWrapperPath)),
	)
	rendered, err := renderAugmentDocument(document, hooks)
	if err != nil {
		t.Fatalf("render Auggie document: %v", err)
	}
	changes, err := prepareAugmentInstallationChanges(
		fixture.config,
		paths,
		nodePath,
		rendered,
	)
	if err != nil {
		t.Fatalf("prepare Auggie installation: %v", err)
	}
	return changes, paths, document
}

func TestAugmentInstallRollsBackAllSevenFiles(t *testing.T) {
	original := "{\"telemetryEnabled\":true}\n"
	fixture := prepareAugmentFixture(
		t,
		augmentFixtureOptions{existingRaw: &original},
	)
	failed := false
	fixture.plugin.rename = func(source, destination string) error {
		if sameAugmentPath(destination, fixture.configPath) &&
			strings.HasSuffix(source, ".tmp") &&
			!failed {
			failed = true
			return errors.New("injected Auggie settings failure")
		}
		return os.Rename(source, destination)
	}
	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "injected Auggie settings failure") {
		t.Fatalf("install error = %v", err)
	}
	raw, readErr := os.ReadFile(fixture.configPath)
	if readErr != nil || string(raw) != original {
		t.Fatalf("original settings changed: %q, %v", raw, readErr)
	}
	assertNoAugmentRuntimeWrites(t, fixture)
	assertNoAugmentTransactionArtifacts(t, fixture.homeDir)
}

func TestPreparedAugmentInstallRejectsConcurrentSettingsChange(t *testing.T) {
	original := `{"telemetryEnabled":true}`
	fixture := prepareAugmentFixture(
		t,
		augmentFixtureOptions{existingRaw: &original},
	)
	changes, paths, document := prepareAugmentTestInstallation(t, fixture)
	concurrent := []byte(
		"{\"telemetryEnabled\":false,\"hooks\":{\"Notification\":[]}}\n",
	)
	if err := os.WriteFile(fixture.configPath, concurrent, 0600); err != nil {
		t.Fatalf("write concurrent settings: %v", err)
	}
	err := writeAugmentChanges(
		changes,
		"Install Augment Code CLI hooks",
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
		t.Fatalf("concurrent settings changed: %q, %v", actual, readErr)
	}
	assertNoAugmentRuntimeWrites(t, fixture)
	assertNoAugmentTransactionArtifacts(t, fixture.homeDir)
}

func TestAugmentInstallDetectsConcurrentSettingsIdentityReplacement(t *testing.T) {
	source := []byte(`{"owner":"original"}`)
	fixture := prepareAugmentFixture(
		t,
		augmentFixtureOptions{existingRaw: augmentString(string(source))},
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
	assertNoAugmentRuntimeWrites(t, fixture)
	assertNoAugmentTransactionArtifacts(t, fixture.homeDir)
}

func TestPreparedAugmentInstallRejectsStaleSettingsBeforeStaging(t *testing.T) {
	fixture := prepareAugmentFixture(t, augmentFixtureOptions{})
	document, err := readAugmentDocument()
	if err != nil {
		t.Fatalf("read Auggie document: %v", err)
	}
	paths, nodePath, err := preflightAugmentInstallation(fixture.config, document)
	if err != nil {
		t.Fatalf("preflight Auggie installation: %v", err)
	}
	hooks, _ := removeManagedAugmentHooks(document.hooks, "", paths.runtimeRoot)
	hooks["PreToolUse"] = append(
		hooks["PreToolUse"],
		buildAugmentGroup(buildAugmentHandler(paths.guardWrapperPath)),
	)
	hooks["PostToolUse"] = append(
		hooks["PostToolUse"],
		buildAugmentGroup(buildAugmentHandler(paths.auditWrapperPath)),
	)
	rendered, err := renderAugmentDocument(document, hooks)
	if err != nil {
		t.Fatalf("render Auggie document: %v", err)
	}
	writeAugmentTestFile(
		t,
		fixture.configPath,
		[]byte("{\"hooks\":{\"Notification\":[]}}\n"),
		0600,
	)
	_, err = prepareAugmentInstallationChanges(
		fixture.config,
		paths,
		nodePath,
		rendered,
	)
	if err == nil || !strings.Contains(err.Error(), "changed before update") {
		t.Fatalf("stale settings error = %v", err)
	}
	assertNoAugmentRuntimeWrites(t, fixture)
	assertNoAugmentTransactionArtifacts(t, fixture.homeDir)
}

func TestAugmentOrphanRuntimeArtifactsFailBeforeSettingsWrites(t *testing.T) {
	for _, runtimeName := range []string{
		"private.key",
		augmentGuardScript,
		augmentAuditScript,
		augmentGuardWrapperName(),
		augmentAuditWrapperName(),
		"chain-state.json",
		"status-cache.json",
		"error.log",
	} {
		t.Run(runtimeName, func(t *testing.T) {
			fixture := prepareAugmentFixture(t, augmentFixtureOptions{})
			artifact := filepath.Join(fixture.agentDir, runtimeName)
			writeAugmentTestFile(t, artifact, []byte("orphan\n"), 0600)
			err := fixture.plugin.Install(fixture.config)
			if err == nil ||
				!strings.Contains(err.Error(), "identity cannot be verified") {
				t.Fatalf("orphan runtime error = %v", err)
			}
			raw, readErr := os.ReadFile(artifact)
			if readErr != nil || string(raw) != "orphan\n" {
				t.Fatalf("orphan artifact changed: %q, %v", raw, readErr)
			}
			if _, err := os.Lstat(fixture.configPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("settings written after orphan error: %v", err)
			}
		})
	}
}

func TestAugmentMismatchedRuntimeIdentityFailsBeforeWrites(t *testing.T) {
	fixture := prepareAugmentFixture(t, augmentFixtureOptions{})
	writeAugmentTestObject(t, fixture.runtimeConfig, map[string]any{
		"agent_id": "another-agent", "agent_name": augmentAgentKey,
	})
	original, err := os.ReadFile(fixture.runtimeConfig)
	if err != nil {
		t.Fatalf("read runtime config: %v", err)
	}
	err = fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "identity does not match") {
		t.Fatalf("identity error = %v", err)
	}
	actual, readErr := os.ReadFile(fixture.runtimeConfig)
	if readErr != nil || string(actual) != string(original) {
		t.Fatalf("runtime config changed: %q, %v", actual, readErr)
	}
	if _, err := os.Lstat(fixture.configPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("settings written after identity error: %v", err)
	}
}

func TestAugmentLinkedSettingsRuntimeAndWrappersAreRejected(t *testing.T) {
	for _, kind := range []string{
		"configuration",
		"settings",
		"runtime",
		"agent-directory",
		"runtime-config",
		"wrapper",
	} {
		t.Run(kind, func(t *testing.T) {
			fixture := prepareAugmentFixture(t, augmentFixtureOptions{})
			target := filepath.Join(t.TempDir(), kind+"-target")
			switch kind {
			case "settings":
				writeAugmentTestFile(t, target, []byte("{}\n"), 0600)
				if err := os.MkdirAll(filepath.Dir(fixture.configPath), 0700); err != nil {
					t.Fatalf("create settings directory: %v", err)
				}
				augmentSymlinkOrSkip(t, target, fixture.configPath)
			case "configuration":
				if err := os.MkdirAll(target, 0700); err != nil {
					t.Fatalf("create target directory: %v", err)
				}
				if err := os.MkdirAll(filepath.Dir(filepath.Dir(fixture.configPath)), 0700); err != nil {
					t.Fatalf("create configuration parent: %v", err)
				}
				augmentSymlinkOrSkip(t, target, filepath.Dir(fixture.configPath))
			case "runtime":
				if err := os.MkdirAll(target, 0700); err != nil {
					t.Fatalf("create target directory: %v", err)
				}
				if err := os.MkdirAll(filepath.Dir(filepath.Dir(fixture.agentDir)), 0700); err != nil {
					t.Fatalf("create runtime parent: %v", err)
				}
				augmentSymlinkOrSkip(t, target, filepath.Dir(fixture.agentDir))
			case "agent-directory":
				if err := os.MkdirAll(target, 0700); err != nil {
					t.Fatalf("create target directory: %v", err)
				}
				if err := os.MkdirAll(filepath.Dir(fixture.agentDir), 0700); err != nil {
					t.Fatalf("create runtime root: %v", err)
				}
				augmentSymlinkOrSkip(t, target, fixture.agentDir)
			case "runtime-config":
				writeAugmentTestFile(
					t,
					target,
					[]byte(`{"agent_id":"agent-1","agent_name":"augment"}`),
					0600,
				)
				if err := os.MkdirAll(fixture.agentDir, 0700); err != nil {
					t.Fatalf("create agent directory: %v", err)
				}
				augmentSymlinkOrSkip(t, target, fixture.runtimeConfig)
			case "wrapper":
				if err := fixture.plugin.Install(fixture.config); err != nil {
					t.Fatalf("install Auggie hooks: %v", err)
				}
				writeAugmentTestFile(t, target, []byte("external wrapper\n"), 0600)
				if err := os.Remove(fixture.guardWrapper); err != nil {
					t.Fatalf("remove guard wrapper: %v", err)
				}
				augmentSymlinkOrSkip(t, target, fixture.guardWrapper)
			}
			err := fixture.plugin.Install(fixture.config)
			if err == nil || !strings.Contains(err.Error(), "physical") {
				t.Fatalf("linked %s error = %v", kind, err)
			}
		})
	}
}

func TestAugmentInstallValidatesRuntimeInputsBeforeWrites(t *testing.T) {
	for _, testCase := range []struct {
		name, field, value, want string
	}{
		{"agent-name", "agent_name", "codex", "requires agent name augment"},
		{"agent-id", "agent_id", "../escape", "invalid agent ID"},
		{"organization", "org_id", " ", "organization ID is required"},
		{"key-id", "kid", " ", "key ID is required"},
		{"private-key", "private_key", "invalid", "canonical 32-byte"},
		{"token", "token", " ", "non-whitespace"},
		{"base-url", "base_url", "https://api.elydora.com?q=1", "query parameters"},
		{"guard-path", "guard_script_path", "outside", "managed agent directory"},
		{"audit-path", "hook_script", "outside", "managed agent directory"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := prepareAugmentFixture(t, augmentFixtureOptions{})
			switch testCase.field {
			case "agent_name":
				fixture.config.AgentName = testCase.value
			case "agent_id":
				fixture.config.AgentID = testCase.value
			case "org_id":
				fixture.config.OrgID = testCase.value
			case "kid":
				fixture.config.KID = testCase.value
			case "private_key":
				fixture.config.PrivateKey = testCase.value
			case "token":
				fixture.config.Token = testCase.value
			case "base_url":
				fixture.config.BaseURL = testCase.value
			case "guard_script_path":
				fixture.config.GuardScriptPath = testCase.value
			case "hook_script":
				fixture.config.HookScript = testCase.value
			}
			err := fixture.plugin.Install(fixture.config)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("validation error = %v, want %q", err, testCase.want)
			}
			if _, err := os.Lstat(fixture.configPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("settings written after validation error: %v", err)
			}
			assertNoAugmentRuntimeWrites(t, fixture)
		})
	}
}

func TestAugmentInstallLeavesNoTransactionArtifacts(t *testing.T) {
	fixture := prepareAugmentFixture(t, augmentFixtureOptions{})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Auggie hooks: %v", err)
	}
	assertNoAugmentTransactionArtifacts(t, fixture.homeDir)
}

func TestAugmentUninstallPreservesSettingsAfterCommitFailure(t *testing.T) {
	original := `{"owner":"user"}`
	fixture := prepareAugmentFixture(
		t,
		augmentFixtureOptions{existingRaw: &original},
	)
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Auggie hooks: %v", err)
	}
	before, err := os.ReadFile(fixture.configPath)
	if err != nil {
		t.Fatalf("read installed settings: %v", err)
	}
	fixture.plugin.rename = func(source, destination string) error {
		if sameAugmentPath(destination, fixture.configPath) &&
			strings.HasSuffix(source, ".tmp") {
			return errors.New("injected Auggie uninstall failure")
		}
		return os.Rename(source, destination)
	}
	err = fixture.plugin.Uninstall(augmentTestAgentID)
	if err == nil || !strings.Contains(err.Error(), "injected Auggie uninstall failure") {
		t.Fatalf("uninstall error = %v", err)
	}
	after, readErr := os.ReadFile(fixture.configPath)
	if readErr != nil || string(after) != string(before) {
		t.Fatalf("Auggie settings changed: %q, %v", after, readErr)
	}
	assertNoAugmentTransactionArtifacts(t, fixture.homeDir)
}
