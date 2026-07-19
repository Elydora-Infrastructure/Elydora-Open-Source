package plugins

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClineStatusRequiresIntactHooksAndRuntimes(t *testing.T) {
	fixture := prepareClineFixture(t, clineFixtureOptions{})
	installClineFixture(t, fixture)
	status, err := fixture.plugin.Status()
	if err != nil || !status.Installed || !status.HookConfigured || !status.HookScriptExists {
		t.Fatalf("installed status = %#v, %v", status, err)
	}
	if status.AgentName != "cline" || status.DisplayName != "Cline" || status.ConfigPath != fixture.hooksDir {
		t.Fatalf("status metadata = %#v", status)
	}
	if err := os.Remove(fixture.guardPath); err != nil {
		t.Fatalf("remove guard runtime: %v", err)
	}
	status, err = fixture.plugin.Status()
	if err != nil || status.Installed || !status.HookConfigured || status.HookScriptExists {
		t.Fatalf("degraded status = %#v, %v", status, err)
	}
	if err := os.WriteFile(fixture.guardPath, []byte("process.exit(0);\n"), 0700); err != nil {
		t.Fatalf("restore guard runtime: %v", err)
	}
	if err := os.Remove(fixture.auditWrapper); err != nil {
		t.Fatalf("remove audit wrapper: %v", err)
	}
	status, err = fixture.plugin.Status()
	if err != nil || status.Installed || status.HookConfigured {
		t.Fatalf("partial status = %#v, %v", status, err)
	}
}

func TestClineStatusSurfacesCorruptHooksAndRuntimeMetadata(t *testing.T) {
	fixture := prepareClineFixture(t, clineFixtureOptions{})
	installClineFixture(t, fixture)
	guard := readClineTestFile(t, fixture.guardWrapper)
	if err := os.WriteFile(fixture.guardWrapper, []byte(guard+"\n// tampered\n"), 0700); err != nil {
		t.Fatalf("tamper guard wrapper: %v", err)
	}
	if _, err := fixture.plugin.Status(); err == nil || !strings.Contains(err.Error(), "managed template") {
		t.Fatalf("tampered status error = %v", err)
	}
	installClineFixture(t, fixture)
	if err := os.WriteFile(fixture.runtimeConfig, []byte("{ malformed"), 0600); err != nil {
		t.Fatalf("corrupt runtime config: %v", err)
	}
	if _, err := fixture.plugin.Status(); err == nil || !strings.Contains(err.Error(), "parse Elydora runtime config") {
		t.Fatalf("runtime status error = %v", err)
	}
}

func TestClineInstallRejectsUserFilenameCollisionsBeforeWrites(t *testing.T) {
	for _, collision := range []string{"guard", "audit"} {
		t.Run(collision, func(t *testing.T) {
			options := clineFixtureOptions{}
			if collision == "guard" {
				options.ExistingGuard = clineString("// user PreToolUse hook\n")
			} else {
				options.ExistingAudit = clineString("// user PostToolUse hook\n")
			}
			fixture := prepareClineFixture(t, options)
			err := fixture.plugin.Install(fixture.config)
			if err == nil || !strings.Contains(err.Error(), "owned by another integration") {
				t.Fatalf("install error = %v", err)
			}
			for _, path := range []string{fixture.hookPath, fixture.runtimeConfig, fixture.privateKey} {
				requireMissingClineTestFile(t, path)
			}
			if options.ExistingGuard != nil && readClineTestFile(t, fixture.guardWrapper) != *options.ExistingGuard {
				t.Fatalf("user guard wrapper changed")
			}
			if options.ExistingAudit != nil && readClineTestFile(t, fixture.auditWrapper) != *options.ExistingAudit {
				t.Fatalf("user audit wrapper changed")
			}
		})
	}
}

func TestClineInstallPreservesCorruptOwnedMetadataForRecovery(t *testing.T) {
	corrupt := "#!/usr/bin/env node\n// @elydora-cline-hook invalid\n"
	fixture := prepareClineFixture(t, clineFixtureOptions{ExistingGuard: &corrupt})
	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "parse Elydora Cline hook metadata") {
		t.Fatalf("install error = %v", err)
	}
	if readClineTestFile(t, fixture.guardWrapper) != corrupt {
		t.Fatalf("corrupt hook changed")
	}
	requireMissingClineTestFile(t, fixture.auditWrapper)
	requireMissingClineTestFile(t, fixture.hookPath)
}

func TestClineInstallRejectsMissingGuardBeforeWrites(t *testing.T) {
	fixture := prepareClineFixture(t, clineFixtureOptions{SkipGuard: true})
	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "guard runtime is missing") {
		t.Fatalf("install error = %v", err)
	}
	for _, path := range []string{
		fixture.guardWrapper,
		fixture.auditWrapper,
		fixture.hookPath,
		fixture.runtimeConfig,
		fixture.privateKey,
	} {
		requireMissingClineTestFile(t, path)
	}
}

func TestClineUninstallRemovesExactOwnershipAndPreservesOtherHooks(t *testing.T) {
	fixture := prepareClineFixture(t, clineFixtureOptions{})
	installClineFixture(t, fixture)
	userHook := filepath.Join(fixture.hooksDir, "PreToolUse.py")
	if err := os.WriteFile(userHook, []byte("# user hook\n"), 0600); err != nil {
		t.Fatalf("write user hook: %v", err)
	}
	if err := fixture.plugin.Uninstall("agent-10"); err != nil {
		t.Fatalf("uninstall other agent: %v", err)
	}
	readClineTestFile(t, fixture.guardWrapper)
	readClineTestFile(t, fixture.auditWrapper)
	if err := fixture.plugin.Uninstall(clineTestAgentID); err != nil {
		t.Fatalf("uninstall Cline hooks: %v", err)
	}
	requireMissingClineTestFile(t, fixture.guardWrapper)
	requireMissingClineTestFile(t, fixture.auditWrapper)
	if readClineTestFile(t, userHook) != "# user hook\n" {
		t.Fatalf("user hook changed")
	}
}

func TestClineHookPairRollsBackFirstCommitWhenSecondCommitFails(t *testing.T) {
	fixture := prepareClineFixture(t, clineFixtureOptions{})
	installClineFixture(t, fixture)
	originalGuard := readClineTestFile(t, fixture.guardWrapper)
	originalAudit := readClineTestFile(t, fixture.auditWrapper)
	guardState, err := readClineHookFile(fixture.guardWrapper)
	if err != nil {
		t.Fatalf("read guard state: %v", err)
	}
	auditState, err := readClineHookFile(fixture.auditWrapper)
	if err != nil {
		t.Fatalf("read audit state: %v", err)
	}
	guardMetadata, err := buildClineMetadata("guard", "agent-2", fixture.guardPath)
	if err != nil {
		t.Fatalf("build replacement guard metadata: %v", err)
	}
	auditMetadata, err := buildClineMetadata("audit", "agent-2", fixture.hookPath)
	if err != nil {
		t.Fatalf("build replacement audit metadata: %v", err)
	}
	replacementGuard, err := buildClineWrapper(guardMetadata)
	if err != nil {
		t.Fatalf("build replacement guard: %v", err)
	}
	replacementAudit, err := buildClineWrapper(auditMetadata)
	if err != nil {
		t.Fatalf("build replacement audit: %v", err)
	}
	failureInjected := false
	rename := func(source, destination string) error {
		if !failureInjected && sameClineTestPath(destination, fixture.auditWrapper) {
			failureInjected = true
			return errors.New("simulated audit commit failure")
		}
		return os.Rename(source, destination)
	}
	err = writeClineHookPairWithRename(
		clinePendingWrite{state: guardState, source: replacementGuard},
		clinePendingWrite{state: auditState, source: replacementAudit},
		rename,
	)
	if err == nil || !strings.Contains(err.Error(), "write Cline hook pair") {
		t.Fatalf("write pair error = %v", err)
	}
	if !failureInjected {
		t.Fatalf("audit commit failure was not injected")
	}
	if readClineTestFile(t, fixture.guardWrapper) != originalGuard ||
		readClineTestFile(t, fixture.auditWrapper) != originalAudit {
		t.Fatalf("hook pair was not restored")
	}
	entries, err := os.ReadDir(fixture.hooksDir)
	if err != nil {
		t.Fatalf("read hook directory: %v", err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Fatalf("temporary hook remains: %s", entry.Name())
		}
	}
}

func sameClineTestPath(left, right string) bool {
	leftPath, leftErr := filepath.Abs(left)
	rightPath, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && strings.EqualFold(leftPath, rightPath)
}
