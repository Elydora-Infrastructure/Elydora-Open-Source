package plugins

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"runtime"
	"sort"
	"strings"
)

const (
	grokAgentKey    = "grok"
	grokGuardScript = "guard.js"
	grokAuditScript = "hook.js"
	grokHookTimeout = float64(10)
	grokConfigFile  = "elydora-audit.json"
)

var grokMatcherRejectingEvents = stringSet(
	"SessionStart",
	"SessionEnd",
	"Stop",
	"UserPromptSubmit",
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
	raw        []byte
}

type grokRenderedDocument struct {
	document *grokDocument
	changed  bool
	next     []byte
	remove   bool
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

func cloneGrokHooks(source grokHooks) grokHooks {
	clone := make(grokHooks, len(source))
	for event, groups := range source {
		clone[event] = append([]grokGroup(nil), groups...)
	}
	return clone
}

func validateGrokHandler(
	value any,
	event string,
	groupIndex int,
	handlerIndex int,
) (map[string]any, error) {
	label := fmt.Sprintf(
		"Grok user hooks handler hooks.%s[%d].hooks[%d]",
		event,
		groupIndex,
		handlerIndex,
	)
	handler, ok := value.(map[string]any)
	if !ok || handler == nil {
		return nil, fmt.Errorf("%s must be an object", label)
	}
	handlerType, ok := handler["type"].(string)
	if !ok || (handlerType != "command" && handlerType != "http") {
		return nil, fmt.Errorf(
			`%s has unsupported type %q`,
			label,
			fmt.Sprint(handler["type"]),
		)
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
	if value, exists := handler["timeout"]; exists {
		timeout, ok := value.(float64)
		if !ok || math.IsNaN(timeout) || math.IsInf(timeout, 0) ||
			timeout < 0 || timeout > 9007199254740991 ||
			math.Trunc(timeout) != timeout {
			return nil, fmt.Errorf(
				"%s timeout must be a non-negative integer",
				label,
			)
		}
	}
	if value, exists := handler["env"]; exists {
		environment, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s env must map names to strings", label)
		}
		for _, item := range environment {
			if _, ok := item.(string); !ok {
				return nil, fmt.Errorf("%s env must map names to strings", label)
			}
		}
	}
	return cloneGrokObject(handler), nil
}

func validateGrokGroup(value any, event string, groupIndex int) (grokGroup, error) {
	label := fmt.Sprintf("Grok user hooks group hooks.%s[%d]", event, groupIndex)
	object, ok := value.(map[string]any)
	if !ok || object == nil {
		return grokGroup{}, fmt.Errorf("%s must be an object", label)
	}
	if matcher, exists := object["matcher"]; exists {
		if _, ok := matcher.(string); !ok {
			return grokGroup{}, fmt.Errorf("%s matcher must be a string", label)
		}
		if _, rejects := grokMatcherRejectingEvents[event]; rejects {
			return grokGroup{}, fmt.Errorf(
				"%s cannot declare a matcher for %s",
				label,
				event,
			)
		}
	}
	values, ok := object["hooks"].([]any)
	if !ok {
		return grokGroup{}, fmt.Errorf("%s must contain a hooks array", label)
	}
	handlers := make([]map[string]any, 0, len(values))
	for handlerIndex, value := range values {
		handler, err := validateGrokHandler(
			value,
			event,
			groupIndex,
			handlerIndex,
		)
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
		return nil, fmt.Errorf(`Grok user hooks field "hooks" must be an object`)
	}
	hooks := make(grokHooks, len(object))
	for event, groupValue := range object {
		values, ok := groupValue.([]any)
		if !ok {
			return nil, fmt.Errorf(
				`Grok user hooks field "hooks.%s" must be an array`,
				event,
			)
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

func parseGrokDocument(configPath string, raw []byte) (*grokDocument, error) {
	label := fmt.Sprintf("Grok user hooks at %s", configPath)
	root, err := decodeStrictJSONObject(raw, label)
	if err != nil {
		return nil, err
	}
	hooks, err := readGrokHooks(root)
	if err != nil {
		return nil, err
	}
	return &grokDocument{
		exists: true, configPath: configPath, root: root,
		hooks: hooks, raw: append([]byte(nil), raw...),
	}, nil
}

func createGrokDocument(configPath string) *grokDocument {
	return &grokDocument{
		configPath: configPath,
		root:       map[string]any{},
		hooks:      grokHooks{},
	}
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

func buildGrokHandler(command string) map[string]any {
	return map[string]any{
		"type": "command", "command": command, "timeout": grokHookTimeout,
	}
}

func buildGrokGroup(command string) grokGroup {
	return grokGroup{
		object: map[string]any{}, handlers: []map[string]any{buildGrokHandler(command)},
	}
}

func exactManagedGrokGroup(group grokGroup) bool {
	_, hasHooks := group.object["hooks"]
	return len(group.object) == 1 && hasHooks
}

func managedGrokReference(
	handler map[string]any,
	scriptName string,
) (*grokRuntimeReference, error) {
	if len(handler) != 3 || handler["type"] != "command" ||
		handler["timeout"] != grokHookTimeout {
		return nil, nil
	}
	command, ok := handler["command"].(string)
	if !ok {
		return nil, nil
	}
	return grokRuntimeReferenceForCommand(command, scriptName)
}

func managedGrokEvent(event string) (string, bool) {
	switch event {
	case "PreToolUse":
		return grokGuardScript, true
	case "PostToolUse", "PostToolUseFailure":
		return grokAuditScript, true
	default:
		return "", false
	}
}

func removeManagedGrokGroups(
	groups []grokGroup,
	scriptName string,
	agentID string,
) ([]grokGroup, error) {
	result := make([]grokGroup, 0, len(groups))
	for _, group := range groups {
		if !exactManagedGrokGroup(group) {
			result = append(result, group)
			continue
		}
		kept := make([]map[string]any, 0, len(group.handlers))
		for _, handler := range group.handlers {
			reference, err := managedGrokReference(handler, scriptName)
			if err != nil {
				return nil, err
			}
			remove := reference != nil &&
				(agentID == "" || sameGrokAgentID(reference.agentID, agentID))
			if !remove {
				kept = append(kept, handler)
			}
		}
		if len(kept) > 0 {
			result = append(result, grokGroup{
				object:   map[string]any{"hooks": group.object["hooks"]},
				handlers: kept,
			})
		}
	}
	return result, nil
}

func removeManagedGrokHooks(hooks grokHooks, agentID string) (grokHooks, error) {
	result := cloneGrokHooks(hooks)
	for _, contract := range []struct{ event, script string }{
		{"PreToolUse", grokGuardScript},
		{"PostToolUse", grokAuditScript},
		{"PostToolUseFailure", grokAuditScript},
	} {
		groups, err := removeManagedGrokGroups(
			result[contract.event],
			contract.script,
			agentID,
		)
		if err != nil {
			return nil, err
		}
		if len(groups) == 0 {
			delete(result, contract.event)
		} else {
			result[contract.event] = groups
		}
	}
	return result, nil
}

func entirelyManagedGrokDocument(document *grokDocument) bool {
	if !document.exists || len(document.root) != 1 || len(document.hooks) == 0 {
		return false
	}
	if _, hasHooks := document.root["hooks"]; !hasHooks {
		return false
	}
	handlerCount := 0
	for event, groups := range document.hooks {
		scriptName, managedEvent := managedGrokEvent(event)
		if !managedEvent || len(groups) == 0 {
			return false
		}
		for _, group := range groups {
			if !exactManagedGrokGroup(group) || len(group.handlers) == 0 {
				return false
			}
			for _, handler := range group.handlers {
				reference, err := managedGrokReference(handler, scriptName)
				if err != nil || reference == nil {
					return false
				}
				handlerCount++
			}
		}
	}
	return handlerCount > 0
}

func renderGrokDocument(
	document *grokDocument,
	hooks grokHooks,
) (*grokRenderedDocument, error) {
	if !document.exists && len(hooks) == 0 {
		return &grokRenderedDocument{document: document}, nil
	}
	if reflect.DeepEqual(hooks, document.hooks) {
		return &grokRenderedDocument{document: document}, nil
	}
	if len(hooks) == 0 && entirelyManagedGrokDocument(document) {
		return &grokRenderedDocument{
			document: document, changed: true, remove: true,
		}, nil
	}
	root := cloneGrokObject(document.root)
	if len(hooks) == 0 {
		delete(root, "hooks")
	} else {
		root["hooks"] = renderGrokHooks(hooks)
	}
	next, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode Grok user hooks: %w", err)
	}
	next = append(next, '\n')
	if _, err := parseGrokDocument(document.configPath, next); err != nil {
		return nil, fmt.Errorf("validate rendered Grok user hooks: %w", err)
	}
	return &grokRenderedDocument{
		document: document,
		changed:  !document.exists || !bytes.Equal(next, document.raw),
		next:     next,
	}, nil
}

func grokReferenceKey(agentID string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(agentID)
	}
	return agentID
}

func grokReferencesForEvent(
	groups []grokGroup,
	scriptName string,
) (map[string][]grokRuntimeReference, error) {
	result := map[string][]grokRuntimeReference{}
	for _, group := range groups {
		if !exactManagedGrokGroup(group) {
			continue
		}
		for _, handler := range group.handlers {
			reference, err := managedGrokReference(handler, scriptName)
			if err != nil {
				return nil, err
			}
			if reference == nil {
				continue
			}
			key := grokReferenceKey(reference.agentID)
			result[key] = append(result[key], *reference)
		}
	}
	return result, nil
}

func grokRuntimeContracts(hooks grokHooks) ([]grokRuntimeContract, error) {
	guards, err := grokReferencesForEvent(hooks["PreToolUse"], grokGuardScript)
	if err != nil {
		return nil, err
	}
	successes, err := grokReferencesForEvent(hooks["PostToolUse"], grokAuditScript)
	if err != nil {
		return nil, err
	}
	failures, err := grokReferencesForEvent(
		hooks["PostToolUseFailure"],
		grokAuditScript,
	)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(guards))
	for key := range guards {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	contracts := make([]grokRuntimeContract, 0, len(keys))
	for _, key := range keys {
		guard := guards[key]
		success := successes[key]
		failure := failures[key]
		if len(guard) != 1 || len(success) != 1 || len(failure) != 1 ||
			!sameGrokPath(success[0].scriptPath, failure[0].scriptPath) {
			continue
		}
		contracts = append(contracts, grokRuntimeContract{
			agentID: guard[0].agentID, guardPath: guard[0].scriptPath,
			auditPath: success[0].scriptPath,
		})
	}
	return contracts, nil
}
