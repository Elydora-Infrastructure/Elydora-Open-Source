package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestKiroIdeRegistryFactoryAndRuntimeOwnership(t *testing.T) {
	entry := SupportedAgents[kiroIdeAgentKey]
	if entry.Name != "Kiro IDE" || entry.ConfigDir != ".kiro/hooks" ||
		entry.ConfigFile != kiroIdeConfigFile {
		t.Fatalf("Kiro IDE registry entry = %#v", entry)
	}
	plugin, ok := NewPlugin(kiroIdeAgentKey).(*KiroIdePlugin)
	if !ok || !plugin.ManagesGuardRuntime() {
		t.Fatalf("Kiro IDE plugin = %#v", NewPlugin(kiroIdeAgentKey))
	}
}

func TestKiroIdeInstallUsesWorkspaceV1ContractAndIsIdempotent(t *testing.T) {
	userHook := map[string]any{
		"name": "workspace-context", "trigger": "SessionStart",
		"action":  map[string]any{"type": "agent", "prompt": "Read AGENTS.md"},
		"enabled": true,
	}
	existing := map[string]any{
		"version": "v1", "owner": "workspace", "hooks": []any{userHook},
	}
	raw, _ := json.Marshal(existing)
	fixture := prepareKiroIdeFixture(t, kiroIdeString(string(raw)), nil)
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Kiro IDE hooks: %v", err)
	}
	first, err := os.ReadFile(fixture.configPath)
	if err != nil {
		t.Fatalf("read Kiro IDE hooks: %v", err)
	}
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("repeat Kiro IDE install: %v", err)
	}
	second, err := os.ReadFile(fixture.configPath)
	if err != nil || string(second) != string(first) {
		t.Fatalf("idempotent Kiro IDE source changed: %v", err)
	}
	document := readKiroIdeObject(t, fixture.configPath)
	if document["version"] != "v1" || document["owner"] != "workspace" {
		t.Fatalf("Kiro IDE root changed: %#v", document)
	}
	if !reflect.DeepEqual(requireArray(t, document["hooks"])[0], userHook) {
		t.Fatalf("user hook changed: %#v", requireArray(t, document["hooks"])[0])
	}
	for _, item := range []struct{ name, trigger string }{
		{kiroIdeGuardName, "PreToolUse"},
		{kiroIdeAuditName, "PostToolUse"},
	} {
		hook := findKiroIdeHook(t, document, item.name)
		if hook["trigger"] != item.trigger || hook["matcher"] != ".*" ||
			hook["timeout"] != float64(10) || hook["enabled"] != true ||
			requireObject(t, hook["action"])["type"] != "command" {
			t.Fatalf("%s hook = %#v", item.name, hook)
		}
		command := kiroIdeHookCommand(t, hook)
		if runtime.GOOS == "windows" && !strings.Contains(command, " -EncodedCommand ") {
			t.Fatalf("Windows Kiro IDE command is not encoded PowerShell: %q", command)
		}
	}
	runtimeConfig := readKiroIdeObject(t, fixture.runtimeConfig)
	if runtimeConfig["agent_name"] != kiroIdeAgentKey ||
		runtimeConfig["agent_id"] != kiroIdeTestAgentID ||
		runtimeConfig["org_id"] != "org-1" || runtimeConfig["kid"] != "kid-1" {
		t.Fatalf("runtime config = %#v", runtimeConfig)
	}
	key, err := os.ReadFile(fixture.privateKey)
	if err != nil || string(key) != kiroIdePrivateKey {
		t.Fatalf("private key = %q, %v", key, err)
	}
	if _, err := os.Lstat(filepath.Join(fixture.homeDir, ".kiro", "hooks", kiroIdeConfigFile)); !os.IsNotExist(err) {
		t.Fatalf("global Kiro IDE hook file exists: %v", err)
	}
	assertNoKiroIdeTransactionArtifacts(t, fixture.homeDir)
	assertNoKiroIdeTransactionArtifacts(t, fixture.workspace)
}

func TestKiroIdeCommandsBlockAndPreserveNativePayload(t *testing.T) {
	fixture := prepareKiroIdeFixture(t, nil, nil)
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Kiro IDE hooks: %v", err)
	}
	document := readKiroIdeObject(t, fixture.configPath)
	cache := map[string]any{
		"status": "frozen", "cached_at": float64(time.Now().UnixMilli()),
	}
	writeKiroIdeJSON(t, filepath.Join(fixture.agentDir, "status-cache.json"), cache)
	payload := map[string]any{
		"hook_event_name": "preToolUse", "cwd": fixture.workspace,
		"session_id": "session-1", "tool_name": "execute_bash",
		"tool_input":   map[string]any{"command": "go test ./..."},
		"future_field": map[string]any{"retained": true},
	}
	encoded, _ := json.Marshal(payload)
	exitCode, stdout, stderr := runKiroIdeCommand(
		t,
		kiroIdeHookCommand(t, findKiroIdeHook(t, document, kiroIdeGuardName)),
		fixture,
		encoded,
	)
	if exitCode != 2 || stdout != "" || !strings.Contains(stderr, "Tool execution blocked") {
		t.Fatalf("guard result = %d, %q, %q", exitCode, stdout, stderr)
	}

	capturePath := filepath.Join(fixture.workspace, "captured-event.json")
	captureJSON, _ := json.Marshal(capturePath)
	captureScript := "const fs=require('node:fs');const chunks=[];" +
		"process.stdin.on('data',c=>chunks.push(c));" +
		"process.stdin.on('end',()=>fs.writeFileSync(" + string(captureJSON) + ",Buffer.concat(chunks)));\n"
	if err := os.WriteFile(fixture.auditPath, []byte(captureScript), 0700); err != nil {
		t.Fatalf("replace audit runtime with capture: %v", err)
	}
	payload["hook_event_name"] = "postToolUse"
	payload["tool_response"] = map[string]any{"success": true, "result": "ok"}
	encoded, _ = json.Marshal(payload)
	exitCode, stdout, stderr = runKiroIdeCommand(
		t,
		kiroIdeHookCommand(t, findKiroIdeHook(t, document, kiroIdeAuditName)),
		fixture,
		encoded,
	)
	if exitCode != 0 || stdout != "" || stderr != "" {
		t.Fatalf("audit result = %d, %q, %q", exitCode, stdout, stderr)
	}
	captured, err := os.ReadFile(capturePath)
	if err != nil || string(captured) != string(encoded) {
		t.Fatalf("captured Kiro IDE payload = %q, %v", captured, err)
	}
}

func TestKiroIdeStatusRequiresUniqueExactPairAndRuntimeIntegrity(t *testing.T) {
	fixture := prepareKiroIdeFixture(t, nil, nil)
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Kiro IDE hooks: %v", err)
	}
	assertStatus := func(installed, configured bool) {
		t.Helper()
		status, err := fixture.plugin.Status()
		if err != nil || status.Installed != installed ||
			status.HookConfigured != configured {
			t.Fatalf("Kiro IDE status = %#v, %v", status, err)
		}
	}
	assertStatus(true, true)
	document := readKiroIdeObject(t, fixture.configPath)
	findKiroIdeHook(t, document, kiroIdeGuardName)["enabled"] = false
	writeKiroIdeJSON(t, fixture.configPath, document)
	assertStatus(false, false)
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("repair Kiro IDE hooks: %v", err)
	}
	document = readKiroIdeObject(t, fixture.configPath)
	document["hooks"] = append(
		requireArray(t, document["hooks"]),
		cloneKiroIdeObject(findKiroIdeHook(t, document, kiroIdeGuardName)),
	)
	writeKiroIdeJSON(t, fixture.configPath, document)
	assertStatus(false, false)
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("repair duplicate Kiro IDE hooks: %v", err)
	}
	if err := os.WriteFile(fixture.guardPath, []byte("tampered\n"), 0700); err != nil {
		t.Fatalf("tamper guard runtime: %v", err)
	}
	assertStatus(false, true)
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("repair Kiro IDE runtime: %v", err)
	}
	validRuntimeConfig, err := os.ReadFile(fixture.runtimeConfig)
	if err != nil {
		t.Fatalf("read valid runtime config: %v", err)
	}
	if err := os.WriteFile(fixture.runtimeConfig, []byte("{ malformed"), 0600); err != nil {
		t.Fatalf("corrupt runtime config: %v", err)
	}
	if _, err := fixture.plugin.Status(); err == nil ||
		!strings.Contains(err.Error(), "parse Elydora runtime config") {
		t.Fatalf("malformed runtime status error = %v", err)
	}
	if err := os.WriteFile(fixture.runtimeConfig, validRuntimeConfig, 0600); err != nil {
		t.Fatalf("restore runtime config: %v", err)
	}
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("repair runtime config: %v", err)
	}
	if err := os.WriteFile(fixture.privateKey, []byte("invalid"), 0600); err != nil {
		t.Fatalf("corrupt private key: %v", err)
	}
	if _, err := fixture.plugin.Status(); err == nil ||
		!strings.Contains(err.Error(), "canonical 32-byte") {
		t.Fatalf("invalid private key status error = %v", err)
	}
}

func TestKiroIdeStatusRejectsExtraOrphanAndStaleRuntimeCommands(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*testing.T, *kiroIdeFixture, map[string]any)
	}{
		{
			name: "extra complete pair",
			mutate: func(t *testing.T, fixture *kiroIdeFixture, document map[string]any) {
				nodePath, err := resolveNodeRuntime()
				if err != nil {
					t.Fatalf("resolve Node.js: %v", err)
				}
				agentDirectory := filepath.Join(fixture.homeDir, ".elydora", "agent-2")
				guard, _ := buildKiroIdeHook(
					kiroIdeGuardName,
					nodePath,
					filepath.Join(agentDirectory, kiroIdeGuardScript),
				)
				audit, _ := buildKiroIdeHook(
					kiroIdeAuditName,
					nodePath,
					filepath.Join(agentDirectory, kiroIdeAuditScript),
				)
				document["hooks"] = append(requireArray(t, document["hooks"]), guard, audit)
			},
		},
		{
			name: "orphan guard",
			mutate: func(t *testing.T, fixture *kiroIdeFixture, document map[string]any) {
				nodePath, err := resolveNodeRuntime()
				if err != nil {
					t.Fatalf("resolve Node.js: %v", err)
				}
				guard, _ := buildKiroIdeHook(
					kiroIdeGuardName,
					nodePath,
					filepath.Join(
						fixture.homeDir,
						".elydora",
						"agent-2",
						kiroIdeGuardScript,
					),
				)
				document["hooks"] = append(requireArray(t, document["hooks"]), guard)
			},
		},
		{
			name: "split agent pair",
			mutate: func(t *testing.T, fixture *kiroIdeFixture, document map[string]any) {
				nodePath, err := resolveNodeRuntime()
				if err != nil {
					t.Fatalf("resolve Node.js: %v", err)
				}
				audit, _ := buildKiroIdeHook(
					kiroIdeAuditName,
					nodePath,
					filepath.Join(
						fixture.homeDir,
						".elydora",
						"agent-2",
						kiroIdeAuditScript,
					),
				)
				hooks := requireArray(t, document["hooks"])
				for index, value := range hooks {
					if requireObject(t, value)["name"] == kiroIdeAuditName {
						hooks[index] = audit
					}
				}
			},
		},
		{
			name: "stale Node runtime",
			mutate: func(t *testing.T, fixture *kiroIdeFixture, document map[string]any) {
				name := "node"
				if runtime.GOOS == "windows" {
					name = "node.exe"
				}
				staleNode := filepath.Join(fixture.homeDir, "old-node", name)
				command, err := buildKiroIdeCommand(staleNode, fixture.guardPath)
				if err != nil {
					t.Fatalf("build stale Kiro IDE command: %v", err)
				}
				requireObject(
					t,
					findKiroIdeHook(t, document, kiroIdeGuardName)["action"],
				)["command"] = command
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := prepareKiroIdeFixture(t, nil, nil)
			if err := fixture.plugin.Install(fixture.config); err != nil {
				t.Fatalf("install Kiro IDE hooks: %v", err)
			}
			document := readKiroIdeObject(t, fixture.configPath)
			testCase.mutate(t, fixture, document)
			writeKiroIdeJSON(t, fixture.configPath, document)
			status, err := fixture.plugin.Status()
			if err != nil || status.Installed || status.HookConfigured {
				t.Fatalf("mutated Kiro IDE status = %#v, %v", status, err)
			}
			if err := fixture.plugin.Install(fixture.config); err != nil {
				t.Fatalf("repair Kiro IDE command contract: %v", err)
			}
			status, err = fixture.plugin.Status()
			if err != nil || !status.Installed {
				t.Fatalf("repaired Kiro IDE status = %#v, %v", status, err)
			}
		})
	}
}

func TestKiroIdeStatusRejectsStaleWindowsPowerShellLauncher(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell launcher contract")
	}
	fixture := prepareKiroIdeFixture(t, nil, nil)
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Kiro IDE hooks: %v", err)
	}
	document := readKiroIdeObject(t, fixture.configPath)
	guard := findKiroIdeHook(t, document, kiroIdeGuardName)
	action := requireObject(t, guard["action"])
	command := action["command"].(string)
	action["command"] = strings.Replace(
		command,
		codexPowerShellPath(),
		`C:\StaleWindows\System32\WindowsPowerShell\v1.0\powershell.exe`,
		1,
	)
	writeKiroIdeJSON(t, fixture.configPath, document)
	status, err := fixture.plugin.Status()
	if err != nil || status.Installed || status.HookConfigured {
		t.Fatalf("stale PowerShell status = %#v, %v", status, err)
	}
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("repair PowerShell launcher: %v", err)
	}
	status, err = fixture.plugin.Status()
	if err != nil || !status.Installed {
		t.Fatalf("repaired PowerShell status = %#v, %v", status, err)
	}
}

func TestKiroIdeRejectsMalformedContractsAndNameCollisionsBeforeWrites(t *testing.T) {
	for _, testCase := range []struct{ name, source, want string }{
		{"malformed", "{ malformed", "parse Kiro IDE hooks"},
		{"duplicate", `{"version":"v1","version":"v1","hooks":[]}`, "duplicate"},
		{"version", `{"version":"v0","hooks":[]}`, `version must be "v1"`},
		{"hooks", `{"version":"v1","hooks":null}`, `field "hooks" must be an array`},
		{"trigger", `{"version":"v1","hooks":[{"name":"bad","trigger":"FutureEvent","action":{"type":"command","command":"x"}}]}`, "unsupported trigger"},
		{"action", `{"version":"v1","hooks":[{"name":"bad","trigger":"PreToolUse","action":{"type":"future"}}]}`, "unsupported type"},
		{"timeout", `{"version":"v1","hooks":[{"name":"bad","trigger":"PreToolUse","timeout":1.5,"action":{"type":"command","command":"x"}}]}`, "non-negative integer"},
		{"collision", `{"version":"v1","hooks":[{"name":"elydora-guard","trigger":"PreToolUse","action":{"type":"command","command":"user-command"}}]}`, "conflicts with the Elydora contract"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := prepareKiroIdeFixture(t, &testCase.source, nil)
			err := fixture.plugin.Install(fixture.config)
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("install error = %v, want %q", err, testCase.want)
			}
			actual, readErr := os.ReadFile(fixture.configPath)
			if readErr != nil || string(actual) != testCase.source {
				t.Fatalf("invalid source changed: %q, %v", actual, readErr)
			}
			assertNoKiroIdeRuntimeFiles(t, fixture)
		})
	}
}

func TestKiroIdeMigratesOnlyExactLegacyOwnership(t *testing.T) {
	fixture := prepareKiroIdeFixture(t, nil, nil)
	writeKiroIdeJSON(
		t,
		fixture.legacyPath,
		legacyKiroIdeGoDocument(fixture, kiroIdeTestAgentID),
	)
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("migrate legacy Kiro IDE hook: %v", err)
	}
	if _, err := os.Lstat(fixture.legacyPath); !os.IsNotExist(err) {
		t.Fatalf("legacy Kiro IDE hook remains: %v", err)
	}

	lookalike := legacyKiroIdeGoDocument(fixture, kiroIdeTestAgentID)
	lookalike["owner"] = "user"
	writeKiroIdeJSON(t, fixture.legacyPath, lookalike)
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install with legacy lookalike: %v", err)
	}
	remainingLookalike := readKiroIdeObject(t, fixture.legacyPath)
	if remainingLookalike["owner"] != "user" || remainingLookalike["name"] != "Elydora Audit" {
		t.Fatalf("legacy ownership lookalike changed: %#v", remainingLookalike)
	}

	if err := os.WriteFile(fixture.legacyPath, []byte("{ malformed"), 0600); err != nil {
		t.Fatalf("write malformed legacy hook: %v", err)
	}
	if err := fixture.plugin.Install(fixture.config); err == nil ||
		!strings.Contains(err.Error(), "parse legacy Kiro IDE hook") {
		t.Fatalf("malformed legacy error = %v", err)
	}
}

func TestKiroIdeUninstallPreservesWorkspaceHooksAndExactAgentOwnership(t *testing.T) {
	userHook := map[string]any{
		"name": "workspace-context", "trigger": "SessionStart",
		"action": map[string]any{"type": "agent", "prompt": "Read AGENTS.md"},
	}
	existing := map[string]any{
		"version": "v1", "owner": "workspace", "hooks": []any{userHook},
	}
	raw, _ := json.Marshal(existing)
	fixture := prepareKiroIdeFixture(t, kiroIdeString(string(raw)), nil)
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Kiro IDE hooks: %v", err)
	}
	if err := fixture.plugin.Uninstall("agent-2"); err != nil {
		t.Fatalf("uninstall unrelated Kiro IDE agent: %v", err)
	}
	if len(requireArray(t, readKiroIdeObject(t, fixture.configPath)["hooks"])) != 3 {
		t.Fatal("unrelated uninstall changed Kiro IDE hooks")
	}
	if err := fixture.plugin.Uninstall(kiroIdeTestAgentID); err != nil {
		t.Fatalf("uninstall Kiro IDE hooks: %v", err)
	}
	remaining := readKiroIdeObject(t, fixture.configPath)
	if remaining["owner"] != "workspace" ||
		!reflect.DeepEqual(requireArray(t, remaining["hooks"]), []any{userHook}) {
		t.Fatalf("workspace hooks after uninstall = %#v", remaining)
	}

	owned := prepareKiroIdeFixture(t, nil, nil)
	if err := owned.plugin.Install(owned.config); err != nil {
		t.Fatalf("install owned Kiro IDE hooks: %v", err)
	}
	if err := owned.plugin.Uninstall(kiroIdeTestAgentID); err != nil {
		t.Fatalf("uninstall owned Kiro IDE hooks: %v", err)
	}
	if _, err := os.Lstat(owned.configPath); !os.IsNotExist(err) {
		t.Fatalf("owned Kiro IDE config remains: %v", err)
	}
}
