package plugins

import "strings"

// extractElydoraScriptPath supports adapters that still use the legacy shared
// command-hook shape. Provider-specific adapters should define exact ownership.
func extractElydoraScriptPath(hookArray any) string {
	entries, _ := hookArray.([]any)
	for _, entry := range entries {
		object, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if hooks, ok := object["hooks"].([]any); ok {
			for _, hook := range hooks {
				handler, ok := hook.(map[string]any)
				if !ok {
					continue
				}
				if command, _ := handler["command"].(string); strings.Contains(
					command,
					"elydora",
				) {
					return extractPathFromNodeCommand(command)
				}
			}
		}
		if command, _ := object["command"].(string); strings.Contains(
			command,
			"elydora",
		) {
			return extractPathFromNodeCommand(command)
		}
	}
	return ""
}

func extractPathFromNodeCommand(command string) string {
	command = strings.TrimSpace(command)
	if strings.HasPrefix(command, "node ") {
		return strings.TrimSpace(command[5:])
	}
	return ""
}

func hasElydoraEntry(hookArray any) bool {
	entries, _ := hookArray.([]any)
	for _, entry := range entries {
		if object, ok := entry.(map[string]any); ok && isElydoraHookEntry(object) {
			return true
		}
	}
	return false
}

func isElydoraHookEntry(object map[string]any) bool {
	if hooks, ok := object["hooks"].([]any); ok {
		for _, hook := range hooks {
			handler, ok := hook.(map[string]any)
			if !ok {
				continue
			}
			if command, _ := handler["command"].(string); strings.Contains(
				command,
				"elydora",
			) {
				return true
			}
		}
	}
	command, _ := object["command"].(string)
	return strings.Contains(command, "elydora")
}
