package plugins

import (
	"path/filepath"
	"strings"
)

func kiroIdeRuntimeReferenceForCommand(
	command string,
	scriptName string,
) (*managedScriptReference, error) {
	runtimePath, scriptPath, ok := parseEncodedCommand(command)
	if !ok || !filepath.IsAbs(runtimePath) || !isNodeExecutable(runtimePath) {
		return nil, nil
	}
	return resolveManagedScript(scriptPath, scriptName)
}

func currentKiroIdeReference(
	hook map[string]any,
	nodePath string,
) (*managedScriptReference, error) {
	reference, err := managedKiroIdeReference(hook)
	if err != nil || reference == nil {
		return reference, err
	}
	action := hook["action"].(map[string]any)
	command := action["command"].(string)
	expected, err := buildEncodedCommand("Kiro IDE", nodePath, reference.scriptPath)
	if err != nil {
		return nil, err
	}
	if command != expected {
		return nil, nil
	}
	return reference, nil
}

// legacyKiroIdeGoReference reads the pre-2.1 `node <script>` form.
func legacyKiroIdeGoReference(
	command any,
	scriptName string,
) (*managedScriptReference, error) {
	text, ok := command.(string)
	if !ok || !strings.HasPrefix(text, "node ") {
		return nil, nil
	}
	return resolveManagedScript(strings.TrimPrefix(text, "node "), scriptName)
}
