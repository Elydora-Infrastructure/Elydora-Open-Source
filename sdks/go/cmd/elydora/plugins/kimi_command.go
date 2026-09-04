package plugins

import (
	"path/filepath"
	"strings"
)

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

// parseKimiLegacyWindowsCommand reads the pre-2.1 quoteWindowsArgument form.
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
	if runtimePath, scriptPath, ok := parseEncodedCommand(command); ok {
		return runtimePath, scriptPath, true
	}
	return parseKimiLegacyWindowsCommand(command)
}

func kimiRuntimeReferenceForCommand(
	command string,
	scriptName string,
) (*managedScriptReference, error) {
	runtimePath, scriptPath, ok := parseKimiCommand(command)
	if !ok || !filepath.IsAbs(runtimePath) || !isNodeExecutable(runtimePath) {
		return nil, nil
	}
	return resolveManagedScript(scriptPath, scriptName)
}
