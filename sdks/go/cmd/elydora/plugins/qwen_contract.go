package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const (
	qwenAgentKey        = "qwen"
	qwenGuardScript     = "guard.js"
	qwenAuditScript     = "hook.js"
	qwenHookTimeout     = float64(10_000)
	qwenOwnedFileMarker = "// Managed by Elydora"
	qwenPOSIXApostrophe = `'"'"'`
)

var qwenToolEvents = [...]string{"PreToolUse", "PostToolUse"}

type qwenHookSettings map[string]any

type qwenManagedRemoval struct {
	event          string
	groupIndex     int
	handlerIndexes []int
	removeGroup    bool
}

type qwenRuntimeContract struct {
	agentID   string
	guardPath string
	auditPath string
}

func buildQwenCommand(nodePath, scriptPath string) string {
	if runtime.GOOS == "windows" {
		return "& " + quoteQwenPowerShell(nodePath) + " " + quoteQwenPowerShell(scriptPath) + "; exit $LASTEXITCODE"
	}
	return quotePOSIXArgument(nodePath) + " " + quotePOSIXArgument(scriptPath)
}

func quoteQwenPowerShell(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func buildQwenGroup(nodePath, scriptPath string) map[string]any {
	shell := "bash"
	if runtime.GOOS == "windows" {
		shell = "powershell"
	}
	return map[string]any{
		"matcher": "*",
		"hooks": []any{map[string]any{
			"type": "command", "command": buildQwenCommand(nodePath, scriptPath),
			"shell": shell, "timeout": qwenHookTimeout,
		}},
	}
}

func parseQwenCommand(command string) (string, string, bool) {
	start := 0
	if runtime.GOOS == "windows" {
		if !strings.HasPrefix(command, "& ") {
			return "", "", false
		}
		start = 2
	}
	executable, next, ok := readQwenQuotedArgument(command, start)
	if !ok || next >= len(command) || command[next] != ' ' {
		return "", "", false
	}
	script, end, ok := readQwenQuotedArgument(command, next+1)
	expectedSuffix := ""
	if runtime.GOOS == "windows" {
		expectedSuffix = "; exit $LASTEXITCODE"
	}
	return executable, script, ok && command[end:] == expectedSuffix && executable != "" && script != ""
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

func qwenRuntimeRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".elydora"), nil
}

func managedQwenAgentID(handler map[string]any, scriptName, runtimeRoot string) (string, bool) {
	if len(handler) != 4 || handler["type"] != "command" || handler["timeout"] != qwenHookTimeout {
		return "", false
	}
	expectedShell := "bash"
	if runtime.GOOS == "windows" {
		expectedShell = "powershell"
	}
	if handler["shell"] != expectedShell {
		return "", false
	}
	command, ok := handler["command"].(string)
	if !ok {
		return "", false
	}
	executable, scriptPath, ok := parseQwenCommand(command)
	if !ok || !filepath.IsAbs(executable) || !filepath.IsAbs(scriptPath) ||
		!isQwenNodeExecutable(executable) || !sameQwenFileName(filepath.Base(scriptPath), scriptName) {
		return "", false
	}
	agentDirectory := filepath.Dir(scriptPath)
	if !sameQwenPath(filepath.Dir(agentDirectory), runtimeRoot) {
		return "", false
	}
	agentID := filepath.Base(agentDirectory)
	return agentID, agentID != "" && agentID != "." && agentID != ".."
}

func isQwenNodeExecutable(path string) bool {
	name := filepath.Base(path)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(name, "node.exe")
	}
	return name == "node"
}

func sameQwenFileName(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func managedQwenRemovals(settings qwenHookSettings, agentID, runtimeRoot string) []qwenManagedRemoval {
	removals := make([]qwenManagedRemoval, 0)
	for _, contract := range []struct{ event, script string }{
		{"PreToolUse", qwenGuardScript}, {"PostToolUse", qwenAuditScript},
	} {
		groups, _ := settings[contract.event].([]any)
		for groupIndex, groupValue := range groups {
			group := groupValue.(map[string]any)
			handlers := group["hooks"].([]any)
			indexes := make([]int, 0)
			for handlerIndex, handlerValue := range handlers {
				managedID, managed := managedQwenAgentID(handlerValue.(map[string]any), contract.script, runtimeRoot)
				if managed && (agentID == "" || sameQwenAgentID(managedID, agentID)) {
					indexes = append(indexes, handlerIndex)
				}
			}
			if len(indexes) == 0 {
				continue
			}
			_, hasMatcher := group["matcher"]
			_, hasHooks := group["hooks"]
			exactGroup := len(group) == 2 && hasMatcher && hasHooks && group["matcher"] == "*" && len(indexes) == len(handlers)
			removals = append(removals, qwenManagedRemoval{contract.event, groupIndex, indexes, exactGroup})
		}
	}
	return removals
}

func qwenRuntimeContracts(settings qwenHookSettings, runtimeRoot string) []qwenRuntimeContract {
	guards := managedQwenIDs(settings, "PreToolUse", qwenGuardScript, runtimeRoot)
	audits := managedQwenIDs(settings, "PostToolUse", qwenAuditScript, runtimeRoot)
	ids := make([]string, 0, len(guards))
	for guardID := range guards {
		for auditID := range audits {
			if sameQwenAgentID(guardID, auditID) {
				ids = append(ids, guardID)
				break
			}
		}
	}
	sort.Slice(ids, func(left, right int) bool {
		if runtime.GOOS == "windows" {
			return strings.ToLower(ids[left]) < strings.ToLower(ids[right])
		}
		return ids[left] < ids[right]
	})
	contracts := make([]qwenRuntimeContract, 0, len(ids))
	for _, agentID := range ids {
		root := filepath.Join(runtimeRoot, agentID)
		contracts = append(contracts, qwenRuntimeContract{
			agentID, filepath.Join(root, qwenGuardScript), filepath.Join(root, qwenAuditScript),
		})
	}
	return contracts
}

func managedQwenIDs(settings qwenHookSettings, event, script, runtimeRoot string) map[string]bool {
	ids := map[string]bool{}
	groups, _ := settings[event].([]any)
	for _, groupValue := range groups {
		for _, handlerValue := range groupValue.(map[string]any)["hooks"].([]any) {
			if agentID, ok := managedQwenAgentID(handlerValue.(map[string]any), script, runtimeRoot); ok {
				ids[agentID] = true
			}
		}
	}
	return ids
}
