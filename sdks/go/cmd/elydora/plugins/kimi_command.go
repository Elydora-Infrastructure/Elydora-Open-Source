package plugins

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

type kimiRuntimeReference struct {
	agentID    string
	scriptPath string
}

func sameKimiPath(left, right string) bool {
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

func sameKimiAgentID(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func buildKimiCommand(runtimePath, scriptPath string) (string, error) {
	if !filepath.IsAbs(runtimePath) || !filepath.IsAbs(scriptPath) {
		return "", fmt.Errorf("kimi hook commands require absolute runtime and script paths")
	}
	if runtime.GOOS == "windows" {
		return codexWindowsCommand(runtimePath, scriptPath), nil
	}
	return quotePOSIXArgument(runtimePath) + " " + quotePOSIXArgument(scriptPath), nil
}

func readKimiLegacyWindowsArgument(command string, start int) (string, int, bool) {
	if start >= len(command) {
		return "", start, false
	}
	if command[start] == '"' {
		closing := strings.IndexByte(command[start+1:], '"')
		if closing < 0 {
			return "", start, false
		}
		closing += start + 1
		return command[start+1 : closing], closing + 1, true
	}
	end := start
	for end < len(command) && command[end] != ' ' {
		if command[end] == '"' || command[end] == '\t' || command[end] == '\r' || command[end] == '\n' {
			return "", start, false
		}
		end++
	}
	return command[start:end], end, end > start
}

func parseKimiLegacyWindowsCommand(command string) (string, string, bool) {
	runtimePath, next, ok := readKimiLegacyWindowsArgument(command, 0)
	if !ok || next >= len(command) || command[next] != ' ' {
		return "", "", false
	}
	scriptPath, end, ok := readKimiLegacyWindowsArgument(command, next+1)
	if !ok || end != len(command) {
		return "", "", false
	}
	expected := quoteWindowsArgument(runtimePath) + " " + quoteWindowsArgument(scriptPath)
	return runtimePath, scriptPath, command == expected
}

func parseKimiCommand(command string) (string, string, bool) {
	if runtimePath, scriptPath, ok := parseCodexPOSIXCommand(command); ok {
		return runtimePath, scriptPath, true
	}
	if runtimePath, scriptPath, ok := parseCodexWindowsCommand(command); ok {
		return runtimePath, scriptPath, true
	}
	return parseKimiLegacyWindowsCommand(command)
}

func isKimiNodeExecutable(path string) bool {
	name := filepath.Base(path)
	return name == "node" || strings.EqualFold(name, "node.exe")
}

func kimiRuntimeReferenceForCommand(
	command string,
	scriptName string,
) (*kimiRuntimeReference, error) {
	runtimePath, scriptPath, ok := parseKimiCommand(command)
	if !ok || !filepath.IsAbs(runtimePath) || !filepath.IsAbs(scriptPath) ||
		!isKimiNodeExecutable(runtimePath) || filepath.Base(scriptPath) != scriptName {
		return nil, nil
	}
	agentDirectory := filepath.Dir(scriptPath)
	runtimeRoot, err := AgentRuntimeRoot()
	if err != nil {
		return nil, err
	}
	if !sameKimiPath(filepath.Dir(agentDirectory), runtimeRoot) {
		return nil, nil
	}
	agentID := filepath.Base(agentDirectory)
	if agentID == "" || agentID == "." || agentID == ".." {
		return nil, nil
	}
	return &kimiRuntimeReference{agentID: agentID, scriptPath: scriptPath}, nil
}
