package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	droidAgentKey        = "droid"
	droidGuardScript     = "guard.js"
	droidAuditScript     = "hook.js"
	droidHookTimeout     = float64(10)
	droidOwnedFileMarker = "// Managed by Elydora"
	droidPOSIXApostrophe = `'"'"'`
)

var droidToolEvents = [...]string{"PreToolUse", "PostToolUse"}

var droidEventNames = map[string]bool{
	"PreToolUse": true, "PostToolUse": true, "Notification": true,
	"UserPromptSubmit": true, "Stop": true, "SubagentStop": true,
	"PreCompact": true, "SessionStart": true, "SessionEnd": true,
}

var droidFlagNames = map[string]bool{"hooksDisabled": true, "showHookOutput": true}

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
	settings := make(droidHookSettings, len(object))
	for key, item := range object {
		if droidFlagNames[key] {
			if _, ok := item.(bool); !ok {
				return nil, fmt.Errorf(`%s field %q must be a boolean`, label, key)
			}
			settings[key] = item
			continue
		}
		if !droidEventNames[key] {
			return nil, fmt.Errorf(`%s contains unsupported field %q`, label, key)
		}
		groups, ok := item.([]any)
		if !ok {
			return nil, fmt.Errorf(`%s field %q must be an array`, label, key)
		}
		for groupIndex, group := range groups {
			if err := validateDroidGroup(group, label, key, groupIndex); err != nil {
				return nil, err
			}
		}
		settings[key] = item
	}
	return settings, nil
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
	if _, ok := handler["command"].(string); !ok {
		return fmt.Errorf("%s command must be a string", location)
	}
	if timeout, exists := handler["timeout"]; exists {
		number, ok := timeout.(float64)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
			return fmt.Errorf("%s timeout must be a finite number", location)
		}
	}
	return nil
}

func validateDroidRegexes(settings ...droidHookSettings) error {
	entries := make([]droidRegexEntry, 0)
	for _, source := range settings {
		for event, value := range source {
			if droidFlagNames[event] {
				continue
			}
			for index, groupValue := range value.([]any) {
				group := groupValue.(map[string]any)
				if matcher, ok := group["matcher"].(string); ok && matcher != "" && matcher != "*" {
					entries = append(entries, droidRegexEntry{
						Label: fmt.Sprintf(`Factory Droid hooks field %q[%d] matcher`, event, index), Pattern: matcher,
					})
				}
				if expression, ok := group["commandRegex"].(string); ok {
					entries = append(entries, droidRegexEntry{
						Label: fmt.Sprintf(`Factory Droid hooks field %q[%d] commandRegex`, event, index), Pattern: expression,
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
		return quoteDroidWindowsArgument(nodePath) + " " + quoteDroidWindowsArgument(scriptPath)
	}
	return quotePOSIXArgument(nodePath) + " " + quotePOSIXArgument(scriptPath)
}

func quoteDroidWindowsArgument(value string) string {
	quoted := quoteWindowsArgument(value)
	if strings.HasPrefix(quoted, `"`) {
		return quoted
	}
	return `"` + quoted + `"`
}

func buildDroidGroup(nodePath, scriptPath string) map[string]any {
	return map[string]any{
		"matcher": "*",
		"hooks": []any{map[string]any{
			"type": "command", "command": buildDroidCommand(nodePath, scriptPath), "timeout": droidHookTimeout,
		}},
	}
}

func parseDroidCommand(command string) (string, string, bool) {
	reader := readDroidPOSIXArgument
	if runtime.GOOS == "windows" {
		reader = readDroidWindowsArgument
	}
	executable, next, ok := reader(command, 0)
	if !ok || next >= len(command) || command[next] != ' ' {
		return "", "", false
	}
	script, end, ok := reader(command, next+1)
	return executable, script, ok && end == len(command) && executable != "" && script != ""
}

func readDroidWindowsArgument(command string, start int) (string, int, bool) {
	if start >= len(command) || command[start] != '"' {
		return "", start, false
	}
	var value strings.Builder
	for index := start + 1; index < len(command); index++ {
		if command[index] == '\\' && index+1 < len(command) && command[index+1] == '"' {
			value.WriteByte('"')
			index++
			continue
		}
		if command[index] == '"' {
			return value.String(), index + 1, true
		}
		value.WriteByte(command[index])
	}
	return "", start, false
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
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".elydora"), nil
}

func managedDroidAgentID(handler map[string]any, scriptName, runtimeRoot string) (string, bool) {
	if len(handler) != 3 || handler["type"] != "command" || handler["timeout"] != droidHookTimeout {
		return "", false
	}
	command, ok := handler["command"].(string)
	if !ok {
		return "", false
	}
	executable, scriptPath, ok := parseDroidCommand(command)
	if !ok || !filepath.IsAbs(executable) || !filepath.IsAbs(scriptPath) || !isDroidNodeExecutable(executable) ||
		!sameDroidFileName(filepath.Base(scriptPath), scriptName) {
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
	if runtime.GOOS == "windows" {
		return strings.EqualFold(name, "node.exe")
	}
	return name == "node"
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
				managedID, managed := managedDroidAgentID(handlerValue.(map[string]any), contract.script, runtimeRoot)
				if managed && (agentID == "" || sameDroidAgentID(managedID, agentID)) {
					indexes = append(indexes, handlerIndex)
				}
			}
			if len(indexes) == 0 {
				continue
			}
			_, hasMatcher := group["matcher"]
			_, hasHooks := group["hooks"]
			exactGroup := len(group) == 2 && hasMatcher && hasHooks && group["matcher"] == "*" && len(indexes) == len(handlers)
			removals = append(removals, droidManagedRemoval{contract.event, groupIndex, indexes, exactGroup})
		}
	}
	return removals
}

func droidRuntimeContracts(settings droidHookSettings, runtimeRoot string) []droidRuntimeContract {
	guards := managedDroidIDs(settings, "PreToolUse", droidGuardScript, runtimeRoot)
	audits := managedDroidIDs(settings, "PostToolUse", droidAuditScript, runtimeRoot)
	contracts := make([]droidRuntimeContract, 0)
	for guardID := range guards {
		matched := false
		for auditID := range audits {
			if sameDroidAgentID(guardID, auditID) {
				matched = true
				break
			}
		}
		if matched {
			root := filepath.Join(runtimeRoot, guardID)
			contracts = append(contracts, droidRuntimeContract{
				guardID, filepath.Join(root, droidGuardScript), filepath.Join(root, droidAuditScript),
			})
		}
	}
	return contracts
}

func managedDroidIDs(settings droidHookSettings, event, script, runtimeRoot string) map[string]bool {
	ids := map[string]bool{}
	groups, _ := settings[event].([]any)
	for _, groupValue := range groups {
		for _, handlerValue := range groupValue.(map[string]any)["hooks"].([]any) {
			if agentID, ok := managedDroidAgentID(handlerValue.(map[string]any), script, runtimeRoot); ok {
				ids[agentID] = true
			}
		}
	}
	return ids
}

func mergeDroidSettings(primary, fallback droidHookSettings) droidHookSettings {
	merged := make(droidHookSettings, len(primary)+len(fallback))
	for key, value := range fallback {
		merged[key] = value
	}
	for key, value := range primary {
		merged[key] = value
	}
	return merged
}
