package plugins

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
)

const (
	augmentAgentKey        = "augment"
	augmentGuardScript     = "guard.js"
	augmentAuditScript     = "hook.js"
	augmentHookTimeout     = float64(10_000)
	augmentPOSIXApostrophe = `'"'"'`
)

var (
	augmentToolEvents = map[string]struct{}{
		"PreToolUse":  {},
		"PostToolUse": {},
	}
	augmentSessionEvents = map[string]struct{}{
		"Stop":         {},
		"SessionStart": {},
		"SessionEnd":   {},
		"Notification": {},
		"PromptSubmit": {},
	}
)

type augmentGroup struct {
	object   map[string]any
	handlers []map[string]any
}

type augmentHooks map[string][]augmentGroup

type augmentDocument struct {
	exists     bool
	configPath string
	root       map[string]any
	hooks      augmentHooks
	raw        []byte
}

type augmentRenderedDocument struct {
	document *augmentDocument
	changed  bool
	next     []byte
	remove   bool
}

type augmentWrapperPaths struct {
	guard string
	audit string
}

type augmentRuntimeContract struct {
	agentID      string
	guardPath    string
	auditPath    string
	guardWrapper string
	auditWrapper string
}

func cloneAugmentObject(value map[string]any) map[string]any {
	clone := make(map[string]any, len(value))
	for key, item := range value {
		clone[key] = item
	}
	return clone
}

func validateAugmentHandler(
	value any,
	event string,
	groupIndex, handlerIndex int,
) (map[string]any, error) {
	label := fmt.Sprintf(
		"Auggie settings handler hooks.%s[%d].hooks[%d]",
		event, groupIndex, handlerIndex,
	)
	handler, ok := value.(map[string]any)
	if !ok || handler == nil {
		return nil, fmt.Errorf("%s must be an object", label)
	}
	handlerType, ok := handler["type"].(string)
	if !ok || handlerType != "command" {
		return nil, fmt.Errorf(`%s type must be "command"`, label)
	}
	command, ok := handler["command"].(string)
	if !ok || command == "" {
		return nil, fmt.Errorf("%s requires a non-empty command", label)
	}
	if arguments, exists := handler["args"]; exists {
		values, ok := arguments.([]any)
		if !ok {
			return nil, fmt.Errorf("%s args must be an array of strings", label)
		}
		for _, argument := range values {
			if _, ok := argument.(string); !ok {
				return nil, fmt.Errorf("%s args must be an array of strings", label)
			}
		}
	}
	if timeoutValue, exists := handler["timeout"]; exists {
		timeout, ok := timeoutValue.(float64)
		if !ok || math.IsNaN(timeout) || math.IsInf(timeout, 0) || timeout <= 0 {
			return nil, fmt.Errorf("%s timeout must be a positive finite number", label)
		}
	}
	return cloneAugmentObject(handler), nil
}

func validateAugmentMetadata(value any, label string) error {
	metadata, ok := value.(map[string]any)
	if !ok || metadata == nil {
		return fmt.Errorf("%s metadata must be an object", label)
	}
	for _, key := range []string{
		"includeConversationData",
		"includeMCPMetadata",
		"includeUserContext",
	} {
		if field, exists := metadata[key]; exists {
			if _, ok := field.(bool); !ok {
				return fmt.Errorf("%s metadata.%s must be a boolean", label, key)
			}
		}
	}
	return nil
}

func validateAugmentGroup(value any, event string, groupIndex int) (augmentGroup, error) {
	label := fmt.Sprintf("Auggie settings group hooks.%s[%d]", event, groupIndex)
	object, ok := value.(map[string]any)
	if !ok || object == nil {
		return augmentGroup{}, fmt.Errorf("%s must be an object", label)
	}
	if _, sessionEvent := augmentSessionEvents[event]; sessionEvent {
		if _, hasMatcher := object["matcher"]; hasMatcher {
			return augmentGroup{}, fmt.Errorf("%s matcher is only supported for tool events", label)
		}
	}
	if matcher, exists := object["matcher"]; exists {
		if _, ok := matcher.(string); !ok {
			return augmentGroup{}, fmt.Errorf("%s matcher must be a string", label)
		}
	}
	if metadata, exists := object["metadata"]; exists {
		if err := validateAugmentMetadata(metadata, label); err != nil {
			return augmentGroup{}, err
		}
	}
	values, ok := object["hooks"].([]any)
	if !ok {
		return augmentGroup{}, fmt.Errorf("%s must contain a hooks array", label)
	}
	handlers := make([]map[string]any, 0, len(values))
	for handlerIndex, value := range values {
		handler, err := validateAugmentHandler(value, event, groupIndex, handlerIndex)
		if err != nil {
			return augmentGroup{}, err
		}
		handlers = append(handlers, handler)
	}
	return augmentGroup{object: cloneAugmentObject(object), handlers: handlers}, nil
}

func readAugmentHooks(root map[string]any) (augmentHooks, error) {
	value, exists := root["hooks"]
	if !exists {
		return augmentHooks{}, nil
	}
	object, ok := value.(map[string]any)
	if !ok || object == nil {
		return nil, fmt.Errorf(`Auggie settings field "hooks" must be an object`)
	}
	hooks := make(augmentHooks, len(object))
	for event, groupValue := range object {
		if _, toolEvent := augmentToolEvents[event]; !toolEvent {
			if _, sessionEvent := augmentSessionEvents[event]; !sessionEvent {
				return nil, fmt.Errorf(
					`Auggie settings field "hooks.%s" uses an unsupported event`,
					event,
				)
			}
		}
		values, ok := groupValue.([]any)
		if !ok {
			return nil, fmt.Errorf(
				`Auggie settings field "hooks.%s" must be an array`,
				event,
			)
		}
		groups := make([]augmentGroup, 0, len(values))
		for groupIndex, value := range values {
			group, err := validateAugmentGroup(value, event, groupIndex)
			if err != nil {
				return nil, err
			}
			groups = append(groups, group)
		}
		hooks[event] = groups
	}
	return hooks, nil
}

func parseAugmentDocument(configPath string, raw []byte) (*augmentDocument, error) {
	label := fmt.Sprintf("Auggie user settings at %s", configPath)
	root, err := decodeStrictJSONObject(raw, label)
	if err != nil {
		return nil, err
	}
	hooks, err := readAugmentHooks(root)
	if err != nil {
		return nil, err
	}
	return &augmentDocument{
		exists: true, configPath: configPath, root: root, hooks: hooks,
		raw: append([]byte(nil), raw...),
	}, nil
}

func createAugmentDocument(configPath string) *augmentDocument {
	return &augmentDocument{
		configPath: configPath,
		root:       map[string]any{},
		hooks:      augmentHooks{},
	}
}

func renderAugmentDocument(
	document *augmentDocument,
	hooks augmentHooks,
) (*augmentRenderedDocument, error) {
	if document == nil {
		return nil, fmt.Errorf("Auggie settings document is required")
	}
	if reflect.DeepEqual(hooks, document.hooks) {
		return &augmentRenderedDocument{document: document}, nil
	}
	if !document.exists && len(hooks) == 0 {
		return &augmentRenderedDocument{document: document}, nil
	}
	root := cloneAugmentObject(document.root)
	if len(hooks) == 0 {
		delete(root, "hooks")
	} else {
		root["hooks"] = renderAugmentHooks(hooks)
	}
	if len(root) == 0 {
		return &augmentRenderedDocument{
			document: document,
			changed:  true,
			remove:   true,
		}, nil
	}
	next, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode Auggie user settings: %w", err)
	}
	next = append(next, '\n')
	if _, err := parseAugmentDocument(document.configPath, next); err != nil {
		return nil, fmt.Errorf("validate rendered Auggie user settings: %w", err)
	}
	return &augmentRenderedDocument{
		document: document,
		changed:  !document.exists || !bytes.Equal(next, document.raw),
		next:     next,
	}, nil
}

func renderAugmentHooks(hooks augmentHooks) map[string]any {
	result := make(map[string]any, len(hooks))
	for event, groups := range hooks {
		values := make([]any, 0, len(groups))
		for _, group := range groups {
			object := cloneAugmentObject(group.object)
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

func readAugmentWindowsArgument(command string) (string, bool) {
	if len(command) < 2 || command[0] != '"' {
		return "", false
	}
	var value strings.Builder
	for index := 1; index < len(command); index++ {
		if command[index] == '\\' && index+1 < len(command) && command[index+1] == '"' {
			value.WriteByte('"')
			index++
			continue
		}
		if command[index] == '"' {
			return value.String(), index == len(command)-1 && value.Len() > 0
		}
		value.WriteByte(command[index])
	}
	return "", false
}

func readAugmentPOSIXArgument(command string) (string, bool) {
	if len(command) < 2 || command[0] != '\'' {
		return "", false
	}
	var value strings.Builder
	for index := 1; index < len(command); {
		if strings.HasPrefix(command[index:], augmentPOSIXApostrophe) {
			value.WriteByte('\'')
			index += len(augmentPOSIXApostrophe)
			continue
		}
		if command[index] == '\'' {
			return value.String(), index == len(command)-1 && value.Len() > 0
		}
		value.WriteByte(command[index])
		index++
	}
	return "", false
}

func parseAugmentCommand(command string) (string, bool) {
	if runtime.GOOS == "windows" {
		return readAugmentWindowsArgument(command)
	}
	return readAugmentPOSIXArgument(command)
}

func normalizeAugmentPath(value string) string {
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

func sameAugmentPath(left, right string) bool {
	return normalizeAugmentPath(left) == normalizeAugmentPath(right)
}

func sameAugmentAgentID(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func sameAugmentFileName(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func managedAugmentAgentID(
	handler map[string]any,
	wrapperName, runtimeRoot string,
) (string, bool) {
	if handler["type"] != "command" || handler["timeout"] != augmentHookTimeout {
		return "", false
	}
	if _, hasArguments := handler["args"]; hasArguments {
		return "", false
	}
	command, ok := handler["command"].(string)
	if !ok {
		return "", false
	}
	wrapperPath, ok := parseAugmentCommand(command)
	if !ok || !sameAugmentFileName(filepath.Base(wrapperPath), wrapperName) {
		return "", false
	}
	agentDirectory := filepath.Dir(wrapperPath)
	if !sameAugmentPath(filepath.Dir(agentDirectory), runtimeRoot) {
		return "", false
	}
	agentID := filepath.Base(agentDirectory)
	return agentID, agentID != "" && agentID != "." && agentID != ".."
}

func removeManagedAugmentGroups(
	groups []augmentGroup,
	wrapperName, agentID, runtimeRoot string,
) ([]augmentGroup, bool) {
	result := make([]augmentGroup, 0, len(groups))
	changed := false
	for _, group := range groups {
		handlers := make([]map[string]any, 0, len(group.handlers))
		groupChanged := false
		for _, handler := range group.handlers {
			managedID, managed := managedAugmentAgentID(handler, wrapperName, runtimeRoot)
			remove := managed && (agentID == "" || sameAugmentAgentID(managedID, agentID))
			if remove {
				changed = true
				groupChanged = true
				continue
			}
			handlers = append(handlers, handler)
		}
		if len(handlers) > 0 || !groupChanged {
			result = append(result, augmentGroup{object: group.object, handlers: handlers})
		}
	}
	return result, changed
}

func removeManagedAugmentHooks(
	hooks augmentHooks,
	agentID, runtimeRoot string,
) (augmentHooks, bool) {
	result := make(augmentHooks, len(hooks))
	for event, groups := range hooks {
		result[event] = groups
	}
	changed := false
	for _, contract := range []struct{ event, wrapper string }{
		{"PreToolUse", augmentGuardWrapperName()},
		{"PostToolUse", augmentAuditWrapperName()},
	} {
		groups, eventChanged := removeManagedAugmentGroups(
			result[contract.event], contract.wrapper, agentID, runtimeRoot,
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
