package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

var windowsDeviceName = regexp.MustCompile(
	`(?i)^(?:con|prn|aux|nul|com[1-9¹²³]|lpt[1-9¹²³])(?:\.|$)`,
)

// AgentRuntimeRoot returns the local root for all Elydora agent runtimes.
func AgentRuntimeRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	root, err := filepath.Abs(filepath.Join(home, ".elydora"))
	if err != nil {
		return "", fmt.Errorf("resolve Elydora runtime root: %w", err)
	}
	return root, nil
}

// ResolveAgentRuntimeDirectory confines an agent ID to one portable path segment.
func ResolveAgentRuntimeDirectory(agentID string) (string, error) {
	if invalidAgentDirectoryName(agentID) {
		return "", fmt.Errorf(
			"invalid agent ID for local storage: %q must be a single non-empty path segment with a portable file name",
			agentID,
		)
	}
	root, err := AgentRuntimeRoot()
	if err != nil {
		return "", err
	}
	candidate := filepath.Join(root, agentID)
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return "", fmt.Errorf("resolve agent runtime path for %q: %w", agentID, err)
	}
	if relative == "." || relative == ".." || filepath.IsAbs(relative) ||
		strings.HasPrefix(relative, ".."+string(os.PathSeparator)) ||
		strings.ContainsRune(relative, os.PathSeparator) {
		return "", fmt.Errorf("agent ID escapes the local storage directory: %q", agentID)
	}
	if _, err := RequirePhysicalDirectory(root); err != nil {
		return "", err
	}
	if _, err := RequirePhysicalDirectory(candidate); err != nil {
		return "", err
	}
	return candidate, nil
}

func invalidAgentDirectoryName(agentID string) bool {
	if agentID == "" || agentID == "." || agentID == ".." ||
		strings.ContainsAny(agentID, `<>:"/\|?*`) ||
		strings.HasPrefix(agentID, " ") || strings.HasSuffix(agentID, ".") ||
		strings.HasSuffix(agentID, " ") ||
		windowsDeviceName.MatchString(agentID) {
		return true
	}
	for _, character := range agentID {
		if character <= 31 {
			return true
		}
	}
	return false
}

// EnsurePrivateDirectory creates an owner-only physical directory.
func EnsurePrivateDirectory(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return fmt.Errorf("create private directory %s: %w", path, err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect private directory %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("private directory path is not a physical directory: %s", path)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0700); err != nil {
			return fmt.Errorf("restrict private directory %s: %w", path, err)
		}
	}
	return nil
}

// PrepareAgentRuntimeDirectory validates and creates the private runtime path.
func PrepareAgentRuntimeDirectory(agentID string) (string, error) {
	root, err := AgentRuntimeRoot()
	if err != nil {
		return "", err
	}
	agentDirectory, err := ResolveAgentRuntimeDirectory(agentID)
	if err != nil {
		return "", err
	}
	if err := EnsurePrivateDirectory(root); err != nil {
		return "", err
	}
	if err := EnsurePrivateDirectory(agentDirectory); err != nil {
		return "", err
	}
	return agentDirectory, nil
}

// RequirePhysicalDirectory reports whether a path is an existing physical directory.
func RequirePhysicalDirectory(path string) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect agent runtime directory %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false, fmt.Errorf("agent runtime path is not a physical directory: %s", path)
	}
	return true, nil
}

// RequirePhysicalFile reports whether a path is an existing physical regular file.
func RequirePhysicalFile(path string) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect agent runtime file %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false, fmt.Errorf("agent runtime config is not a physical file: %s", path)
	}
	return true, nil
}
