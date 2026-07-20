package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const (
	clineTestAgentID = "agent-1"
	clinePrivateKey  = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE"
)

type clineFixtureOptions struct {
	ExistingAudit *string
	ExistingGuard *string
}

type clineFixture struct {
	plugin        *ClinePlugin
	config        InstallConfig
	homeDir       string
	workspaceDir  string
	clineDir      string
	hooksDir      string
	agentDir      string
	guardPath     string
	hookPath      string
	guardWrapper  string
	auditWrapper  string
	runtimeConfig string
	privateKey    string
}

type clineCommandResult struct {
	exitCode int
	stdout   string
	stderr   string
}

func clineString(value string) *string {
	return &value
}

func prepareClineFixture(t *testing.T, options clineFixtureOptions) *clineFixture {
	t.Helper()
	rootDir := t.TempDir()
	homeDir := filepath.Join(rootDir, "home with spaces and 'quote")
	workspaceDir := filepath.Join(rootDir, "workspace")
	clineDir := filepath.Join(rootDir, "custom-cline-home")
	hooksDir := filepath.Join(clineDir, "hooks")
	agentDir := filepath.Join(homeDir, ".elydora", clineTestAgentID)
	guardPath := filepath.Join(agentDir, "guard.js")
	hookPath := filepath.Join(agentDir, "hook.js")
	guardWrapper := filepath.Join(hooksDir, "PreToolUse.mjs")
	auditWrapper := filepath.Join(hooksDir, "PostToolUse.mjs")

	for _, directory := range []string{workspaceDir} {
		if err := os.MkdirAll(directory, 0755); err != nil {
			t.Fatalf("create fixture directory %s: %v", directory, err)
		}
	}
	writeOptionalClineTestFile(t, guardWrapper, options.ExistingGuard)
	writeOptionalClineTestFile(t, auditWrapper, options.ExistingAudit)

	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)
	t.Setenv("CLINE_DIR", clineDir)
	return &clineFixture{
		plugin:        &ClinePlugin{},
		homeDir:       homeDir,
		workspaceDir:  workspaceDir,
		clineDir:      clineDir,
		hooksDir:      hooksDir,
		agentDir:      agentDir,
		guardPath:     guardPath,
		hookPath:      hookPath,
		guardWrapper:  guardWrapper,
		auditWrapper:  auditWrapper,
		runtimeConfig: filepath.Join(agentDir, "config.json"),
		privateKey:    filepath.Join(agentDir, "private.key"),
		config: InstallConfig{
			AgentName:       "cline",
			OrgID:           "org-1",
			AgentID:         clineTestAgentID,
			PrivateKey:      clinePrivateKey,
			KID:             "kid-1",
			Token:           "token-1",
			BaseURL:         "https://api.elydora.test",
			GuardScriptPath: guardPath,
		},
	}
}

func writeOptionalClineTestFile(t *testing.T, path string, contents *string) {
	t.Helper()
	if contents == nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("create directory for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(*contents), 0600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func installClineFixture(t *testing.T, fixture *clineFixture) {
	t.Helper()
	if err := fixture.plugin.Install(fixture.config); err != nil {
		t.Fatalf("install Cline hooks: %v", err)
	}
}

func runClineWrapper(
	t *testing.T,
	fixture *clineFixture,
	wrapperPath string,
	payload []byte,
) clineCommandResult {
	t.Helper()
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Fatalf("resolve Node.js runtime: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// #nosec G204 -- nodePath is resolved with exec.LookPath and the wrapper path is test-owned.
	command := exec.CommandContext(ctx, nodePath, wrapperPath)
	command.Dir = fixture.workspaceDir
	command.Env = append(
		os.Environ(),
		"HOME="+fixture.homeDir,
		"USERPROFILE="+fixture.homeDir,
	)
	command.Stdin = bytes.NewReader(payload)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	runErr := command.Run()
	if ctx.Err() != nil {
		t.Fatalf("Cline wrapper timed out: %v", ctx.Err())
	}
	exitCode := 0
	if runErr != nil {
		var exitError *exec.ExitError
		if !errors.As(runErr, &exitError) {
			t.Fatalf("run Cline wrapper: %v", runErr)
		}
		exitCode = exitError.ExitCode()
	}
	return clineCommandResult{exitCode: exitCode, stdout: stdout.String(), stderr: stderr.String()}
}

func readClineTestFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(contents)
}

func requireMissingClineTestFile(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be missing, got %v", path, err)
	}
}

func requireClineTestObject(t *testing.T, value any) map[string]any {
	t.Helper()
	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("value is not an object: %#v", value)
	}
	return object
}

func decodeClineControl(t *testing.T, stdout string) map[string]any {
	t.Helper()
	var control map[string]any
	if err := json.Unmarshal([]byte(stdout), &control); err != nil {
		t.Fatalf("decode Cline control output: %v", err)
	}
	return control
}

func writeClineTestFile(
	t *testing.T,
	path string,
	contents []byte,
	mode os.FileMode,
) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("create parent directory for %s: %v", path, err)
	}
	if err := os.WriteFile(path, contents, mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func writeClineTestObject(t *testing.T, path string, value map[string]any) {
	t.Helper()
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("marshal Cline test object: %v", err)
	}
	writeClineTestFile(t, path, append(encoded, '\n'), 0600)
}

func readClineTestObject(t *testing.T, path string) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(readClineTestFile(t, path)), &value); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return value
}

func assertNoClineRuntimeWrites(t *testing.T, fixture *clineFixture) {
	t.Helper()
	for _, path := range []string{
		fixture.guardPath,
		fixture.runtimeConfig,
		fixture.privateKey,
		fixture.hookPath,
		fixture.guardWrapper,
		fixture.auditWrapper,
	} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("managed file exists at %s: %v", path, err)
		}
	}
}

func assertNoClineTransactionArtifacts(t *testing.T, root string) {
	t.Helper()
	if _, err := os.Lstat(root); errors.Is(err, os.ErrNotExist) {
		return
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".tmp") || strings.HasSuffix(name, ".rollback") {
			t.Errorf("transaction artifact remains at %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk Cline fixture: %v", err)
	}
}

func clineSymlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symbolic links unavailable: %v", err)
		}
		return
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
}

func sameClineTestPath(left, right string) bool {
	leftPath, leftErr := filepath.Abs(left)
	rightPath, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && strings.EqualFold(leftPath, rightPath)
}
