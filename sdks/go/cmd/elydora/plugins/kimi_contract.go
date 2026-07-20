package plugins

import (
	"fmt"
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
	generation     string
	runtimeName    string
	label          string
	directoryLabel string
	configPath     string
	events         map[string]struct{}
}

type kimiHook struct {
	event      string
	matcher    *string
	command    string
	timeout    *int64
	fieldCount int
}

type kimiRuntimeContract struct {
	agentID    string
	guardPath  string
	auditPath  string
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

	hook := kimiHook{event: event, command: command, fieldCount: len(object)}
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

func buildKimiHook(event, command string) (kimiHook, error) {
	switch event {
	case "PreToolUse", "PostToolUse", "PostToolUseFailure":
	default:
		return kimiHook{}, fmt.Errorf("unsupported managed Kimi event: %s", event)
	}
	timeout := kimiHookTimeoutSeconds
	return kimiHook{
		event: event, command: command, timeout: &timeout, fieldCount: 3,
	}, nil
}

func managedKimiReference(
	hook kimiHook,
	event string,
	scriptName string,
) (*kimiRuntimeReference, error) {
	if hook.fieldCount != 3 || hook.event != event || hook.matcher != nil ||
		hook.timeout == nil || *hook.timeout != kimiHookTimeoutSeconds {
		return nil, nil
	}
	return kimiRuntimeReferenceForCommand(hook.command, scriptName)
}

func managedKimiEvent(event string) (string, string, bool) {
	switch event {
	case "PreToolUse":
		return event, kimiGuardScript, true
	case "PostToolUse", "PostToolUseFailure":
		return event, kimiAuditScript, true
	default:
		return "", "", false
	}
}

func keptKimiHookIndices(hooks []kimiHook, agentID string) ([]int, error) {
	indices := make([]int, 0, len(hooks))
	for index, hook := range hooks {
		event, scriptName, managedEvent := managedKimiEvent(hook.event)
		var reference *kimiRuntimeReference
		var err error
		if managedEvent {
			reference, err = managedKimiReference(hook, event, scriptName)
			if err != nil {
				return nil, err
			}
		}
		remove := reference != nil &&
			(agentID == "" || sameKimiAgentID(reference.agentID, agentID))
		if !remove {
			indices = append(indices, index)
		}
	}
	return indices, nil
}

func kimiReferenceKey(agentID string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(agentID)
	}
	return agentID
}

func kimiReferencesForEvent(
	hooks []kimiHook,
	event string,
	scriptName string,
) (map[string][]kimiRuntimeReference, error) {
	result := map[string][]kimiRuntimeReference{}
	for _, hook := range hooks {
		reference, err := managedKimiReference(hook, event, scriptName)
		if err != nil {
			return nil, err
		}
		if reference == nil {
			continue
		}
		key := kimiReferenceKey(reference.agentID)
		result[key] = append(result[key], *reference)
	}
	return result, nil
}

func kimiRuntimeContracts(documents []kimiDocument) ([]kimiRuntimeContract, error) {
	contracts := make([]kimiRuntimeContract, 0)
	for _, document := range documents {
		guards, err := kimiReferencesForEvent(document.hooks, "PreToolUse", kimiGuardScript)
		if err != nil {
			return nil, err
		}
		successes, err := kimiReferencesForEvent(document.hooks, "PostToolUse", kimiAuditScript)
		if err != nil {
			return nil, err
		}
		failures, err := kimiReferencesForEvent(
			document.hooks, "PostToolUseFailure", kimiAuditScript,
		)
		if err != nil {
			return nil, err
		}
		keys := make([]string, 0, len(guards))
		for key := range guards {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			guard := guards[key]
			success := successes[key]
			failure := failures[key]
			if len(guard) != 1 || len(success) != 1 || len(failure) != 1 ||
				!sameKimiPath(success[0].scriptPath, failure[0].scriptPath) {
				continue
			}
			contracts = append(contracts, kimiRuntimeContract{
				agentID: guard[0].agentID, guardPath: guard[0].scriptPath,
				auditPath: success[0].scriptPath, configPath: document.contract.configPath,
			})
		}
	}
	return contracts, nil
}
