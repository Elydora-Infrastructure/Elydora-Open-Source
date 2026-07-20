package plugins

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const (
	droidTestAgentID    = "agent-1"
	droidTestPrivateKey = "DQ0NDQ0NDQ0NDQ0NDQ0NDQ0NDQ0NDQ0NDQ0NDQ0NDQ0"
)

type droidFixtureOptions struct {
	root                 *string
	legacy               *string
	settings             *string
	localSettings        *string
	projectSettings      *string
	projectLocalSettings *string
	baseURL              string
	agentID              string
}

type droidFixture struct {
	plugin                   *DroidPlugin
	config                   InstallConfig
	homeDir                  string
	workspaceDir             string
	factoryDir               string
	configPath               string
	legacyPath               string
	settingsPath             string
	localSettingsPath        string
	projectSettingsPath      string
	projectLocalSettingsPath string
	systemSettingsPath       string
	agentDir                 string
	guardPath                string
	hookPath                 string
	runtimeConfig            string
	privateKey               string
}

type droidCommandResult struct {
	exitCode int
	stdout   string
	stderr   string
}

func droidString(value string) *string {
	return &value
}

func droidJSON(value any) *string {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		panic(err)
	}
	return droidString(string(encoded) + "\n")
}

func prepareDroidFixture(t *testing.T, options droidFixtureOptions) *droidFixture {
	t.Helper()
	rootDir := t.TempDir()
	homeDir := filepath.Join(rootDir, "home with spaces and 'quote %DROID%")
	workspaceDir := filepath.Join(rootDir, "workspace with spaces")
	factoryDir := filepath.Join(homeDir, ".factory")
	configPath := filepath.Join(factoryDir, "hooks.json")
	legacyPath := filepath.Join(factoryDir, "hooks", "hooks.json")
	settingsPath := filepath.Join(factoryDir, "settings.json")
	localSettingsPath := filepath.Join(factoryDir, "settings.local.json")
	projectSettingsPath := filepath.Join(workspaceDir, ".factory", "settings.json")
	projectLocalSettingsPath := filepath.Join(workspaceDir, ".factory", "settings.local.json")
	systemSettingsPath := filepath.Join(rootDir, "managed factory", "settings.json")
	agentID := options.agentID
	if agentID == "" {
		agentID = droidTestAgentID
	}
	agentDir := filepath.Join(homeDir, ".elydora", agentID)
	guardPath := filepath.Join(agentDir, droidGuardScript)
	hookPath := filepath.Join(agentDir, droidAuditScript)
	if err := os.MkdirAll(filepath.Join(workspaceDir, ".git"), 0755); err != nil {
		t.Fatalf("create fixture workspace: %v", err)
	}
	writeOptionalDroidFile(t, configPath, options.root)
	writeOptionalDroidFile(t, legacyPath, options.legacy)
	writeOptionalDroidFile(t, settingsPath, options.settings)
	writeOptionalDroidFile(t, localSettingsPath, options.localSettings)
	writeOptionalDroidFile(t, projectSettingsPath, options.projectSettings)
	writeOptionalDroidFile(t, projectLocalSettingsPath, options.projectLocalSettings)
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	oldDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("read working directory: %v", err)
	}
	if err := os.Chdir(workspaceDir); err != nil {
		t.Fatalf("enter fixture workspace: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldDirectory); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
	previousManagedPath := droidManagedSettingsPath
	droidManagedSettingsPath = func() string { return systemSettingsPath }
	t.Cleanup(func() { droidManagedSettingsPath = previousManagedPath })
	baseURL := options.baseURL
	if baseURL == "" {
		baseURL = "http://127.0.0.1:9"
	}
	return &droidFixture{
		plugin:                   &DroidPlugin{},
		homeDir:                  homeDir,
		workspaceDir:             workspaceDir,
		factoryDir:               factoryDir,
		configPath:               configPath,
		legacyPath:               legacyPath,
		settingsPath:             settingsPath,
		localSettingsPath:        localSettingsPath,
		projectSettingsPath:      projectSettingsPath,
		projectLocalSettingsPath: projectLocalSettingsPath,
		systemSettingsPath:       systemSettingsPath,
		agentDir:                 agentDir,
		guardPath:                guardPath,
		hookPath:                 hookPath,
		runtimeConfig:            filepath.Join(agentDir, "config.json"),
		privateKey:               filepath.Join(agentDir, "private.key"),
		config: InstallConfig{
			AgentName:       droidAgentKey,
			OrgID:           "org-1",
			AgentID:         agentID,
			PrivateKey:      droidTestPrivateKey,
			KID:             "kid-1",
			Token:           "token-1",
			BaseURL:         baseURL,
			GuardScriptPath: guardPath,
		},
	}
}

func writeOptionalDroidFile(t *testing.T, path string, source *string) {
	t.Helper()
	if source == nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("create directory for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(*source), 0600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func writeDroidTestObject(t *testing.T, path string, value any) {
	t.Helper()
	writeOptionalDroidFile(t, path, droidJSON(value))
}

func installDroidFixture(t *testing.T, fixture *droidFixture) {
	t.Helper()
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Factory Droid hooks: %v", err)
	}
}

func readDroidTestFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

func requireMissingDroidFile(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be missing, got %v", path, err)
	}
}

func readDroidTestObject(t *testing.T, path string) map[string]any {
	t.Helper()
	standard, err := standardizeJSONC(
		[]byte(readDroidTestFile(t, path)),
		"Factory Droid test source",
		true,
	)
	if err != nil {
		t.Fatalf("standardize %s: %v", path, err)
	}
	var object map[string]any
	if err := json.Unmarshal(standard, &object); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return object
}

func droidCurrentHooks(t *testing.T, path string) map[string]any {
	t.Helper()
	return requireDroidObject(t, readDroidTestObject(t, path)["hooks"])
}

func requireDroidObject(t *testing.T, value any) map[string]any {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("value is not an object: %#v", value)
	}
	return object
}

func requireDroidArray(t *testing.T, value any) []any {
	t.Helper()
	array, ok := value.([]any)
	if !ok {
		t.Fatalf("value is not an array: %#v", value)
	}
	return array
}

func droidManagedGroup(t *testing.T, groupsValue any, scriptPath string) map[string]any {
	t.Helper()
	for _, groupValue := range requireDroidArray(t, groupsValue) {
		group := requireDroidObject(t, groupValue)
		for _, handlerValue := range requireDroidArray(t, group["hooks"]) {
			handler := requireDroidObject(t, handlerValue)
			command, _ := handler["command"].(string)
			_, configuredPath, managed := parseDroidCommand(command, true)
			if managed && sameDroidPath(configuredPath, scriptPath) {
				return group
			}
		}
	}
	t.Fatalf("managed group for %s not found", scriptPath)
	return nil
}

func droidManagedHandler(t *testing.T, groupsValue any, scriptPath string) map[string]any {
	t.Helper()
	group := droidManagedGroup(t, groupsValue, scriptPath)
	for _, handlerValue := range requireDroidArray(t, group["hooks"]) {
		handler := requireDroidObject(t, handlerValue)
		command, _ := handler["command"].(string)
		_, configuredPath, managed := parseDroidCommand(command, true)
		if managed && sameDroidPath(configuredPath, scriptPath) {
			return handler
		}
	}
	t.Fatalf("managed handler for %s not found", scriptPath)
	return nil
}

func requireDroidNativeGroup(t *testing.T, group map[string]any) {
	t.Helper()
	if len(group) != 2 || group["matcher"] != "*" {
		t.Fatalf("managed group = %#v", group)
	}
	handlers := requireDroidArray(t, group["hooks"])
	if len(handlers) != 1 {
		t.Fatalf("managed handlers = %#v", handlers)
	}
	handler := requireDroidObject(t, handlers[0])
	if len(handler) != 3 || handler["type"] != "command" || handler["timeout"] != float64(10) {
		t.Fatalf("managed handler = %#v", handler)
	}
}

func runDroidCommand(t *testing.T, command, homeDir, payload string) droidCommandResult {
	t.Helper()
	var process *exec.Cmd
	if runtime.GOOS == "windows" {
		process = exec.Command(
			"powershell.exe",
			"-NoLogo",
			"-NoProfile",
			"-NonInteractive",
			"-Command",
			command,
		)
	} else {
		process = exec.Command("sh", "-c", command)
	}
	process.Env = append(os.Environ(), "HOME="+homeDir, "USERPROFILE="+homeDir)
	process.Stdin = bytes.NewBufferString(payload)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	process.Stdout = &stdout
	process.Stderr = &stderr
	err := process.Run()
	result := droidCommandResult{stdout: stdout.String(), stderr: stderr.String()}
	if err == nil {
		return result
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		result.exitCode = exitError.ExitCode()
		return result
	}
	t.Fatalf("run Factory Droid hook command: %v", err)
	return droidCommandResult{}
}

func snapshotDroidFiles(t *testing.T, paths ...string) map[string]string {
	t.Helper()
	result := make(map[string]string, len(paths))
	for _, path := range paths {
		result[path] = readDroidTestFile(t, path)
	}
	return result
}

func requireDroidSnapshot(t *testing.T, expected map[string]string) {
	t.Helper()
	for path, source := range expected {
		if current := readDroidTestFile(t, path); current != source {
			t.Fatalf("%s changed", path)
		}
	}
}

func requireNoDroidStagingFiles(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasSuffix(entry.Name(), ".tmp") || strings.HasSuffix(entry.Name(), ".rollback") {
			t.Errorf("staging file remains: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk staging files: %v", err)
	}
}
