package plugins

import (
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const (
	qwenAgentKey        = "qwen"
	qwenGuardScript     = "guard.js"
	qwenAuditScript     = "hook.js"
	qwenGuardHookName   = "elydora-guard"
	qwenAuditHookName   = "elydora-audit"
	qwenHookTimeout     = float64(10_000)
	qwenOwnedFileMarker = "// Managed by Elydora"
)

var qwenManagedEvents = [...]string{
	"PreToolUse",
	"PostToolUse",
	"PostToolUseFailure",
}

var qwenEventContracts = [...]struct {
	event, script, name string
}{
	{"PreToolUse", qwenGuardScript, qwenGuardHookName},
	{"PostToolUse", qwenAuditScript, qwenAuditHookName},
	{"PostToolUseFailure", qwenAuditScript, qwenAuditHookName},
}

type qwenHookSettings map[string]any

type qwenManagedRemoval struct {
	event          string
	groupIndex     int
	handlerIndexes []int
	removeGroup    bool
}

type qwenRuntimeContract struct {
	agentID   string
	guardPath string
	auditPath string
}

func qwenExpectedShell() string {
	if runtime.GOOS == "windows" {
		return "powershell"
	}
	return "bash"
}

func buildQwenGroup(nodePath, scriptPath, name string) map[string]any {
	return map[string]any{
		"hooks": []any{map[string]any{
			"type":    "command",
			"name":    name,
			"command": buildQwenCommand(nodePath, scriptPath),
			"shell":   qwenExpectedShell(),
			"timeout": qwenHookTimeout,
		}},
	}
}

func currentQwenReference(
	handler map[string]any,
	scriptName, hookName, runtimeRoot string,
) (qwenRuntimeReference, bool) {
	if len(handler) != 5 || handler["type"] != "command" ||
		handler["name"] != hookName || handler["shell"] != qwenExpectedShell() ||
		handler["timeout"] != qwenHookTimeout {
		return qwenRuntimeReference{}, false
	}
	command, ok := handler["command"].(string)
	if !ok {
		return qwenRuntimeReference{}, false
	}
	return qwenRuntimeReferenceForCommand(command, scriptName, runtimeRoot)
}

func legacyQwenReference(
	handler map[string]any,
	scriptName, runtimeRoot string,
) (qwenRuntimeReference, bool) {
	if len(handler) != 4 || handler["type"] != "command" ||
		handler["shell"] != qwenExpectedShell() ||
		handler["timeout"] != qwenHookTimeout {
		return qwenRuntimeReference{}, false
	}
	command, ok := handler["command"].(string)
	if !ok {
		return qwenRuntimeReference{}, false
	}
	return qwenRuntimeReferenceForCommand(command, scriptName, runtimeRoot)
}

func managedQwenReference(
	handler map[string]any,
	scriptName, hookName, runtimeRoot string,
	includeLegacy bool,
) (qwenRuntimeReference, bool) {
	if reference, ok := currentQwenReference(
		handler,
		scriptName,
		hookName,
		runtimeRoot,
	); ok {
		return reference, true
	}
	if includeLegacy {
		return legacyQwenReference(handler, scriptName, runtimeRoot)
	}
	return qwenRuntimeReference{}, false
}

func exactCurrentQwenGroup(group map[string]any) bool {
	_, hasHooks := group["hooks"]
	return len(group) == 1 && hasHooks
}

func exactLegacyQwenGroup(group map[string]any) bool {
	_, hasHooks := group["hooks"]
	return len(group) == 2 && hasHooks && group["matcher"] == "*"
}

func managedQwenRemovals(
	settings qwenHookSettings,
	agentID, runtimeRoot string,
) []qwenManagedRemoval {
	removals := make([]qwenManagedRemoval, 0)
	for _, contract := range qwenEventContracts {
		groups, _ := settings[contract.event].([]any)
		for groupIndex, groupValue := range groups {
			group := groupValue.(map[string]any)
			handlers := group["hooks"].([]any)
			indexes := make([]int, 0)
			for handlerIndex, handlerValue := range handlers {
				reference, managed := managedQwenReference(
					handlerValue.(map[string]any),
					contract.script,
					contract.name,
					runtimeRoot,
					true,
				)
				if managed && (agentID == "" || sameQwenAgentID(reference.agentID, agentID)) {
					indexes = append(indexes, handlerIndex)
				}
			}
			if len(indexes) == 0 {
				continue
			}
			removeGroup := (exactCurrentQwenGroup(group) || exactLegacyQwenGroup(group)) &&
				len(indexes) == len(handlers)
			removals = append(removals, qwenManagedRemoval{
				event: contract.event, groupIndex: groupIndex,
				handlerIndexes: indexes, removeGroup: removeGroup,
			})
		}
	}
	return removals
}

func normalizedQwenAgentID(agentID string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(agentID)
	}
	return agentID
}

func qwenReferencesForEvent(
	settings qwenHookSettings,
	event, scriptName, hookName, runtimeRoot string,
) map[string][]qwenRuntimeReference {
	references := map[string][]qwenRuntimeReference{}
	groups, _ := settings[event].([]any)
	for _, groupValue := range groups {
		group := groupValue.(map[string]any)
		if !exactCurrentQwenGroup(group) {
			continue
		}
		for _, handlerValue := range group["hooks"].([]any) {
			reference, ok := currentQwenReference(
				handlerValue.(map[string]any),
				scriptName,
				hookName,
				runtimeRoot,
			)
			if !ok {
				continue
			}
			key := normalizedQwenAgentID(reference.agentID)
			references[key] = append(references[key], reference)
		}
	}
	return references
}

func qwenRuntimeContracts(
	settings qwenHookSettings,
	runtimeRoot string,
) []qwenRuntimeContract {
	guards := qwenReferencesForEvent(
		settings,
		"PreToolUse",
		qwenGuardScript,
		qwenGuardHookName,
		runtimeRoot,
	)
	posts := qwenReferencesForEvent(
		settings,
		"PostToolUse",
		qwenAuditScript,
		qwenAuditHookName,
		runtimeRoot,
	)
	failures := qwenReferencesForEvent(
		settings,
		"PostToolUseFailure",
		qwenAuditScript,
		qwenAuditHookName,
		runtimeRoot,
	)
	contracts := make([]qwenRuntimeContract, 0)
	for key, guard := range guards {
		post := posts[key]
		failure := failures[key]
		if len(guard) != 1 || len(post) != 1 || len(failure) != 1 ||
			!sameQwenPath(post[0].scriptPath, failure[0].scriptPath) {
			continue
		}
		contracts = append(contracts, qwenRuntimeContract{
			agentID:   guard[0].agentID,
			guardPath: guard[0].scriptPath,
			auditPath: post[0].scriptPath,
		})
	}
	sort.Slice(contracts, func(left, right int) bool {
		return normalizedQwenAgentID(contracts[left].agentID) <
			normalizedQwenAgentID(contracts[right].agentID)
	})
	return contracts
}

func qwenRuntimeRoot() (string, error) {
	return AgentRuntimeRoot()
}

func qwenContractDirectory(contract qwenRuntimeContract) string {
	return filepath.Dir(contract.guardPath)
}
