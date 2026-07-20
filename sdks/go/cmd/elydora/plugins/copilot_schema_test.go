package plugins

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestCopilotSettingsPrecedenceAllowsLaterFalseAndPreservesJSONC(t *testing.T) {
	userSource := "{\n  // user policy\n  \"disableAllHooks\": true,\n}\n"
	claudeSource := "{\n  \"disableAllHooks\": true,\n}\n"
	repositorySource := "{\n  \"disableAllHooks\": false,\n}\n"
	fixture := prepareCopilotFixture(t, copilotFixtureOptions{
		userSettingsRaw:        copilotString(userSource),
		claudeLocalSettingsRaw: copilotString(claudeSource),
		repositorySettingsRaw:  copilotString(repositorySource),
	})

	installCopilotFixture(t, fixture)

	assertCopilotFileEquals(t, fixture.userSettings, userSource)
	assertCopilotFileEquals(t, fixture.claudeLocalSettings, claudeSource)
	assertCopilotFileEquals(t, fixture.repositorySettings, repositorySource)
	status, err := fixture.plugin.Status()
	if err != nil || !status.Installed {
		t.Fatalf("Copilot status = %#v, %v", status, err)
	}
}

func TestCopilotEffectiveDisabledSettingsRejectBeforeWrites(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		options copilotFixtureOptions
		want    string
	}{
		{
			name: "repository",
			options: copilotFixtureOptions{
				repositorySettingsRaw: copilotJSON(map[string]any{"disableAllHooks": true}),
			},
			want: "repository settings",
		},
		{
			name: "local repository",
			options: copilotFixtureOptions{
				claudeLocalSettingsRaw: copilotJSON(map[string]any{"disableAllHooks": true}),
				localSettingsRaw:       copilotJSON(map[string]any{"disableAllHooks": true}),
			},
			want: "local settings",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := prepareCopilotFixture(t, testCase.options)
			err := fixture.plugin.Install(fixture.config)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("install error = %v, want %q", err, testCase.want)
			}
			if _, err := os.Lstat(fixture.configPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("hook source exists after rejection: %v", err)
			}
			assertCopilotRuntimeAbsent(t, fixture)
		})
	}
}

func TestCopilotInvalidSettingsRemainUnchanged(t *testing.T) {
	for name, source := range map[string]string{
		"malformed":       "{ malformed",
		"duplicate":       `{"disableAllHooks":true,"disableAllHooks":false}`,
		"invalid boolean": `{"disableAllHooks":"yes"}`,
	} {
		t.Run(name, func(t *testing.T) {
			fixture := prepareCopilotFixture(t, copilotFixtureOptions{
				userSettingsRaw: copilotString(source),
			})
			if err := fixture.plugin.Install(fixture.config); err == nil {
				t.Fatal("install accepted invalid GitHub Copilot settings")
			}
			assertCopilotFileEquals(t, fixture.userSettings, source)
			assertCopilotRuntimeAbsent(t, fixture)
		})
	}
}

func TestCopilotRejectsCompleteInvalidHookSchemaBeforeWrites(t *testing.T) {
	tests := map[string]string{
		"duplicate key":   `{"version":1,"version":1,"hooks":{}}`,
		"boolean version": `{"version":true,"hooks":{}}`,
		"unknown event":   `{"version":1,"hooks":{"unknownEvent":[]}}`,
		"empty command":   `{"version":1,"hooks":{"preToolUse":[{}]}}`,
		"invalid command": `{"version":1,"hooks":{"preToolUse":[{"command":1}]}}`,
		"zero timeout":    `{"version":1,"hooks":{"preToolUse":[{"command":"x","timeoutSec":0}]}}`,
		"prompt event":    `{"version":1,"hooks":{"postToolUse":[{"type":"prompt","prompt":"x"}]}}`,
		"insecure HTTP":   `{"version":1,"hooks":{"postToolUse":[{"type":"http","url":"http://example.com"}]}}`,
		"invalid matcher": `{"version":1,"hooks":{"preToolUse":[{"command":"x","matcher":"["}]}}`,
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			fixture := prepareCopilotFixture(t, copilotFixtureOptions{
				userRaw: copilotString(source),
			})
			if err := fixture.plugin.Install(fixture.config); err == nil {
				t.Fatal("install accepted an invalid GitHub Copilot hook document")
			}
			assertCopilotFileEquals(t, fixture.configPath, source)
			assertCopilotRuntimeAbsent(t, fixture)
		})
	}
}

func TestCopilotMatchersUseJavaScriptRegularExpressionSyntax(t *testing.T) {
	fixture := prepareCopilotFixture(t, copilotFixtureOptions{
		userRaw: copilotJSON(map[string]any{
			"version": float64(1),
			"hooks": map[string]any{
				"preToolUse": []any{map[string]any{
					"type": "command", "command": "user-pre", "matcher": "(?<tool>shell)",
				}},
			},
		}),
	})

	installCopilotFixture(t, fixture)

	settings := readCopilotObject(t, fixture.configPath)
	hooks := requireCopilotObject(t, settings["hooks"])
	pre := requireCopilotArray(t, hooks["preToolUse"])
	if requireCopilotObject(t, pre[0])["matcher"] != "(?<tool>shell)" {
		t.Fatalf("preserved matcher = %#v", pre[0])
	}
}

func TestCopilotUninstallRemainsAvailableWithoutNodeMatcherValidation(t *testing.T) {
	fixture := prepareCopilotFixture(t, copilotFixtureOptions{})
	installCopilotFixture(t, fixture)
	settings := readCopilotObject(t, fixture.configPath)
	hooks := requireCopilotObject(t, settings["hooks"])
	pre := requireCopilotArray(t, hooks["preToolUse"])
	pre = append(pre, map[string]any{
		"type": "command", "command": "user-pre", "matcher": "[",
	})
	hooks["preToolUse"] = pre
	writeCopilotObject(t, fixture.configPath, settings)
	t.Setenv("PATH", "")

	if err := fixture.plugin.Uninstall(copilotTestAgentID); err != nil {
		t.Fatalf("uninstall GitHub Copilot hooks: %v", err)
	}

	remaining := readCopilotObject(t, fixture.configPath)
	remainingHooks := requireCopilotObject(t, remaining["hooks"])
	if len(requireCopilotArray(t, remainingHooks["preToolUse"])) != 1 {
		t.Fatalf("remaining hooks = %#v", remainingHooks)
	}
}
