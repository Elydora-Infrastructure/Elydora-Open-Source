package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestKimiRegistryUsesModernGlobalContract(t *testing.T) {
	entry := SupportedAgents[kimiAgentKey]
	if entry.Name != "Kimi Code" || entry.ConfigDir != "~/.kimi-code" || entry.ConfigFile != "config.toml" {
		t.Fatalf("Kimi registry entry = %#v", entry)
	}
	if _, ok := NewPlugin(kimiAgentKey).(*KimiPlugin); !ok {
		t.Fatalf("Kimi registry plugin = %T", NewPlugin(kimiAgentKey))
	}
}

func TestKimiInstallPreservesBothConfigsAndIsIdempotent(t *testing.T) {
	modern := "# modern user config\ndefault_model = \"kimi-code/k3\"\n\n" +
		"[[hooks]]\nevent = \"SessionStart\"\ncommand = \"existing-modern\"\n" +
		"timeout = 30 # keep modern hook\n"
	legacy := "# legacy user config\ntelemetry = false\n\n" +
		"[[hooks]]\nevent = \"SessionEnd\"\ncommand = \"existing-legacy\"\n"
	fixture := prepareKimiFixture(t, kimiFixtureOptions{
		modernConfig: kimiString(modern),
		legacyConfig: kimiString(legacy),
	})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Kimi hooks: %v", err)
	}
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("repeat Kimi install: %v", err)
	}

	for _, contract := range []struct{ path, comment, command string }{
		{fixture.modernPath, "# modern user config", "existing-modern"},
		{fixture.legacyPath, "# legacy user config", "existing-legacy"},
	} {
		raw, err := os.ReadFile(contract.path)
		if err != nil {
			t.Fatalf("read Kimi config: %v", err)
		}
		if !strings.Contains(string(raw), contract.comment) || !strings.Contains(string(raw), contract.command) {
			t.Fatalf("user config changed: %s", raw)
		}
		hooks := readKimiTestHooks(t, contract.path)
		if len(hooks) != 3 {
			t.Fatalf("hooks = %#v, want three entries", hooks)
		}
		requireStrictKimiHook(t, findKimiTestHook(t, hooks, "PreToolUse", kimiGuardScript))
		requireStrictKimiHook(t, findKimiTestHook(t, hooks, "PostToolUse", kimiAuditScript))
	}
	defaultModern := filepath.Join(fixture.homeDir, ".kimi-code", "config.toml")
	if _, err := os.Stat(defaultModern); !os.IsNotExist(err) {
		t.Fatalf("false default migration target exists: %v", err)
	}
}

func TestKimiInstallPreservesInlineHookArrayStyle(t *testing.T) {
	modern := "# inline user hook\nhooks = [\n  # keep leading\n  { event = \"SessionStart\", " +
		"command = \"existing-inline\" }, # keep gap\n] # keep array\n"
	fixture := prepareKimiFixture(t, kimiFixtureOptions{
		modernConfig:     kimiString(modern),
		withoutLegacyCLI: true,
	})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install inline Kimi hooks: %v", err)
	}
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("repeat inline Kimi install: %v", err)
	}
	raw, err := os.ReadFile(fixture.modernPath)
	if err != nil {
		t.Fatalf("read inline Kimi config: %v", err)
	}
	for _, marker := range []string{
		"# inline user hook",
		"# keep leading",
		"# keep gap",
		"# keep array",
		"hooks = [",
	} {
		if !strings.Contains(string(raw), marker) {
			t.Fatalf("inline marker %q missing from %s", marker, raw)
		}
	}
	if len(readKimiTestHooks(t, fixture.modernPath)) != 3 {
		t.Fatalf("inline Kimi hooks are not idempotent: %s", raw)
	}
}

func TestKimiModernDefaultAvoidsLegacyMigrationMarker(t *testing.T) {
	fixture := prepareKimiFixture(t, kimiFixtureOptions{
		useDefaultHome:   true,
		withoutLegacyCLI: true,
	})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install modern Kimi hooks: %v", err)
	}
	if len(readKimiTestHooks(t, fixture.modernPath)) != 2 {
		t.Fatalf("modern Kimi config has unexpected hooks")
	}
	if _, err := os.Stat(fixture.legacyPath); !os.IsNotExist(err) {
		t.Fatalf("false legacy migration target exists: %v", err)
	}
	status, err := fixture.plugin.Status()
	if err != nil || !status.Installed {
		t.Fatalf("empty KIMI_CODE_HOME status = %#v, %v", status, err)
	}
}

func TestKimiLegacyInstallAvoidsPrematureModernTarget(t *testing.T) {
	fixture := prepareKimiFixture(t, kimiFixtureOptions{useDefaultHome: true})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install legacy Kimi hooks: %v", err)
	}
	if len(readKimiTestHooks(t, fixture.legacyPath)) != 2 {
		t.Fatalf("legacy Kimi config has unexpected hooks")
	}
	if _, err := os.Stat(fixture.modernPath); !os.IsNotExist(err) {
		t.Fatalf("premature modern migration target exists: %v", err)
	}
	if err := os.Remove(fixture.legacyCLIPath); err != nil {
		t.Fatalf("remove legacy CLI marker: %v", err)
	}
	status, err := fixture.plugin.Status()
	if err != nil || !status.Installed {
		t.Fatalf("legacy status after PATH change = %#v, %v", status, err)
	}
}

func TestKimiCommandsBlockAndForwardOfficialPayload(t *testing.T) {
	fixture := prepareKimiFixture(t, kimiFixtureOptions{})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Kimi hooks: %v", err)
	}
	capturePath := filepath.Join(t.TempDir(), "captured-event.json")
	encodedPath, err := json.Marshal(capturePath)
	if err != nil {
		t.Fatalf("marshal capture path: %v", err)
	}
	source := "const fs = require('node:fs'); const chunks = []; " +
		"process.stdin.on('data', chunk => chunks.push(chunk)); " +
		"process.stdin.on('end', () => fs.writeFileSync(" + string(encodedPath) + ", Buffer.concat(chunks)));\n"
	if err := os.WriteFile(fixture.hookPath, []byte(source), 0700); err != nil {
		t.Fatalf("write capture runtime: %v", err)
	}
	hooks := readKimiTestHooks(t, fixture.modernPath)
	payload := map[string]any{
		"hook_event_name": "PreToolUse",
		"session_id":      "session-1",
		"cwd":             fixture.homeDir,
		"tool_name":       "Bash",
		"tool_input":      map[string]any{"command": "echo test"},
		"tool_call_id":    "call-1",
	}
	guard := findKimiTestHook(t, hooks, "PreToolUse", kimiGuardScript)
	exitCode, stderr := runKimiCommand(t, guard["command"].(string), fixture.homeDir, payload)
	if exitCode != 2 || !strings.Contains(stderr, "Agent is frozen by Elydora") {
		t.Fatalf("guard exit = %d, stderr = %q", exitCode, stderr)
	}
	payload["hook_event_name"] = "PostToolUse"
	payload["tool_output"] = map[string]any{"output": "test"}
	audit := findKimiTestHook(t, hooks, "PostToolUse", kimiAuditScript)
	exitCode, stderr = runKimiCommand(t, audit["command"].(string), fixture.homeDir, payload)
	if exitCode != 0 {
		t.Fatalf("audit exit = %d, stderr = %q", exitCode, stderr)
	}
	capturedRaw, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatalf("read captured event: %v", err)
	}
	var captured map[string]any
	if err := json.Unmarshal(capturedRaw, &captured); err != nil {
		t.Fatalf("decode captured event: %v", err)
	}
	if !reflect.DeepEqual(captured, payload) {
		t.Fatalf("captured event = %#v, want %#v", captured, payload)
	}
}

func TestKimiStatusAcceptsEitherContractAndRequiresBothScripts(t *testing.T) {
	fixture := prepareKimiFixture(t, kimiFixtureOptions{})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Kimi hooks: %v", err)
	}
	if err := os.Remove(fixture.modernPath); err != nil {
		t.Fatalf("remove modern config: %v", err)
	}
	requireKimiInstalledStatus(t, fixture.plugin, fixture.legacyPath)
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("reinstall Kimi hooks: %v", err)
	}
	if err := os.Remove(fixture.legacyPath); err != nil {
		t.Fatalf("remove legacy config: %v", err)
	}
	requireKimiInstalledStatus(t, fixture.plugin, fixture.modernPath)
	if err := os.Remove(fixture.guardPath); err != nil {
		t.Fatalf("remove Kimi guard: %v", err)
	}
	status, err := fixture.plugin.Status()
	if err != nil || status.Installed || !status.HookConfigured || status.HookScriptExists {
		t.Fatalf("degraded Kimi status = %#v, %v", status, err)
	}
}

func requireKimiInstalledStatus(t *testing.T, plugin *KimiPlugin, configPath string) {
	t.Helper()
	status, err := plugin.Status()
	if err != nil || !status.Installed || status.ConfigPath != configPath {
		t.Fatalf("Kimi status = %#v, %v; want installed at %s", status, err, configPath)
	}
}

func TestKimiUninstallPreservesUserHooks(t *testing.T) {
	userHook := "# user hook\n[[hooks]]\nevent = \"SessionStart\"\n" +
		"command = \"existing-command\"\ntimeout = 30 # keep timeout\n"
	fixture := prepareKimiFixture(t, kimiFixtureOptions{
		modernConfig: kimiString(userHook),
		legacyConfig: kimiString(userHook),
	})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Kimi hooks: %v", err)
	}
	if err := fixture.plugin.Uninstall(kimiTestAgentID); err != nil {
		t.Fatalf("uninstall Kimi hooks: %v", err)
	}
	for _, path := range []string{fixture.modernPath, fixture.legacyPath} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read user config after uninstall: %v", err)
		}
		if !strings.Contains(string(raw), "# user hook") || !strings.Contains(string(raw), "# keep timeout") {
			t.Fatalf("user comments changed: %s", raw)
		}
		hooks := readKimiTestHooks(t, path)
		if len(hooks) != 1 || hooks[0]["command"] != "existing-command" {
			t.Fatalf("user hook changed: %#v", hooks)
		}
	}
}

func TestKimiOwnershipRequiresExactAgentAndScript(t *testing.T) {
	fixture := prepareKimiFixture(t, kimiFixtureOptions{})
	backupPath := filepath.Join(fixture.homeDir, ".elydora", kimiTestAgentID, "hook.js.backup")
	otherAgentPath := filepath.Join(fixture.homeDir, ".elydora", "agent-10", kimiAuditScript)
	userHooks := "[[hooks]]\nevent = \"PostToolUse\"\ncommand = " +
		strconv.Quote("node "+backupPath) + "\n\n[[hooks]]\nevent = \"PostToolUse\"\ncommand = " +
		strconv.Quote("node "+otherAgentPath) + "\n"
	if err := os.MkdirAll(filepath.Dir(fixture.modernPath), 0755); err != nil {
		t.Fatalf("create modern Kimi directory: %v", err)
	}
	if err := os.WriteFile(fixture.modernPath, []byte(userHooks), 0600); err != nil {
		t.Fatalf("write ownership fixture: %v", err)
	}
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Kimi hooks: %v", err)
	}
	if err := fixture.plugin.Uninstall(kimiTestAgentID); err != nil {
		t.Fatalf("uninstall Kimi hooks: %v", err)
	}
	hooks := readKimiTestHooks(t, fixture.modernPath)
	if len(hooks) != 2 {
		t.Fatalf("ownership filter removed user hooks: %#v", hooks)
	}
	commands := []any{hooks[0]["command"], hooks[1]["command"]}
	if !reflect.DeepEqual(commands, []any{"node " + backupPath, "node " + otherAgentPath}) {
		t.Fatalf("remaining commands = %#v", commands)
	}
}

func TestKimiUninstallRemovesOwnedConfigs(t *testing.T) {
	fixture := prepareKimiFixture(t, kimiFixtureOptions{})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Kimi hooks: %v", err)
	}
	if err := fixture.plugin.Uninstall(kimiTestAgentID); err != nil {
		t.Fatalf("uninstall Kimi hooks: %v", err)
	}
	for _, path := range []string{fixture.modernPath, fixture.legacyPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("owned Kimi config remains at %s: %v", path, err)
		}
	}
}

func TestKimiParsesEveryConfigBeforeRuntimeWrites(t *testing.T) {
	modern := "# untouched modern\ndefault_model = \"kimi-code/k3\"\n"
	legacy := "[malformed"
	fixture := prepareKimiFixture(t, kimiFixtureOptions{
		modernConfig: kimiString(modern),
		legacyConfig: kimiString(legacy),
	})
	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "parse kimi-cli legacy hooks config") {
		t.Fatalf("install error = %v", err)
	}
	for path, want := range map[string]string{fixture.modernPath: modern, fixture.legacyPath: legacy} {
		raw, readErr := os.ReadFile(path)
		if readErr != nil || string(raw) != want {
			t.Fatalf("config %s changed: %q, %v", path, raw, readErr)
		}
	}
	requireNoKimiRuntimeWrites(t, fixture)
}

func TestKimiRejectsInvalidHookContractsBeforeWrites(t *testing.T) {
	for _, testCase := range []struct{ name, raw, want string }{
		{"unsupported-field", "[[hooks]]\nevent=\"PreToolUse\"\ncommand=\"x\"\ncwd=\"/tmp\"\n", `unsupported field "cwd"`},
		{"unsupported-event", "[[hooks]]\nevent=\"Interrupt\"\ncommand=\"x\"\n", "unsupported event"},
		{"bad-matcher", "[[hooks]]\nevent=\"PreToolUse\"\ncommand=\"x\"\nmatcher=1\n", "matcher must be a string"},
		{"bad-timeout", "[[hooks]]\nevent=\"PreToolUse\"\ncommand=\"x\"\ntimeout=601\n", "integer from 1 to 600"},
		{"hooks-object", "hooks = { event=\"PreToolUse\", command=\"x\" }\n", `field "hooks" must be an array`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := prepareKimiFixture(t, kimiFixtureOptions{
				useDefaultHome: true,
				legacyConfig:   kimiString(testCase.raw),
			})
			err := fixture.plugin.Install(fixture.config)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("install error = %v, want %q", err, testCase.want)
			}
			raw, readErr := os.ReadFile(fixture.legacyPath)
			if readErr != nil || string(raw) != testCase.raw {
				t.Fatalf("invalid config changed: %q, %v", raw, readErr)
			}
			requireNoKimiRuntimeWrites(t, fixture)
		})
	}
}

func TestKimiRejectsMissingGuardBeforeWrites(t *testing.T) {
	fixture := prepareKimiFixture(t, kimiFixtureOptions{withoutGuard: true})
	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "guard runtime is missing") {
		t.Fatalf("install error = %v", err)
	}
	for _, path := range []string{fixture.modernPath, fixture.legacyPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("config write occurred at %s: %v", path, err)
		}
	}
	requireNoKimiRuntimeWrites(t, fixture)
}

func requireNoKimiRuntimeWrites(t *testing.T, fixture *kimiFixture) {
	t.Helper()
	for _, path := range []string{fixture.hookPath, fixture.runtimeConfig, fixture.privateKey} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("runtime write occurred at %s: %v", path, err)
		}
	}
}

func TestKimiStatusSurfacesMalformedRuntimeMetadata(t *testing.T) {
	fixture := prepareKimiFixture(t, kimiFixtureOptions{})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Kimi hooks: %v", err)
	}
	if err := os.WriteFile(fixture.runtimeConfig, []byte("{ malformed"), 0600); err != nil {
		t.Fatalf("corrupt runtime config: %v", err)
	}
	if _, err := fixture.plugin.Status(); err == nil || !strings.Contains(err.Error(), "parse Elydora runtime config") {
		t.Fatalf("status error = %v", err)
	}
}

func TestKimiAtomicWritesLeaveNoTemporaryFiles(t *testing.T) {
	fixture := prepareKimiFixture(t, kimiFixtureOptions{})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Kimi hooks: %v", err)
	}
	for _, directory := range []string{filepath.Dir(fixture.modernPath), filepath.Dir(fixture.legacyPath)} {
		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Fatalf("read config directory: %v", err)
		}
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".tmp") {
				t.Fatalf("temporary Kimi config remains at %s", filepath.Join(directory, entry.Name()))
			}
		}
	}
}
