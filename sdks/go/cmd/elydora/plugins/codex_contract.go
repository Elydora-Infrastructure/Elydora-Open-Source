package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	codexAgentKey           = "codex"
	codexOwnedDescription   = "Elydora audit and freeze enforcement"
	codexGuardStatusMessage = "Checking Elydora agent state"
	codexAuditStatusMessage = "Recording Elydora tool use"
)

func cloneCodexObject(value map[string]any) map[string]any {
	clone := make(map[string]any, len(value))
	for key, item := range value {
		clone[key] = item
	}
	return clone
}

func codexHooksObject(settings map[string]any) (map[string]any, error) {
	value, exists := settings["hooks"]
	if !exists {
		return map[string]any{}, nil
	}
	hooks, ok := value.(map[string]any)
	if !ok || hooks == nil {
		return nil, fmt.Errorf(`Codex hooks config field "hooks" must be an object`)
	}
	return cloneCodexObject(hooks), nil
}

func codexEventGroups(hooks map[string]any, event string) ([]map[string]any, error) {
	value, exists := hooks[event]
	if !exists {
		return []map[string]any{}, nil
	}
	groups, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf(`Codex hooks config field "hooks.%s" must be an array of objects`, event)
	}
	objects := make([]map[string]any, 0, len(groups))
	for _, group := range groups {
		object, ok := group.(map[string]any)
		if !ok || object == nil {
			return nil, fmt.Errorf(`Codex hooks config field "hooks.%s" must be an array of objects`, event)
		}
		objects = append(objects, object)
	}
	return objects, nil
}

func codexGroupHandlers(group map[string]any) ([]map[string]any, error) {
	value, exists := group["hooks"]
	if !exists {
		return nil, fmt.Errorf("Codex hook matcher group must contain a hooks array")
	}
	handlers, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("Codex hook matcher group must contain a hooks array")
	}
	objects := make([]map[string]any, 0, len(handlers))
	for _, handler := range handlers {
		object, ok := handler.(map[string]any)
		if !ok || object == nil {
			return nil, fmt.Errorf("Codex hook matcher group must contain a hooks array")
		}
		objects = append(objects, object)
	}
	return objects, nil
}

func normalizeCodexPath(value string) string {
	normalized := filepath.ToSlash(value)
	if runtime.GOOS == "windows" {
		return strings.ToLower(normalized)
	}
	return normalized
}

func isManagedCodexCommand(command any, scriptName, agentID string) bool {
	text, ok := command.(string)
	if !ok {
		return false
	}
	normalized := normalizeCodexPath(text)
	if !strings.Contains(normalized, "/.elydora/") {
		return false
	}
	suffix := "/" + normalizeCodexPath(scriptName)
	if agentID == "" {
		return codexCommandEndsWith(normalized, suffix)
	}
	expected := normalizeCodexPath(filepath.Join(".elydora", agentID, scriptName))
	return codexCommandEndsWith(normalized, "/"+expected)
}

func codexCommandEndsWith(command, scriptPath string) bool {
	trimmed := strings.TrimSpace(command)
	return strings.HasSuffix(trimmed, scriptPath) ||
		strings.HasSuffix(trimmed, scriptPath+`'`) ||
		strings.HasSuffix(trimmed, scriptPath+`"`)
}

func isElydoraCodexHandler(handler map[string]any, agentID string) bool {
	var scriptName string
	switch handler["statusMessage"] {
	case codexGuardStatusMessage:
		scriptName = "guard.js"
	case codexAuditStatusMessage:
		scriptName = "hook.js"
	default:
		return false
	}
	return isManagedCodexCommand(handler["command"], scriptName, agentID) ||
		isManagedCodexCommand(handler["commandWindows"], scriptName, agentID)
}

func withoutCodexHandlers(groups []map[string]any, agentID string) ([]map[string]any, bool, error) {
	filteredGroups := make([]map[string]any, 0, len(groups))
	changed := false
	for _, group := range groups {
		handlers, err := codexGroupHandlers(group)
		if err != nil {
			return nil, false, err
		}
		filteredHandlers := make([]any, 0, len(handlers))
		removed := false
		for _, handler := range handlers {
			if isElydoraCodexHandler(handler, agentID) {
				changed = true
				removed = true
				continue
			}
			filteredHandlers = append(filteredHandlers, handler)
		}
		if len(filteredHandlers) == 0 && removed {
			continue
		}
		nextGroup := cloneCodexObject(group)
		nextGroup["hooks"] = filteredHandlers
		filteredGroups = append(filteredGroups, nextGroup)
	}
	return filteredGroups, changed, nil
}

func findCodexHandler(hooks map[string]any, event, status string) (map[string]any, error) {
	groups, err := codexEventGroups(hooks, event)
	if err != nil {
		return nil, err
	}
	for _, group := range groups {
		handlers, err := codexGroupHandlers(group)
		if err != nil {
			return nil, err
		}
		for _, handler := range handlers {
			if handler["statusMessage"] == status && isElydoraCodexHandler(handler, "") {
				return handler, nil
			}
		}
	}
	return nil, nil
}

func codexSettingsOwned(settings, hooks map[string]any) bool {
	for key := range settings {
		if key != "description" && key != "hooks" {
			return false
		}
	}
	if settings["description"] != codexOwnedDescription {
		return false
	}
	for event, value := range hooks {
		if event != "PreToolUse" && event != "PostToolUse" {
			return false
		}
		groups, ok := value.([]map[string]any)
		if !ok || len(groups) != 0 {
			return false
		}
	}
	return true
}

func codexCommandReferences(handler map[string]any, scriptPath string) bool {
	expected := normalizeCodexPath(scriptPath)
	for _, key := range []string{"command", "commandWindows"} {
		command, ok := handler[key].(string)
		if ok && codexCommandEndsWith(normalizeCodexPath(command), expected) {
			return true
		}
	}
	return false
}

func codexRuntimeScriptsExist(guard, audit map[string]any) (bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, fmt.Errorf("resolve home directory: %w", err)
	}
	root := filepath.Join(home, ".elydora")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read Elydora runtime directory at %s: %w", root, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		agentDir := filepath.Join(root, entry.Name())
		guardPath := filepath.Join(agentDir, "guard.js")
		hookPath := filepath.Join(agentDir, "hook.js")
		if !codexCommandReferences(guard, guardPath) || !codexCommandReferences(audit, hookPath) {
			continue
		}
		configPath := filepath.Join(agentDir, "config.json")
		config, exists, err := readHookJSONObject(configPath, "Elydora runtime config")
		if err != nil {
			return false, err
		}
		if !exists {
			continue
		}
		agentName, ok := config["agent_name"].(string)
		if !ok {
			return false, fmt.Errorf(`Elydora runtime config at %s field "agent_name" must be a string`, configPath)
		}
		if agentName != codexAgentKey {
			continue
		}
		guardExists, err := regularFileExists(guardPath, "Elydora guard runtime")
		if err != nil {
			return false, err
		}
		hookExists, err := regularFileExists(hookPath, "Elydora audit runtime")
		if err != nil {
			return false, err
		}
		return guardExists && hookExists, nil
	}
	return false, nil
}
