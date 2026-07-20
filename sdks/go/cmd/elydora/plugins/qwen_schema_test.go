package plugins

import (
	"strings"
	"testing"
)

func TestQwenSchemaTracksOfficialEventAndMatcherSets(t *testing.T) {
	expectedEvents := []string{
		"PreToolUse",
		"PostToolUse",
		"PostToolUseFailure",
		"PostToolBatch",
		"Notification",
		"UserPromptSubmit",
		"UserPromptExpansion",
		"SessionStart",
		"Stop",
		"MessageDisplay",
		"SubagentStart",
		"SubagentStop",
		"PreCompact",
		"PostCompact",
		"SessionEnd",
		"PermissionRequest",
		"PermissionDenied",
		"StopFailure",
		"TodoCreated",
		"TodoCompleted",
		"InstructionsLoaded",
	}
	if len(qwenEventNames) != len(expectedEvents) {
		t.Fatalf("known event count = %d", len(qwenEventNames))
	}
	for _, event := range expectedEvents {
		if _, exists := qwenEventNames[event]; !exists {
			t.Fatalf("official event is missing: %s", event)
		}
	}
	if len(qwenRegexMatcherEvents) != 15 {
		t.Fatalf("matcher event count = %d", len(qwenRegexMatcherEvents))
	}
	hooks := qwenHookSettings{}
	for _, event := range expectedEvents {
		hooks[event] = []any{map[string]any{
			"matcher": "tool-(?<name>.+)",
			"hooks":   []any{},
		}}
	}
	entries := collectQwenRegexes([]qwenHookSettings{hooks})
	if len(entries) != 15 {
		t.Fatalf("collected matcher count = %d", len(entries))
	}
}

func TestQwenMatcherValidationUsesJavaScriptRegExp(t *testing.T) {
	t.Run("JavaScript named capture", func(t *testing.T) {
		fixture := prepareQwenFixture(t, qwenFixtureOptions{
			settings: qwenString(`{
  "hooks": {
    "PreToolUse": [{ "matcher": "(?<tool>read|write)", "hooks": [] }]
  }
}`),
		})
		installQwenFixture(t, fixture)
	})

	t.Run("Python named capture", func(t *testing.T) {
		fixture := prepareQwenFixture(t, qwenFixtureOptions{
			settings: qwenString(`{
  "hooks": {
    "PreToolUse": [{ "matcher": "(?P<tool>read|write)", "hooks": [] }]
  }
}`),
		})
		err := fixture.plugin.Install(fixture.config)
		if err == nil || !strings.Contains(err.Error(), "JavaScript regular expression") {
			t.Fatalf("install error = %v", err)
		}
		requireMissingQwenFile(t, fixture.guardPath)
	})
}

func TestQwenSchemaAcceptsOfficialCommandHTTPAndPromptHandlers(t *testing.T) {
	settings := `{
  "hooks": {
    "PreToolUse": [{
      "matcher": "read|write",
      "sequential": true,
      "hooks": [
        {
          "type": "command",
          "name": "command-hook",
          "command": "echo test",
          "shell": "powershell",
          "timeout": 1000,
          "env": { "MODE": "test" },
          "async": false,
          "description": "command"
        },
        {
          "type": "http",
          "name": "http-hook",
          "url": "https://hooks.example.test",
          "headers": { "Authorization": "Bearer token" },
          "allowedEnvVars": ["HOME"],
          "once": true,
          "if": "always"
        },
        {
          "type": "prompt",
          "name": "prompt-hook",
          "prompt": "Review this tool call",
          "model": "qwen3-coder"
        }
      ]
    }]
  }
}`
	fixture := prepareQwenFixture(t, qwenFixtureOptions{settings: qwenString(settings)})
	installQwenFixture(t, fixture)
	raw := readQwenTestFile(t, fixture.configPath)
	for _, marker := range []string{"command-hook", "http-hook", "prompt-hook"} {
		if !strings.Contains(raw, marker) {
			t.Fatalf("handler %q was lost", marker)
		}
	}
}

func TestQwenSchemaRejectsInvalidOfficialHandlerFields(t *testing.T) {
	tests := []struct {
		name, handler, pattern string
	}{
		{
			"command env",
			`{"type":"command","command":"x","env":{"A":1}}`,
			"map names to strings",
		},
		{
			"command async",
			`{"type":"command","command":"x","async":"yes"}`,
			"must be a boolean",
		},
		{
			"command shell",
			`{"type":"command","command":"x","shell":"cmd"}`,
			"shell must be",
		},
		{
			"http headers",
			`{"type":"http","url":"https://x.test","headers":{"A":1}}`,
			"map names to strings",
		},
		{
			"http environment",
			`{"type":"http","url":"https://x.test","allowedEnvVars":[1]}`,
			"array of strings",
		},
		{
			"prompt model",
			`{"type":"prompt","prompt":"x","model":1}`,
			"must be a string",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := `{"hooks":{"PreToolUse":[{"hooks":[` + test.handler + `]}]}}`
			fixture := prepareQwenFixture(
				t,
				qwenFixtureOptions{settings: qwenString(source)},
			)
			err := fixture.plugin.Install(fixture.config)
			if err == nil || !strings.Contains(err.Error(), test.pattern) {
				t.Fatalf("install error = %v", err)
			}
			requireMissingQwenFile(t, fixture.guardPath)
		})
	}
}
