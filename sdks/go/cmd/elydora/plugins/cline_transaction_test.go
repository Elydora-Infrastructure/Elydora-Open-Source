package plugins

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func prepareClineTestChanges(
	t *testing.T,
	fixture *clineFixture,
) ([]*fileChange, *clineRuntimePaths) {
	t.Helper()
	paths, guard, audit, err := readClineHookPair()
	if err != nil {
		t.Fatalf("read Cline hook pair: %v", err)
	}
	runtimePaths, err := preflightClineInstallation(fixture.config, paths)
	if err != nil {
		t.Fatalf("preflight Cline installation: %v", err)
	}
	changes, err := prepareClineInstallationChanges(
		fixture.config,
		runtimePaths,
		guard,
		audit,
	)
	if err != nil {
		t.Fatalf("prepare Cline installation: %v", err)
	}
	return changes, runtimePaths
}

func snapshotClineFiles(t *testing.T, paths ...string) map[string]string {
	t.Helper()
	result := make(map[string]string, len(paths))
	for _, path := range paths {
		result[path] = readClineTestFile(t, path)
	}
	return result
}

func requireClineSnapshot(t *testing.T, snapshot map[string]string) {
	t.Helper()
	for path, expected := range snapshot {
		if actual := readClineTestFile(t, path); actual != expected {
			t.Fatalf("snapshot changed at %s", path)
		}
	}
}

func TestClineInstallRollsBackAllSixFiles(t *testing.T) {
	fixture := prepareClineFixture(t, clineFixtureOptions{})
	failed := false
	fixture.plugin.rename = func(source, destination string) error {
		if !failed && sameClineTestPath(destination, fixture.auditWrapper) &&
			strings.HasSuffix(source, ".tmp") {
			failed = true
			return errors.New("injected Cline audit hook failure")
		}
		return os.Rename(source, destination)
	}
	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "injected Cline audit hook failure") {
		t.Fatalf("install error = %v", err)
	}
	if !failed {
		t.Fatal("audit hook failure was not injected")
	}
	assertNoClineRuntimeWrites(t, fixture)
	assertNoClineTransactionArtifacts(t, fixture.homeDir)
}

func TestClineInstallRestoresAllSixExistingFilesAfterCommitFailure(t *testing.T) {
	fixture := prepareClineFixture(t, clineFixtureOptions{})
	installClineFixture(t, fixture)
	writeClineTestObject(t, fixture.runtimeConfig, map[string]any{
		"org_id": "old-org", "agent_id": clineTestAgentID, "kid": "old-kid",
		"base_url": "https://old.elydora.test", "agent_name": clineAgentKey,
	})
	for path, source := range map[string]string{
		fixture.guardPath:    "stale guard\n",
		fixture.privateKey:   "BwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwc",
		fixture.hookPath:     "stale audit\n",
		fixture.guardWrapper: readClineTestFile(t, fixture.guardWrapper) + "// stale\n",
		fixture.auditWrapper: readClineTestFile(t, fixture.auditWrapper) + "// stale\n",
	} {
		writeClineTestFile(t, path, []byte(source), 0600)
	}
	paths := []string{
		fixture.guardPath,
		fixture.runtimeConfig,
		fixture.privateKey,
		fixture.hookPath,
		fixture.guardWrapper,
		fixture.auditWrapper,
	}
	before := snapshotClineFiles(t, paths...)
	failed := false
	fixture.plugin.rename = func(source, destination string) error {
		if !failed && sameClineTestPath(destination, fixture.auditWrapper) &&
			strings.HasSuffix(source, ".tmp") {
			failed = true
			return errors.New("injected existing Cline audit hook failure")
		}
		return os.Rename(source, destination)
	}
	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "injected existing Cline audit hook failure") {
		t.Fatalf("install error = %v", err)
	}
	requireClineSnapshot(t, before)
	assertNoClineTransactionArtifacts(t, fixture.homeDir)
}

func TestPreparedClineInstallRejectsStaleHookBeforeStaging(t *testing.T) {
	fixture := prepareClineFixture(t, clineFixtureOptions{})
	paths, guard, audit, err := readClineHookPair()
	if err != nil {
		t.Fatalf("read Cline hook pair: %v", err)
	}
	runtimePaths, err := preflightClineInstallation(fixture.config, paths)
	if err != nil {
		t.Fatalf("preflight Cline installation: %v", err)
	}
	concurrent := []byte("// concurrent user hook\n")
	writeClineTestFile(t, fixture.auditWrapper, concurrent, 0600)
	_, err = prepareClineInstallationChanges(
		fixture.config,
		runtimePaths,
		guard,
		audit,
	)
	if err == nil || !strings.Contains(err.Error(), "changed before update") {
		t.Fatalf("stale hook error = %v", err)
	}
	if readClineTestFile(t, fixture.auditWrapper) != string(concurrent) {
		t.Fatal("concurrent hook changed")
	}
	for _, path := range []string{
		fixture.guardPath, fixture.hookPath, fixture.runtimeConfig, fixture.privateKey,
	} {
		requireMissingClineTestFile(t, path)
	}
}

func TestPreparedClineInstallRejectsConcurrentHookChange(t *testing.T) {
	fixture := prepareClineFixture(t, clineFixtureOptions{})
	changes, paths := prepareClineTestChanges(t, fixture)
	concurrent := []byte("// concurrent user hook\n")
	writeClineTestFile(t, fixture.auditWrapper, concurrent, 0600)
	err := writeClineChanges(changes, "Install Cline hooks", nil, paths)
	if err == nil || !strings.Contains(err.Error(), "changed during installation") {
		t.Fatalf("concurrent hook error = %v", err)
	}
	if readClineTestFile(t, fixture.auditWrapper) != string(concurrent) {
		t.Fatal("concurrent hook changed")
	}
	for _, path := range []string{
		fixture.guardPath, fixture.hookPath, fixture.runtimeConfig, fixture.privateKey,
	} {
		requireMissingClineTestFile(t, path)
	}
	assertNoClineTransactionArtifacts(t, fixture.homeDir)
}

func TestClineInstallDetectsConcurrentHookIdentityReplacement(t *testing.T) {
	fixture := prepareClineFixture(t, clineFixtureOptions{})
	installClineFixture(t, fixture)
	tampered := readClineTestFile(t, fixture.auditWrapper) + "// stale\n"
	writeClineTestFile(t, fixture.auditWrapper, []byte(tampered), 0700)
	fixture.config.OrgID = "org-updated"
	before := snapshotClineFiles(
		t,
		fixture.guardPath,
		fixture.runtimeConfig,
		fixture.privateKey,
		fixture.hookPath,
		fixture.guardWrapper,
		fixture.auditWrapper,
	)
	mutated := false
	fixture.plugin.rename = func(source, destination string) error {
		if !mutated && strings.HasSuffix(source, ".tmp") {
			mutated = true
			external := fixture.auditWrapper + ".external"
			if err := os.Rename(fixture.auditWrapper, external); err != nil {
				return err
			}
			if err := os.WriteFile(fixture.auditWrapper, []byte(tampered), 0700); err != nil {
				return err
			}
		}
		return os.Rename(source, destination)
	}
	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "changed during installation") {
		t.Fatalf("concurrent identity error = %v", err)
	}
	requireClineSnapshot(t, before)
	assertNoClineTransactionArtifacts(t, fixture.homeDir)
}

func TestClineOrphanRuntimeArtifactsFailBeforeHookWrites(t *testing.T) {
	for _, name := range []string{
		"private.key", clineGuardScript, clineAuditScript,
		"chain-state.json", "status-cache.json", "error.log",
	} {
		t.Run(name, func(t *testing.T) {
			fixture := prepareClineFixture(t, clineFixtureOptions{})
			artifact := filepath.Join(fixture.agentDir, name)
			writeClineTestFile(t, artifact, []byte("orphan\n"), 0600)
			err := fixture.plugin.Install(fixture.config)
			if err == nil || !strings.Contains(err.Error(), "identity cannot be verified") {
				t.Fatalf("orphan runtime error = %v", err)
			}
			if readClineTestFile(t, artifact) != "orphan\n" {
				t.Fatal("orphan artifact changed")
			}
			requireMissingClineTestFile(t, fixture.guardWrapper)
			requireMissingClineTestFile(t, fixture.auditWrapper)
		})
	}
}

func TestClineMismatchedRuntimeIdentityFailsBeforeWrites(t *testing.T) {
	fixture := prepareClineFixture(t, clineFixtureOptions{})
	writeClineTestObject(t, fixture.runtimeConfig, map[string]any{
		"agent_id": "another-agent", "agent_name": clineAgentKey,
	})
	original := readClineTestFile(t, fixture.runtimeConfig)
	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "identity does not match") {
		t.Fatalf("identity error = %v", err)
	}
	if readClineTestFile(t, fixture.runtimeConfig) != original {
		t.Fatal("runtime config changed")
	}
	requireMissingClineTestFile(t, fixture.guardWrapper)
}

func TestClineLinkedDirectoriesAndFilesAreRejected(t *testing.T) {
	for _, kind := range []string{
		"configuration", "hooks", "runtime", "agent-directory", "runtime-config", "hook",
	} {
		t.Run(kind, func(t *testing.T) {
			fixture := prepareClineFixture(t, clineFixtureOptions{})
			target := filepath.Join(t.TempDir(), kind+"-target")
			switch kind {
			case "configuration":
				if err := os.MkdirAll(target, 0700); err != nil {
					t.Fatalf("create target: %v", err)
				}
				clineSymlinkOrSkip(t, target, fixture.clineDir)
			case "hooks":
				if err := os.MkdirAll(target, 0700); err != nil {
					t.Fatalf("create target: %v", err)
				}
				if err := os.MkdirAll(fixture.clineDir, 0700); err != nil {
					t.Fatalf("create Cline directory: %v", err)
				}
				clineSymlinkOrSkip(t, target, fixture.hooksDir)
			case "runtime":
				if err := os.MkdirAll(target, 0700); err != nil {
					t.Fatalf("create target: %v", err)
				}
				clineSymlinkOrSkip(t, target, filepath.Dir(fixture.agentDir))
			case "agent-directory":
				if err := os.MkdirAll(target, 0700); err != nil {
					t.Fatalf("create target: %v", err)
				}
				if err := os.MkdirAll(filepath.Dir(fixture.agentDir), 0700); err != nil {
					t.Fatalf("create runtime root: %v", err)
				}
				clineSymlinkOrSkip(t, target, fixture.agentDir)
			case "runtime-config":
				writeClineTestFile(t, target, []byte(`{"agent_id":"agent-1","agent_name":"cline"}`), 0600)
				if err := os.MkdirAll(fixture.agentDir, 0700); err != nil {
					t.Fatalf("create agent directory: %v", err)
				}
				clineSymlinkOrSkip(t, target, fixture.runtimeConfig)
			case "hook":
				installClineFixture(t, fixture)
				writeClineTestFile(t, target, []byte("external hook\n"), 0600)
				if err := os.Remove(fixture.guardWrapper); err != nil {
					t.Fatalf("remove guard wrapper: %v", err)
				}
				clineSymlinkOrSkip(t, target, fixture.guardWrapper)
			}
			err := fixture.plugin.Install(fixture.config)
			if err == nil || !strings.Contains(err.Error(), "physical") {
				t.Fatalf("linked %s error = %v", kind, err)
			}
		})
	}
}

func TestClineInstallValidatesInputsBeforeWrites(t *testing.T) {
	tests := []struct{ name, field, value, want string }{
		{"agent-name", "agent_name", "codex", "requires agent name cline"},
		{"agent-id", "agent_id", "../escape", "invalid agent ID"},
		{"organization", "org_id", " ", "organization ID is required"},
		{"key-id", "kid", " ", "key ID is required"},
		{"private-key", "private_key", "invalid", "canonical 32-byte"},
		{"token", "token", " ", "non-whitespace"},
		{"base-url", "base_url", "https://api.elydora.com?q=1", "query parameters"},
		{"guard-path", "guard_script_path", "outside", "managed agent directory"},
		{"audit-path", "hook_script", "outside", "managed agent directory"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := prepareClineFixture(t, clineFixtureOptions{})
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
			assertNoClineRuntimeWrites(t, fixture)
		})
	}
}

func TestClineUninstallRestoresBothHooksWhenSecondRemovalFails(t *testing.T) {
	fixture := prepareClineFixture(t, clineFixtureOptions{})
	installClineFixture(t, fixture)
	before := snapshotClineFiles(t, fixture.guardWrapper, fixture.auditWrapper)
	calls := 0
	fixture.plugin.rename = func(source, destination string) error {
		calls++
		if calls == 2 {
			return errors.New("injected Cline uninstall failure")
		}
		return os.Rename(source, destination)
	}
	err := fixture.plugin.Uninstall(clineTestAgentID)
	if err == nil || !strings.Contains(err.Error(), "injected Cline uninstall failure") {
		t.Fatalf("uninstall error = %v", err)
	}
	requireClineSnapshot(t, before)
	assertNoClineTransactionArtifacts(t, fixture.homeDir)
}

func TestClineInstallLeavesNoTransactionArtifacts(t *testing.T) {
	fixture := prepareClineFixture(t, clineFixtureOptions{})
	installClineFixture(t, fixture)
	assertNoClineTransactionArtifacts(t, fixture.homeDir)
}
