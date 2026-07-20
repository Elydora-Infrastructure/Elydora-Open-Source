package plugins

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestKimiRegistryUsesStableGlobalContract(t *testing.T) {
	entry := SupportedAgents[kimiAgentKey]
	if entry.Name != "Kimi Code" || entry.ConfigDir != "~/.kimi-code" ||
		entry.ConfigFile != "config.toml" {
		t.Fatalf("Kimi registry entry = %#v", entry)
	}
	plugin, ok := NewPlugin(kimiAgentKey).(*KimiPlugin)
	if !ok || !plugin.ManagesGuardRuntime() {
		t.Fatalf("Kimi registry plugin = %T", NewPlugin(kimiAgentKey))
	}
}

func TestKimiInstallPreservesBothConfigsAndIsIdempotent(t *testing.T) {
	modern := "# stable user config\ndefault_model = \"kimi-code/k3\"\n\n" +
		"[[hooks]]\nevent = \"SessionStart\"\ncommand = \"existing-stable\"\n" +
		"timeout = 30 # keep stable hook\n"
	legacy := "# legacy user config\ntelemetry = false\n\n" +
		"[[hooks]]\nevent = \"SessionEnd\"\ncommand = \"existing-legacy\"\n"
	fixture := prepareKimiFixture(t, kimiFixtureOptions{
		modernConfig: kimiString(modern), legacyConfig: kimiString(legacy),
	})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Kimi hooks: %v", err)
	}
	first := map[string][]byte{}
	for _, path := range []string{fixture.modernPath, fixture.legacyPath} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read first Kimi source: %v", err)
		}
		first[path] = raw
	}
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("repeat Kimi install: %v", err)
	}

	for _, contract := range []struct{ path, comment, command string }{
		{fixture.modernPath, "# stable user config", "existing-stable"},
		{fixture.legacyPath, "# legacy user config", "existing-legacy"},
	} {
		raw, err := os.ReadFile(contract.path)
		if err != nil {
			t.Fatalf("read Kimi config: %v", err)
		}
		if !reflect.DeepEqual(raw, first[contract.path]) ||
			!strings.Contains(string(raw), contract.comment) ||
			!strings.Contains(string(raw), contract.command) {
			t.Fatalf("user config changed: %s", raw)
		}
		hooks := readKimiTestHooks(t, contract.path)
		if len(hooks) != 4 {
			t.Fatalf("hooks = %#v, want four entries", hooks)
		}
		requireKimiManagedTriple(t, hooks)
	}
	defaultStable := filepath.Join(fixture.homeDir, ".kimi-code", "config.toml")
	if _, err := os.Stat(defaultStable); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unselected default config exists: %v", err)
	}
	for _, path := range []string{
		fixture.runtimeConfig, fixture.privateKey, fixture.guardPath, fixture.hookPath,
	} {
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			t.Fatalf("managed runtime missing at %s: %v", path, err)
		}
	}
}

func TestKimiInstallPreservesInlineHookArrayStyle(t *testing.T) {
	modern := "# inline user hook\nhooks = [\n  # keep leading\n" +
		"  { event = \"SessionStart\", command = \"existing-inline\" }, # keep gap\n" +
		"] # keep array\n"
	fixture := prepareKimiFixture(t, kimiFixtureOptions{
		modernConfig: kimiString(modern), withoutLegacyEvidence: true,
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
		"# inline user hook", "# keep leading", "# keep gap", "# keep array", "hooks = [",
	} {
		if !strings.Contains(string(raw), marker) {
			t.Fatalf("inline marker %q missing from %s", marker, raw)
		}
	}
	hooks := readKimiTestHooks(t, fixture.modernPath)
	if len(hooks) != 4 {
		t.Fatalf("inline hooks = %#v", hooks)
	}
	requireKimiManagedTriple(t, hooks)
}

func TestKimiUninstallPreservesUserOwnedEmptyInlineArray(t *testing.T) {
	source := "# user container\nhooks = [] # keep empty array\n"
	fixture := prepareKimiFixture(t, kimiFixtureOptions{
		modernConfig: kimiString(source), withoutLegacyEvidence: true,
	})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Kimi hooks: %v", err)
	}
	if err := fixture.plugin.Uninstall(kimiTestAgentID); err != nil {
		t.Fatalf("uninstall Kimi hooks: %v", err)
	}
	raw, err := os.ReadFile(fixture.modernPath)
	if err != nil {
		t.Fatalf("read empty inline config: %v", err)
	}
	if !strings.Contains(string(raw), "# user container") ||
		!strings.Contains(string(raw), "# keep empty array") ||
		len(readKimiTestHooks(t, fixture.modernPath)) != 0 {
		t.Fatalf("empty inline array changed: %s", raw)
	}
}

func TestKimiUsesDefaultStableAndLegacyEvidence(t *testing.T) {
	t.Run("stable", func(t *testing.T) {
		fixture := prepareKimiFixture(t, kimiFixtureOptions{
			useDefaultHome: true, withoutLegacyEvidence: true,
		})
		if err := fixture.plugin.Install(fixture.config); err != nil {
			t.Fatalf("install stable Kimi hooks: %v", err)
		}
		requireKimiManagedTriple(t, readKimiTestHooks(t, fixture.modernPath))
		if _, err := os.Stat(fixture.legacyPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("legacy config exists: %v", err)
		}
	})

	t.Run("legacy", func(t *testing.T) {
		fixture := prepareKimiFixture(t, kimiFixtureOptions{
			useDefaultHome: true, withoutModernEvidence: true,
		})
		if err := fixture.plugin.Install(fixture.config); err != nil {
			t.Fatalf("install legacy Kimi hooks: %v", err)
		}
		requireKimiManagedTriple(t, readKimiTestHooks(t, fixture.legacyPath))
		if _, err := os.Stat(fixture.modernPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stable config exists: %v", err)
		}
		status, err := fixture.plugin.Status()
		if err != nil || !status.Installed {
			t.Fatalf("legacy Kimi status = %#v, %v", status, err)
		}
	})
}

func TestKimiParsesEverySelectedConfigBeforeRuntimeWrites(t *testing.T) {
	modern := "# untouched stable\ndefault_model = \"kimi-code/k3\"\n"
	legacy := "[malformed"
	fixture := prepareKimiFixture(t, kimiFixtureOptions{
		modernConfig: kimiString(modern), legacyConfig: kimiString(legacy),
	})
	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "parse kimi-cli legacy hooks config") {
		t.Fatalf("install error = %v", err)
	}
	for path, want := range map[string]string{
		fixture.modernPath: modern, fixture.legacyPath: legacy,
	} {
		raw, readErr := os.ReadFile(path)
		if readErr != nil || string(raw) != want {
			t.Fatalf("config %s changed: %q, %v", path, raw, readErr)
		}
	}
	assertNoKimiRuntimeWrites(t, fixture)
}

func TestKimiRejectsFieldsAndEventsOutsideEachContract(t *testing.T) {
	for _, testCase := range []struct {
		name, modern, legacy, want string
	}{
		{
			"unsupported-field",
			"[[hooks]]\nevent=\"PreToolUse\"\ncommand=\"x\"\ncwd=\"/tmp\"\n",
			"", `unsupported field "cwd"`,
		},
		{
			"legacy-stable-event", "",
			"[[hooks]]\nevent=\"Interrupt\"\ncommand=\"x\"\n",
			"unsupported event",
		},
		{
			"bad-timeout",
			"[[hooks]]\nevent=\"PreToolUse\"\ncommand=\"x\"\ntimeout=601\n",
			"", "integer from 1 to 600",
		},
		{
			"hooks-object", "hooks = { event=\"PreToolUse\", command=\"x\" }\n",
			"", `field "hooks" must be an array`,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			options := kimiFixtureOptions{}
			if testCase.modern != "" {
				options.modernConfig = kimiString(testCase.modern)
			}
			if testCase.legacy != "" {
				options.legacyConfig = kimiString(testCase.legacy)
			}
			fixture := prepareKimiFixture(t, options)
			err := fixture.plugin.Install(fixture.config)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("install error = %v, want %q", err, testCase.want)
			}
			assertNoKimiRuntimeWrites(t, fixture)
		})
	}
}

func TestKimiMigratesExactLegacyCommandsAndPreservesLookalikes(t *testing.T) {
	fixture := prepareKimiFixture(t, kimiFixtureOptions{withoutLegacyEvidence: true})
	guard := legacyKimiCommand(t, fixture.guardPath)
	audit := legacyKimiCommand(t, fixture.hookPath)
	lookalike := guard + " --inspect"
	source := "[[hooks]]\nevent = \"PreToolUse\"\ncommand = " + strconv.Quote(guard) +
		"\ntimeout = 10\n\n[[hooks]]\nevent = \"PostToolUse\"\ncommand = " +
		strconv.Quote(audit) + "\ntimeout = 10\n\n[[hooks]]\nevent = \"PreToolUse\"\ncommand = " +
		strconv.Quote(lookalike) + "\ntimeout = 10\n"
	writeOptionalKimiConfig(t, fixture.modernPath, kimiString(source))

	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("migrate legacy Kimi commands: %v", err)
	}
	hooks := readKimiTestHooks(t, fixture.modernPath)
	if len(hooks) != 4 {
		t.Fatalf("migrated hooks = %#v", hooks)
	}
	requireKimiManagedTriple(t, hooks)
	if !containsKimiCommand(hooks, lookalike) {
		t.Fatalf("lookalike command removed: %#v", hooks)
	}
	if err := fixture.plugin.Uninstall(kimiTestAgentID); err != nil {
		t.Fatalf("uninstall migrated Kimi hooks: %v", err)
	}
	hooks = readKimiTestHooks(t, fixture.modernPath)
	if len(hooks) != 1 || hooks[0]["command"] != lookalike {
		t.Fatalf("remaining hooks = %#v", hooks)
	}
}

func containsKimiCommand(hooks []map[string]any, command string) bool {
	for _, hook := range hooks {
		if hook["command"] == command {
			return true
		}
	}
	return false
}

func TestKimiUninstallPreservesUserConfigsAndRemovesManagedConfigs(t *testing.T) {
	userHook := "# user hook\n[[hooks]]\nevent = \"SessionStart\"\n" +
		"command = \"existing-command\"\ntimeout = 30 # keep timeout\n"
	fixture := prepareKimiFixture(t, kimiFixtureOptions{
		modernConfig: kimiString(userHook), legacyConfig: kimiString(userHook),
	})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Kimi hooks: %v", err)
	}
	if err := fixture.plugin.Uninstall(kimiTestAgentID); err != nil {
		t.Fatalf("uninstall Kimi hooks: %v", err)
	}
	for _, path := range []string{fixture.modernPath, fixture.legacyPath} {
		raw, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(raw), "# keep timeout") {
			t.Fatalf("user config changed: %s, %v", raw, err)
		}
		hooks := readKimiTestHooks(t, path)
		if len(hooks) != 1 || hooks[0]["command"] != "existing-command" {
			t.Fatalf("user hook changed: %#v", hooks)
		}
	}

	managed := prepareKimiFixture(t, kimiFixtureOptions{})
	if err := managed.plugin.Install(managed.config); err != nil {
		t.Fatalf("install managed Kimi configs: %v", err)
	}
	if err := managed.plugin.Uninstall(kimiTestAgentID); err != nil {
		t.Fatalf("remove managed Kimi configs: %v", err)
	}
	for _, path := range []string{managed.modernPath, managed.legacyPath} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("managed config remains at %s: %v", path, err)
		}
	}
}

func TestKimiStatusRequiresCompleteTripleAndEveryRuntimeFile(t *testing.T) {
	fixture := prepareKimiFixture(t, kimiFixtureOptions{withoutLegacyEvidence: true})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Kimi hooks: %v", err)
	}
	status, err := fixture.plugin.Status()
	if err != nil || !status.Installed || !status.HookConfigured {
		t.Fatalf("Kimi status = %#v, %v", status, err)
	}

	raw, err := os.ReadFile(fixture.modernPath)
	if err != nil {
		t.Fatalf("read Kimi config: %v", err)
	}
	document, err := parseKimiDocument(
		stableKimiContract(fixture.modernPath), raw, true,
	)
	if err != nil {
		t.Fatalf("parse Kimi config: %v", err)
	}
	keep := make([]int, 0, len(document.hooks)-1)
	for index, hook := range document.hooks {
		if hook.event != "PostToolUseFailure" {
			keep = append(keep, index)
		}
	}
	withoutFailure, err := renderKimiHooks(document, keep, nil)
	if err != nil {
		t.Fatalf("render incomplete Kimi hooks: %v", err)
	}
	if err := os.WriteFile(fixture.modernPath, withoutFailure, 0600); err != nil {
		t.Fatalf("write incomplete Kimi hooks: %v", err)
	}
	status, err = fixture.plugin.Status()
	if err != nil || status.Installed || status.HookConfigured {
		t.Fatalf("incomplete Kimi status = %#v, %v", status, err)
	}

	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("repair Kimi hooks: %v", err)
	}
	for _, path := range []string{
		fixture.guardPath, fixture.hookPath, fixture.runtimeConfig, fixture.privateKey,
	} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read runtime file: %v", err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatalf("remove runtime file: %v", err)
			}
			status, err := fixture.plugin.Status()
			if err != nil || status.Installed || !status.HookConfigured {
				t.Fatalf("degraded Kimi status = %#v, %v", status, err)
			}
			if err := os.WriteFile(path, content, 0600); err != nil {
				t.Fatalf("restore runtime file: %v", err)
			}
		})
	}
}

func TestKimiInstallationLeavesNoTransactionArtifacts(t *testing.T) {
	fixture := prepareKimiFixture(t, kimiFixtureOptions{})
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Kimi hooks: %v", err)
	}
	assertNoKimiTransactionArtifacts(t, fixture.homeDir)
	if runtime.GOOS != "windows" {
		for _, path := range []string{
			fixture.modernPath, fixture.legacyPath,
			fixture.runtimeConfig, fixture.privateKey,
		} {
			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("inspect mode for %s: %v", path, err)
			}
			if info.Mode().Perm() != 0600 {
				t.Fatalf("mode for %s = %v", path, info.Mode().Perm())
			}
		}
	}
}

func TestKimiRuntimeConfigOmitsEmptyOptionalToken(t *testing.T) {
	fixture := prepareKimiFixture(t, kimiFixtureOptions{withoutLegacyEvidence: true})
	fixture.config.Token = ""
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Kimi hooks without token: %v", err)
	}
	raw, err := os.ReadFile(fixture.runtimeConfig)
	if err != nil {
		t.Fatalf("read runtime config: %v", err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("decode runtime config: %v", err)
	}
	if _, exists := config["token"]; exists {
		t.Fatalf("empty optional token persisted: %#v", config)
	}
}
