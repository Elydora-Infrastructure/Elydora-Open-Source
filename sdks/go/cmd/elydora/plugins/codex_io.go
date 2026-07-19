package plugins

import (
	"fmt"
	"os"
	"path/filepath"
)

func codexConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".codex", "hooks.json"), nil
}

func codexHandler(runtimePath, scriptPath, statusMessage string) map[string]any {
	return map[string]any{
		"type":           "command",
		"command":        quotePOSIXArgument(runtimePath) + " " + quotePOSIXArgument(scriptPath),
		"commandWindows": quoteWindowsArgument(runtimePath) + " " + quoteWindowsArgument(scriptPath),
		"timeout":        10,
		"statusMessage":  statusMessage,
	}
}

func codexMatcherGroup(handler map[string]any) map[string]any {
	return map[string]any{
		"matcher": "*",
		"hooks":   []any{handler},
	}
}
