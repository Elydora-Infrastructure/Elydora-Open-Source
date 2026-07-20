package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQwenInstallValidatesCredentialsAndManagedPathsBeforeWrites(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*qwenFixture)
		pattern string
	}{
		{
			"agent name",
			func(f *qwenFixture) { f.config.AgentName = "other" },
			"requires agent name qwen",
		},
		{
			"organization",
			func(f *qwenFixture) { f.config.OrgID = "   " },
			"organization ID is required",
		},
		{
			"agent id",
			func(f *qwenFixture) { f.config.AgentID = "../agent" },
			"single non-empty path segment",
		},
		{
			"key id",
			func(f *qwenFixture) { f.config.KID = "   " },
			"key ID is required",
		},
		{
			"private key",
			func(f *qwenFixture) { f.config.PrivateKey = "invalid" },
			"private key",
		},
		{
			"base URL",
			func(f *qwenFixture) { f.config.BaseURL = "https://api.test/path?secret=x" },
			"base URL",
		},
		{
			"token",
			func(f *qwenFixture) { f.config.Token = "   " },
			"token must contain",
		},
		{
			"relative guard",
			func(f *qwenFixture) { f.config.GuardScriptPath = "guard.js" },
			"managed agent directory",
		},
		{
			"guard outside runtime",
			func(f *qwenFixture) {
				f.config.GuardScriptPath = filepath.Join(f.homeDir, "guard.js")
			},
			"managed agent directory",
		},
		{
			"audit outside runtime",
			func(f *qwenFixture) {
				f.config.HookScript = filepath.Join(f.homeDir, "hook.js")
			},
			"audit runtime must use",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := prepareQwenFixture(t, qwenFixtureOptions{})
			test.mutate(fixture)
			err := fixture.plugin.Install(fixture.config)
			if err == nil || !strings.Contains(err.Error(), test.pattern) {
				t.Fatalf("install error = %v, want substring %q", err, test.pattern)
			}
			for _, path := range []string{
				fixture.configPath,
				fixture.guardPath,
				fixture.hookPath,
				fixture.runtimeConfig,
				fixture.privateKey,
			} {
				requireMissingQwenFile(t, path)
			}
		})
	}
}

func TestQwenInstallRejectsNonPhysicalSources(t *testing.T) {
	t.Run("home environment directory", func(t *testing.T) {
		fixture := prepareQwenFixture(t, qwenFixtureOptions{})
		envPath := filepath.Join(fixture.qwenDir, ".env")
		if err := os.MkdirAll(envPath, 0755); err != nil {
			t.Fatalf("create environment directory: %v", err)
		}
		err := fixture.plugin.Install(fixture.config)
		if err == nil || !strings.Contains(err.Error(), "physical file") {
			t.Fatalf("install error = %v", err)
		}
		requireMissingQwenFile(t, fixture.guardPath)
	})

	t.Run("user settings symlink", func(t *testing.T) {
		fixture := prepareQwenFixture(t, qwenFixtureOptions{})
		target := filepath.Join(filepath.Dir(fixture.homeDir), "settings-target.json")
		writeOptionalQwenFile(t, target, qwenString("{}\n"))
		if err := os.MkdirAll(filepath.Dir(fixture.configPath), 0755); err != nil {
			t.Fatalf("create settings directory: %v", err)
		}
		if err := os.Symlink(target, fixture.configPath); err != nil {
			t.Skipf("create settings symlink: %v", err)
		}
		err := fixture.plugin.Install(fixture.config)
		if err == nil || !strings.Contains(err.Error(), "physical file") {
			t.Fatalf("install error = %v", err)
		}
		requireMissingQwenFile(t, fixture.guardPath)
	})

	t.Run("agent runtime symlink", func(t *testing.T) {
		fixture := prepareQwenFixture(t, qwenFixtureOptions{})
		runtimeRoot := filepath.Dir(fixture.agentDir)
		target := filepath.Join(filepath.Dir(fixture.homeDir), "agent-target")
		for _, directory := range []string{runtimeRoot, target} {
			if err := os.MkdirAll(directory, 0700); err != nil {
				t.Fatalf("create runtime directory: %v", err)
			}
		}
		if err := os.Symlink(target, fixture.agentDir); err != nil {
			t.Skipf("create runtime symlink: %v", err)
		}
		err := fixture.plugin.PreflightInstall(fixture.config)
		if err == nil || !strings.Contains(err.Error(), "physical directory") {
			t.Fatalf("preflight error = %v", err)
		}
	})
}

func TestQwenPreflightProtectsExistingRuntimeIdentity(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*testing.T, *qwenFixture)
		pattern   string
	}{
		{
			"orphan artifact",
			func(t *testing.T, f *qwenFixture) {
				writeOptionalQwenFile(t, f.guardPath, qwenString("external\n"))
			},
			"cannot be verified without config.json",
		},
		{
			"different identity",
			func(t *testing.T, f *qwenFixture) {
				writeOptionalQwenFile(t, f.runtimeConfig, qwenJSON(map[string]any{
					"org_id":     "org-1",
					"agent_id":   "other-agent",
					"kid":        "kid-1",
					"base_url":   "https://api.elydora.test",
					"agent_name": qwenAgentKey,
				}))
			},
			"identity does not match",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := prepareQwenFixture(t, qwenFixtureOptions{})
			test.configure(t, fixture)
			err := fixture.plugin.PreflightInstall(fixture.config)
			if err == nil || !strings.Contains(err.Error(), test.pattern) {
				t.Fatalf("preflight error = %v", err)
			}
		})
	}
}

func TestQwenStatusRejectsRuntimeSymlink(t *testing.T) {
	fixture := prepareQwenFixture(t, qwenFixtureOptions{})
	installQwenFixture(t, fixture)
	target := filepath.Join(filepath.Dir(fixture.homeDir), "guard-target.js")
	writeOptionalQwenFile(t, target, qwenString(readQwenTestFile(t, fixture.guardPath)))
	if err := os.Remove(fixture.guardPath); err != nil {
		t.Fatalf("remove guard runtime: %v", err)
	}
	if err := os.Symlink(target, fixture.guardPath); err != nil {
		t.Skipf("create guard symlink: %v", err)
	}
	if _, err := fixture.plugin.Status(); err == nil ||
		!strings.Contains(err.Error(), "physical file") {
		t.Fatalf("status error = %v", err)
	}
}
