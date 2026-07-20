package plugins

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

type grokRuntimeReference struct {
	agentID    string
	scriptPath string
}

func sameGrokPath(left, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if absolute, err := filepath.Abs(left); err == nil {
		left = absolute
	}
	if absolute, err := filepath.Abs(right); err == nil {
		right = absolute
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func sameGrokAgentID(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func buildGrokCommand(runtimePath, scriptPath string) (string, error) {
	if !filepath.IsAbs(runtimePath) || !filepath.IsAbs(scriptPath) {
		return "", fmt.Errorf(
			"Grok hook commands require absolute runtime and script paths",
		)
	}
	if runtime.GOOS == "windows" {
		return codexWindowsCommand(runtimePath, scriptPath), nil
	}
	return quotePOSIXArgument(runtimePath) + " " + quotePOSIXArgument(scriptPath), nil
}

func quoteGrokLegacyWindowsArgument(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}

func readGrokLegacyWindowsArgument(
	command string,
	start int,
) (string, int, bool) {
	if start >= len(command) || command[start] != '"' {
		return "", start, false
	}
	var value strings.Builder
	for index := start + 1; index < len(command); index++ {
		if command[index] == '"' {
			return value.String(), index + 1, true
		}
		if command[index] == '\r' || command[index] == '\n' {
			return "", start, false
		}
		value.WriteByte(command[index])
	}
	return "", start, false
}

func parseGrokLegacyWindowsCommand(command string) (string, string, bool) {
	runtimePath, next, ok := readGrokLegacyWindowsArgument(command, 0)
	if !ok || next >= len(command) || command[next] != ' ' {
		return "", "", false
	}
	scriptPath, end, ok := readGrokLegacyWindowsArgument(command, next+1)
	if !ok || end != len(command) {
		return "", "", false
	}
	expected := quoteGrokLegacyWindowsArgument(runtimePath) + " " +
		quoteGrokLegacyWindowsArgument(scriptPath)
	return runtimePath, scriptPath, command == expected
}

func parseGrokCommand(command string) (string, string, bool) {
	if runtimePath, scriptPath, ok := parseCodexPOSIXCommand(command); ok {
		return runtimePath, scriptPath, true
	}
	if runtimePath, scriptPath, ok := parseCodexWindowsCommand(command); ok {
		return runtimePath, scriptPath, true
	}
	return parseGrokLegacyWindowsCommand(command)
}

func isGrokNodeExecutable(path string) bool {
	name := filepath.Base(path)
	return name == "node" || strings.EqualFold(name, "node.exe")
}

func grokRuntimeReferenceForCommand(
	command string,
	scriptName string,
) (*grokRuntimeReference, error) {
	runtimePath, scriptPath, ok := parseGrokCommand(command)
	if !ok || !filepath.IsAbs(runtimePath) || !filepath.IsAbs(scriptPath) ||
		!isGrokNodeExecutable(runtimePath) ||
		!sameGrokFileName(filepath.Base(scriptPath), scriptName) {
		return nil, nil
	}
	agentDirectory := filepath.Dir(scriptPath)
	runtimeRoot, err := AgentRuntimeRoot()
	if err != nil {
		return nil, err
	}
	if !sameGrokPath(filepath.Dir(agentDirectory), runtimeRoot) {
		return nil, nil
	}
	agentID := filepath.Base(agentDirectory)
	if agentID == "" || agentID == "." || agentID == ".." {
		return nil, nil
	}
	return &grokRuntimeReference{agentID: agentID, scriptPath: scriptPath}, nil
}

func sameGrokFileName(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}
