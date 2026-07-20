package plugins

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestKimiRejectsLinkedConfigBeforeCreatingRuntime(t *testing.T) {
	fixture := prepareKimiFixture(t, kimiFixtureOptions{withoutLegacyEvidence: true})
	target := filepath.Join(t.TempDir(), "config-target.toml")
	source := []byte("# protected target\ntelemetry = false\n")
	if err := os.WriteFile(target, source, 0600); err != nil {
		t.Fatalf("write config target: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(fixture.modernPath), 0700); err != nil {
		t.Fatalf("create Kimi config directory: %v", err)
	}
	kimiSymlinkOrSkip(t, target, fixture.modernPath)

	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "physical file") {
		t.Fatalf("linked config error = %v", err)
	}
	actual, readErr := os.ReadFile(target)
	if readErr != nil || string(actual) != string(source) {
		t.Fatalf("linked config target changed: %q, %v", actual, readErr)
	}
	assertNoKimiRuntimeWrites(t, fixture)
}

func TestKimiRejectsLinkedConfigAndRuntimeDirectories(t *testing.T) {
	for _, kind := range []string{"config", "runtime"} {
		t.Run(kind, func(t *testing.T) {
			fixture := prepareKimiFixture(t, kimiFixtureOptions{withoutLegacyEvidence: true})
			target := filepath.Join(t.TempDir(), kind+"-target")
			if err := os.MkdirAll(target, 0700); err != nil {
				t.Fatalf("create link target: %v", err)
			}
			link := fixture.kimiHome
			if kind == "runtime" {
				link = filepath.Dir(fixture.agentDir)
			}
			if err := os.MkdirAll(filepath.Dir(link), 0700); err != nil {
				t.Fatalf("create link parent: %v", err)
			}
			kimiSymlinkOrSkip(t, target, link)

			err := fixture.plugin.Install(fixture.config)
			if err == nil || !strings.Contains(err.Error(), "physical directory") {
				t.Fatalf("linked %s directory error = %v", kind, err)
			}
			entries, readErr := os.ReadDir(target)
			if readErr != nil || len(entries) != 0 {
				t.Fatalf("linked directory target changed: %#v, %v", entries, readErr)
			}
		})
	}
}

func TestKimiRejectsLinkedRuntimeFilesBeforeConfigWrites(t *testing.T) {
	for _, name := range []string{"config", "key", "guard", "audit"} {
		t.Run(name, func(t *testing.T) {
			fixture := prepareKimiFixture(t, kimiFixtureOptions{withoutLegacyEvidence: true})
			path := map[string]string{
				"config": fixture.runtimeConfig, "key": fixture.privateKey,
				"guard": fixture.guardPath, "audit": fixture.hookPath,
			}[name]
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatalf("create runtime directory: %v", err)
			}
			target := filepath.Join(t.TempDir(), name+"-target")
			source := []byte("external\n")
			if err := os.WriteFile(target, source, 0600); err != nil {
				t.Fatalf("write runtime target: %v", err)
			}
			kimiSymlinkOrSkip(t, target, path)

			err := fixture.plugin.Install(fixture.config)
			if err == nil || !strings.Contains(err.Error(), "physical file") {
				t.Fatalf("linked %s error = %v", name, err)
			}
			actual, readErr := os.ReadFile(target)
			if readErr != nil || string(actual) != string(source) {
				t.Fatalf("linked runtime target changed: %q, %v", actual, readErr)
			}
			if _, statErr := os.Stat(fixture.modernPath); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("Kimi config exists after rejection: %v", statErr)
			}
		})
	}
}

func TestKimiRejectsOrphanAndMismatchedRuntimeIdentity(t *testing.T) {
	for _, testCase := range []struct{ name, config string }{
		{"orphan", ""},
		{"mismatch", `{"agent_name":"kimi","agent_id":"another-agent"}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := prepareKimiFixture(t, kimiFixtureOptions{withoutLegacyEvidence: true})
			if err := os.MkdirAll(fixture.agentDir, 0700); err != nil {
				t.Fatalf("create agent directory: %v", err)
			}
			if testCase.config == "" {
				if err := os.WriteFile(fixture.guardPath, []byte("orphan\n"), 0700); err != nil {
					t.Fatalf("write orphan guard: %v", err)
				}
			} else if err := os.WriteFile(
				fixture.runtimeConfig, []byte(testCase.config), 0600,
			); err != nil {
				t.Fatalf("write mismatched config: %v", err)
			}
			err := fixture.plugin.Install(fixture.config)
			if err == nil || !strings.Contains(err.Error(), "runtime") {
				t.Fatalf("runtime identity error = %v", err)
			}
			if _, err := os.Stat(fixture.modernPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("Kimi config exists after rejection: %v", err)
			}
		})
	}
}

func TestKimiValidatesIdentityCredentialsAndOriginBeforeWrites(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*InstallConfig)
	}{
		{"agent name", func(config *InstallConfig) { config.AgentName = "codex" }},
		{"organization", func(config *InstallConfig) { config.OrgID = "   " }},
		{"agent ID", func(config *InstallConfig) { config.AgentID = "../escape" }},
		{"key ID", func(config *InstallConfig) { config.KID = "\t" }},
		{"private key", func(config *InstallConfig) { config.PrivateKey = "invalid" }},
		{"token", func(config *InstallConfig) { config.Token = "   " }},
		{"scheme", func(config *InstallConfig) { config.BaseURL = "file:///tmp/elydora" }},
		{"credentials", func(config *InstallConfig) { config.BaseURL = "https://user:secret@api.elydora.com" }},
		{"query", func(config *InstallConfig) { config.BaseURL = "https://api.elydora.com?tenant=one" }},
		{"backslash", func(config *InstallConfig) { config.BaseURL = `https://api.elydora.com\evil` }},
		{"space", func(config *InstallConfig) { config.BaseURL = "https://api.elydora.com/invalid path" }},
		{"port", func(config *InstallConfig) { config.BaseURL = "https://api.elydora.com:invalid" }},
		{"guard path", func(config *InstallConfig) { config.GuardScriptPath = "unmanaged.js" }},
		{"audit path", func(config *InstallConfig) { config.HookScript = "unmanaged.js" }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := prepareKimiFixture(t, kimiFixtureOptions{withoutLegacyEvidence: true})
			testCase.mutate(&fixture.config)
			err := fixture.plugin.Install(fixture.config)
			if err == nil {
				t.Fatal("install accepted invalid configuration")
			}
			if _, err := os.Stat(fixture.modernPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("Kimi config exists after validation failure: %v", err)
			}
			assertNoKimiRuntimeWrites(t, fixture)
		})
	}
}

func TestKimiStatusRejectsLinkedRuntimeFiles(t *testing.T) {
	for _, name := range []string{"config", "key", "guard", "audit"} {
		t.Run(name, func(t *testing.T) {
			fixture := prepareKimiFixture(t, kimiFixtureOptions{withoutLegacyEvidence: true})
			if err := fixture.plugin.Install(fixture.config); err != nil {
				t.Fatalf("install Kimi hooks: %v", err)
			}
			path := map[string]string{
				"config": fixture.runtimeConfig, "key": fixture.privateKey,
				"guard": fixture.guardPath, "audit": fixture.hookPath,
			}[name]
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read runtime file: %v", err)
			}
			target := filepath.Join(t.TempDir(), name+"-target")
			if err := os.WriteFile(target, content, 0600); err != nil {
				t.Fatalf("write linked target: %v", err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatalf("remove runtime file: %v", err)
			}
			kimiSymlinkOrSkip(t, target, path)
			status, err := fixture.plugin.Status()
			if err == nil || status.Installed {
				t.Fatalf("linked runtime status = %#v, %v", status, err)
			}
		})
	}
}

func TestKimiStatusValidatesRuntimeMetadataAndKey(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(map[string]any)
		want   string
	}{
		{"extra", func(value map[string]any) { value["extra"] = true }, "unsupported field"},
		{"organization", func(value map[string]any) { value["org_id"] = "" }, "org_id is invalid"},
		{"identity", func(value map[string]any) { value["agent_id"] = "other" }, "identity"},
		{"base URL", func(value map[string]any) { value["base_url"] = "file:///tmp" }, "base URL"},
		{"token", func(value map[string]any) { value["token"] = "" }, "token is invalid"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := prepareKimiFixture(t, kimiFixtureOptions{withoutLegacyEvidence: true})
			if err := fixture.plugin.Install(fixture.config); err != nil {
				t.Fatalf("install Kimi hooks: %v", err)
			}
			raw, err := os.ReadFile(fixture.runtimeConfig)
			if err != nil {
				t.Fatalf("read runtime config: %v", err)
			}
			var value map[string]any
			if err := json.Unmarshal(raw, &value); err != nil {
				t.Fatalf("decode runtime config: %v", err)
			}
			testCase.mutate(value)
			encoded, err := json.Marshal(value)
			if err != nil {
				t.Fatalf("encode runtime config: %v", err)
			}
			if err := os.WriteFile(fixture.runtimeConfig, encoded, 0600); err != nil {
				t.Fatalf("write runtime config: %v", err)
			}
			status, err := fixture.plugin.Status()
			if err == nil || status.Installed || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("invalid runtime status = %#v, %v", status, err)
			}
		})
	}

	fixture := prepareKimiFixture(t, kimiFixtureOptions{withoutLegacyEvidence: true})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Kimi hooks: %v", err)
	}
	if err := os.WriteFile(fixture.privateKey, []byte("invalid"), 0600); err != nil {
		t.Fatalf("write invalid private key: %v", err)
	}
	if status, err := fixture.plugin.Status(); err == nil || status.Installed ||
		!strings.Contains(err.Error(), "canonical 32-byte") {
		t.Fatalf("invalid key status = %#v, %v", status, err)
	}
}

func TestKimiOwnershipRequiresExactSupportedFields(t *testing.T) {
	fixture := prepareKimiFixture(t, kimiFixtureOptions{withoutLegacyEvidence: true})
	legacy := legacyKimiCommand(t, fixture.guardPath)
	source := "[[hooks]]\nevent = \"PreToolUse\"\ncommand = " + strconv.Quote(legacy) +
		"\ntimeout = 10\nmatcher = \"Bash\"\n"
	writeOptionalKimiConfig(t, fixture.modernPath, kimiString(source))
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Kimi hooks: %v", err)
	}
	hooks := readKimiTestHooks(t, fixture.modernPath)
	if len(hooks) != 4 || hooks[0]["matcher"] != "Bash" {
		t.Fatalf("user-owned matcher hook changed: %#v", hooks)
	}
	requireKimiManagedTriple(t, hooks)
	if err := fixture.plugin.Uninstall(kimiTestAgentID); err != nil {
		t.Fatalf("uninstall Kimi hooks: %v", err)
	}
	hooks = readKimiTestHooks(t, fixture.modernPath)
	if len(hooks) != 1 || hooks[0]["matcher"] != "Bash" {
		t.Fatalf("user-owned matcher hook removed: %#v", hooks)
	}
}

func TestKimiStatusSurfacesMalformedRuntimeMetadata(t *testing.T) {
	fixture := prepareKimiFixture(t, kimiFixtureOptions{withoutLegacyEvidence: true})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Kimi hooks: %v", err)
	}
	if err := os.WriteFile(fixture.runtimeConfig, []byte("{ malformed"), 0600); err != nil {
		t.Fatalf("corrupt runtime config: %v", err)
	}
	if _, err := fixture.plugin.Status(); err == nil ||
		!strings.Contains(err.Error(), "parse Elydora runtime config") {
		t.Fatalf("status error = %v", err)
	}
}
