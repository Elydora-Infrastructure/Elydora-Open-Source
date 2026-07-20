package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestQwenCommandsBlockAndForwardNativePayloadByteForByte(t *testing.T) {
	fixture := prepareQwenFixture(t, qwenFixtureOptions{})
	installQwenFixture(t, fixture)
	capturePath := filepath.Join(t.TempDir(), "captured-event.json")
	encodedPath, err := json.Marshal(capturePath)
	if err != nil {
		t.Fatalf("marshal capture path: %v", err)
	}
	guardScript := "const fs = require('node:fs');\n" +
		"fs.readFileSync(0);\n" +
		"process.stderr.write('Agent is frozen by Elydora.');\n" +
		"process.exit(2);\n"
	if err := os.WriteFile(fixture.guardPath, []byte(guardScript), 0700); err != nil {
		t.Fatalf("write blocking guard runtime: %v", err)
	}
	captureScript := "const fs = require('node:fs');\n" +
		"fs.writeFileSync(" + string(encodedPath) + ", fs.readFileSync(0));\n"
	if err := os.WriteFile(fixture.hookPath, []byte(captureScript), 0700); err != nil {
		t.Fatalf("write capture audit runtime: %v", err)
	}
	settings := readQwenTestObject(t, fixture.configPath)
	prePayload := `{"session_id":"session-1","cwd":"C:/workspace","hook_event_name":"PreToolUse","timestamp":"2026-07-19T00:00:00.000Z","tool_name":"run_shell_command","tool_input":{"command":"echo test"}}` + "\n"
	guard := qwenManagedHandler(t, settings, "PreToolUse", fixture.guardPath)
	guardResult := runQwenHandler(t, guard, fixture.homeDir, prePayload)
	if guardResult.exitCode != 2 || guardResult.stdout != "" ||
		!strings.Contains(guardResult.stderr, "Agent is frozen by Elydora") {
		t.Fatalf("guard result = %#v", guardResult)
	}
	for _, event := range []string{"PostToolUse", "PostToolUseFailure"} {
		payload := strings.Replace(prePayload, "PreToolUse", event, 1)
		handler := qwenManagedHandler(t, settings, event, fixture.hookPath)
		result := runQwenHandler(t, handler, fixture.homeDir, payload)
		if result.exitCode != 0 || result.stdout != "" || result.stderr != "" {
			t.Fatalf("%s result = %#v", event, result)
		}
		if readQwenTestFile(t, capturePath) != payload {
			t.Fatalf("%s changed the native Qwen Code payload", event)
		}
	}
}

func TestQwenPowerShellCommandPropagatesNodeExitCode(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PowerShell command contract is Windows-specific")
	}
	fixture := prepareQwenFixture(t, qwenFixtureOptions{})
	installQwenFixture(t, fixture)
	handler := qwenManagedHandler(
		t,
		readQwenTestObject(t, fixture.configPath),
		"PreToolUse",
		fixture.guardPath,
	)
	command := handler["command"].(string)
	if !strings.HasPrefix(command, "& '") ||
		!strings.HasSuffix(command, "; exit $LASTEXITCODE") {
		t.Fatalf("PowerShell command = %q", command)
	}
}

func TestQwenStatusRequiresEnabledExactTripleAndRuntimeFiles(t *testing.T) {
	fixture := prepareQwenFixture(t, qwenFixtureOptions{})
	installQwenFixture(t, fixture)
	status, err := fixture.plugin.Status()
	if err != nil || !status.Installed || !status.HookConfigured ||
		!status.HookScriptExists {
		t.Fatalf("installed status = %#v, %v", status, err)
	}

	settings := readQwenTestObject(t, fixture.configPath)
	hooks := requireQwenObject(t, settings["hooks"])
	failureGroups := requireQwenArray(t, hooks["PostToolUseFailure"])
	hooks["PostToolUseFailure"] = append(failureGroups, failureGroups[len(failureGroups)-1])
	writeQwenTestObject(t, fixture.configPath, settings)
	status, err = fixture.plugin.Status()
	if err != nil || status.HookConfigured || status.Installed {
		t.Fatalf("duplicate status = %#v, %v", status, err)
	}

	installQwenFixture(t, fixture)
	settings = readQwenTestObject(t, fixture.configPath)
	hooks = requireQwenObject(t, settings["hooks"])
	delete(hooks, "PostToolUseFailure")
	writeQwenTestObject(t, fixture.configPath, settings)
	status, err = fixture.plugin.Status()
	if err != nil || status.HookConfigured || status.Installed {
		t.Fatalf("incomplete status = %#v, %v", status, err)
	}

	installQwenFixture(t, fixture)
	writeOptionalQwenFile(
		t,
		fixture.systemConfig,
		qwenJSON(map[string]any{"disableAllHooks": true}),
	)
	status, err = fixture.plugin.Status()
	if err != nil || status.HookConfigured || status.Installed {
		t.Fatalf("disabled status = %#v, %v", status, err)
	}
	if err := os.Remove(fixture.systemConfig); err != nil {
		t.Fatalf("remove system settings: %v", err)
	}

	if err := os.Remove(fixture.guardPath); err != nil {
		t.Fatalf("remove guard runtime: %v", err)
	}
	status, err = fixture.plugin.Status()
	if err != nil || !status.HookConfigured || status.HookScriptExists || status.Installed {
		t.Fatalf("missing runtime status = %#v, %v", status, err)
	}
}

func TestQwenStatusRequiresCanonicalRuntimeContents(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*testing.T, *qwenFixture)
		wantErr string
	}{
		{
			"guard content",
			func(t *testing.T, f *qwenFixture) {
				if err := os.WriteFile(f.guardPath, []byte("tampered\n"), 0700); err != nil {
					t.Fatalf("tamper guard: %v", err)
				}
			},
			"",
		},
		{
			"audit content",
			func(t *testing.T, f *qwenFixture) {
				if err := os.WriteFile(f.hookPath, []byte("tampered\n"), 0700); err != nil {
					t.Fatalf("tamper audit: %v", err)
				}
			},
			"",
		},
		{
			"private key",
			func(t *testing.T, f *qwenFixture) {
				if err := os.WriteFile(f.privateKey, []byte("invalid"), 0600); err != nil {
					t.Fatalf("tamper private key: %v", err)
				}
			},
			"private key",
		},
		{
			"extra config field",
			func(t *testing.T, f *qwenFixture) {
				config := readQwenTestObject(t, f.runtimeConfig)
				config["extra"] = true
				writeQwenTestObject(t, f.runtimeConfig, config)
			},
			"unsupported field",
		},
		{
			"runtime identity",
			func(t *testing.T, f *qwenFixture) {
				config := readQwenTestObject(t, f.runtimeConfig)
				config["agent_id"] = "other-agent"
				writeQwenTestObject(t, f.runtimeConfig, config)
			},
			"identity does not match",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := prepareQwenFixture(t, qwenFixtureOptions{})
			installQwenFixture(t, fixture)
			test.mutate(t, fixture)
			status, err := fixture.plugin.Status()
			if test.wantErr == "" {
				if err != nil || status.Installed || status.HookScriptExists {
					t.Fatalf("status = %#v, %v", status, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) ||
				status.Installed || status.HookScriptExists {
				t.Fatalf("status = %#v, %v", status, err)
			}
		})
	}
}
