package plugins

import (
	"path/filepath"
	"runtime"
	"strings"
)

const qwenPOSIXApostrophe = `'"'"'`

type qwenRuntimeReference struct {
	agentID    string
	scriptPath string
}

func buildQwenCommand(nodePath, scriptPath string) string {
	if runtime.GOOS == "windows" {
		return "& " + quoteQwenPowerShell(nodePath) + " " +
			quoteQwenPowerShell(scriptPath) + "; exit $LASTEXITCODE"
	}
	return quotePOSIXArgument(nodePath) + " " + quotePOSIXArgument(scriptPath)
}

func quoteQwenPowerShell(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func parseQwenCommand(command string) (string, string, bool) {
	start := 0
	suffix := ""
	if runtime.GOOS == "windows" {
		if !strings.HasPrefix(command, "& ") {
			return "", "", false
		}
		start = 2
		suffix = "; exit $LASTEXITCODE"
	}
	executable, next, ok := readQwenQuotedArgument(command, start)
	if !ok || next >= len(command) || command[next] != ' ' {
		return "", "", false
	}
	script, end, ok := readQwenQuotedArgument(command, next+1)
	return executable, script, ok && command[end:] == suffix &&
		executable != "" && script != ""
}

func readQwenQuotedArgument(command string, start int) (string, int, bool) {
	if start >= len(command) || command[start] != '\'' {
		return "", start, false
	}
	var value strings.Builder
	for index := start + 1; index < len(command); {
		if runtime.GOOS == "windows" && strings.HasPrefix(command[index:], "''") {
			value.WriteByte('\'')
			index += 2
			continue
		}
		if runtime.GOOS != "windows" && strings.HasPrefix(command[index:], qwenPOSIXApostrophe) {
			value.WriteByte('\'')
			index += len(qwenPOSIXApostrophe)
			continue
		}
		if command[index] == '\'' {
			return value.String(), index + 1, true
		}
		value.WriteByte(command[index])
		index++
	}
	return "", start, false
}

func sameQwenPath(left, right string) bool {
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

func sameQwenAgentID(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func isQwenNodeExecutable(filePath string) bool {
	name := strings.ToLower(filepath.Base(filePath))
	return name == "node" || name == "node.exe"
}

func qwenRuntimeReferenceForCommand(
	command, scriptName, runtimeRoot string,
) (qwenRuntimeReference, bool) {
	executable, scriptPath, ok := parseQwenCommand(command)
	if !ok || !filepath.IsAbs(executable) || !filepath.IsAbs(scriptPath) ||
		!isQwenNodeExecutable(executable) || filepath.Base(scriptPath) != scriptName {
		return qwenRuntimeReference{}, false
	}
	agentDirectory := filepath.Dir(scriptPath)
	if !sameQwenPath(filepath.Dir(agentDirectory), runtimeRoot) {
		return qwenRuntimeReference{}, false
	}
	agentID := filepath.Base(agentDirectory)
	if agentID == "" || agentID == "." || agentID == ".." {
		return qwenRuntimeReference{}, false
	}
	return qwenRuntimeReference{agentID: agentID, scriptPath: scriptPath}, true
}
