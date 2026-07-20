package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	droidAgentKey          = "droid"
	droidGuardScript       = "guard.js"
	droidAuditScript       = "hook.js"
	droidHookTimeout       = float64(10)
	droidOwnedFileMarker   = "// Managed by Elydora"
	droidPOSIXApostrophe   = `'"'"'`
	droidWindowsExitSuffix = "; exit $LASTEXITCODE"
)

var droidToolEvents = [...]string{"PreToolUse", "PostToolUse"}

type droidHookSettings map[string]any

type droidManagedRemoval struct {
	event          string
	groupIndex     int
	handlerIndexes []int
	removeGroup    bool
}

type droidRuntimeContract struct {
	agentID   string
	guardPath string
	auditPath string
}

type droidRegexEntry struct {
	Label   string `json:"label"`
	Pattern string `json:"pattern"`
}

func readDroidHookSettings(value any, label string) (droidHookSettings, error) {
	object, ok := value.(map[string]any)
	if !ok || object == nil {
		return nil, fmt.Errorf("%s must contain a JSON object", label)
	}
	hooks := make(droidHookSettings, len(object))
	for event, item := range object {
		groups, ok := item.([]any)
		if !ok {
			return nil, fmt.Errorf(`%s field %q must be an array`, label, event)
		}
		for groupIndex, group := range groups {
			if err := validateDroidGroup(group, label, event, groupIndex); err != nil {
				return nil, err
			}
		}
		hooks[event] = item
	}
	return hooks, nil
}

func validateDroidGroup(value any, label, event string, groupIndex int) error {
	location := fmt.Sprintf(`%s field %q[%d]`, label, event, groupIndex)
	group, ok := value.(map[string]any)
	if !ok || group == nil {
		return fmt.Errorf("%s must be an object", location)
	}
	if matcher, exists := group["matcher"]; exists {
		if _, ok := matcher.(string); !ok {
			return fmt.Errorf("%s matcher must be a string", location)
		}
	}
	if expression, exists := group["commandRegex"]; exists {
		if _, ok := expression.(string); !ok {
			return fmt.Errorf("%s commandRegex must be a string", location)
		}
	}
	handlers, ok := group["hooks"].([]any)
	if !ok {
		return fmt.Errorf("%s must contain a hooks array", location)
	}
	for handlerIndex, handler := range handlers {
		if err := validateDroidHandler(handler, location, handlerIndex); err != nil {
			return err
		}
	}
	return nil
}

func validateDroidHandler(value any, groupLabel string, handlerIndex int) error {
	location := fmt.Sprintf("%s.hooks[%d]", groupLabel, handlerIndex)
	handler, ok := value.(map[string]any)
	if !ok || handler == nil {
		return fmt.Errorf("%s must be an object", location)
	}
	if handler["type"] != "command" {
		return fmt.Errorf(`%s type must be "command"`, location)
	}
	command, ok := handler["command"].(string)
	if !ok || strings.TrimSpace(command) == "" {
		return fmt.Errorf("%s command must be a non-empty string", location)
	}
	if timeout, exists := handler["timeout"]; exists {
		number, ok := timeout.(float64)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) || number <= 0 {
			return fmt.Errorf("%s timeout must be a positive finite number", location)
		}
	}
	return nil
}

func validateDroidRegexes(settings ...droidHookSettings) error {
	entries := make([]droidRegexEntry, 0)
	for _, source := range settings {
		for event, value := range source {
			groups, _ := value.([]any)
			for index, groupValue := range groups {
				group := groupValue.(map[string]any)
				if matcher, ok := group["matcher"].(string); ok && matcher != "" && matcher != "*" {
					entries = append(entries, droidRegexEntry{
						Label:   fmt.Sprintf(`Factory Droid hooks field %q[%d] matcher`, event, index),
						Pattern: matcher,
					})
				}
				if expression, ok := group["commandRegex"].(string); ok {
					entries = append(entries, droidRegexEntry{
						Label:   fmt.Sprintf(`Factory Droid hooks field %q[%d] commandRegex`, event, index),
						Pattern: expression,
					})
				}
			}
		}
	}
	if len(entries) == 0 {
		return nil
	}
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		return fmt.Errorf("Node.js runtime is required to validate Factory Droid hook expressions: %w", err)
	}
	payload, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("encode Factory Droid regular expressions: %w", err)
	}
	validator := `import fs from 'node:fs';
const entries = JSON.parse(fs.readFileSync(0, 'utf8'));
for (const entry of entries) {
  try { new RegExp(entry.pattern); }
  catch (error) {
    process.stderr.write(entry.label + ': ' + (error instanceof Error ? error.message : String(error)));
    process.exit(1);
  }
}`
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// #nosec G204 -- nodePath is resolved by exec.LookPath and every argument is fixed.
	command := exec.CommandContext(ctx, nodePath, "--input-type=module", "--eval", validator)
	command.Stdin = bytes.NewReader(payload)
	output, runErr := command.CombinedOutput()
	if ctx.Err() != nil {
		return fmt.Errorf("Factory Droid regular expression validation timed out: %w", ctx.Err())
	}
	if runErr != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = runErr.Error()
		}
		return fmt.Errorf("Factory Droid matcher must be a valid JavaScript regular expression: %s", message)
	}
	return nil
}

func buildDroidCommand(nodePath, scriptPath string) string {
	if runtime.GOOS == "windows" {
		return "& " + quoteDroidPowerShellArgument(nodePath) + " " +
			quoteDroidPowerShellArgument(scriptPath) + droidWindowsExitSuffix
	}
	return quotePOSIXArgument(nodePath) + " " + quotePOSIXArgument(scriptPath)
}

func quoteDroidPowerShellArgument(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func buildDroidGroup(nodePath, scriptPath string) map[string]any {
	return map[string]any{
		"matcher": "*",
		"hooks": []any{map[string]any{
			"type": "command", "command": buildDroidCommand(nodePath, scriptPath), "timeout": droidHookTimeout,
		}},
	}
}

func parseDroidCommand(command string, includeLegacy ...bool) (string, string, bool) {
	if runtime.GOOS != "windows" {
		return parseDroidPOSIXCommand(command)
	}
	executable, script, ok := parseDroidPowerShellCommand(command)
	if ok || len(includeLegacy) == 0 || !includeLegacy[0] {
		return executable, script, ok
	}
	return parseDroidLegacyWindowsCommand(command)
}

func parseDroidPOSIXCommand(command string) (string, string, bool) {
	executable, next, ok := readDroidPOSIXArgument(command, 0)
	if !ok || next >= len(command) || command[next] != ' ' {
		return "", "", false
	}
	script, end, ok := readDroidPOSIXArgument(command, next+1)
	return executable, script, ok && end == len(command) && executable != "" && script != ""
}

func parseDroidPowerShellCommand(command string) (string, string, bool) {
	if !strings.HasPrefix(command, "& ") {
		return "", "", false
	}
	executable, next, ok := readDroidPowerShellArgument(command, 2)
	if !ok || next >= len(command) || command[next] != ' ' {
		return "", "", false
	}
	script, end, ok := readDroidPowerShellArgument(command, next+1)
	return executable, script, ok && command[end:] == droidWindowsExitSuffix && executable != "" && script != ""
}

func parseDroidLegacyWindowsCommand(command string) (string, string, bool) {
	executable, next, ok := readDroidLegacyWindowsArgument(command, 0)
	if !ok || next >= len(command) || command[next] != ' ' {
		return "", "", false
	}
	script, end, ok := readDroidLegacyWindowsArgument(command, next+1)
	return executable, script, ok && end == len(command) && executable != "" && script != ""
}

func readDroidPowerShellArgument(command string, start int) (string, int, bool) {
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

func readDroidLegacyWindowsArgument(command string, start int) (string, int, bool) {
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

func readDroidPOSIXArgument(command string, start int) (string, int, bool) {
	if start >= len(command) || command[start] != '\'' {
		return "", start, false
	}
	var value strings.Builder
	for index := start + 1; index < len(command); {
		if strings.HasPrefix(command[index:], droidPOSIXApostrophe) {
			value.WriteByte('\'')
			index += len(droidPOSIXApostrophe)
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

func sameDroidPath(left, right string) bool {
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

func sameDroidAgentID(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func droidRuntimeRoot() (string, error) {
	return AgentRuntimeRoot()
}

func managedDroidAgentID(
	handler map[string]any,
	scriptName, runtimeRoot string,
	includeLegacy ...bool,
) (string, bool) {
	if len(handler) != 3 || handler["type"] != "command" || handler["timeout"] != droidHookTimeout {
		return "", false
	}
	command, ok := handler["command"].(string)
	if !ok {
		return "", false
	}
	executable, scriptPath, ok := parseDroidCommand(command, includeLegacy...)
	if !ok || !filepath.IsAbs(executable) || !filepath.IsAbs(scriptPath) ||
		!isDroidNodeExecutable(executable) || !sameDroidFileName(filepath.Base(scriptPath), scriptName) {
		return "", false
	}
	agentDirectory := filepath.Dir(scriptPath)
	if !sameDroidPath(filepath.Dir(agentDirectory), runtimeRoot) {
		return "", false
	}
	agentID := filepath.Base(agentDirectory)
	return agentID, agentID != "" && agentID != "." && agentID != ".."
}

func isDroidNodeExecutable(path string) bool {
	name := filepath.Base(path)
	return name == "node" || strings.EqualFold(name, "node.exe")
}

func sameDroidFileName(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func managedDroidRemovals(settings droidHookSettings, agentID, runtimeRoot string) []droidManagedRemoval {
	removals := make([]droidManagedRemoval, 0)
	for _, contract := range []struct{ event, script string }{
		{"PreToolUse", droidGuardScript}, {"PostToolUse", droidAuditScript},
	} {
		groups, _ := settings[contract.event].([]any)
		for groupIndex, value := range groups {
			group := value.(map[string]any)
			handlers := group["hooks"].([]any)
			indexes := make([]int, 0)
			for handlerIndex, handlerValue := range handlers {
				managedID, managed := managedDroidAgentID(
					handlerValue.(map[string]any), contract.script, runtimeRoot, true,
				)
				if managed && (agentID == "" || sameDroidAgentID(managedID, agentID)) {
					indexes = append(indexes, handlerIndex)
				}
			}
			if len(indexes) == 0 {
				continue
			}
			_, hasMatcher := group["matcher"]
			_, hasHooks := group["hooks"]
			exactGroup := len(group) == 2 && hasMatcher && hasHooks &&
				group["matcher"] == "*" && len(indexes) == len(handlers)
			removals = append(removals, droidManagedRemoval{
				contract.event, groupIndex, indexes, exactGroup,
			})
		}
	}
	return removals
}

func droidRuntimeContracts(settings droidHookSettings, runtimeRoot string) []droidRuntimeContract {
	guards := managedDroidIDs(settings, "PreToolUse", droidGuardScript, runtimeRoot)
	audits := managedDroidIDs(settings, "PostToolUse", droidAuditScript, runtimeRoot)
	guardIDs := make([]string, 0, len(guards))
	for agentID := range guards {
		guardIDs = append(guardIDs, agentID)
	}
	sort.Strings(guardIDs)
	contracts := make([]droidRuntimeContract, 0, len(guardIDs))
	for _, guardID := range guardIDs {
		for auditID := range audits {
			if !sameDroidAgentID(guardID, auditID) {
				continue
			}
			root := filepath.Join(runtimeRoot, guardID)
			contracts = append(contracts, droidRuntimeContract{
				guardID,
				filepath.Join(root, droidGuardScript),
				filepath.Join(root, droidAuditScript),
			})
			break
		}
	}
	return contracts
}

func managedDroidIDs(settings droidHookSettings, event, script, runtimeRoot string) map[string]bool {
	ids := map[string]bool{}
	groups, _ := settings[event].([]any)
	for _, groupValue := range groups {
		group := groupValue.(map[string]any)
		handlers := group["hooks"].([]any)
		if len(group) != 2 || group["matcher"] != "*" || len(handlers) != 1 {
			continue
		}
		if agentID, ok := managedDroidAgentID(handlers[0].(map[string]any), script, runtimeRoot); ok {
			ids[agentID] = true
		}
	}
	return ids
}
