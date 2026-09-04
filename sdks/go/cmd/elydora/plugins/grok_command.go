package plugins

import "path/filepath"

func parseGrokCommand(command string) (string, string, bool) {
	if runtimePath, scriptPath, ok := parseEncodedCommand(command); ok {
		return runtimePath, scriptPath, true
	}
	return parseQuotedWindowsCommand(command)
}

func grokRuntimeReferenceForCommand(
	command string,
	scriptName string,
) (*managedScriptReference, error) {
	runtimePath, scriptPath, ok := parseGrokCommand(command)
	if !ok || !filepath.IsAbs(runtimePath) || !isNodeExecutable(runtimePath) {
		return nil, nil
	}
	return resolveManagedScript(scriptPath, scriptName)
}
