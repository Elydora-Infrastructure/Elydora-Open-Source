package plugins

import (
	"fmt"
	"math"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	grokAgentKey        = "grok"
	grokGuardScript     = "guard.js"
	grokAuditScript     = "hook.js"
	grokHookTimeout     = float64(10)
	grokConfigFile      = "elydora-audit.json"
	grokPOSIXApostrophe = `'"'"'`
)

type grokGroup struct {
	object   map[string]any
	handlers []map[string]any
}

type grokHooks map[string][]grokGroup

type grokDocument struct {
	exists     bool
	configPath string
	root       map[string]any
	hooks      grokHooks
}

type grokRuntimeContract struct {
	agentID   string
	guardPath string
	auditPath string
}

func cloneGrokObject(value map[string]any) map[string]any {
	clone := make(map[string]any, len(value))
	for key, item := range value {
		clone[key] = item
	}
	return clone
}

func validateGrokHandler(value any, event string, groupIndex, handlerIndex int) (map[string]any, error) {
	label := fmt.Sprintf("Grok hooks config handler hooks.%s[%d].hooks[%d]", event, groupIndex, handlerIndex)
	handler, ok := value.(map[string]any)
	if !ok || handler == nil {
		return nil, fmt.Errorf("%s must be an object", label)
	}
	handlerType, ok := handler["type"].(string)
	if !ok || (handlerType != "command" && handlerType != "http") {
		return nil, fmt.Errorf(`%s has unsupported type %q`, label, fmt.Sprint(handler["type"]))
	}
	if handlerType == "command" {
		command, ok := handler["command"].(string)
		if !ok || command == "" {
			return nil, fmt.Errorf("%s requires a non-empty command", label)
		}
	}
	if handlerType == "http" {
		url, ok := handler["url"].(string)
		if !ok || url == "" {
			return nil, fmt.Errorf("%s requires a non-empty url", label)
		}
	}
	if timeoutValue, exists := handler["timeout"]; exists {
		timeout, ok := timeoutValue.(float64)
		if !ok || math.IsNaN(timeout) || math.IsInf(timeout, 0) || timeout <= 0 {
			return nil, fmt.Errorf("%s timeout must be a positive finite number", label)
		}
	}
	return cloneGrokObject(handler), nil
}

func validateGrokGroup(value any, event string, groupIndex int) (grokGroup, error) {
	label := fmt.Sprintf("Grok hooks config group hooks.%s[%d]", event, groupIndex)
	object, ok := value.(map[string]any)
	if !ok || object == nil {
		return grokGroup{}, fmt.Errorf("%s must be an object", label)
	}
	if matcher, exists := object["matcher"]; exists {
		if _, ok := matcher.(string); !ok {
			return grokGroup{}, fmt.Errorf("%s matcher must be a string", label)
		}
	}
	values, ok := object["hooks"].([]any)
	if !ok {
		return grokGroup{}, fmt.Errorf("%s must contain a hooks array", label)
	}
	handlers := make([]map[string]any, 0, len(values))
	for handlerIndex, value := range values {
		handler, err := validateGrokHandler(value, event, groupIndex, handlerIndex)
		if err != nil {
			return grokGroup{}, err
		}
		handlers = append(handlers, handler)
	}
	return grokGroup{object: cloneGrokObject(object), handlers: handlers}, nil
}

func readGrokHooks(root map[string]any) (grokHooks, error) {
	value, exists := root["hooks"]
	if !exists {
		return grokHooks{}, nil
	}
	object, ok := value.(map[string]any)
	if !ok || object == nil {
		return nil, fmt.Errorf(`Grok hooks config field "hooks" must be an object`)
	}
	hooks := make(grokHooks, len(object))
	for event, groupValue := range object {
		values, ok := groupValue.([]any)
		if !ok {
			return nil, fmt.Errorf(`Grok hooks config field "hooks.%s" must be an array`, event)
		}
		groups := make([]grokGroup, 0, len(values))
		for groupIndex, value := range values {
			group, err := validateGrokGroup(value, event, groupIndex)
			if err != nil {
				return nil, err
			}
			groups = append(groups, group)
		}
		hooks[event] = groups
	}
	return hooks, nil
}

func renderGrokHooks(hooks grokHooks) map[string]any {
	result := make(map[string]any, len(hooks))
	for event, groups := range hooks {
		values := make([]any, 0, len(groups))
		for _, group := range groups {
			object := cloneGrokObject(group.object)
			handlers := make([]any, 0, len(group.handlers))
			for _, handler := range group.handlers {
				handlers = append(handlers, handler)
			}
			object["hooks"] = handlers
			values = append(values, object)
		}
		result[event] = values
	}
	return result
}

func readGrokWindowsArgument(command string, start int) (string, int, bool) {
	if start >= len(command) || command[start] != '"' {
		return "", start, false
	}
	var value strings.Builder
	for index := start + 1; index < len(command); index++ {
		if command[index] == '"' {
			return value.String(), index + 1, true
		}
		value.WriteByte(command[index])
	}
	return "", start, false
}

func readGrokPOSIXArgument(command string, start int) (string, int, bool) {
	if start >= len(command) || command[start] != '\'' {
		return "", start, false
	}
	var value strings.Builder
	for index := start + 1; index < len(command); {
		if strings.HasPrefix(command[index:], grokPOSIXApostrophe) {
			value.WriteByte('\'')
			index += len(grokPOSIXApostrophe)
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

func parseGrokCommand(command string) (string, string, bool) {
	reader := readGrokPOSIXArgument
	if runtime.GOOS == "windows" {
		reader = readGrokWindowsArgument
	}
	executable, next, ok := reader(command, 0)
	if !ok || next >= len(command) || command[next] != ' ' {
		return "", "", false
	}
	script, end, ok := reader(command, next+1)
	if !ok || end != len(command) || executable == "" || script == "" {
		return "", "", false
	}
	return executable, script, true
}

func normalizeGrokPath(value string) string {
	absolute, err := filepath.Abs(value)
	if err == nil {
		value = absolute
	}
	value = filepath.ToSlash(filepath.Clean(value))
	if runtime.GOOS == "windows" {
		return strings.ToLower(value)
	}
	return value
}

func sameGrokPath(left, right string) bool {
	return normalizeGrokPath(left) == normalizeGrokPath(right)
}

func sameGrokAgentID(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func sameGrokFileName(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func managedGrokAgentID(handler map[string]any, scriptName, runtimeRoot string) (string, bool) {
	if handler["type"] != "command" || handler["timeout"] != grokHookTimeout {
		return "", false
	}
	command, ok := handler["command"].(string)
	if !ok {
		return "", false
	}
	_, scriptPath, ok := parseGrokCommand(command)
	if !ok || !sameGrokFileName(filepath.Base(scriptPath), scriptName) {
		return "", false
	}
	agentDirectory := filepath.Dir(scriptPath)
	if !sameGrokPath(filepath.Dir(agentDirectory), runtimeRoot) {
		return "", false
	}
	agentID := filepath.Base(agentDirectory)
	return agentID, agentID != "" && agentID != "." && agentID != ".."
}

func removeManagedGrokGroups(
	groups []grokGroup,
	scriptName, agentID, runtimeRoot string,
) ([]grokGroup, bool) {
	result := make([]grokGroup, 0, len(groups))
	changed := false
	for _, group := range groups {
		if _, hasMatcher := group.object["matcher"]; hasMatcher {
			result = append(result, group)
			continue
		}
		handlers := make([]map[string]any, 0, len(group.handlers))
		groupChanged := false
		for _, handler := range group.handlers {
			managedID, managed := managedGrokAgentID(handler, scriptName, runtimeRoot)
			remove := managed && (agentID == "" || sameGrokAgentID(managedID, agentID))
			if remove {
				changed = true
				groupChanged = true
				continue
			}
			handlers = append(handlers, handler)
		}
		if len(handlers) > 0 || !groupChanged {
			result = append(result, grokGroup{object: group.object, handlers: handlers})
		}
	}
	return result, changed
}

func removeManagedGrokHooks(hooks grokHooks, agentID, runtimeRoot string) (grokHooks, bool) {
	result := make(grokHooks, len(hooks))
	for event, groups := range hooks {
		result[event] = groups
	}
	changed := false
	for _, contract := range []struct{ event, script string }{
		{"PreToolUse", grokGuardScript},
		{"PostToolUse", grokAuditScript},
	} {
		groups, eventChanged := removeManagedGrokGroups(
			result[contract.event], contract.script, agentID, runtimeRoot,
		)
		if !eventChanged {
			continue
		}
		changed = true
		if len(groups) == 0 {
			delete(result, contract.event)
		} else {
			result[contract.event] = groups
		}
	}
	return result, changed
}
