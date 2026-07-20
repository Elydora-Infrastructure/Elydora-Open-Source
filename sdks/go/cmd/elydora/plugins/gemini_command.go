package plugins

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

type geminiRuntimeReference struct {
	agentID    string
	scriptPath string
}

func sameGeminiPath(left, right string) bool {
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

func sameGeminiAgentID(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func sameGeminiFileName(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func isGeminiNodeExecutable(path string) bool {
	name := filepath.Base(path)
	return name == "node" || strings.EqualFold(name, "node.exe")
}

func buildGeminiCommand(runtimePath, scriptPath string) (string, error) {
	if !filepath.IsAbs(runtimePath) || !filepath.IsAbs(scriptPath) {
		return "", fmt.Errorf(
			"Gemini CLI hook commands require absolute runtime and script paths",
		)
	}
	if runtime.GOOS != "windows" {
		return quotePOSIXArgument(runtimePath) + " " + quotePOSIXArgument(scriptPath), nil
	}
	source := "& " + quoteCodexPowerShell(runtimePath) + " " +
		quoteCodexPowerShell(scriptPath) + "; exit $LASTEXITCODE"
	return "& " + quoteCodexPowerShell(codexPowerShellPath()) +
		" -NoLogo -NoProfile -NonInteractive -EncodedCommand " +
		encodeCodexPowerShell(source), nil
}

func parseGeminiWindowsCommand(command string) (string, string, bool) {
	if !strings.HasPrefix(command, "& ") {
		return "", "", false
	}
	powerShell, next, ok := readCodexPowerShellArgument(command, 2)
	if !ok || !isAbsoluteWindowsPath(powerShell) ||
		!strings.EqualFold(windowsPathBase(powerShell), "powershell.exe") {
		return "", "", false
	}
	prefix := " -NoLogo -NoProfile -NonInteractive -EncodedCommand "
	if !strings.HasPrefix(command[next:], prefix) {
		return "", "", false
	}
	source, ok := decodeCodexPowerShell(command[next+len(prefix):])
	if !ok {
		return "", "", false
	}
	return parseCodexPowerShellSource(source)
}

func parseGeminiCommand(command string) (string, string, bool) {
	if runtimePath, scriptPath, ok := parseCodexPOSIXCommand(command); ok {
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
	validRuntime := (current && filepath.IsAbs(runtimePath) &&
		isGeminiNodeExecutable(runtimePath)) || (legacy && runtimePath == "node")
	if !validRuntime || !filepath.IsAbs(scriptPath) ||
		!sameGeminiFileName(filepath.Base(scriptPath), scriptName) {
		return nil, nil
	}
	agentDirectory := filepath.Dir(scriptPath)
	runtimeRoot, err := AgentRuntimeRoot()
	if err != nil {
		return nil, err
	}
	if !sameGeminiPath(filepath.Dir(agentDirectory), runtimeRoot) {
		return nil, nil
	}
	agentID := filepath.Base(agentDirectory)
	if agentID == "" || agentID == "." || agentID == ".." {
		return nil, nil
	}
	return &geminiRuntimeReference{agentID: agentID, scriptPath: scriptPath}, nil
}
