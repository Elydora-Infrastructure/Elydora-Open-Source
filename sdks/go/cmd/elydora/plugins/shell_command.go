package plugins

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf16"
)

const (
	posixApostrophe        = `'"'"'`
	powerShellExitSuffix   = "; exit $LASTEXITCODE"
	powerShellEncodedFlags = " -NoLogo -NoProfile -NonInteractive -EncodedCommand "
)

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

func resolveAbsoluteNodeRuntime(product string) (string, error) {
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(nodePath) || !isNodeExecutable(nodePath) {
		return "", fmt.Errorf("%s hooks require an absolute Node.js executable path", product)
	}
	return nodePath, nil
}

func isNodeExecutable(path string) bool {
	name := filepath.Base(path)
	return name == "node" || strings.EqualFold(name, "node.exe")
}

func quotePOSIXArgument(value string) string {
	return "'" + strings.ReplaceAll(value, "'", posixApostrophe) + "'"
}

func quotePowerShellArgument(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func posixSource(runtimePath, scriptPath string) string {
	return quotePOSIXArgument(runtimePath) + " " + quotePOSIXArgument(scriptPath)
}

func powerShellSource(runtimePath, scriptPath string) string {
	return "& " + quotePowerShellArgument(runtimePath) + " " +
		quotePowerShellArgument(scriptPath) + powerShellExitSuffix
}

func windowsPowerShellPath() string {
	root := "C:\\Windows"
	if runtime.GOOS == "windows" {
		configured := os.Getenv("SystemRoot")
		if filepath.IsAbs(configured) && !strings.ContainsAny(configured, "\"%\r\n") {
			root = configured
		}
	}
	return strings.TrimRight(root, `\/`) + `\System32\WindowsPowerShell\v1.0\powershell.exe`
}

func encodePowerShellSource(source string) string {
	units := utf16.Encode([]rune(source))
	raw := make([]byte, len(units)*2)
	for index, value := range units {
		binary.LittleEndian.PutUint16(raw[index*2:], value)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func decodePowerShellSource(encoded string) (string, bool) {
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

func encodedWindowsCommand(runtimePath, scriptPath string) string {
	return `"` + windowsPowerShellPath() + `"` + powerShellEncodedFlags +
		encodePowerShellSource(powerShellSource(runtimePath, scriptPath))
}

func readPOSIXArgument(command string, start int) (string, int, bool) {
	if start >= len(command) || command[start] != '\'' {
		return "", start, false
	}
	var value strings.Builder
	for index := start + 1; index < len(command); {
		if strings.HasPrefix(command[index:], posixApostrophe) {
			value.WriteByte('\'')
			index += len(posixApostrophe)
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

func parsePOSIXCommand(command string) (string, string, bool) {
	runtimePath, next, ok := readPOSIXArgument(command, 0)
	if !ok || next >= len(command) || command[next] != ' ' {
		return "", "", false
	}
	scriptPath, end, ok := readPOSIXArgument(command, next+1)
	return runtimePath, scriptPath, ok && end == len(command)
}

func readPowerShellArgument(command string, start int) (string, int, bool) {
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

func parsePowerShellSource(source string) (string, string, bool) {
	if !strings.HasPrefix(source, "& ") {
		return "", "", false
	}
	runtimePath, next, ok := readPowerShellArgument(source, 2)
	if !ok || next >= len(source) || source[next] != ' ' {
		return "", "", false
	}
	scriptPath, end, ok := readPowerShellArgument(source, next+1)
	return runtimePath, scriptPath, ok && source[end:] == powerShellExitSuffix
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

func isPowerShellExecutable(value string) bool {
	return isAbsoluteWindowsPath(value) &&
		strings.EqualFold(windowsPathBase(value), "powershell.exe")
}

func parseEncodedWindowsCommand(command string) (string, string, bool) {
	if !strings.HasPrefix(command, `"`) {
		return "", "", false
	}
	closing := strings.IndexByte(command[1:], '"')
	if closing < 0 {
		return "", "", false
	}
	closing++
	prefix := `"` + powerShellEncodedFlags
	if !isPowerShellExecutable(command[1:closing]) || !strings.HasPrefix(command[closing:], prefix) {
		return "", "", false
	}
	source, ok := decodePowerShellSource(command[closing+len(prefix):])
	if !ok {
		return "", "", false
	}
	return parsePowerShellSource(source)
}

func buildEncodedCommand(product, runtimePath, scriptPath string) (string, error) {
	if !filepath.IsAbs(runtimePath) || !filepath.IsAbs(scriptPath) {
		return "", fmt.Errorf("%s hook commands require absolute runtime and script paths", product)
	}
	if runtime.GOOS == "windows" {
		return encodedWindowsCommand(runtimePath, scriptPath), nil
	}
	return posixSource(runtimePath, scriptPath), nil
}

func parseEncodedCommand(command string) (string, string, bool) {
	if runtimePath, scriptPath, ok := parsePOSIXCommand(command); ok {
		return runtimePath, scriptPath, true
	}
	return parseEncodedWindowsCommand(command)
}

func buildShellCommand(runtimePath, scriptPath string) string {
	if runtime.GOOS == "windows" {
		return powerShellSource(runtimePath, scriptPath)
	}
	return posixSource(runtimePath, scriptPath)
}

func parseShellCommand(command string) (string, string, bool) {
	if runtime.GOOS == "windows" {
		return parsePowerShellSource(command)
	}
	return parsePOSIXCommand(command)
}

// parseQuotedWindowsCommand reads the pre-2.1 `"node" "script"` form.
func parseQuotedWindowsCommand(command string) (string, string, bool) {
	runtimePath, next, ok := readQuotedWindowsArgument(command, 0)
	if !ok || next >= len(command) || command[next] != ' ' {
		return "", "", false
	}
	scriptPath, end, ok := readQuotedWindowsArgument(command, next+1)
	return runtimePath, scriptPath, ok && end == len(command)
}

func readQuotedWindowsArgument(command string, start int) (string, int, bool) {
	if start >= len(command) || command[start] != '"' {
		return "", start, false
	}
	end := strings.IndexByte(command[start+1:], '"')
	if end < 0 {
		return "", start, false
	}
	end += start + 1
	value := command[start+1 : end]
	return value, end + 1, value != "" && !strings.ContainsAny(value, "\r\n")
}
