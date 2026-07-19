package plugins

import (
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const (
	kimiAgentKey           = "kimi"
	kimiGuardScript        = "guard.js"
	kimiAuditScript        = "hook.js"
	kimiHookTimeoutSeconds = int64(10)
)

var kimiSharedEvents = stringSet(
	"PreToolUse",
	"PostToolUse",
	"PostToolUseFailure",
	"UserPromptSubmit",
	"Stop",
	"StopFailure",
	"SessionStart",
	"SessionEnd",
	"SubagentStart",
	"SubagentStop",
	"PreCompact",
	"PostCompact",
	"Notification",
)

var kimiModernEvents = mergeStringSets(kimiSharedEvents, stringSet(
	"PermissionRequest",
	"PermissionResult",
	"Interrupt",
))

type kimiContract struct {
	runtimeName string
	label       string
	configPath  string
	events      map[string]struct{}
}

type kimiHook struct {
	event   string
	matcher *string
	command string
	timeout *int64
}

type kimiRuntimeContract struct {
	guard      string
	audit      string
	configPath string
}

func stringSet(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func mergeStringSets(sets ...map[string]struct{}) map[string]struct{} {
	result := map[string]struct{}{}
	for _, set := range sets {
		for value := range set {
			result[value] = struct{}{}
		}
	}
	return result
}

func validateKimiHook(value any, contract kimiContract, index int) (kimiHook, error) {
	object, ok := value.(map[string]any)
	if !ok || object == nil {
		return kimiHook{}, fmt.Errorf("%s hook %d must be a table", contract.label, index+1)
	}
	fields := make([]string, 0, len(object))
	for field := range object {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	for _, field := range fields {
		switch field {
		case "event", "matcher", "command", "timeout":
		default:
			return kimiHook{}, fmt.Errorf(
				`%s hook %d has unsupported field %q`,
				contract.label,
				index+1,
				field,
			)
		}
	}

	event, ok := object["event"].(string)
	if _, supported := contract.events[event]; !ok || !supported {
		return kimiHook{}, fmt.Errorf(
			`%s hook %d has unsupported event %q`,
			contract.label,
			index+1,
			fmt.Sprint(object["event"]),
		)
	}
	command, ok := object["command"].(string)
	if !ok || command == "" {
		return kimiHook{}, fmt.Errorf("%s hook %d requires a non-empty command", contract.label, index+1)
	}

	hook := kimiHook{event: event, command: command}
	if value, exists := object["matcher"]; exists {
		matcher, ok := value.(string)
		if !ok {
			return kimiHook{}, fmt.Errorf("%s hook %d matcher must be a string", contract.label, index+1)
		}
		hook.matcher = &matcher
	}
	if value, exists := object["timeout"]; exists {
		timeout, ok := value.(int64)
		if !ok || timeout < 1 || timeout > 600 {
			return kimiHook{}, fmt.Errorf(
				"%s hook %d timeout must be an integer from 1 to 600",
				contract.label,
				index+1,
			)
		}
		hook.timeout = &timeout
	}
	return hook, nil
}

func kimiHooks(root map[string]any, contract kimiContract) ([]kimiHook, error) {
	value, exists := root["hooks"]
	if !exists {
		return []kimiHook{}, nil
	}
	array, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf(`%s field "hooks" must be an array`, contract.label)
	}
	hooks := make([]kimiHook, 0, len(array))
	for index, value := range array {
		hook, err := validateKimiHook(value, contract, index)
		if err != nil {
			return nil, err
		}
		hooks = append(hooks, hook)
	}
	return hooks, nil
}

func normalizeKimiPath(value string) string {
	normalized := filepath.ToSlash(value)
	if runtime.GOOS == "windows" {
		return strings.ToLower(normalized)
	}
	return normalized
}

func kimiCommandEndsWithPath(command, filePath string) bool {
	normalized := strings.TrimSpace(normalizeKimiPath(command))
	expected := normalizeKimiPath(filePath)
	return strings.HasSuffix(normalized, expected) ||
		strings.HasSuffix(normalized, expected+`'`) ||
		strings.HasSuffix(normalized, expected+`"`)
}

func isManagedKimiHook(hook kimiHook, event, scriptName, agentID string) bool {
	if hook.event != event || hook.matcher != nil || hook.timeout == nil || *hook.timeout != kimiHookTimeoutSeconds {
		return false
	}
	if agentID != "" {
		expected := normalizeKimiPath(filepath.Join(".elydora", agentID, scriptName))
		return kimiCommandEndsWithPath(hook.command, "/"+expected)
	}
	normalized := normalizeKimiPath(hook.command)
	return strings.Contains(normalized, "/.elydora/") &&
		kimiCommandEndsWithPath(hook.command, "/"+normalizeKimiPath(scriptName))
}

func keptKimiHookIndices(hooks []kimiHook, agentID string) []int {
	indices := make([]int, 0, len(hooks))
	for index, hook := range hooks {
		managed := isManagedKimiHook(hook, "PreToolUse", kimiGuardScript, agentID) ||
			isManagedKimiHook(hook, "PostToolUse", kimiAuditScript, agentID)
		if !managed {
			indices = append(indices, index)
		}
	}
	return indices
}

func kimiRuntimeForDocument(document kimiDocument) *kimiRuntimeContract {
	var guard, audit string
	for _, hook := range document.hooks {
		if isManagedKimiHook(hook, "PreToolUse", kimiGuardScript, "") {
			guard = hook.command
		}
		if isManagedKimiHook(hook, "PostToolUse", kimiAuditScript, "") {
			audit = hook.command
		}
	}
	if guard == "" || audit == "" {
		return nil
	}
	return &kimiRuntimeContract{guard: guard, audit: audit, configPath: document.contract.configPath}
}
