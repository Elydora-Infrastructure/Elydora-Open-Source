package plugins

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func codexSymlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
}

func assertCodexRuntimeAbsent(t *testing.T, fixture *codexFixture) {
	t.Helper()
	for _, path := range []string{
		fixture.runtimeConfig,
		fixture.privateKey,
		fixture.guardPath,
		fixture.hookPath,
	} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("runtime file exists at %s: %v", path, err)
		}
	}
}

func assertNoCodexTransactionArtifacts(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasSuffix(entry.Name(), ".tmp") ||
			strings.HasSuffix(entry.Name(), ".rollback") {
			t.Errorf("transaction artifact remains at %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk transaction root: %v", err)
	}
}

func TestCodexUsesConfiguredHomeAndLeavesAdditiveSourcesUntouched(t *testing.T) {
	fixture := prepareCodexFixture(t, "")
	customHome := filepath.Join(t.TempDir(), "custom codex home")
	if err := os.MkdirAll(customHome, 0700); err != nil {
		t.Fatalf("create CODEX_HOME: %v", err)
	}
	t.Setenv("CODEX_HOME", customHome)
	userTOML := filepath.Join(customHome, "config.toml")
	source := []byte("[hooks]\nexample = true\n")
	if err := os.WriteFile(userTOML, source, 0600); err != nil {
		t.Fatalf("write additive Codex config: %v", err)
	}

	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Codex hooks: %v", err)
	}
	wantHooks, err := codexConfigPath()
	if err != nil {
		t.Fatalf("resolve configured Codex hooks: %v", err)
	}
	status, err := fixture.plugin.Status()
	if err != nil || status.ConfigPath != wantHooks || !status.Installed {
		t.Fatalf("Codex status = %#v, %v", status, err)
	}
	actual, err := os.ReadFile(userTOML)
	if err != nil || string(actual) != string(source) {
		t.Fatalf("additive config changed: %q, %v", actual, err)
	}
}

func TestCodexConfiguredHomeMustExistAndBeDirectory(t *testing.T) {
	fixture := prepareCodexFixture(t, "")
	missing := filepath.Join(t.TempDir(), "missing")
	t.Setenv("CODEX_HOME", missing)
	if err := fixture.plugin.PreflightInstall(fixture.config); err == nil ||
		!strings.Contains(err.Error(), "resolve CODEX_HOME") {
		t.Fatalf("missing CODEX_HOME error = %v", err)
	}

	filePath := filepath.Join(t.TempDir(), "codex-home-file")
	if err := os.WriteFile(filePath, []byte("file"), 0600); err != nil {
		t.Fatalf("write CODEX_HOME file: %v", err)
	}
	t.Setenv("CODEX_HOME", filePath)
	if _, err := codexConfigPath(); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("file CODEX_HOME error = %v", err)
	}
	assertCodexRuntimeAbsent(t, fixture)
}

func TestCodexConfiguredHomeCanonicalizesDirectoryLinks(t *testing.T) {
	fixture := prepareCodexFixture(t, "")
	target := filepath.Join(t.TempDir(), "codex-target")
	link := filepath.Join(t.TempDir(), "codex-link")
	if err := os.MkdirAll(target, 0700); err != nil {
		t.Fatalf("create CODEX_HOME target: %v", err)
	}
	codexSymlinkOrSkip(t, target, link)
	t.Setenv("CODEX_HOME", link)

	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install through canonical CODEX_HOME: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, codexConfigFile)); err != nil {
		t.Fatalf("canonical hooks file missing: %v", err)
	}
}

func TestCodexUninstallPreservesUnconfiguredEmptyHooks(t *testing.T) {
	source := []byte("{\r\n  \"description\": \"user\",\r\n  \"hooks\": {}\r\n}\r\n")
	fixture := prepareCodexFixture(t, string(source))
	if err := fixture.plugin.Uninstall(codexTestAgentID); err != nil {
		t.Fatalf("uninstall unconfigured Codex hooks: %v", err)
	}
	actual, err := os.ReadFile(fixture.configPath)
	if err != nil || string(actual) != string(source) {
		t.Fatalf("unconfigured hooks changed: %q, %v", actual, err)
	}
}

func TestCodexInstallMigratesLegacyCommandHandlers(t *testing.T) {
	fixture := prepareCodexFixture(t, "")
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		t.Fatalf("resolve Node.js: %v", err)
	}
	legacy := func(scriptPath, status string) map[string]any {
		return map[string]any{
			"type":    "command",
			"command": quotePOSIXArgument(nodePath) + " " + quotePOSIXArgument(scriptPath),
			"commandWindows": quoteWindowsArgument(nodePath) + " " +
				quoteWindowsArgument(scriptPath),
			"timeout": codexHookTimeout, "statusMessage": status,
		}
	}
	settings := map[string]any{"hooks": map[string]any{
		"PreToolUse":  []any{codexMatcherGroup(legacy(fixture.guardPath, codexGuardStatus))},
		"PostToolUse": []any{codexMatcherGroup(legacy(fixture.hookPath, codexAuditStatus))},
	}}
	encoded, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("encode legacy hooks: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(fixture.configPath), 0700); err != nil {
		t.Fatalf("create hooks directory: %v", err)
	}
	if err := os.WriteFile(fixture.configPath, encoded, 0600); err != nil {
		t.Fatalf("write legacy hooks: %v", err)
	}

	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("migrate legacy Codex hooks: %v", err)
	}
	hooks := requireObject(t, readCodexTestObject(t, fixture.configPath)["hooks"])
	if len(requireArray(t, hooks["PreToolUse"])) != 1 ||
		len(requireArray(t, hooks["PostToolUse"])) != 1 {
		t.Fatalf("legacy handlers remain: %#v", hooks)
	}
	guard := requireCodexHandler(t, map[string]any{"hooks": hooks}, "PreToolUse", codexGuardStatus)
	if !strings.Contains(guard["commandWindows"].(string), "EncodedCommand") {
		t.Fatalf("migrated Windows command = %q", guard["commandWindows"])
	}
}

func TestCodexRejectsLinkedDefaultHooksDirectory(t *testing.T) {
	fixture := prepareCodexFixture(t, "")
	target := t.TempDir()
	codexSymlinkOrSkip(t, target, filepath.Dir(fixture.configPath))

	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "physical directory") {
		t.Fatalf("linked hooks directory error = %v", err)
	}
	assertCodexRuntimeAbsent(t, fixture)
	entries, readErr := os.ReadDir(target)
	if readErr != nil || len(entries) != 0 {
		t.Fatalf("linked directory target changed: %#v, %v", entries, readErr)
	}
}

func TestCodexRejectsLinkedHookAndRuntimeFiles(t *testing.T) {
	for _, name := range []string{"hooks", "config", "key", "guard", "audit"} {
		t.Run(name, func(t *testing.T) {
			fixture := prepareCodexFixture(t, "")
			path := map[string]string{
				"hooks": fixture.configPath, "config": fixture.runtimeConfig,
				"key": fixture.privateKey, "guard": fixture.guardPath,
				"audit": fixture.hookPath,
			}[name]
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatalf("create linked file directory: %v", err)
			}
			target := filepath.Join(t.TempDir(), name+"-target")
			source := []byte("external\n")
			if err := os.WriteFile(target, source, 0600); err != nil {
				t.Fatalf("write linked target: %v", err)
			}
			codexSymlinkOrSkip(t, target, path)

			err := fixture.plugin.Install(fixture.config)
			if err == nil || !strings.Contains(err.Error(), "physical file") {
				t.Fatalf("linked %s error = %v", name, err)
			}
			actual, readErr := os.ReadFile(target)
			if readErr != nil || string(actual) != string(source) {
				t.Fatalf("linked %s target changed: %q, %v", name, actual, readErr)
			}
		})
	}
}

func TestCodexRejectsOrphanAndMismatchedRuntimeIdentity(t *testing.T) {
	for _, testCase := range []struct {
		name, config string
	}{
		{"orphan", ""},
		{"mismatch", `{"agent_name":"cursor","agent_id":"agent-1"}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := prepareCodexFixture(t, "")
			if testCase.config != "" {
				if err := os.WriteFile(fixture.runtimeConfig, []byte(testCase.config), 0600); err != nil {
					t.Fatalf("write runtime config: %v", err)
				}
			} else if err := os.WriteFile(fixture.guardPath, []byte("orphan"), 0700); err != nil {
				t.Fatalf("write orphan guard: %v", err)
			}
			err := fixture.plugin.Install(fixture.config)
			if err == nil || !strings.Contains(err.Error(), "runtime") {
				t.Fatalf("runtime identity error = %v", err)
			}
			if _, err := os.Stat(fixture.configPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("Codex hooks exist after identity rejection: %v", err)
			}
		})
	}
}

func TestCodexInstallRollsBackAllFilesOnHooksCommitFailure(t *testing.T) {
	source := []byte(`{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"keep"}]}]}}`)
	fixture := prepareCodexFixture(t, string(source))
	fixture.plugin.rename = func(source, destination string) error {
		if sameCodexPath(destination, fixture.configPath) {
			return errors.New("injected Codex hooks commit failure")
		}
		return os.Rename(source, destination)
	}

	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "injected Codex hooks commit failure") {
		t.Fatalf("install error = %v", err)
	}
	actual, readErr := os.ReadFile(fixture.configPath)
	if readErr != nil || string(actual) != string(source) {
		t.Fatalf("hooks config changed: %q, %v", actual, readErr)
	}
	assertCodexRuntimeAbsent(t, fixture)
	assertNoCodexTransactionArtifacts(t, fixture.homeDir)
}

func TestCodexInstallPreservesConcurrentHooksMutation(t *testing.T) {
	fixture := prepareCodexFixture(t, `{"hooks":{}}`)
	concurrent := []byte(`{"hooks":{"SessionStart":[{"hooks":[{"command":"concurrent"}]}]}}`)
	mutated := false
	fixture.plugin.rename = func(source, destination string) error {
		if !mutated && strings.HasSuffix(source, ".tmp") {
			mutated = true
			if err := os.WriteFile(fixture.configPath, concurrent, 0600); err != nil {
				return err
			}
		}
		return os.Rename(source, destination)
	}

	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "changed during installation") {
		t.Fatalf("concurrent install error = %v", err)
	}
	actual, readErr := os.ReadFile(fixture.configPath)
	if readErr != nil || string(actual) != string(concurrent) {
		t.Fatalf("concurrent hooks changed: %q, %v", actual, readErr)
	}
	assertCodexRuntimeAbsent(t, fixture)
	assertNoCodexTransactionArtifacts(t, fixture.homeDir)
}

func TestCodexInstallDetectsConcurrentHooksIdentityReplacement(t *testing.T) {
	source := []byte(`{"hooks":{}}`)
	fixture := prepareCodexFixture(t, string(source))
	mutated := false
	fixture.plugin.rename = func(stagedPath, destination string) error {
		if !mutated && strings.HasSuffix(stagedPath, ".tmp") {
			mutated = true
			oldPath := fixture.configPath + ".external"
			if err := os.Rename(fixture.configPath, oldPath); err != nil {
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
		t.Fatalf("replacement hooks changed: %q, %v", actual, readErr)
	}
	assertCodexRuntimeAbsent(t, fixture)
	assertNoCodexTransactionArtifacts(t, fixture.homeDir)
}

func TestCodexStatusRejectsLinkedRuntimeFiles(t *testing.T) {
	for _, name := range []string{"config", "key", "guard", "audit"} {
		t.Run(name, func(t *testing.T) {
			fixture := prepareCodexFixture(t, "")
			if err := fixture.plugin.Install(fixture.config); err != nil {
				t.Fatalf("install Codex hooks: %v", err)
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
				t.Fatalf("write linked status target: %v", err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatalf("remove runtime file: %v", err)
			}
			codexSymlinkOrSkip(t, target, path)
			status, err := fixture.plugin.Status()
			if err == nil || status.Installed {
				t.Fatalf("Codex status = %#v, %v", status, err)
			}
		})
	}
}

func TestCodexStatusRequiresExactMatcherAndHandlerIdentity(t *testing.T) {
	fixture := prepareCodexFixture(t, "")
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Codex hooks: %v", err)
	}
	settings := readCodexTestObject(t, fixture.configPath)
	hooks := requireObject(t, settings["hooks"])
	preGroup := requireObject(t, requireArray(t, hooks["PreToolUse"])[0])
	preGroup["matcher"] = "Bash"
	encoded, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("encode modified hooks: %v", err)
	}
	if err := os.WriteFile(fixture.configPath, encoded, 0600); err != nil {
		t.Fatalf("write modified hooks: %v", err)
	}
	status, err := fixture.plugin.Status()
	if err != nil || status.Installed || status.HookConfigured {
		t.Fatalf("modified matcher status = %#v, %v", status, err)
	}

	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("repair Codex hooks: %v", err)
	}
	settings = readCodexTestObject(t, fixture.configPath)
	handler := requireCodexHandler(t, settings, "PreToolUse", codexGuardStatus)
	handler["extra"] = true
	encoded, err = json.Marshal(settings)
	if err != nil {
		t.Fatalf("encode modified handler: %v", err)
	}
	if err := os.WriteFile(fixture.configPath, encoded, 0600); err != nil {
		t.Fatalf("write modified handler: %v", err)
	}
	status, err = fixture.plugin.Status()
	if err != nil || status.Installed || status.HookConfigured {
		t.Fatalf("modified handler status = %#v, %v", status, err)
	}
}

func TestCodexInstallValidatesIdentityCredentialsAndOrigin(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*InstallConfig)
	}{
		{"agent name", func(config *InstallConfig) { config.AgentName = "cursor" }},
		{"agent ID", func(config *InstallConfig) { config.AgentID = "../escape" }},
		{"private key", func(config *InstallConfig) { config.PrivateKey = "invalid" }},
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
			fixture := prepareCodexFixture(t, "")
			testCase.mutate(&fixture.config)
			err := fixture.plugin.Install(fixture.config)
			if err == nil {
				t.Fatal("install accepted invalid configuration")
			}
			if _, err := os.Stat(fixture.configPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("Codex hooks exist after validation failure: %v", err)
			}
		})
	}
}
