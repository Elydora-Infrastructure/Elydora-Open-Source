package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	kiroAgentKey      = "kirocli"
	kiroV2AgentName   = "elydora-audit"
	kiroV2Description = "Kiro CLI with Elydora audit and freeze enforcement"
	kiroV3GuardName   = "elydora-guard"
	kiroV3AuditName   = "elydora-audit"
)

type kiroHookContract struct {
	guardCommand string
	auditCommand string
	configPath   string
}

type kiroConfigMutation struct {
	path    string
	value   map[string]any
	changed bool
	remove  bool
}

func cloneKiroObject(value map[string]any) map[string]any {
	clone := make(map[string]any, len(value))
	copyKiroObject(clone, value)
	return clone
}

func copyKiroObject(destination, source map[string]any) {
	for key, value := range source {
		destination[key] = value
	}
}

func kiroHooksObject(settings map[string]any, label string) (map[string]any, error) {
	value, exists := settings["hooks"]
	if !exists {
		return map[string]any{}, nil
	}
	hooks, ok := value.(map[string]any)
	if !ok || hooks == nil {
		return nil, fmt.Errorf(`%s field "hooks" must be an object`, label)
	}
	return cloneKiroObject(hooks), nil
}

func kiroHookEntries(hooks map[string]any, event, label string) ([]map[string]any, error) {
	value, exists := hooks[event]
	if !exists {
		return []map[string]any{}, nil
	}
	entries, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf(`%s field "hooks.%s" must be an array of objects`, label, event)
	}
	objects := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		object, ok := entry.(map[string]any)
		if !ok || object == nil {
			return nil, fmt.Errorf(`%s field "hooks.%s" must be an array of objects`, label, event)
		}
		objects = append(objects, object)
	}
	return objects, nil
}

func isManagedKiroCommand(command any, scriptName, agentID string) bool {
	text, ok := command.(string)
	if !ok {
		return false
	}
	normalized := strings.ToLower(text)
	if !strings.Contains(normalized, ".elydora") || !strings.Contains(normalized, strings.ToLower(scriptName)) {
		return false
	}
	return agentID == "" || strings.Contains(text, agentID)
}

func withoutKiroV2Hooks(entries []map[string]any, agentID string) []any {
	filtered := make([]any, 0, len(entries))
	for _, hook := range entries {
		if isManagedKiroCommand(hook["command"], "guard.js", agentID) ||
			isManagedKiroCommand(hook["command"], "hook.js", agentID) {
			continue
		}
		filtered = append(filtered, hook)
	}
	return filtered
}

func buildKiroV2Hook(runtimePath, scriptPath string) map[string]any {
	return map[string]any{
		"matcher":    "*",
		"command":    buildKiroCommand(runtimePath, scriptPath),
		"timeout_ms": 5000,
	}
}

func kiroV3Hooks(settings map[string]any) ([]map[string]any, error) {
	if value, exists := settings["version"]; exists {
		version, ok := value.(string)
		if !ok || version != "v1" {
			return nil, fmt.Errorf(`Kiro CLI v3 hooks config field "version" must be "v1"`)
		}
	}
	value, exists := settings["hooks"]
	if !exists {
		return []map[string]any{}, nil
	}
	hooks, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf(`Kiro CLI v3 hooks config field "hooks" must be an array of objects`)
	}
	objects := make([]map[string]any, 0, len(hooks))
	for _, hook := range hooks {
		object, ok := hook.(map[string]any)
		if !ok || object == nil {
			return nil, fmt.Errorf(`Kiro CLI v3 hooks config field "hooks" must be an array of objects`)
		}
		objects = append(objects, object)
	}
	return objects, nil
}

func kiroV3ActionCommand(hook map[string]any) any {
	action, ok := hook["action"].(map[string]any)
	if !ok {
		return nil
	}
	return action["command"]
}

func isManagedKiroV3Hook(hook map[string]any, agentID string) bool {
	switch hook["name"] {
	case kiroV3GuardName:
		return isManagedKiroCommand(kiroV3ActionCommand(hook), "guard.js", agentID)
	case kiroV3AuditName:
		return isManagedKiroCommand(kiroV3ActionCommand(hook), "hook.js", agentID)
	default:
		return false
	}
}

func buildKiroV3Hook(name, description, trigger, runtimePath, scriptPath string) map[string]any {
	return map[string]any{
		"name":        name,
		"description": description,
		"trigger":     trigger,
		"matcher":     ".*",
		"action": map[string]any{
			"type":    "command",
			"command": buildKiroCommand(runtimePath, scriptPath),
		},
		"timeout": 5,
		"enabled": true,
	}
}

func prepareKiroV2Uninstall(
	path string,
	settings map[string]any,
	exists bool,
	agentID string,
) (kiroConfigMutation, error) {
	mutation := kiroConfigMutation{path: path}
	if !exists {
		return mutation, nil
	}
	hooks, err := kiroHooksObject(settings, "Kiro CLI v2 agent config")
	if err != nil {
		return mutation, err
	}
	pre, err := kiroHookEntries(hooks, "preToolUse", "Kiro CLI v2 agent config")
	if err != nil {
		return mutation, err
	}
	post, err := kiroHookEntries(hooks, "postToolUse", "Kiro CLI v2 agent config")
	if err != nil {
		return mutation, err
	}
	filteredPre := withoutKiroV2Hooks(pre, agentID)
	filteredPost := withoutKiroV2Hooks(post, agentID)
	if len(filteredPre) == len(pre) && len(filteredPost) == len(post) {
		return mutation, nil
	}
	hooks["preToolUse"] = filteredPre
	hooks["postToolUse"] = filteredPost
	next := cloneKiroObject(settings)
	next["hooks"] = hooks
	mutation.value = next
	mutation.changed = true
	mutation.remove = isKiroV2Owned(settings, hooks)
	return mutation, nil
}

func isKiroV2Owned(settings, hooks map[string]any) bool {
	allowedConfigKeys := map[string]bool{
		"name": true, "description": true, "tools": true, "includeMcpJson": true, "hooks": true,
	}
	for key := range settings {
		if !allowedConfigKeys[key] {
			return false
		}
	}
	tools, ok := settings["tools"].([]any)
	if settings["name"] != kiroV2AgentName || settings["description"] != kiroV2Description ||
		!ok || len(tools) != 1 || tools[0] != "*" || settings["includeMcpJson"] != true {
		return false
	}
	for event, value := range hooks {
		if event != "preToolUse" && event != "postToolUse" {
			return false
		}
		entries, ok := value.([]any)
		if !ok || len(entries) != 0 {
			return false
		}
	}
	return true
}

func prepareKiroV3Uninstall(
	path string,
	settings map[string]any,
	exists bool,
	agentID string,
) (kiroConfigMutation, error) {
	mutation := kiroConfigMutation{path: path}
	if !exists {
		return mutation, nil
	}
	hooks, err := kiroV3Hooks(settings)
	if err != nil {
		return mutation, err
	}
	filtered := make([]any, 0, len(hooks))
	for _, hook := range hooks {
		if !isManagedKiroV3Hook(hook, agentID) {
			filtered = append(filtered, hook)
		}
	}
	if len(filtered) == len(hooks) {
		return mutation, nil
	}
	next := cloneKiroObject(settings)
	next["hooks"] = filtered
	owned := true
	for key := range settings {
		if key != "version" && key != "hooks" {
			owned = false
		}
	}
	mutation.value = next
	mutation.changed = true
	mutation.remove = owned && len(filtered) == 0
	return mutation, nil
}

func applyKiroMutation(mutation kiroConfigMutation) error {
	if !mutation.changed {
		return nil
	}
	if mutation.remove {
		return removeHookFile(mutation.path, "Kiro CLI config")
	}
	return writeHookJSONObjectAtomic(mutation.path, mutation.value)
}

func configuredKiroContracts(
	v2Path string,
	v2Settings map[string]any,
	v2Exists bool,
	v3Path string,
	v3Settings map[string]any,
	v3Exists bool,
) ([]kiroHookContract, error) {
	contracts := make([]kiroHookContract, 0, 2)
	if v2Exists {
		hooks, err := kiroHooksObject(v2Settings, "Kiro CLI v2 agent config")
		if err != nil {
			return nil, err
		}
		pre, err := kiroHookEntries(hooks, "preToolUse", "Kiro CLI v2 agent config")
		if err != nil {
			return nil, err
		}
		post, err := kiroHookEntries(hooks, "postToolUse", "Kiro CLI v2 agent config")
		if err != nil {
			return nil, err
		}
		guard := findKiroV2Command(pre, "guard.js")
		audit := findKiroV2Command(post, "hook.js")
		if guard != "" && audit != "" {
			contracts = append(contracts, kiroHookContract{guard, audit, v2Path})
		}
	}
	if v3Exists {
		hooks, err := kiroV3Hooks(v3Settings)
		if err != nil {
			return nil, err
		}
		guard := findKiroV3Command(hooks, kiroV3GuardName, "guard.js")
		audit := findKiroV3Command(hooks, kiroV3AuditName, "hook.js")
		if guard != "" && audit != "" {
			contracts = append(contracts, kiroHookContract{guard, audit, v3Path})
		}
	}
	return contracts, nil
}

func findKiroV2Command(entries []map[string]any, scriptName string) string {
	for _, hook := range entries {
		if isManagedKiroCommand(hook["command"], scriptName, "") {
			return hook["command"].(string)
		}
	}
	return ""
}

func findKiroV3Command(entries []map[string]any, name, scriptName string) string {
	for _, hook := range entries {
		command := kiroV3ActionCommand(hook)
		if hook["name"] == name && isManagedKiroCommand(command, scriptName, "") {
			return command.(string)
		}
	}
	return ""
}

func regularFileExists(path, label string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read %s at %s: %w", label, path, err)
	}
	return info.Mode().IsRegular(), nil
}

func kiroRuntimeScriptsExist(contracts []kiroHookContract) (bool, error) {
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
		referenced := false
		for _, contract := range contracts {
			if strings.Contains(contract.guardCommand, guardPath) &&
				strings.Contains(contract.auditCommand, hookPath) {
				referenced = true
				break
			}
		}
		if !referenced {
			continue
		}
		configPath := filepath.Join(agentDir, "config.json")
		config, exists, err := readHookJSONObject(configPath, "Elydora runtime config")
		if err != nil {
			return false, err
		}
		if !exists || config["agent_name"] != kiroAgentKey {
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
