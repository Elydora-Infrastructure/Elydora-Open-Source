package plugins

import "path/filepath"

func codexHandler(runtimePath, scriptPath, statusMessage string) map[string]any {
	return map[string]any{
		"type":           "command",
		"command":        posixSource(runtimePath, scriptPath),
		"commandWindows": encodedWindowsCommand(runtimePath, scriptPath),
		"timeout":        codexHookTimeout,
		"statusMessage":  statusMessage,
	}
}

func codexMatcherGroup(handler map[string]any) map[string]any {
	return map[string]any{"matcher": "*", "hooks": []any{handler}}
}

func exactCodexHandlerKeys(handler map[string]any) bool {
	if len(handler) != 5 {
		return false
	}
	for _, key := range []string{"type", "command", "commandWindows", "timeout", "statusMessage"} {
		if _, exists := handler[key]; !exists {
			return false
		}
	}
	return true
}

func codexManagedScriptPath(handler map[string]any, status string) (string, bool) {
	if !exactCodexHandlerKeys(handler) || handler["type"] != "command" ||
		handler["timeout"] != codexHookTimeout || handler["statusMessage"] != status {
		return "", false
	}
	command, _ := handler["command"].(string)
	commandWindows, _ := handler["commandWindows"].(string)
	posixRuntime, posixScript, posixOK := parsePOSIXCommand(command)
	windowsRuntime, windowsScript, windowsOK := parseEncodedWindowsCommand(commandWindows)
	if !windowsOK && posixOK {
		expected := quoteWindowsArgument(posixRuntime) + " " + quoteWindowsArgument(posixScript)
		if commandWindows == expected {
			windowsRuntime, windowsScript, windowsOK = posixRuntime, posixScript, true
		}
	}
	if !posixOK || !windowsOK || !filepath.IsAbs(posixRuntime) ||
		!filepath.IsAbs(posixScript) || !isNodeExecutable(posixRuntime) ||
		!isNodeExecutable(windowsRuntime) ||
		!sameManagedPath(posixRuntime, windowsRuntime) ||
		!sameManagedPath(posixScript, windowsScript) {
		return "", false
	}
	return posixScript, true
}

func codexManagedAgentID(
	handler map[string]any,
	scriptName string,
	status string,
) (string, bool) {
	scriptPath, managed := codexManagedScriptPath(handler, status)
	if !managed {
		return "", false
	}
	reference, err := resolveManagedScript(scriptPath, scriptName)
	if err != nil || reference == nil {
		return "", false
	}
	return reference.agentID, true
}
