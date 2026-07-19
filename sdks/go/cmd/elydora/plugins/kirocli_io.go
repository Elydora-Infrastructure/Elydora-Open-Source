package plugins

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func kiroConfigPaths() (string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".kiro", "agents", kiroV2AgentName+".json"),
		filepath.Join(home, ".kiro", "hooks", "elydora-audit.json"), nil
}

func resolveNodeRuntime() (string, error) {
	nodePath, err := exec.LookPath("node")
	if err != nil {
		return "", fmt.Errorf("resolve Node.js runtime: %w", err)
	}
	absolute, err := filepath.Abs(nodePath)
	if err != nil {
		return "", fmt.Errorf("resolve Node.js runtime path: %w", err)
	}
	return absolute, nil
}

func buildKiroCommand(runtimePath, scriptPath string) string {
	if runtime.GOOS == "windows" {
		return quoteWindowsArgument(runtimePath) + " " + quoteWindowsArgument(scriptPath)
	}
	return quotePOSIXArgument(runtimePath) + " " + quotePOSIXArgument(scriptPath)
}

func quotePOSIXArgument(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func quoteWindowsArgument(value string) string {
	if value != "" && !strings.ContainsAny(value, " \t\n\v\"") {
		return value
	}
	var result strings.Builder
	result.WriteByte('"')
	backslashes := 0
	for _, character := range value {
		if character == '\\' {
			backslashes++
			continue
		}
		if character == '"' {
			result.WriteString(strings.Repeat("\\", backslashes*2+1))
			result.WriteRune(character)
			backslashes = 0
			continue
		}
		result.WriteString(strings.Repeat("\\", backslashes))
		backslashes = 0
		result.WriteRune(character)
	}
	result.WriteString(strings.Repeat("\\", backslashes*2))
	result.WriteByte('"')
	return result.String()
}
