package plugins

import (
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type claudeRuntimeReference struct {
	agentID    string
	scriptPath string
}

func sameClaudePath(left, right string) bool {
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

func sameClaudeAgentID(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func isClaudeNodeExecutable(path string) bool {
	name := filepath.Base(path)
	return name == "node" || strings.EqualFold(name, "node.exe")
}

func sameClaudeFileName(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func claudeRuntimeReferenceForScript(
	scriptPath string,
	scriptName string,
) (*claudeRuntimeReference, error) {
	if !filepath.IsAbs(scriptPath) ||
		!sameClaudeFileName(filepath.Base(scriptPath), scriptName) {
		return nil, nil
	}
	agentDirectory := filepath.Dir(scriptPath)
	runtimeRoot, err := AgentRuntimeRoot()
	if err != nil {
		return nil, err
	}
	if !sameClaudePath(filepath.Dir(agentDirectory), runtimeRoot) {
		return nil, nil
	}
	agentID := filepath.Base(agentDirectory)
	if agentID == "" || agentID == "." || agentID == ".." {
		return nil, nil
	}
	return &claudeRuntimeReference{agentID: agentID, scriptPath: scriptPath}, nil
}

func claudeLegacyReference(
	command string,
	scriptName string,
) (*claudeRuntimeReference, error) {
	if !strings.HasPrefix(command, "node ") || strings.ContainsAny(command, "\r\n") {
		return nil, nil
	}
	scriptPath := strings.TrimPrefix(command, "node ")
	if scriptPath == "" || strings.TrimSpace(scriptPath) != scriptPath {
		return nil, nil
	}
	return claudeRuntimeReferenceForScript(scriptPath, scriptName)
}

func managedClaudeReference(
	handler map[string]any,
	scriptName string,
	statusMessage string,
	includeLegacy bool,
) (*claudeRuntimeReference, error) {
	if len(handler) == 5 && handler["type"] == "command" &&
		handler["timeout"] == claudeHookTimeout &&
		handler["statusMessage"] == statusMessage {
		nodePath, nodeOK := handler["command"].(string)
		args, argsOK := handler["args"].([]any)
		if nodeOK && filepath.IsAbs(nodePath) && isClaudeNodeExecutable(nodePath) &&
			argsOK && len(args) == 1 {
			if scriptPath, ok := args[0].(string); ok {
				return claudeRuntimeReferenceForScript(scriptPath, scriptName)
			}
		}
	}
	if includeLegacy && len(handler) == 2 && handler["type"] == "command" {
		if command, ok := handler["command"].(string); ok {
			return claudeLegacyReference(command, scriptName)
		}
	}
	return nil, nil
}

func buildClaudeGroup(nodePath, scriptPath, statusMessage string) claudeGroup {
	return claudeGroup{
		object: map[string]any{},
		handlers: []map[string]any{{
			"type": "command", "command": nodePath, "args": []any{scriptPath},
			"timeout": claudeHookTimeout, "statusMessage": statusMessage,
		}},
	}
}

func exactManagedClaudeGroup(group claudeGroup) bool {
	_, hasHooks := group.object["hooks"]
	return len(group.object) == 1 && hasHooks
}

func removeManagedClaudeGroups(
	groups []claudeGroup,
	scriptName string,
	statusMessage string,
	agentID string,
) ([]claudeGroup, bool, error) {
	result := make([]claudeGroup, 0, len(groups))
	removed := false
	for _, group := range groups {
		kept := make([]map[string]any, 0, len(group.handlers))
		for _, handler := range group.handlers {
			reference, err := managedClaudeReference(
				handler,
				scriptName,
				statusMessage,
				true,
			)
			if err != nil {
				return nil, false, err
			}
			owned := reference != nil &&
				(agentID == "" || sameClaudeAgentID(reference.agentID, agentID))
			if owned {
				removed = true
				continue
			}
			kept = append(kept, handler)
		}
		if len(kept) > 0 || !exactManagedClaudeGroup(group) {
			object := cloneClaudeObject(group.object)
			object["hooks"] = group.object["hooks"]
			result = append(result, claudeGroup{object: object, handlers: kept})
		}
	}
	return result, removed, nil
}

func removeManagedClaudeHooks(hooks claudeHooks, agentID string) (claudeHooks, error) {
	result := cloneClaudeHooks(hooks)
	for _, contract := range []struct{ event, script, status string }{
		{"PreToolUse", claudeGuardScript, claudeGuardStatusMessage},
		{"PostToolUse", claudeAuditScript, claudeAuditStatusMessage},
		{"PostToolUseFailure", claudeAuditScript, claudeAuditStatusMessage},
	} {
		groups, removed, err := removeManagedClaudeGroups(
			result[contract.event],
			contract.script,
			contract.status,
			agentID,
		)
		if err != nil {
			return nil, err
		}
		if !removed {
			continue
		}
		if len(groups) == 0 {
			delete(result, contract.event)
		} else {
			result[contract.event] = groups
		}
	}
	return result, nil
}

func managedClaudeEvent(event string) (string, string, bool) {
	switch event {
	case "PreToolUse":
		return claudeGuardScript, claudeGuardStatusMessage, true
	case "PostToolUse", "PostToolUseFailure":
		return claudeAuditScript, claudeAuditStatusMessage, true
	default:
		return "", "", false
	}
}

func entirelyManagedClaudeDocument(document *claudeDocument) bool {
	if !document.exists || len(document.root) != 1 || len(document.hooks) == 0 {
		return false
	}
	if _, hasHooks := document.root["hooks"]; !hasHooks {
		return false
	}
	handlerCount := 0
	for event, groups := range document.hooks {
		scriptName, statusMessage, managedEvent := managedClaudeEvent(event)
		if !managedEvent || len(groups) == 0 {
			return false
		}
		for _, group := range groups {
			if !exactManagedClaudeGroup(group) || len(group.handlers) == 0 {
				return false
			}
			for _, handler := range group.handlers {
				reference, err := managedClaudeReference(
					handler,
					scriptName,
					statusMessage,
					true,
				)
				if err != nil || reference == nil {
					return false
				}
				handlerCount++
			}
		}
	}
	return handlerCount > 0
}

func claudeReferenceKey(agentID string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(agentID)
	}
	return agentID
}

func claudeReferencesForEvent(
	groups []claudeGroup,
	scriptName string,
	statusMessage string,
) (map[string][]claudeRuntimeReference, error) {
	result := map[string][]claudeRuntimeReference{}
	for _, group := range groups {
		if !exactManagedClaudeGroup(group) {
			continue
		}
		for _, handler := range group.handlers {
			reference, err := managedClaudeReference(
				handler,
				scriptName,
				statusMessage,
				false,
			)
			if err != nil {
				return nil, err
			}
			if reference != nil {
				key := claudeReferenceKey(reference.agentID)
				result[key] = append(result[key], *reference)
			}
		}
	}
	return result, nil
}

func claudeRuntimeContracts(hooks claudeHooks) ([]claudeRuntimeContract, error) {
	guards, err := claudeReferencesForEvent(
		hooks["PreToolUse"],
		claudeGuardScript,
		claudeGuardStatusMessage,
	)
	if err != nil {
		return nil, err
	}
	successes, err := claudeReferencesForEvent(
		hooks["PostToolUse"],
		claudeAuditScript,
		claudeAuditStatusMessage,
	)
	if err != nil {
		return nil, err
	}
	failures, err := claudeReferencesForEvent(
		hooks["PostToolUseFailure"],
		claudeAuditScript,
		claudeAuditStatusMessage,
	)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(guards))
	for key := range guards {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	contracts := make([]claudeRuntimeContract, 0, len(keys))
	for _, key := range keys {
		guard := guards[key]
		success := successes[key]
		failure := failures[key]
		if len(guard) != 1 || len(success) != 1 || len(failure) != 1 ||
			!sameClaudePath(success[0].scriptPath, failure[0].scriptPath) {
			continue
		}
		contracts = append(contracts, claudeRuntimeContract{
			agentID: guard[0].agentID, guardPath: guard[0].scriptPath,
			auditPath: success[0].scriptPath,
		})
	}
	return contracts, nil
}

func requireClaudeAbsoluteNode(nodePath string) error {
	if !filepath.IsAbs(nodePath) || !isClaudeNodeExecutable(nodePath) {
		return fmt.Errorf("Claude Code hooks require an absolute Node.js executable path")
	}
	return nil
}
