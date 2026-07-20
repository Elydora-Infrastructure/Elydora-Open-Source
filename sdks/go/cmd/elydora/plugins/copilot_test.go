package plugins

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestCopilotRegistryUsesNativeUserHooks(t *testing.T) {
	want := AgentRegistryEntry{
		Name:       "GitHub Copilot CLI",
		ConfigDir:  "~/.copilot/hooks",
		ConfigFile: "elydora-audit.json",
	}
	if got := SupportedAgents[copilotAgentKey]; !reflect.DeepEqual(got, want) {
		t.Fatalf("Copilot registry = %#v, want %#v", got, want)
	}
	plugin := NewPlugin(copilotAgentKey)
	manager, ok := plugin.(GuardRuntimeManager)
	if !ok || !manager.ManagesGuardRuntime() {
		t.Fatal("Copilot must own its generated guard transactionally")
	}
}

func TestCopilotInstallPreservesUserHooksMigratesLegacyAndIsIdempotent(t *testing.T) {
	fixture := prepareCopilotFixture(t, copilotFixtureOptions{
		userRaw: copilotJSON(map[string]any{
			"version":         float64(1),
			"disableAllHooks": false,
			"hooks": map[string]any{
				"sessionStart": []any{map[string]any{"type": "command", "command": "user-session"}},
				"preToolUse":   []any{map[string]any{"type": "command", "command": "user-pre"}},
			},
		}),
	})
	writeCopilotObject(t, fixture.legacyPath, legacyCopilotConfig(fixture, map[string]any{
		"notification": []any{map[string]any{"type": "command", "command": "user-notification"}},
	}))

	installCopilotFixture(t, fixture)
	installCopilotFixture(t, fixture)

	settings := readCopilotObject(t, fixture.configPath)
	if settings["disableAllHooks"] != false {
		t.Fatalf("disableAllHooks = %#v", settings["disableAllHooks"])
	}
	hooks := requireCopilotObject(t, settings["hooks"])
	if got := requireCopilotArray(t, hooks["sessionStart"]); len(got) != 1 {
		t.Fatalf("sessionStart hooks = %#v", got)
	}
	preHooks := requireCopilotArray(t, hooks["preToolUse"])
	postHooks := requireCopilotArray(t, hooks["postToolUse"])
	failureHooks := requireCopilotArray(t, hooks["postToolUseFailure"])
	if len(preHooks) != 2 || len(postHooks) != 1 || len(failureHooks) != 1 {
		t.Fatalf("Copilot hooks = %#v", hooks)
	}
	if requireCopilotObject(t, preHooks[0])["command"] != "user-pre" {
		t.Fatalf("first preToolUse hook = %#v", preHooks[0])
	}
	assertNativeCopilotHandler(t, managedCopilotTestHandler(
		t, settings, "preToolUse", copilotGuardScript,
	))
	assertNativeCopilotHandler(t, managedCopilotTestHandler(
		t, settings, "postToolUse", copilotAuditScript,
	))
	assertNativeCopilotHandler(t, managedCopilotTestHandler(
		t, settings, "postToolUseFailure", copilotAuditScript,
	))
	legacy := readCopilotObject(t, fixture.legacyPath)
	legacyHooks := requireCopilotObject(t, legacy["hooks"])
	if len(legacyHooks) != 1 || legacyHooks["notification"] == nil {
		t.Fatalf("migrated project hooks = %#v", legacyHooks)
	}
	runtimeConfig := readCopilotObject(t, fixture.runtimeConfig)
	if runtimeConfig["agent_name"] != copilotAgentKey ||
		runtimeConfig["agent_id"] != copilotTestAgentID {
		t.Fatalf("runtime config = %#v", runtimeConfig)
	}
	privateKey, err := os.ReadFile(fixture.privateKey)
	if err != nil || string(privateKey) != fixture.config.PrivateKey {
		t.Fatalf("private key = %q, %v", privateKey, err)
	}
	if exists, err := regularFileExists(fixture.hookPath, "test audit runtime"); err != nil || !exists {
		t.Fatalf("audit runtime exists = %v, %v", exists, err)
	}
}

func TestCopilotMigrationRemovesOwnedProjectFile(t *testing.T) {
	fixture := prepareCopilotFixture(t, copilotFixtureOptions{})
	writeCopilotObject(t, fixture.legacyPath, legacyCopilotConfig(fixture, nil))

	installCopilotFixture(t, fixture)

	if _, err := os.Stat(fixture.legacyPath); !os.IsNotExist(err) {
		t.Fatalf("owned project hook file remains: %v", err)
	}
	settings := readCopilotObject(t, fixture.configPath)
	assertNativeCopilotHandler(t, managedCopilotTestHandler(
		t, settings, "preToolUse", copilotGuardScript,
	))
}

func TestCopilotCommandsBlockAndForwardNativePayloadByteForByte(t *testing.T) {
	fixture := prepareCopilotFixture(t, copilotFixtureOptions{})
	installCopilotFixture(t, fixture)
	capturePath := filepath.Join(t.TempDir(), "captured-event.json")
	guardScript := "const fs = require('node:fs');\n" +
		"fs.readFileSync(0);\n" +
		"process.stderr.write('Agent is frozen by Elydora.');\n" +
		"process.exit(2);\n"
	if err := os.WriteFile(fixture.guardPath, []byte(guardScript), 0700); err != nil {
		t.Fatalf("write blocking guard runtime: %v", err)
	}
	captureScript := "const fs = require('node:fs');\n" +
		"fs.writeFileSync(process.env.ELYDORA_CAPTURE, fs.readFileSync(0));\n"
	if err := os.WriteFile(fixture.hookPath, []byte(captureScript), 0700); err != nil {
		t.Fatalf("write capture audit runtime: %v", err)
	}
	settings := readCopilotObject(t, fixture.configPath)
	prePayload := `{"sessionId":"session-1","timestamp":1,"cwd":"project","toolName":"powershell","toolArgs":{"command":"Get-ChildItem"}}` + "\n"
	guard := managedCopilotTestHandler(t, settings, "preToolUse", copilotGuardScript)
	guardResult := runCopilotHandler(t, guard, prePayload)
	if guardResult.exitCode != 2 || !strings.Contains(guardResult.stderr, "Agent is frozen by Elydora") {
		t.Fatalf("guard result = %#v", guardResult)
	}
	postPayload := `{"sessionId":"session-1","timestamp":2,"cwd":"project","toolName":"powershell","toolArgs":{"command":"Get-ChildItem"},"toolResult":{"resultType":"success","textResultForLlm":"ok"}}` + "\n"
	audit := managedCopilotTestHandler(t, settings, "postToolUse", copilotAuditScript)
	auditResult := runCopilotHandler(t, audit, postPayload, "ELYDORA_CAPTURE="+capturePath)
	if auditResult.exitCode != 0 {
		t.Fatalf("audit result = %#v", auditResult)
	}
	captured, err := os.ReadFile(capturePath)
	if err != nil || string(captured) != postPayload {
		t.Fatalf("captured payload = %q, %v", captured, err)
	}
	failurePayload := `{"sessionId":"session-1","timestamp":3,"cwd":"project","toolName":"powershell","toolArgs":{"command":"Get-ChildItem"},"error":"command failed"}` + "\n"
	failure := managedCopilotTestHandler(t, settings, "postToolUseFailure", copilotAuditScript)
	failureResult := runCopilotHandler(t, failure, failurePayload, "ELYDORA_CAPTURE="+capturePath)
	if failureResult.exitCode != 0 {
		t.Fatalf("failure audit result = %#v", failureResult)
	}
	captured, err = os.ReadFile(capturePath)
	if err != nil || string(captured) != failurePayload {
		t.Fatalf("captured failure payload = %q, %v", captured, err)
	}
}

func TestCopilotEmptyHomeOverrideUsesOfficialDefault(t *testing.T) {
	fixture := prepareCopilotFixture(t, copilotFixtureOptions{emptyOverride: true})
	installCopilotFixture(t, fixture)

	settings := readCopilotObject(t, fixture.configPath)
	assertNativeCopilotHandler(t, managedCopilotTestHandler(
		t, settings, "preToolUse", copilotGuardScript,
	))
	status, err := fixture.plugin.Status()
	if err != nil || status.ConfigPath != fixture.configPath {
		t.Fatalf("Copilot status = %#v, %v", status, err)
	}
}

func TestCopilotStatusRequiresEnabledPairIdentityAndRuntimeFiles(t *testing.T) {
	fixture := prepareCopilotFixture(t, copilotFixtureOptions{})
	installCopilotFixture(t, fixture)
	status, err := fixture.plugin.Status()
	if err != nil || !status.Installed || !status.HookConfigured || !status.HookScriptExists {
		t.Fatalf("installed status = %#v, %v", status, err)
	}

	settings := readCopilotObject(t, fixture.configPath)
	hooks := requireCopilotObject(t, settings["hooks"])
	delete(hooks, "postToolUseFailure")
	writeCopilotObject(t, fixture.configPath, settings)
	status, err = fixture.plugin.Status()
	if err != nil || status.HookConfigured || status.Installed {
		t.Fatalf("incomplete status = %#v, %v", status, err)
	}

	installCopilotFixture(t, fixture)
	settings = readCopilotObject(t, fixture.configPath)
	settings["disableAllHooks"] = true
	writeCopilotObject(t, fixture.configPath, settings)
	status, err = fixture.plugin.Status()
	if err != nil || status.HookConfigured || status.Installed {
		t.Fatalf("disabled status = %#v, %v", status, err)
	}

	settings["disableAllHooks"] = false
	writeCopilotObject(t, fixture.configPath, settings)
	if err := os.Remove(fixture.hookPath); err != nil {
		t.Fatalf("remove audit runtime: %v", err)
	}
	status, err = fixture.plugin.Status()
	if err != nil || !status.HookConfigured || status.HookScriptExists || status.Installed {
		t.Fatalf("missing runtime status = %#v, %v", status, err)
	}

	installCopilotFixture(t, fixture)
	runtimeConfig := readCopilotObject(t, fixture.runtimeConfig)
	runtimeConfig["agent_id"] = "another-agent"
	writeCopilotObject(t, fixture.runtimeConfig, runtimeConfig)
	status, err = fixture.plugin.Status()
	if err == nil || status.HookScriptExists || status.Installed {
		t.Fatalf("identity mismatch status = %#v, %v", status, err)
	}
}

func TestCopilotStatusRecognizesActiveProjectDelivery(t *testing.T) {
	fixture := prepareCopilotFixture(t, copilotFixtureOptions{})
	writeCopilotObject(t, fixture.legacyPath, legacyCopilotConfig(fixture, nil))
	writeCopilotObject(t, fixture.runtimeConfig, map[string]any{
		"org_id": "org-1", "agent_id": copilotTestAgentID, "kid": "kid-1",
		"base_url": "https://api.elydora.test", "agent_name": copilotAgentKey,
	})
	if err := os.WriteFile(fixture.privateKey, []byte(copilotTestPrivateKey), 0600); err != nil {
		t.Fatalf("write private key: %v", err)
	}
	if err := os.WriteFile(
		fixture.guardPath,
		[]byte(generateGuardScript(copilotAgentKey, copilotTestAgentID, "", false, "")),
		0700,
	); err != nil {
		t.Fatalf("write guard runtime: %v", err)
	}
	if err := os.WriteFile(
		fixture.hookPath,
		[]byte(buildHookScriptWithOutput(copilotAgentKey, copilotTestAgentID, "", false, true)),
		0700,
	); err != nil {
		t.Fatalf("write audit runtime: %v", err)
	}

	status, err := fixture.plugin.Status()
	if err != nil || !status.Installed || status.ConfigPath != fixture.legacyPath {
		t.Fatalf("project delivery status = %#v, %v", status, err)
	}
}

func TestCopilotUninstallRemovesExactOwnershipAndPreservesUserEntries(t *testing.T) {
	fixture := prepareCopilotFixture(t, copilotFixtureOptions{
		userRaw: copilotJSON(map[string]any{
			"version": float64(1),
			"hooks": map[string]any{
				"notification": []any{map[string]any{"type": "command", "command": "keep"}},
			},
		}),
	})
	installCopilotFixture(t, fixture)
	settings := readCopilotObject(t, fixture.configPath)
	hooks := requireCopilotObject(t, settings["hooks"])
	preHooks := requireCopilotArray(t, hooks["preToolUse"])
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		t.Fatalf("resolve Node.js: %v", err)
	}
	preHooks = append(preHooks,
		buildCopilotHandler(
			nodePath,
			filepath.Join(filepath.Dir(fixture.agentDir), "agent-10", copilotGuardScript),
		),
		map[string]any{
			"type": "command", "bash": "echo elydora", "powershell": "Write-Output elydora",
			"timeoutSec": copilotHookTimeout,
		},
	)
	hooks["preToolUse"] = preHooks
	writeCopilotObject(t, fixture.configPath, settings)
	uninstallID := copilotTestAgentID
	if runtime.GOOS == "windows" {
		uninstallID = strings.ToUpper(uninstallID)
	}
	if err := fixture.plugin.Uninstall(uninstallID); err != nil {
		t.Fatalf("uninstall GitHub Copilot hooks: %v", err)
	}

	remaining := readCopilotObject(t, fixture.configPath)
	remainingHooks := requireCopilotObject(t, remaining["hooks"])
	if remainingHooks["postToolUse"] != nil || remainingHooks["postToolUseFailure"] != nil ||
		len(requireCopilotArray(t, remainingHooks["preToolUse"])) != 2 {
		t.Fatalf("remaining hooks = %#v", remainingHooks)
	}
	if len(requireCopilotArray(t, remainingHooks["notification"])) != 1 {
		t.Fatalf("notification hooks = %#v", remainingHooks["notification"])
	}
}

func TestCopilotUninstallLeavesAbsentSourcesAbsent(t *testing.T) {
	fixture := prepareCopilotFixture(t, copilotFixtureOptions{})
	if err := fixture.plugin.Uninstall(copilotTestAgentID); err != nil {
		t.Fatalf("uninstall absent GitHub Copilot hooks: %v", err)
	}
	for _, path := range []string{fixture.configPath, fixture.legacyPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("absent source created at %s: %v", path, err)
		}
	}
}
