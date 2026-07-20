package plugins

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const (
	copilotTestAgentID    = "agent-1"
	copilotTestPrivateKey = "CwsLCwsLCwsLCwsLCwsLCwsLCwsLCwsLCwsLCwsLCws"
)

type copilotFixtureOptions struct {
	userRaw                *string
	legacyRaw              *string
	legacyUserConfigRaw    *string
	userSettingsRaw        *string
	claudeSettingsRaw      *string
	claudeLocalSettingsRaw *string
	repositorySettingsRaw  *string
	localSettingsRaw       *string
	emptyOverride          bool
}

type copilotFixture struct {
	plugin              *CopilotPlugin
	config              InstallConfig
	homeDir             string
	projectDir          string
	copilotHome         string
	agentDir            string
	configPath          string
	legacyPath          string
	guardPath           string
	hookPath            string
	runtimeConfig       string
	privateKey          string
	legacyUserConfig    string
	userSettings        string
	claudeSettings      string
	claudeLocalSettings string
	repositorySettings  string
	localSettings       string
}

type copilotCommandResult struct {
	exitCode int
	stdout   string
	stderr   string
}

func copilotString(value string) *string {
	return &value
}

func copilotJSON(value any) *string {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		panic(err)
	}
	return copilotString(string(encoded) + "\n")
}

func prepareCopilotFixture(t *testing.T, options copilotFixtureOptions) *copilotFixture {
	t.Helper()
	rootDir := t.TempDir()
	homeDir := filepath.Join(rootDir, "home with spaces and 'quote")
	projectDir := filepath.Join(rootDir, "project with spaces")
	copilotHome := filepath.Join(homeDir, "custom copilot")
	agentDir := filepath.Join(homeDir, ".elydora", copilotTestAgentID)
	configPath := filepath.Join(copilotHome, "hooks", copilotConfigFile)
	legacyPath := filepath.Join(projectDir, ".github", "hooks", "hooks.json")
	legacyUserConfig := filepath.Join(copilotHome, "config.json")
	userSettings := filepath.Join(copilotHome, "settings.json")
	claudeSettings := filepath.Join(projectDir, ".claude", "settings.json")
	claudeLocalSettings := filepath.Join(projectDir, ".claude", "settings.local.json")
	repositorySettings := filepath.Join(projectDir, ".github", "copilot", "settings.json")
	localSettings := filepath.Join(projectDir, ".github", "copilot", "settings.local.json")
	guardPath := filepath.Join(agentDir, copilotGuardScript)
	hookPath := filepath.Join(agentDir, copilotAuditScript)
	for _, directory := range []string{homeDir, projectDir} {
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatalf("create fixture directory %s: %v", directory, err)
		}
	}
	writeOptionalCopilotFile(t, configPath, options.userRaw)
	writeOptionalCopilotFile(t, legacyPath, options.legacyRaw)
	writeOptionalCopilotFile(t, legacyUserConfig, options.legacyUserConfigRaw)
	writeOptionalCopilotFile(t, userSettings, options.userSettingsRaw)
	writeOptionalCopilotFile(t, claudeSettings, options.claudeSettingsRaw)
	writeOptionalCopilotFile(t, claudeLocalSettings, options.claudeLocalSettingsRaw)
	writeOptionalCopilotFile(t, repositorySettings, options.repositorySettingsRaw)
	writeOptionalCopilotFile(t, localSettings, options.localSettingsRaw)
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	if options.emptyOverride {
		t.Setenv("COPILOT_HOME", "   ")
		configPath = filepath.Join(homeDir, ".copilot", "hooks", copilotConfigFile)
	} else {
		t.Setenv("COPILOT_HOME", copilotHome)
	}
	previousDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("read current directory: %v", err)
	}
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("enter fixture project: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousDirectory); err != nil {
			t.Errorf("restore current directory: %v", err)
		}
	})
	return &copilotFixture{
		plugin:              &CopilotPlugin{},
		homeDir:             homeDir,
		projectDir:          projectDir,
		copilotHome:         copilotHome,
		agentDir:            agentDir,
		configPath:          configPath,
		legacyPath:          legacyPath,
		guardPath:           guardPath,
		hookPath:            hookPath,
		runtimeConfig:       filepath.Join(agentDir, "config.json"),
		privateKey:          filepath.Join(agentDir, "private.key"),
		legacyUserConfig:    legacyUserConfig,
		userSettings:        userSettings,
		claudeSettings:      claudeSettings,
		claudeLocalSettings: claudeLocalSettings,
		repositorySettings:  repositorySettings,
		localSettings:       localSettings,
		config: InstallConfig{
			AgentName: copilotAgentKey, OrgID: "org-1", AgentID: copilotTestAgentID,
			PrivateKey: copilotTestPrivateKey, KID: "kid-1", Token: "token-1",
			BaseURL: "https://api.elydora.test", GuardScriptPath: guardPath,
		},
	}
}

func writeOptionalCopilotFile(t *testing.T, path string, source *string) {
	t.Helper()
	if source == nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("create directory for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(*source), 0600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func writeCopilotObject(t *testing.T, path string, value map[string]any) {
	t.Helper()
	writeOptionalCopilotFile(t, path, copilotJSON(value))
}

func readCopilotObject(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return value
}

func requireCopilotObject(t *testing.T, value any) map[string]any {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("value is not an object: %#v", value)
	}
	return object
}

func requireCopilotArray(t *testing.T, value any) []any {
	t.Helper()
	array, ok := value.([]any)
	if !ok {
		t.Fatalf("value is not an array: %#v", value)
	}
	return array
}

func managedCopilotTestHandler(
	t *testing.T,
	settings map[string]any,
	event string,
	scriptName string,
) map[string]any {
	t.Helper()
	hooks := requireCopilotObject(t, settings["hooks"])
	for _, value := range requireCopilotArray(t, hooks[event]) {
		handler := requireCopilotObject(t, value)
		if strings.Contains(stringValue(handler["bash"]), scriptName) {
			return handler
		}
	}
	t.Fatalf("managed %s handler for %s was not found", event, scriptName)
	return nil
}

func stringValue(value any) string {
	result, _ := value.(string)
	return result
}

func assertNativeCopilotHandler(t *testing.T, handler map[string]any) {
	t.Helper()
	if len(handler) != 4 || handler["type"] != "command" ||
		handler["timeoutSec"] != copilotHookTimeout {
		t.Fatalf("Copilot handler = %#v", handler)
	}
	if !strings.Contains(stringValue(handler["bash"]), "node") {
		t.Fatalf("Copilot bash handler = %#v", handler["bash"])
	}
	powershell := stringValue(handler["powershell"])
	if !strings.HasPrefix(powershell, "& ") || !strings.HasSuffix(powershell, "; exit $LASTEXITCODE") {
		t.Fatalf("Copilot PowerShell handler = %q", powershell)
	}
}

func legacyCopilotConfig(fixture *copilotFixture, extraHooks map[string]any) map[string]any {
	hooks := map[string]any{
		"preToolUse": []any{map[string]any{
			"type": "command", "bash": "node " + fixture.guardPath,
			"powershell": "node " + fixture.guardPath, "timeoutSec": copilotLegacyTimeout,
		}},
		"postToolUse": []any{map[string]any{
			"type": "command", "bash": "node " + fixture.hookPath,
			"powershell": "node " + fixture.hookPath, "timeoutSec": copilotLegacyTimeout,
		}},
		"postToolUseFailure": []any{map[string]any{
			"type": "command", "bash": "node " + fixture.hookPath,
			"powershell": "node " + fixture.hookPath, "timeoutSec": copilotLegacyTimeout,
		}},
	}
	for key, value := range extraHooks {
		hooks[key] = value
	}
	return map[string]any{"version": float64(1), "hooks": hooks}
}

func runCopilotHandler(
	t *testing.T,
	handler map[string]any,
	payload string,
	environment ...string,
) copilotCommandResult {
	t.Helper()
	var process *exec.Cmd
	if runtime.GOOS == "windows" {
		process = exec.Command(
			"powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
			stringValue(handler["powershell"]),
		)
	} else {
		process = exec.Command("sh", "-c", stringValue(handler["bash"]))
	}
	process.Env = append(os.Environ(), environment...)
	process.Stdin = strings.NewReader(payload)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	process.Stdout = &stdout
	process.Stderr = &stderr
	err := process.Run()
	if err == nil {
		return copilotCommandResult{stdout: stdout.String(), stderr: stderr.String()}
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return copilotCommandResult{
			exitCode: exitError.ExitCode(), stdout: stdout.String(), stderr: stderr.String(),
		}
	}
	t.Fatalf("run GitHub Copilot hook: %v", err)
	return copilotCommandResult{exitCode: -1}
}

func installCopilotFixture(t *testing.T, fixture *copilotFixture) {
	t.Helper()
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install GitHub Copilot hooks: %v", err)
	}
}
