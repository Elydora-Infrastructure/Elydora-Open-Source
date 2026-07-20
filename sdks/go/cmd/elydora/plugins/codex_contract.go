package plugins

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
)

const (
	codexAgentKey           = "codex"
	codexConfigFile         = "hooks.json"
	codexGuardScript        = "guard.js"
	codexAuditScript        = "hook.js"
	codexHookTimeout        = float64(10)
	codexOwnedDescription   = "Elydora audit and freeze enforcement"
	codexGuardStatusMessage = "Checking Elydora agent state"
	codexAuditStatusMessage = "Recording Elydora tool use"
	codexPOSIXApostrophe    = `'"'"'`
)

type codexHooks map[string][]map[string]any

type codexDocument struct {
	exists   bool
	filePath string
	root     map[string]any
	hooks    codexHooks
	raw      []byte
}

type codexRenderedDocument struct {
	document *codexDocument
	changed  bool
	next     []byte
	remove   bool
}

type codexRuntimeContract struct {
	agentID   string
	guardPath string
	auditPath string
}

func cloneCodexObject(value map[string]any) map[string]any {
	clone := make(map[string]any, len(value))
	for key, item := range value {
		clone[key] = item
	}
	return clone
}

func cloneCodexHooks(source codexHooks) codexHooks {
	clone := make(codexHooks, len(source))
	for event, groups := range source {
		clone[event] = append([]map[string]any(nil), groups...)
	}
	return clone
}

func sameCodexPath(left, right string) bool {
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

func sameCodexAgentID(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func readCodexHooks(value any, label string) (codexHooks, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf(`%s field "hooks" must be an object`, label)
	}
	hooks := make(codexHooks, len(object))
	for event, candidate := range object {
		values, ok := candidate.([]any)
		if !ok {
			return nil, fmt.Errorf(`%s field "hooks.%s" must be an array`, label, event)
		}
		groups := make([]map[string]any, 0, len(values))
		for groupIndex, value := range values {
			group, ok := value.(map[string]any)
			if !ok || group == nil {
				return nil, fmt.Errorf(
					`%s matcher group hooks.%s[%d] must be an object`,
					label, event, groupIndex,
				)
			}
			handlerValues, ok := group["hooks"].([]any)
			if !ok {
				return nil, fmt.Errorf(
					`%s matcher group hooks.%s[%d] must contain a hooks array`,
					label, event, groupIndex,
				)
			}
			for handlerIndex, handler := range handlerValues {
				if object, ok := handler.(map[string]any); !ok || object == nil {
					return nil, fmt.Errorf(
						`%s handler hooks.%s[%d].hooks[%d] must be an object`,
						label, event, groupIndex, handlerIndex,
					)
				}
			}
			groups = append(groups, group)
		}
		hooks[event] = groups
	}
	return hooks, nil
}

func decodeStrictJSONObject(source []byte, label string) (map[string]any, error) {
	if !json.Valid(source) {
		var value any
		if err := json.Unmarshal(source, &value); err != nil {
			return nil, fmt.Errorf("parse %s: %w", label, err)
		}
	}
	return decodeJSONCObject(source, label, false)
}

func parseCodexDocument(filePath string, raw []byte) (*codexDocument, error) {
	label := fmt.Sprintf("Codex user hooks at %s", filePath)
	root, err := decodeStrictJSONObject(raw, label)
	if err != nil {
		return nil, err
	}
	hooks := codexHooks{}
	if value, exists := root["hooks"]; exists {
		hooks, err = readCodexHooks(value, label)
		if err != nil {
			return nil, err
		}
	}
	return &codexDocument{
		exists: true, filePath: filePath, root: root,
		hooks: hooks, raw: append([]byte(nil), raw...),
	}, nil
}

func createCodexDocument(filePath string) *codexDocument {
	return &codexDocument{
		filePath: filePath,
		root:     map[string]any{"description": codexOwnedDescription},
		hooks:    codexHooks{},
	}
}

func exactCodexMatcherGroup(group map[string]any) bool {
	return len(group) == 2 && group["matcher"] == "*"
}

func removeCodexGroups(
	groups []map[string]any,
	scriptName string,
	status string,
	agentID string,
) []map[string]any {
	result := make([]map[string]any, 0, len(groups))
	for _, group := range groups {
		handlers := group["hooks"].([]any)
		kept := make([]any, 0, len(handlers))
		for _, value := range handlers {
			handler := value.(map[string]any)
			managedID, managed := codexManagedAgentID(handler, scriptName, status)
			if managed && (agentID == "" || sameCodexAgentID(managedID, agentID)) {
				continue
			}
			kept = append(kept, handler)
		}
		if len(kept) > 0 || !exactCodexMatcherGroup(group) {
			next := cloneCodexObject(group)
			next["hooks"] = kept
			result = append(result, next)
		}
	}
	return result
}

func removeManagedCodexHooks(hooks codexHooks, agentID string) codexHooks {
	next := cloneCodexHooks(hooks)
	for _, contract := range []struct{ event, script, status string }{
		{"PreToolUse", codexGuardScript, codexGuardStatusMessage},
		{"PostToolUse", codexAuditScript, codexAuditStatusMessage},
	} {
		groups := removeCodexGroups(
			next[contract.event], contract.script, contract.status, agentID,
		)
		if len(groups) == 0 {
			delete(next, contract.event)
		} else {
			next[contract.event] = groups
		}
	}
	return next
}

func entirelyManagedCodexDocument(document *codexDocument) bool {
	if !document.exists || document.root["description"] != codexOwnedDescription ||
		len(document.hooks) == 0 {
		return false
	}
	for key := range document.root {
		if key != "description" && key != "hooks" {
			return false
		}
	}
	handlerCount := 0
	for event, groups := range document.hooks {
		contract := map[string][2]string{
			"PreToolUse":  {codexGuardScript, codexGuardStatusMessage},
			"PostToolUse": {codexAuditScript, codexAuditStatusMessage},
		}[event]
		if contract[0] == "" || len(groups) == 0 {
			return false
		}
		for _, group := range groups {
			handlers := group["hooks"].([]any)
			if !exactCodexMatcherGroup(group) || len(handlers) == 0 {
				return false
			}
			for _, value := range handlers {
				if _, managed := codexManagedAgentID(
					value.(map[string]any), contract[0], contract[1],
				); !managed {
					return false
				}
				handlerCount++
			}
		}
	}
	return handlerCount > 0
}

func renderCodexDocument(
	document *codexDocument,
	hooks codexHooks,
) (*codexRenderedDocument, error) {
	if !document.exists && len(hooks) == 0 {
		return &codexRenderedDocument{document: document}, nil
	}
	if document.exists && reflect.DeepEqual(hooks, document.hooks) {
		return &codexRenderedDocument{document: document}, nil
	}
	if len(hooks) == 0 && entirelyManagedCodexDocument(document) {
		return &codexRenderedDocument{document: document, changed: true, remove: true}, nil
	}
	root := cloneCodexObject(document.root)
	if len(hooks) == 0 {
		delete(root, "hooks")
	} else {
		root["hooks"] = hooks
	}
	next, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, err
	}
	next = append(next, '\n')
	return &codexRenderedDocument{
		document: document,
		changed:  !document.exists || !bytes.Equal(next, document.raw),
		next:     next,
	}, nil
}

func managedCodexIDs(
	groups []map[string]any,
	scriptName string,
	status string,
) map[string]string {
	result := map[string]string{}
	for _, group := range groups {
		if !exactCodexMatcherGroup(group) {
			continue
		}
		for _, value := range group["hooks"].([]any) {
			agentID, managed := codexManagedAgentID(value.(map[string]any), scriptName, status)
			if !managed {
				continue
			}
			key := agentID
			if runtime.GOOS == "windows" {
				key = strings.ToLower(agentID)
			}
			result[key] = agentID
		}
	}
	return result
}

func codexRuntimeContracts(hooks codexHooks) ([]codexRuntimeContract, error) {
	guards := managedCodexIDs(hooks["PreToolUse"], codexGuardScript, codexGuardStatusMessage)
	audits := managedCodexIDs(hooks["PostToolUse"], codexAuditScript, codexAuditStatusMessage)
	keys := make([]string, 0, len(guards))
	for key := range guards {
		if _, exists := audits[key]; exists {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	root, err := AgentRuntimeRoot()
	if err != nil {
		return nil, err
	}
	contracts := make([]codexRuntimeContract, 0, len(keys))
	for _, key := range keys {
		agentID := guards[key]
		directory := filepath.Join(root, agentID)
		contracts = append(contracts, codexRuntimeContract{
			agentID:   agentID,
			guardPath: filepath.Join(directory, codexGuardScript),
			auditPath: filepath.Join(directory, codexAuditScript),
		})
	}
	return contracts, nil
}
