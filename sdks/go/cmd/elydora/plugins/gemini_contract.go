package plugins

import (
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const (
	geminiAgentKey        = "gemini"
	geminiConfigFile      = "settings.json"
	geminiGuardScript     = "guard.js"
	geminiAuditScript     = "hook.js"
	geminiGuardHookName   = "elydora-guard"
	geminiAuditHookName   = "elydora-audit"
	geminiHookTimeout     = float64(10_000)
	geminiOwnedFileMarker = "// Managed by Elydora"
)

var geminiManagedEvents = [...]string{"BeforeTool", "AfterTool"}

type geminiManagedRemoval struct {
	event          string
	groupIndex     int
	handlerIndexes []int
	removeGroup    bool
}

type geminiRuntimeContract struct {
	agentID   string
	guardPath string
	auditPath string
}

func buildGeminiGroup(runtimePath, scriptPath, name string) (map[string]any, error) {
	command, err := buildGeminiCommand(runtimePath, scriptPath)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"hooks": []any{map[string]any{
			"type": "command", "name": name, "command": command,
			"timeout": geminiHookTimeout,
		}},
	}, nil
}

func exactManagedGeminiGroup(group map[string]any) bool {
	_, hasHooks := group["hooks"]
	return len(group) == 1 && hasHooks
}

func currentManagedGeminiReference(
	handler map[string]any,
	scriptName string,
	hookName string,
) (*geminiRuntimeReference, error) {
	if len(handler) != 4 || handler["type"] != "command" ||
		handler["name"] != hookName || handler["timeout"] != geminiHookTimeout {
		return nil, nil
	}
	command, ok := handler["command"].(string)
	if !ok {
		return nil, nil
	}
	return geminiRuntimeReferenceForCommand(command, scriptName, false)
}

func legacyManagedGeminiReference(
	handler map[string]any,
	scriptName string,
) (*geminiRuntimeReference, error) {
	if len(handler) != 2 || handler["type"] != "command" {
		return nil, nil
	}
	command, ok := handler["command"].(string)
	if !ok {
		return nil, nil
	}
	return geminiRuntimeReferenceForCommand(command, scriptName, true)
}

func managedGeminiReference(
	handler map[string]any,
	scriptName string,
	hookName string,
	includeLegacy bool,
) (*geminiRuntimeReference, error) {
	reference, err := currentManagedGeminiReference(handler, scriptName, hookName)
	if err != nil || reference != nil || !includeLegacy {
		return reference, err
	}
	return legacyManagedGeminiReference(handler, scriptName)
}

func managedGeminiHooksEnabled(controls geminiHookControls) bool {
	if !controls.enabled {
		return false
	}
	for _, entry := range controls.disabled {
		if entry == geminiGuardHookName || entry == geminiAuditHookName {
			return false
		}
	}
	return true
}

func disabledManagedGeminiEntries(
	controls geminiHookControls,
) ([]string, error) {
	disabled := make([]string, 0)
	for _, entry := range controls.disabled {
		managed := entry == geminiGuardHookName || entry == geminiAuditHookName
		for _, script := range []string{geminiGuardScript, geminiAuditScript} {
			reference, err := geminiRuntimeReferenceForCommand(entry, script, true)
			if err != nil {
				return nil, err
			}
			managed = managed || reference != nil
		}
		if managed {
			disabled = append(disabled, entry)
		}
	}
	return disabled, nil
}

func managedGeminiRemovals(
	hooks geminiHooks,
	agentID string,
) ([]geminiManagedRemoval, error) {
	removals := make([]geminiManagedRemoval, 0)
	for _, contract := range []struct{ event, script, name string }{
		{"BeforeTool", geminiGuardScript, geminiGuardHookName},
		{"AfterTool", geminiAuditScript, geminiAuditHookName},
	} {
		for groupIndex, groupValue := range hooks[contract.event] {
			group := groupValue.(map[string]any)
			handlers := group["hooks"].([]any)
			indexes := make([]int, 0)
			for handlerIndex, handlerValue := range handlers {
				handler := handlerValue.(map[string]any)
				reference, err := managedGeminiReference(
					handler,
					contract.script,
					contract.name,
					true,
				)
				if err != nil {
					return nil, err
				}
				if reference != nil &&
					(agentID == "" || sameGeminiAgentID(reference.agentID, agentID)) {
					indexes = append(indexes, handlerIndex)
				}
			}
			if len(indexes) > 0 {
				removals = append(removals, geminiManagedRemoval{
					contract.event,
					groupIndex,
					indexes,
					exactManagedGeminiGroup(group) && len(indexes) == len(handlers),
				})
			}
		}
	}
	return removals, nil
}

func geminiReferenceKey(agentID string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(agentID)
	}
	return agentID
}

func geminiReferencesForEvent(
	groups []any,
	scriptName string,
	hookName string,
) (map[string][]geminiRuntimeReference, error) {
	result := map[string][]geminiRuntimeReference{}
	for _, groupValue := range groups {
		group := groupValue.(map[string]any)
		if !exactManagedGeminiGroup(group) {
			continue
		}
		for _, handlerValue := range group["hooks"].([]any) {
			reference, err := currentManagedGeminiReference(
				handlerValue.(map[string]any),
				scriptName,
				hookName,
			)
			if err != nil {
				return nil, err
			}
			if reference != nil {
				key := geminiReferenceKey(reference.agentID)
				result[key] = append(result[key], *reference)
			}
		}
	}
	return result, nil
}

func geminiRuntimeContracts(
	hooks geminiHooks,
) ([]geminiRuntimeContract, error) {
	guards, err := geminiReferencesForEvent(
		hooks["BeforeTool"],
		geminiGuardScript,
		geminiGuardHookName,
	)
	if err != nil {
		return nil, err
	}
	audits, err := geminiReferencesForEvent(
		hooks["AfterTool"],
		geminiAuditScript,
		geminiAuditHookName,
	)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(guards))
	for key := range guards {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	contracts := make([]geminiRuntimeContract, 0, len(keys))
	for _, key := range keys {
		guard := guards[key]
		audit := audits[key]
		if len(guard) != 1 || len(audit) != 1 {
			continue
		}
		contracts = append(contracts, geminiRuntimeContract{
			agentID:   guard[0].agentID,
			guardPath: guard[0].scriptPath,
			auditPath: audit[0].scriptPath,
		})
	}
	return contracts, nil
}

func geminiRuntimeRoot() (string, error) {
	root, err := AgentRuntimeRoot()
	if err != nil {
		return "", err
	}
	return filepath.Clean(root), nil
}
