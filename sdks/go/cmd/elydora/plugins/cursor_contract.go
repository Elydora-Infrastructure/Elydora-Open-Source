package plugins

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
)

const (
	cursorAgentKey    = "cursor"
	cursorConfigFile  = "hooks.json"
	cursorGuardScript = "guard.js"
	cursorAuditScript = "hook.js"
	cursorHookTimeout = float64(10)
)

type cursorHooks map[string][]map[string]any

type cursorDocument struct {
	exists   bool
	filePath string
	root     map[string]any
	hooks    cursorHooks
	raw      []byte
}

type cursorRenderedDocument struct {
	document *cursorDocument
	changed  bool
	next     []byte
	remove   bool
}

type cursorRuntimeContract = managedRuntimeContract

func cloneCursorHooks(source cursorHooks) cursorHooks {
	clone := make(cursorHooks, len(source))
	for event, handlers := range source {
		clone[event] = append([]map[string]any(nil), handlers...)
	}
	return clone
}

func buildCursorHandler(nodePath, scriptPath string) map[string]any {
	command := posixSource(nodePath, scriptPath)
	if runtime.GOOS == "windows" {
		command = powerShellSource(nodePath, scriptPath)
	}
	return map[string]any{
		"command": command, "timeout": cursorHookTimeout, "failClosed": true,
	}
}

func parseCursorLegacyCommand(command string) (string, bool) {
	separator := strings.IndexByte(command, ' ')
	if separator < 0 {
		return "", false
	}
	executable := command[:separator]
	if !strings.EqualFold(executable, "node") && !strings.EqualFold(executable, "node.exe") {
		return "", false
	}
	script := command[separator+1:]
	if len(script) >= 2 && script[0] == '"' && script[len(script)-1] == '"' {
		script = script[1 : len(script)-1]
	}
	return script, script != "" && !strings.ContainsAny(script, "\r\n")
}

func cursorManagedScriptPath(handler map[string]any) (string, bool) {
	if len(handler) == 3 && handler["timeout"] == cursorHookTimeout && handler["failClosed"] == true {
		command, ok := handler["command"].(string)
		if !ok {
			return "", false
		}
		var nodePath, scriptPath string
		var parsed bool
		if runtime.GOOS == "windows" {
			nodePath, scriptPath, parsed = parsePowerShellSource(command)
		} else {
			nodePath, scriptPath, parsed = parsePOSIXCommand(command)
		}
		if parsed && filepath.IsAbs(nodePath) && filepath.IsAbs(scriptPath) &&
			isNodeExecutable(nodePath) {
			return scriptPath, true
		}
	}
	if len(handler) != 1 {
		return "", false
	}
	command, ok := handler["command"].(string)
	if !ok {
		return "", false
	}
	return parseCursorLegacyCommand(command)
}

func cursorManagedAgentID(
	handler map[string]any,
	scriptName string,
	runtimeRoot string,
) (string, bool) {
	scriptPath, managed := cursorManagedScriptPath(handler)
	if !managed || !filepath.IsAbs(scriptPath) || filepath.Base(scriptPath) != scriptName {
		return "", false
	}
	agentDirectory := filepath.Dir(scriptPath)
	if !sameManagedPath(filepath.Dir(agentDirectory), runtimeRoot) {
		return "", false
	}
	agentID := filepath.Base(agentDirectory)
	return agentID, agentID != "" && agentID != "." && agentID != ".."
}

func readCursorHooks(value any, label string) (cursorHooks, error) {
	object, ok := value.(map[string]any)
	if !ok || object == nil {
		return nil, fmt.Errorf(`%s field "hooks" must be an object`, label)
	}
	hooks := make(cursorHooks, len(object))
	for event, handlerValue := range object {
		values, ok := handlerValue.([]any)
		if !ok {
			return nil, fmt.Errorf(`%s field "hooks.%s" must be an array`, label, event)
		}
		handlers := make([]map[string]any, 0, len(values))
		for index, value := range values {
			handler, ok := value.(map[string]any)
			if !ok || handler == nil {
				return nil, fmt.Errorf(`%s handler hooks.%s[%d] must be an object`, label, event, index)
			}
			handlers = append(handlers, handler)
		}
		hooks[event] = handlers
	}
	return hooks, nil
}

func cursorContainsManagedHook(hooks cursorHooks, runtimeRoot string) bool {
	for _, contract := range []struct{ event, script string }{
		{"preToolUse", cursorGuardScript},
		{"postToolUse", cursorAuditScript},
		{"postToolUseFailure", cursorAuditScript},
	} {
		for _, handler := range hooks[contract.event] {
			if _, managed := cursorManagedAgentID(handler, contract.script, runtimeRoot); managed {
				return true
			}
		}
	}
	return false
}

func parseCursorDocument(filePath string, raw []byte, runtimeRoot string) (*cursorDocument, error) {
	label := fmt.Sprintf("Cursor user hooks at %s", filePath)
	if !json.Valid(raw) {
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fmt.Errorf("parse %s: %w", label, err)
		}
	}
	root, err := decodeJSONCObject(raw, label, false)
	if err != nil {
		return nil, err
	}
	hooks := cursorHooks{}
	if value, exists := root["hooks"]; exists {
		hooks, err = readCursorHooks(value, label)
		if err != nil {
			return nil, err
		}
	}
	version, hasVersion := root["version"]
	current := hasVersion && version == float64(1)
	if !current && (hasVersion || !cursorContainsManagedHook(hooks, runtimeRoot)) {
		return nil, fmt.Errorf("%s must declare version 1", label)
	}
	return &cursorDocument{
		exists: true, filePath: filePath, root: root,
		hooks: hooks, raw: append([]byte(nil), raw...),
	}, nil
}

func createCursorDocument(filePath string) *cursorDocument {
	return &cursorDocument{filePath: filePath, root: map[string]any{}, hooks: cursorHooks{}}
}

func removeManagedCursorHooks(hooks cursorHooks, runtimeRoot, agentID string) cursorHooks {
	result := cloneCursorHooks(hooks)
	for _, contract := range []struct{ event, script string }{
		{"preToolUse", cursorGuardScript},
		{"postToolUse", cursorAuditScript},
		{"postToolUseFailure", cursorAuditScript},
	} {
		kept := make([]map[string]any, 0, len(result[contract.event]))
		for _, handler := range result[contract.event] {
			managedID, managed := cursorManagedAgentID(handler, contract.script, runtimeRoot)
			if managed && (agentID == "" || sameManagedAgentID(managedID, agentID)) {
				continue
			}
			kept = append(kept, handler)
		}
		if len(kept) == 0 {
			delete(result, contract.event)
		} else {
			result[contract.event] = kept
		}
	}
	return result
}

func entirelyManagedCursorDocument(document *cursorDocument, runtimeRoot string) bool {
	if !document.exists || len(document.hooks) == 0 {
		return false
	}
	for key := range document.root {
		if key != "version" && key != "hooks" {
			return false
		}
	}
	count := 0
	for event, handlers := range document.hooks {
		script := map[string]string{
			"preToolUse":         cursorGuardScript,
			"postToolUse":        cursorAuditScript,
			"postToolUseFailure": cursorAuditScript,
		}[event]
		if script == "" || len(handlers) == 0 {
			return false
		}
		count += len(handlers)
		for _, handler := range handlers {
			if _, managed := cursorManagedAgentID(handler, script, runtimeRoot); !managed {
				return false
			}
		}
	}
	return count > 0
}

func renderCursorDocument(
	document *cursorDocument,
	hooks cursorHooks,
	runtimeRoot string,
) (*cursorRenderedDocument, error) {
	if !document.exists && len(hooks) == 0 {
		return &cursorRenderedDocument{document: document}, nil
	}
	if document.exists && reflect.DeepEqual(hooks, document.hooks) {
		return &cursorRenderedDocument{document: document}, nil
	}
	if len(hooks) == 0 && entirelyManagedCursorDocument(document, runtimeRoot) {
		return &cursorRenderedDocument{document: document, changed: true, remove: true}, nil
	}
	root := make(map[string]any, len(document.root)+1)
	for key, value := range document.root {
		root[key] = value
	}
	root["version"] = float64(1)
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
	return &cursorRenderedDocument{
		document: document, changed: !document.exists || string(next) != string(document.raw), next: next,
	}, nil
}

func managedCursorIDs(
	handlers []map[string]any,
	scriptName, runtimeRoot string,
) map[string]string {
	result := map[string]string{}
	for _, handler := range handlers {
		agentID, managed := cursorManagedAgentID(handler, scriptName, runtimeRoot)
		if !managed {
			continue
		}
		result[managedReferenceKey(agentID)] = agentID
	}
	return result
}

func cursorRuntimeContracts(hooks cursorHooks, runtimeRoot string) []cursorRuntimeContract {
	guards := managedCursorIDs(hooks["preToolUse"], cursorGuardScript, runtimeRoot)
	audits := managedCursorIDs(hooks["postToolUse"], cursorAuditScript, runtimeRoot)
	failures := managedCursorIDs(hooks["postToolUseFailure"], cursorAuditScript, runtimeRoot)
	keys := make([]string, 0, len(guards))
	for key := range guards {
		_, hasAudit := audits[key]
		_, hasFailureAudit := failures[key]
		if hasAudit && hasFailureAudit {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	contracts := make([]cursorRuntimeContract, 0, len(keys))
	for _, key := range keys {
		agentID := guards[key]
		directory := filepath.Join(runtimeRoot, agentID)
		contracts = append(contracts, cursorRuntimeContract{
			agentID: agentID, guardPath: filepath.Join(directory, cursorGuardScript),
			auditPath: filepath.Join(directory, cursorAuditScript),
		})
	}
	return contracts
}
