package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDroidUserSettingsDisableBeforeRuntimeCreation(t *testing.T) {
	fixture := prepareDroidFixture(t, droidFixtureOptions{
		settings: droidJSON(map[string]any{"hooksDisabled": true}),
	})
	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "hooksDisabled") {
		t.Fatalf("disabled hooks error = %v", err)
	}
	requireMissingDroidFile(t, fixture.runtimeConfig)
	requireMissingDroidFile(t, fixture.configPath)
}

func TestDroidUserLocalSettingsOverrideBaseSettings(t *testing.T) {
	fixture := prepareDroidFixture(t, droidFixtureOptions{
		settings:      droidJSON(map[string]any{"hooksDisabled": true}),
		localSettings: droidJSON(map[string]any{"hooksDisabled": false}),
	})
	installDroidFixture(t, fixture)
	if _, err := os.Stat(fixture.runtimeConfig); err != nil {
		t.Fatalf("runtime config is missing: %v", err)
	}
}

func TestDroidLegacyDirectFlagsRemainSafetyCompatible(t *testing.T) {
	fixture := prepareDroidFixture(t, droidFixtureOptions{
		legacy:   droidJSON(map[string]any{"hooksDisabled": true, "PreToolUse": []any{}}),
		settings: droidJSON(map[string]any{"hooksDisabled": false}),
	})
	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "legacy hooks") {
		t.Fatalf("legacy safety error = %v", err)
	}
	requireMissingDroidFile(t, fixture.runtimeConfig)
}

func TestDroidProjectPolicyHasExtensionOnlyPrecedence(t *testing.T) {
	t.Run("project blocks user allow", func(t *testing.T) {
		fixture := prepareDroidFixture(t, droidFixtureOptions{
			settings:        droidJSON(map[string]any{"hooksDisabled": false}),
			projectSettings: droidJSON(map[string]any{"hooksDisabled": true}),
		})
		err := fixture.plugin.Install(fixture.config)
		if err == nil || !strings.Contains(err.Error(), "project settings") {
			t.Fatalf("project policy error = %v", err)
		}
		requireMissingDroidFile(t, fixture.runtimeConfig)
	})
	t.Run("project allows through user block", func(t *testing.T) {
		fixture := prepareDroidFixture(t, droidFixtureOptions{
			settings:        droidJSON(map[string]any{"hooksDisabled": true}),
			projectSettings: droidJSON(map[string]any{"hooksDisabled": false}),
		})
		installDroidFixture(t, fixture)
	})
}

func TestDroidProjectLocalSettingsOverrideMatchingBase(t *testing.T) {
	fixture := prepareDroidFixture(t, droidFixtureOptions{
		projectSettings:      droidJSON(map[string]any{"hooksDisabled": false}),
		projectLocalSettings: droidJSON(map[string]any{"hooksDisabled": true}),
	})
	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "project local settings") {
		t.Fatalf("project local policy error = %v", err)
	}
}

func TestDroidProjectValuePrecedesDeeperFolderValue(t *testing.T) {
	fixture := prepareDroidFixture(t, droidFixtureOptions{
		projectSettings: droidJSON(map[string]any{"hooksDisabled": false}),
	})
	child := filepath.Join(fixture.workspaceDir, "packages", "console")
	if err := os.MkdirAll(child, 0755); err != nil {
		t.Fatalf("create child workspace: %v", err)
	}
	writeDroidTestObject(
		t,
		filepath.Join(child, ".factory", "settings.json"),
		map[string]any{"hooksDisabled": true},
	)
	if err := os.Chdir(child); err != nil {
		t.Fatalf("enter child workspace: %v", err)
	}
	installDroidFixture(t, fixture)
}

func TestDroidSystemManagedPolicyBlocksUserHooks(t *testing.T) {
	fixture := prepareDroidFixture(t, droidFixtureOptions{})
	writeDroidTestObject(
		t,
		fixture.systemSettingsPath,
		map[string]any{"allowManagedHooksOnly": true},
	)
	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "allowManagedHooksOnly") {
		t.Fatalf("managed policy error = %v", err)
	}
	requireMissingDroidFile(t, fixture.runtimeConfig)
}

func TestDroidMalformedReadOnlyProjectPolicyIsPreserved(t *testing.T) {
	malformed := "{ malformed"
	fixture := prepareDroidFixture(t, droidFixtureOptions{
		projectSettings: droidString(malformed),
	})
	err := fixture.plugin.Install(fixture.config)
	if err == nil || !strings.Contains(err.Error(), "project settings") {
		t.Fatalf("malformed project policy error = %v", err)
	}
	if readDroidTestFile(t, fixture.projectSettingsPath) != malformed {
		t.Fatal("malformed read-only policy changed")
	}
	requireMissingDroidFile(t, fixture.runtimeConfig)
	requireMissingDroidFile(t, fixture.configPath)
}
