package plugins

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClineStatusRequiresExactPhysicalInstallation(t *testing.T) {
	for _, target := range []string{
		"config", "key", "guard", "audit", "guard-wrapper", "audit-wrapper",
	} {
		t.Run("missing "+target, func(t *testing.T) {
			fixture := prepareClineFixture(t, clineFixtureOptions{})
			installClineFixture(t, fixture)
			status, err := fixture.plugin.Status()
			if err != nil || !status.Installed || !status.HookConfigured || !status.HookScriptExists {
				t.Fatalf("installed status = %#v, %v", status, err)
			}
			paths := map[string]string{
				"config": fixture.runtimeConfig, "key": fixture.privateKey,
				"guard": fixture.guardPath, "audit": fixture.hookPath,
				"guard-wrapper": fixture.guardWrapper,
				"audit-wrapper": fixture.auditWrapper,
			}
			if err := os.Remove(paths[target]); err != nil {
				t.Fatalf("remove %s: %v", target, err)
			}
			status, err = fixture.plugin.Status()
			if err != nil || status.Installed || status.HookScriptExists {
				t.Fatalf("missing %s status = %#v, %v", target, status, err)
			}
			if strings.Contains(target, "wrapper") && status.HookConfigured {
				t.Fatalf("missing wrapper remains configured: %#v", status)
			}
		})
	}
}

func TestClineStatusSurfacesTamperingAndInvalidRuntimeMetadata(t *testing.T) {
	tests := []struct {
		name, target, source, want string
	}{
		{"guard-source", "guard", "tampered\n", ""},
		{"audit-source", "audit", "tampered\n", ""},
		{"private-key", "key", "invalid", "canonical 32-byte"},
		{"config-malformed", "config", "{ malformed", "parse Elydora runtime config"},
		{"config-duplicate", "config", `{"agent_name":"cline","agent_name":"cline"}`, "duplicate key"},
		{"config-unsupported", "config", `{"org_id":"o","agent_id":"agent-1","kid":"k","base_url":"https://api.test","agent_name":"cline","extra":true}`, "unsupported field"},
		{"config-identity", "config", `{"org_id":"o","agent_id":"other","kid":"k","base_url":"https://api.test","agent_name":"cline"}`, "identity does not match"},
		{"guard-wrapper", "guard-wrapper", "tamper", "managed template"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := prepareClineFixture(t, clineFixtureOptions{})
			installClineFixture(t, fixture)
			paths := map[string]string{
				"guard": fixture.guardPath, "audit": fixture.hookPath,
				"key": fixture.privateKey, "config": fixture.runtimeConfig,
				"guard-wrapper": fixture.guardWrapper,
			}
			source := testCase.source
			if testCase.target == "guard-wrapper" {
				source = readClineTestFile(t, paths[testCase.target]) + testCase.source
			}
			writeClineTestFile(t, paths[testCase.target], []byte(source), 0600)
			status, err := fixture.plugin.Status()
			if testCase.want == "" {
				if err != nil || status.Installed || status.HookScriptExists {
					t.Fatalf("tampered status = %#v, %v", status, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("status error = %v, want %q", err, testCase.want)
			}
		})
	}
}

func TestClineStatusRejectsInvalidHookContracts(t *testing.T) {
	for _, testCase := range []struct {
		name, kind, agentID, runtimePath, want string
	}{
		{"kind", "guard", clineTestAgentID, "", "mismatched event metadata"},
		{"agent", "audit", "agent-2", "", "different agents"},
		{"path", "audit", clineTestAgentID, "outside", "unexpected runtime path"},
		{"segment", "audit", "..", "", "invalid agentId"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := prepareClineFixture(t, clineFixtureOptions{})
			installClineFixture(t, fixture)
			runtimePath := testCase.runtimePath
			if runtimePath == "outside" {
				runtimePath = filepath.Join(filepath.Dir(filepath.Dir(fixture.agentDir)), "outside", clineAuditScript)
			}
			if runtimePath == "" {
				runtimePath = fixture.hookPath
				if testCase.agentID == "agent-2" {
					runtimePath = filepath.Join(filepath.Dir(fixture.agentDir), "agent-2", clineAuditScript)
				}
			}
			metadata, err := buildClineMetadata(
				testCase.kind,
				testCase.agentID,
				runtimePath,
			)
			if err != nil {
				t.Fatalf("build invalid contract metadata: %v", err)
			}
			source, err := buildClineWrapper(metadata)
			if err != nil {
				t.Fatalf("build invalid contract wrapper: %v", err)
			}
			writeClineTestFile(t, fixture.auditWrapper, []byte(source), 0700)
			if testCase.name == "segment" {
				guardMetadata, buildErr := buildClineMetadata(
					"guard",
					testCase.agentID,
					fixture.guardPath,
				)
				if buildErr != nil {
					t.Fatalf("build invalid guard metadata: %v", buildErr)
				}
				guardSource, buildErr := buildClineWrapper(guardMetadata)
				if buildErr != nil {
					t.Fatalf("build invalid guard wrapper: %v", buildErr)
				}
				writeClineTestFile(t, fixture.guardWrapper, []byte(guardSource), 0700)
			}
			_, err = fixture.plugin.Status()
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("contract error = %v, want %q", err, testCase.want)
			}
		})
	}
}

func TestClineInstallRejectsCollisionsAndCorruptOwnershipBeforeWrites(t *testing.T) {
	for _, testCase := range []struct {
		name string
		opts clineFixtureOptions
		want string
	}{
		{"guard-collision", clineFixtureOptions{ExistingGuard: clineString("// user PreToolUse hook\n")}, "owned by another integration"},
		{"audit-collision", clineFixtureOptions{ExistingAudit: clineString("// user PostToolUse hook\n")}, "owned by another integration"},
		{"corrupt-metadata", clineFixtureOptions{ExistingGuard: clineString("#!/usr/bin/env node\n// @elydora-cline-hook invalid\n")}, "parse Elydora Cline hook metadata"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := prepareClineFixture(t, testCase.opts)
			err := fixture.plugin.Install(fixture.config)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("install error = %v", err)
			}
			for path, expected := range map[string]*string{
				fixture.guardWrapper: testCase.opts.ExistingGuard,
				fixture.auditWrapper: testCase.opts.ExistingAudit,
			} {
				if expected != nil && readClineTestFile(t, path) != *expected {
					t.Fatalf("existing hook changed at %s", path)
				}
			}
			for _, path := range []string{
				fixture.guardPath, fixture.hookPath, fixture.runtimeConfig, fixture.privateKey,
			} {
				requireMissingClineTestFile(t, path)
			}
		})
	}
}

func TestClineUninstallRemovesExactOwnershipAndPreservesAdjacentHooks(t *testing.T) {
	fixture := prepareClineFixture(t, clineFixtureOptions{})
	installClineFixture(t, fixture)
	userHook := filepath.Join(fixture.hooksDir, "PreToolUse.py")
	writeClineTestFile(t, userHook, []byte("# user hook\n"), 0600)
	if err := fixture.plugin.Uninstall("agent-10"); err != nil {
		t.Fatalf("uninstall other agent: %v", err)
	}
	readClineTestFile(t, fixture.guardWrapper)
	readClineTestFile(t, fixture.auditWrapper)
	if err := fixture.plugin.Uninstall(clineTestAgentID); err != nil {
		t.Fatalf("uninstall Cline hooks: %v", err)
	}
	for _, path := range []string{fixture.guardWrapper, fixture.auditWrapper} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("managed hook remains at %s: %v", path, err)
		}
	}
	if readClineTestFile(t, userHook) != "# user hook\n" {
		t.Fatal("adjacent user hook changed")
	}
}

func TestClineRuntimeConfigOmitsEmptyToken(t *testing.T) {
	fixture := prepareClineFixture(t, clineFixtureOptions{})
	fixture.config.Token = ""
	installClineFixture(t, fixture)
	if _, exists := readClineTestObject(t, fixture.runtimeConfig)["token"]; exists {
		t.Fatal("empty token persisted")
	}
}
