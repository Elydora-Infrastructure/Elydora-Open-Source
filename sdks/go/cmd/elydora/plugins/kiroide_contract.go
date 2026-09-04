package plugins

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
)

const (
	kiroIdeAgentKey         = "kiroide"
	kiroIdeConfigFile       = "elydora-audit.json"
	kiroIdeLegacyFile       = "elydora-audit.kiro.hook"
	kiroIdeGuardScript      = "guard.js"
	kiroIdeAuditScript      = "hook.js"
	kiroIdeGuardName        = "elydora-guard"
	kiroIdeAuditName        = "elydora-audit"
	kiroIdeHookTimeout      = float64(10)
	kiroIdeGuardDescription = "Block tool use when the Elydora agent is frozen"
	kiroIdeAuditDescription = "Record tool use in the Elydora audit trail"
)

var kiroIdeTriggers = stringSet(
	"SessionStart",
	"Stop",
	"PreToolUse",
	"PostToolUse",
	"PreTaskExec",
	"PostTaskExec",
	"UserPromptSubmit",
	"PostFileCreate",
	"PostFileSave",
	"PostFileDelete",
)

type kiroIdeDocument struct {
	exists   bool
	filePath string
	root     map[string]any
	hooks    []map[string]any
	snapshot *managedFileSnapshot
}

type kiroIdeLegacyDocument struct {
	filePath string
	snapshot *managedFileSnapshot
	contract *managedRuntimeContract
}

type kiroIdeRenderedDocument struct {
	document *kiroIdeDocument
	changed  bool
	next     []byte
	remove   bool
}

type kiroIdeHookSpecification struct {
	description string
	trigger     string
	scriptName  string
}

func kiroIdeSpecification(name any) (kiroIdeHookSpecification, bool) {
	switch name {
	case kiroIdeGuardName:
		return kiroIdeHookSpecification{
			kiroIdeGuardDescription, "PreToolUse", kiroIdeGuardScript,
		}, true
	case kiroIdeAuditName:
		return kiroIdeHookSpecification{
			kiroIdeAuditDescription, "PostToolUse", kiroIdeAuditScript,
		}, true
	default:
		return kiroIdeHookSpecification{}, false
	}
}

func validateKiroIdeAction(value any, label string) error {
	action, ok := value.(map[string]any)
	if !ok || action == nil {
		return fmt.Errorf("%s action must be an object", label)
	}
	switch action["type"] {
	case "command":
		command, ok := action["command"].(string)
		if !ok || command == "" {
			return fmt.Errorf("%s command action requires a non-empty command", label)
		}
	case "agent":
		prompt, ok := action["prompt"].(string)
		if !ok || prompt == "" {
			return fmt.Errorf("%s agent action requires a non-empty prompt", label)
		}
	default:
		return fmt.Errorf(
			`%s action has unsupported type %q`,
			label,
			fmt.Sprint(action["type"]),
		)
	}
	return nil
}

func validateKiroIdeHook(value any, index int, label string) (map[string]any, error) {
	hook, ok := value.(map[string]any)
	if !ok || hook == nil {
		return nil, fmt.Errorf("%s hooks[%d] must be an object", label, index)
	}
	hookLabel := fmt.Sprintf("%s hooks[%d]", label, index)
	name, ok := hook["name"].(string)
	if !ok || name == "" {
		return nil, fmt.Errorf("%s requires a name", hookLabel)
	}
	trigger, ok := hook["trigger"].(string)
	if !ok {
		return nil, fmt.Errorf("%s requires a trigger", hookLabel)
	}
	if _, supported := kiroIdeTriggers[trigger]; !supported {
		return nil, fmt.Errorf("%s has unsupported trigger %q", hookLabel, trigger)
	}
	if value, exists := hook["description"]; exists {
		if _, ok := value.(string); !ok {
			return nil, fmt.Errorf("%s description must be a string", hookLabel)
		}
	}
	if value, exists := hook["matcher"]; exists {
		if _, ok := value.(string); !ok {
			return nil, fmt.Errorf("%s matcher must be a string", hookLabel)
		}
	}
	if value, exists := hook["timeout"]; exists {
		timeout, ok := value.(float64)
		if !ok || math.IsNaN(timeout) || math.IsInf(timeout, 0) ||
			timeout < 0 || timeout > 9007199254740991 || math.Trunc(timeout) != timeout {
			return nil, fmt.Errorf("%s timeout must be a non-negative integer", hookLabel)
		}
	}
	if value, exists := hook["enabled"]; exists {
		if _, ok := value.(bool); !ok {
			return nil, fmt.Errorf("%s enabled must be a boolean", hookLabel)
		}
	}
	if err := validateKiroIdeAction(hook["action"], hookLabel); err != nil {
		return nil, err
	}
	return hook, nil
}

func parseKiroIdeDocument(
	filePath string,
	snapshot *managedFileSnapshot,
) (*kiroIdeDocument, error) {
	if snapshot == nil {
		return &kiroIdeDocument{
			filePath: filePath,
			root:     map[string]any{},
			hooks:    []map[string]any{},
		}, nil
	}
	label := fmt.Sprintf("Kiro IDE hooks at %s", filePath)
	root, err := decodeStrictJSONObject(snapshot.contents, label)
	if err != nil {
		return nil, err
	}
	if root["version"] != "v1" {
		return nil, fmt.Errorf(`Kiro IDE hooks version must be "v1": %s`, filePath)
	}
	values, ok := root["hooks"].([]any)
	if !ok {
		return nil, fmt.Errorf(`Kiro IDE hooks field "hooks" must be an array: %s`, filePath)
	}
	hooks := make([]map[string]any, 0, len(values))
	for index, value := range values {
		hook, err := validateKiroIdeHook(value, index, "Kiro IDE hooks")
		if err != nil {
			return nil, err
		}
		hooks = append(hooks, hook)
	}
	return &kiroIdeDocument{
		exists: true, filePath: filePath, root: root, hooks: hooks,
		snapshot: snapshot,
	}, nil
}

func exactKiroIdeKeys(value map[string]any, keys ...string) bool {
	if len(value) != len(keys) {
		return false
	}
	for _, key := range keys {
		if _, exists := value[key]; !exists {
			return false
		}
	}
	return true
}

func ownedKiroIdeReference(
	hook map[string]any,
) (*managedScriptReference, error) {
	specification, managed := kiroIdeSpecification(hook["name"])
	if !managed {
		return nil, nil
	}
	action, ok := hook["action"].(map[string]any)
	if !ok || action["type"] != "command" {
		return nil, nil
	}
	command, ok := action["command"].(string)
	if !ok {
		return nil, nil
	}
	return kiroIdeRuntimeReferenceForCommand(command, specification.scriptName)
}

func managedKiroIdeReference(
	hook map[string]any,
) (*managedScriptReference, error) {
	specification, managed := kiroIdeSpecification(hook["name"])
	if !managed ||
		!exactKiroIdeKeys(
			hook,
			"name", "description", "trigger", "matcher", "action", "timeout", "enabled",
		) ||
		hook["description"] != specification.description ||
		hook["trigger"] != specification.trigger || hook["matcher"] != ".*" ||
		hook["timeout"] != kiroIdeHookTimeout || hook["enabled"] != true {
		return nil, nil
	}
	action, ok := hook["action"].(map[string]any)
	if !ok || !exactKiroIdeKeys(action, "type", "command") ||
		action["type"] != "command" {
		return nil, nil
	}
	return ownedKiroIdeReference(hook)
}

func requireAvailableKiroIdeHooks(hooks []map[string]any) error {
	for _, hook := range hooks {
		if _, managed := kiroIdeSpecification(hook["name"]); !managed {
			continue
		}
		reference, err := ownedKiroIdeReference(hook)
		if err != nil {
			return err
		}
		if reference == nil {
			return fmt.Errorf(
				"Kiro IDE hook name %q conflicts with the Elydora contract",
				hook["name"],
			)
		}
	}
	return nil
}

func buildKiroIdeHook(
	name string,
	runtimePath string,
	scriptPath string,
) (map[string]any, error) {
	specification, managed := kiroIdeSpecification(name)
	if !managed {
		return nil, fmt.Errorf("unsupported Elydora Kiro IDE hook name %q", name)
	}
	command, err := buildEncodedCommand("Kiro IDE", runtimePath, scriptPath)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"name": name, "description": specification.description,
		"trigger": specification.trigger, "matcher": ".*",
		"action":  map[string]any{"type": "command", "command": command},
		"timeout": kiroIdeHookTimeout, "enabled": true,
	}, nil
}

func withoutManagedKiroIdeHooks(
	hooks []map[string]any,
	agentID string,
) []map[string]any {
	filtered := make([]map[string]any, 0, len(hooks))
	for _, hook := range hooks {
		reference, err := ownedKiroIdeReference(hook)
		remove := err == nil && reference != nil &&
			(agentID == "" || sameManagedAgentID(reference.agentID, agentID))
		if !remove {
			filtered = append(filtered, hook)
		}
	}
	return filtered
}

func entirelyManagedKiroIdeDocument(document *kiroIdeDocument) bool {
	if document == nil || !document.exists ||
		!exactKiroIdeKeys(document.root, "version", "hooks") ||
		len(document.hooks) == 0 {
		return false
	}
	for _, hook := range document.hooks {
		reference, err := managedKiroIdeReference(hook)
		if err != nil || reference == nil {
			return false
		}
	}
	return true
}

func renderKiroIdeDocument(
	document *kiroIdeDocument,
	hooks []map[string]any,
) (*kiroIdeRenderedDocument, error) {
	if document == nil {
		return nil, fmt.Errorf("Kiro IDE rendering requires a hook document")
	}
	if !document.exists && len(hooks) == 0 {
		return &kiroIdeRenderedDocument{document: document}, nil
	}
	if reflect.DeepEqual(hooks, document.hooks) {
		return &kiroIdeRenderedDocument{document: document}, nil
	}
	if len(hooks) == 0 && entirelyManagedKiroIdeDocument(document) {
		return &kiroIdeRenderedDocument{
			document: document, changed: true, remove: true,
		}, nil
	}
	root := cloneJSONObject(document.root)
	root["version"] = "v1"
	values := make([]any, 0, len(hooks))
	for _, hook := range hooks {
		values = append(values, hook)
	}
	root["hooks"] = values
	next, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode Kiro IDE hooks: %w", err)
	}
	next = append(next, '\n')
	validation := &managedFileSnapshot{contents: next}
	if _, err := parseKiroIdeDocument(document.filePath, validation); err != nil {
		return nil, fmt.Errorf("validate rendered Kiro IDE hooks: %w", err)
	}
	return &kiroIdeRenderedDocument{
		document: document,
		changed:  !document.exists || !bytes.Equal(next, document.snapshot.contents),
		next:     next,
	}, nil
}

func kiroIdeRuntimeContracts(
	hooks []map[string]any,
) ([]managedRuntimeContract, error) {
	managedHooks := make([]map[string]any, 0, 2)
	for _, hook := range hooks {
		_, managed := kiroIdeSpecification(hook["name"])
		if managed {
			managedHooks = append(managedHooks, hook)
		}
	}
	if len(managedHooks) != 2 {
		return []managedRuntimeContract{}, nil
	}
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		return nil, err
	}
	var guard, audit *managedScriptReference
	for _, hook := range managedHooks {
		reference, err := currentKiroIdeReference(hook, nodePath)
		if err != nil {
			return nil, err
		}
		if reference == nil {
			return []managedRuntimeContract{}, nil
		}
		if hook["name"] == kiroIdeGuardName {
			if guard != nil {
				return []managedRuntimeContract{}, nil
			}
			guard = reference
		} else {
			if audit != nil {
				return []managedRuntimeContract{}, nil
			}
			audit = reference
		}
	}
	if guard == nil || audit == nil ||
		!sameManagedAgentID(guard.agentID, audit.agentID) {
		return []managedRuntimeContract{}, nil
	}
	return []managedRuntimeContract{{
		agentID: guard.agentID, guardPath: guard.scriptPath,
		auditPath: audit.scriptPath,
	}}, nil
}

func parseKiroIdeLegacyDocument(
	filePath string,
	snapshot *managedFileSnapshot,
) (*kiroIdeLegacyDocument, error) {
	document := &kiroIdeLegacyDocument{filePath: filePath, snapshot: snapshot}
	if snapshot == nil {
		return document, nil
	}
	label := fmt.Sprintf("legacy Kiro IDE hook at %s", filePath)
	root, err := decodeStrictJSONObject(snapshot.contents, label)
	if err != nil {
		return nil, err
	}
	if !exactKiroIdeKeys(root, "name", "hooks") || root["name"] != "Elydora Audit" {
		return document, nil
	}
	hooks, ok := root["hooks"].(map[string]any)
	if !ok || !exactKiroIdeKeys(hooks, "pre_tool_use", "post_tool_use") {
		return document, nil
	}
	guard, ok := hooks["pre_tool_use"].(map[string]any)
	if !ok || !exactKiroIdeKeys(guard, "command", "timeout_ms") ||
		guard["timeout_ms"] != float64(5000) {
		return document, nil
	}
	audit, ok := hooks["post_tool_use"].(map[string]any)
	if !ok || !exactKiroIdeKeys(audit, "command", "timeout_ms") ||
		audit["timeout_ms"] != float64(5000) {
		return document, nil
	}
	guardReference, err := legacyKiroIdeGoReference(guard["command"], kiroIdeGuardScript)
	if err != nil {
		return nil, err
	}
	auditReference, err := legacyKiroIdeGoReference(audit["command"], kiroIdeAuditScript)
	if err != nil {
		return nil, err
	}
	if guardReference == nil || auditReference == nil ||
		!sameManagedAgentID(guardReference.agentID, auditReference.agentID) {
		return document, nil
	}
	document.contract = &managedRuntimeContract{
		agentID: guardReference.agentID, guardPath: guardReference.scriptPath,
		auditPath: auditReference.scriptPath,
	}
	return document, nil
}
