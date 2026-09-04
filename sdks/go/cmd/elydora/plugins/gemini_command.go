package plugins

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

type geminiRuntimeReference = managedScriptReference

func buildGeminiCommand(runtimePath, scriptPath string) (string, error) {
	if !filepath.IsAbs(runtimePath) || !filepath.IsAbs(scriptPath) {
		return "", fmt.Errorf("Gemini CLI hook commands require absolute runtime and script paths")
	}
	if runtime.GOOS != "windows" {
		return posixSource(runtimePath, scriptPath), nil
	}
	return "& " + quotePowerShellArgument(windowsPowerShellPath()) + powerShellEncodedFlags +
		encodePowerShellSource(powerShellSource(runtimePath, scriptPath)), nil
}

func parseGeminiWindowsCommand(command string) (string, string, bool) {
	if !strings.HasPrefix(command, "& ") {
		return "", "", false
	}
	powerShell, next, ok := readPowerShellArgument(command, 2)
	if !ok || !isPowerShellExecutable(powerShell) ||
		!strings.HasPrefix(command[next:], powerShellEncodedFlags) {
		return "", "", false
	}
	source, ok := decodePowerShellSource(command[next+len(powerShellEncodedFlags):])
	if !ok {
		return "", "", false
	}
	return parsePowerShellSource(source)
}

func parseGeminiCommand(command string) (string, string, bool) {
	if runtimePath, scriptPath, ok := parsePOSIXCommand(command); ok {
		return runtimePath, scriptPath, true
	}
	return parseGeminiWindowsCommand(command)
}

func parseLegacyGeminiCommand(command string) (string, string, bool) {
	if !strings.HasPrefix(command, "node ") || strings.ContainsAny(command, "\r\n") {
		return "", "", false
	}
	scriptPath := strings.TrimPrefix(command, "node ")
	if scriptPath == "" || strings.TrimSpace(scriptPath) != scriptPath {
		return "", "", false
	}
	return "node", scriptPath, true
}

func geminiRuntimeReferenceForCommand(
	command string,
	scriptName string,
	includeLegacy bool,
) (*geminiRuntimeReference, error) {
	runtimePath, scriptPath, current := parseGeminiCommand(command)
	legacy := false
	if !current && includeLegacy {
		runtimePath, scriptPath, legacy = parseLegacyGeminiCommand(command)
	}
	validRuntime := (current && filepath.IsAbs(runtimePath) && isNodeExecutable(runtimePath)) ||
		(legacy && runtimePath == "node")
	if !validRuntime {
		return nil, nil
	}
	return resolveManagedScript(scriptPath, scriptName)
}
