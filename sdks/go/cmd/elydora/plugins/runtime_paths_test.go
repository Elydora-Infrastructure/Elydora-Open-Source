package plugins

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func setRuntimeTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func TestResolveAgentRuntimeDirectoryRejectsUnsafePortableNames(t *testing.T) {
	setRuntimeTestHome(t)
	invalid := []string{
		"../escape",
		`..\escape`,
		"C:escape",
		"agent.",
		"agent ",
		" agent",
		"CON",
		"COM¹.log",
		".",
		"..",
	}
	for _, agentID := range invalid {
		t.Run(agentID, func(t *testing.T) {
			_, err := ResolveAgentRuntimeDirectory(agentID)
			if err == nil || !strings.Contains(err.Error(), "invalid agent ID") {
				t.Fatalf("ResolveAgentRuntimeDirectory(%q) error = %v", agentID, err)
			}
		})
	}
}

func TestPrepareAgentRuntimeDirectoryCreatesPrivatePhysicalChild(t *testing.T) {
	home := setRuntimeTestHome(t)
	agentDirectory, err := PrepareAgentRuntimeDirectory("agent-1")
	if err != nil {
		t.Fatalf("PrepareAgentRuntimeDirectory() error = %v", err)
	}
	want := filepath.Join(home, ".elydora", "agent-1")
	if agentDirectory != want {
		t.Fatalf("agent directory = %q, want %q", agentDirectory, want)
	}
	for _, directory := range []string{filepath.Dir(agentDirectory), agentDirectory} {
		info, err := os.Lstat(directory)
		if err != nil {
			t.Fatalf("Lstat(%q) error = %v", directory, err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("%q is not a physical directory", directory)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0700 {
			t.Fatalf("%q mode = %o, want 700", directory, info.Mode().Perm())
		}
	}
}

func TestPrepareAgentRuntimeDirectoryRejectsSymlinkRoot(t *testing.T) {
	home := setRuntimeTestHome(t)
	target := t.TempDir()
	root := filepath.Join(home, ".elydora")
	if err := os.Symlink(target, root); err != nil {
		t.Skipf("directory symbolic links unavailable: %v", err)
	}
	_, err := PrepareAgentRuntimeDirectory("agent-1")
	if err == nil || !strings.Contains(err.Error(), "physical directory") {
		t.Fatalf("PrepareAgentRuntimeDirectory() error = %v", err)
	}
}

func TestResolveAgentRuntimeDirectoryRejectsSymlinkChild(t *testing.T) {
	home := setRuntimeTestHome(t)
	root := filepath.Join(home, ".elydora")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	target := t.TempDir()
	link := filepath.Join(root, "agent-1")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("directory symbolic links unavailable: %v", err)
	}
	_, err := ResolveAgentRuntimeDirectory("agent-1")
	if err == nil || !strings.Contains(err.Error(), "physical directory") {
		t.Fatalf("ResolveAgentRuntimeDirectory() error = %v", err)
	}
}

func TestRequirePhysicalFileRejectsSymlink(t *testing.T) {
	setRuntimeTestHome(t)
	target := filepath.Join(t.TempDir(), "target.json")
	if err := os.WriteFile(target, []byte("{}"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	link := filepath.Join(t.TempDir(), "config.json")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("file symbolic links unavailable: %v", err)
	}
	_, err := RequirePhysicalFile(link)
	if err == nil || !strings.Contains(err.Error(), "physical file") {
		t.Fatalf("RequirePhysicalFile() error = %v", err)
	}
}
