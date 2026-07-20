package plugins

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf16"
)

func quoteCodexPowerShell(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func codexPowerShellPath() string {
	root := "C:\\Windows"
	if runtime.GOOS == "windows" {
		configured := os.Getenv("SystemRoot")
		if filepath.IsAbs(configured) && !strings.ContainsAny(configured, "\"%\r\n") {
			root = configured
		}
	}
	return strings.TrimRight(root, `\/`) +
		`\System32\WindowsPowerShell\v1.0\powershell.exe`
}

func encodeCodexPowerShell(source string) string {
	runes := utf16.Encode([]rune(source))
	raw := make([]byte, len(runes)*2)
	for index, value := range runes {
		raw[index*2] = byte(value)
		raw[index*2+1] = byte(value >> 8)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func codexWindowsCommand(runtimePath, scriptPath string) string {
	source := "& " + quoteCodexPowerShell(runtimePath) + " " +
		quoteCodexPowerShell(scriptPath) + "; exit $LASTEXITCODE"
	return `"` + codexPowerShellPath() +
		`" -NoLogo -NoProfile -NonInteractive -EncodedCommand ` +
		encodeCodexPowerShell(source)
}

func codexHandler(runtimePath, scriptPath, statusMessage string) map[string]any {
	return map[string]any{
		"type":           "command",
		"command":        quotePOSIXArgument(runtimePath) + " " + quotePOSIXArgument(scriptPath),
		"commandWindows": codexWindowsCommand(runtimePath, scriptPath),
		"timeout":        codexHookTimeout,
		"statusMessage":  statusMessage,
	}
}

func codexMatcherGroup(handler map[string]any) map[string]any {
	return map[string]any{"matcher": "*", "hooks": []any{handler}}
}

func readCodexPOSIXArgument(command string, start int) (string, int, bool) {
	if start >= len(command) || command[start] != '\'' {
		return "", start, false
	}
	var value strings.Builder
	for index := start + 1; index < len(command); {
		if strings.HasPrefix(command[index:], codexPOSIXApostrophe) {
			value.WriteByte('\'')
			index += len(codexPOSIXApostrophe)
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

func parseCodexPOSIXCommand(value any) (string, string, bool) {
	command, ok := value.(string)
	if !ok {
		return "", "", false
	}
	runtimePath, next, ok := readCodexPOSIXArgument(command, 0)
	if !ok || next >= len(command) || command[next] != ' ' {
		return "", "", false
	}
	scriptPath, end, ok := readCodexPOSIXArgument(command, next+1)
	return runtimePath, scriptPath, ok && end == len(command)
}

func readCodexPowerShellArgument(command string, start int) (string, int, bool) {
	if start >= len(command) || command[start] != '\'' {
		return "", start, false
	}
	var value strings.Builder
	for index := start + 1; index < len(command); index++ {
		if command[index] != '\'' {
			value.WriteByte(command[index])
			continue
		}
		if index+1 < len(command) && command[index+1] == '\'' {
			value.WriteByte('\'')
			index++
			continue
		}
		return value.String(), index + 1, true
	}
	return "", start, false
}

func parseCodexPowerShellSource(source string) (string, string, bool) {
	if !strings.HasPrefix(source, "& ") {
		return "", "", false
	}
	runtimePath, next, ok := readCodexPowerShellArgument(source, 2)
	if !ok || next >= len(source) || source[next] != ' ' {
		return "", "", false
	}
	scriptPath, end, ok := readCodexPowerShellArgument(source, next+1)
	return runtimePath, scriptPath, ok && source[end:] == "; exit $LASTEXITCODE"
}

func windowsPathBase(value string) string {
	index := strings.LastIndexAny(value, `\/`)
	return value[index+1:]
}

func isAbsoluteWindowsPath(value string) bool {
	return len(value) >= 3 && ((value[0] >= 'A' && value[0] <= 'Z') ||
		(value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':' &&
		(value[2] == '\\' || value[2] == '/') || strings.HasPrefix(value, `\\`)
}

func decodeCodexPowerShell(encoded string) (string, bool) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || base64.StdEncoding.EncodeToString(raw) != encoded || len(raw)%2 != 0 {
		return "", false
	}
	units := make([]uint16, len(raw)/2)
	for index := range units {
		units[index] = uint16(raw[index*2]) | uint16(raw[index*2+1])<<8
	}
	return string(utf16.Decode(units)), true
}

func parseCodexWindowsCommand(value any) (string, string, bool) {
	command, ok := value.(string)
	if !ok || !strings.HasPrefix(command, `"`) {
		return "", "", false
	}
	closing := strings.IndexByte(command[1:], '"')
	if closing < 0 {
		return "", "", false
	}
	closing++
	powerShell := command[1:closing]
	prefix := `" -NoLogo -NoProfile -NonInteractive -EncodedCommand `
	if !isAbsoluteWindowsPath(powerShell) ||
		!strings.EqualFold(windowsPathBase(powerShell), "powershell.exe") ||
		!strings.HasPrefix(command[closing:], prefix) {
		return "", "", false
	}
	encoded := command[closing+len(prefix):]
	source, ok := decodeCodexPowerShell(encoded)
	if !ok {
		return "", "", false
	}
	return parseCodexPowerShellSource(source)
}

func isCodexNodeExecutable(path string) bool {
	name := filepath.Base(path)
	return name == "node" || strings.EqualFold(name, "node.exe")
}

func exactCodexHandlerKeys(handler map[string]any) bool {
	if len(handler) != 5 {
		return false
	}
	for _, key := range []string{"type", "command", "commandWindows", "timeout", "statusMessage"} {
		if _, exists := handler[key]; !exists {
			return false
		}
	}
	return true
}

func codexManagedScriptPath(handler map[string]any, status string) (string, bool) {
	if !exactCodexHandlerKeys(handler) || handler["type"] != "command" ||
		handler["timeout"] != codexHookTimeout || handler["statusMessage"] != status {
		return "", false
	}
	posixRuntime, posixScript, posixOK := parseCodexPOSIXCommand(handler["command"])
	windowsRuntime, windowsScript, windowsOK := parseCodexWindowsCommand(handler["commandWindows"])
	if !windowsOK && posixOK {
		legacy, ok := handler["commandWindows"].(string)
		expected := quoteWindowsArgument(posixRuntime) + " " + quoteWindowsArgument(posixScript)
		if ok && legacy == expected {
			windowsRuntime, windowsScript, windowsOK = posixRuntime, posixScript, true
		}
	}
	if !posixOK || !windowsOK || !filepath.IsAbs(posixRuntime) ||
		!filepath.IsAbs(posixScript) || !isCodexNodeExecutable(posixRuntime) ||
		!isCodexNodeExecutable(windowsRuntime) ||
		!sameCodexPath(posixRuntime, windowsRuntime) ||
		!sameCodexPath(posixScript, windowsScript) {
		return "", false
	}
	return posixScript, true
}

func codexManagedAgentID(
	handler map[string]any,
	scriptName string,
	status string,
) (string, bool) {
	scriptPath, managed := codexManagedScriptPath(handler, status)
	if !managed || filepath.Base(scriptPath) != scriptName {
		return "", false
	}
	agentDirectory := filepath.Dir(scriptPath)
	runtimeRoot, err := AgentRuntimeRoot()
	if err != nil || !sameCodexPath(filepath.Dir(agentDirectory), runtimeRoot) {
		return "", false
	}
	agentID := filepath.Base(agentDirectory)
	return agentID, agentID != "" && agentID != "." && agentID != ".."
}
