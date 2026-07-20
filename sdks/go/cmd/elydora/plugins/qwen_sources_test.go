package plugins

import (
	"os"
	"path/filepath"
	"testing"
)

func TestQwenHomeUsesOfficialEnvironmentDiscovery(t *testing.T) {
	fixture := prepareQwenFixture(t, qwenFixtureOptions{})
	firstHome := filepath.Join(filepath.Dir(fixture.homeDir), "first # qwen home")
	secondHome := filepath.Join(filepath.Dir(fixture.homeDir), "second qwen home")
	writeOptionalQwenFile(
		t,
		filepath.Join(fixture.qwenDir, ".env"),
		qwenString(
			"export QWEN_HOME = \""+firstHome+"\" # selected by Qwen\n"+
				"QWEN_RUNTIME_DIR=runtime-one\n",
		),
	)
	writeOptionalQwenFile(
		t,
		filepath.Join(fixture.homeDir, ".env"),
		qwenString("QWEN_HOME="+secondHome+"\n"),
	)
	writeOptionalQwenFile(
		t,
		filepath.Join(firstHome, ".env"),
		qwenString("QWEN_HOME="+secondHome+"\nQWEN_RUNTIME_DIR=runtime-two\n"),
	)
	installQwenFixture(t, fixture)
	selected := filepath.Join(firstHome, "settings.json")
	qwenManagedHandler(t, readQwenTestObject(t, selected), "PreToolUse", fixture.guardPath)
	requireMissingQwenFile(t, filepath.Join(secondHome, "settings.json"))
	requireMissingQwenFile(t, fixture.configPath)
	status, err := fixture.plugin.Status()
	if err != nil || status.ConfigPath != selected || !status.Installed {
		t.Fatalf("Qwen status = %#v, %v", status, err)
	}
}

func TestQwenExplicitHomeOwnershipSupportsRelativeTildeAndEmptyValues(t *testing.T) {
	tests := []struct {
		name, value string
		expected    func(*qwenFixture) string
	}{
		{
			"relative",
			"relative-qwen",
			func(f *qwenFixture) string {
				return filepath.Join(f.workspaceDir, "relative-qwen", "settings.json")
			},
		},
		{
			"tilde",
			"~/custom-qwen",
			func(f *qwenFixture) string {
				return filepath.Join(f.homeDir, "custom-qwen", "settings.json")
			},
		},
		{"empty", "", func(f *qwenFixture) string { return f.configPath }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := prepareQwenFixture(t, qwenFixtureOptions{})
			ignored := filepath.Join(filepath.Dir(fixture.homeDir), "ignored-qwen-home")
			writeOptionalQwenFile(
				t,
				filepath.Join(fixture.qwenDir, ".env"),
				qwenString("QWEN_HOME="+ignored+"\n"),
			)
			t.Setenv("QWEN_HOME", test.value)
			installQwenFixture(t, fixture)
			selected := test.expected(fixture)
			qwenManagedHandler(
				t,
				readQwenTestObject(t, selected),
				"PreToolUse",
				fixture.guardPath,
			)
			requireMissingQwenFile(t, filepath.Join(ignored, "settings.json"))
		})
	}
}

func TestQwenRoutingReadsDiscoveredHomeEnvironment(t *testing.T) {
	fixture := prepareQwenFixture(t, qwenFixtureOptions{})
	discovered := filepath.Join(filepath.Dir(fixture.homeDir), "discovered")
	writeOptionalQwenFile(
		t,
		filepath.Join(fixture.qwenDir, ".env"),
		qwenString("QWEN_HOME="+discovered+"\n"),
	)
	discoveredEnv := filepath.Join(discovered, ".env")
	writeOptionalQwenFile(
		t,
		discoveredEnv,
		qwenString("QWEN_RUNTIME_DIR=runtime-output\n"),
	)
	routing, err := resolveQwenRouting()
	if err != nil {
		t.Fatalf("resolve Qwen routing: %v", err)
	}
	if !sameQwenPath(routing.qwenHome, discovered) {
		t.Fatalf("Qwen home = %s", routing.qwenHome)
	}
	found := false
	for _, condition := range routing.preconditions {
		found = found || sameQwenPath(condition.filePath, discoveredEnv)
	}
	if !found {
		t.Fatalf("discovered environment is absent from preconditions: %#v", routing.preconditions)
	}
}

func TestQwenRoutingSkipsDotenvWhenBothEnvironmentValuesExist(t *testing.T) {
	fixture := prepareQwenFixture(t, qwenFixtureOptions{})
	explicit := filepath.Join(filepath.Dir(fixture.homeDir), "explicit")
	writeOptionalQwenFile(
		t,
		filepath.Join(fixture.qwenDir, ".env"),
		qwenString("QWEN_HOME=ignored\n"),
	)
	t.Setenv("QWEN_HOME", explicit)
	t.Setenv("QWEN_RUNTIME_DIR", "runtime")
	routing, err := resolveQwenRouting()
	if err != nil {
		t.Fatalf("resolve Qwen routing: %v", err)
	}
	if !sameQwenPath(routing.qwenHome, explicit) || len(routing.preconditions) != 0 {
		t.Fatalf("routing = %#v", routing)
	}
}

func TestQwenEffectiveDisableUsesOfficialLayerOrderAndTrust(t *testing.T) {
	tests := []struct {
		name           string
		trustLevel     string
		systemDisabled *bool
		wantDisabled   bool
		wantSource     string
	}{
		{"untrusted workspace", "DO_NOT_TRUST", nil, false, qwenUserKind},
		{"trusted workspace", "TRUST_FOLDER", nil, true, qwenWorkspaceKind},
		{"system override", "TRUST_FOLDER", qwenBool(false), false, qwenSystemKind},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := prepareQwenFixture(t, qwenFixtureOptions{
				settings: qwenJSON(map[string]any{
					"disableAllHooks": false,
					"security": map[string]any{
						"folderTrust": map[string]any{"enabled": true},
					},
				}),
			})
			writeOptionalQwenFile(
				t,
				fixture.systemDefaults,
				qwenJSON(map[string]any{"disableAllHooks": true}),
			)
			writeOptionalQwenFile(
				t,
				fixture.workspaceConfig,
				qwenJSON(map[string]any{"disableAllHooks": true}),
			)
			if test.systemDisabled != nil {
				writeOptionalQwenFile(
					t,
					fixture.systemConfig,
					qwenJSON(map[string]any{"disableAllHooks": *test.systemDisabled}),
				)
			}
			writeOptionalQwenFile(
				t,
				fixture.trustedFolders,
				qwenJSON(map[string]any{fixture.workspaceDir: test.trustLevel}),
			)
			sources, err := readQwenSources()
			if err != nil {
				t.Fatalf("read Qwen sources: %v", err)
			}
			if sources.workspaceTrusted != (test.trustLevel == "TRUST_FOLDER") ||
				sources.disableControl.disabled != test.wantDisabled ||
				sources.disableControl.source == nil ||
				sources.disableControl.source.kind != test.wantSource {
				t.Fatalf("sources = %#v", sources)
			}
		})
	}
}

func TestQwenWorkspaceAtHomeIsInactive(t *testing.T) {
	fixture := prepareQwenFixture(t, qwenFixtureOptions{})
	if err := os.Chdir(fixture.homeDir); err != nil {
		t.Fatalf("enter home directory: %v", err)
	}
	sources, err := readQwenSources()
	if err != nil {
		t.Fatalf("read Qwen sources: %v", err)
	}
	if sources.workspaceActive || sources.workspaceTrusted {
		t.Fatalf("workspace state = active:%v trusted:%v", sources.workspaceActive, sources.workspaceTrusted)
	}
}

func qwenBool(value bool) *bool {
	return &value
}
