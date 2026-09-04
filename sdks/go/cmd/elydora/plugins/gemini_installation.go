package plugins

import "fmt"

func preflightGeminiInstallation(
	config InstallConfig,
	document *geminiDocument,
) (*managedRuntimePaths, string, error) {
	if document == nil || document.filePath == "" {
		return nil, "", fmt.Errorf("Gemini CLI installation requires a user settings document")
	}
	if err := validateManagedInstallConfig(config, geminiAgentKey, "Gemini CLI"); err != nil {
		return nil, "", err
	}
	paths, err := resolveManagedRuntimePaths(config, geminiGuardScript, geminiAuditScript)
	if err != nil {
		return nil, "", err
	}
	if err := validateManagedRuntimeIdentity(
		paths.agentDirectory, config.AgentID, geminiAgentKey, "Gemini CLI",
	); err != nil {
		return nil, "", err
	}
	nodePath, err := resolveAbsoluteNodeRuntime("Gemini CLI")
	if err != nil {
		return nil, "", err
	}
	return paths, nodePath, nil
}

func prepareGeminiInstallationChanges(
	config InstallConfig,
	paths *managedRuntimePaths,
	rendered *geminiRenderedDocument,
) ([]*fileChange, error) {
	changes, err := managedRuntimeFileChanges(
		config,
		paths,
		geminiAgentKey,
		generateGuardScript(geminiAgentKey, config.AgentID, "{}\n", false, ""),
		buildHookScriptWithOutput(geminiAgentKey, config.AgentID, "{}\n", false, true),
	)
	if err != nil {
		return nil, err
	}
	documentChange, err := prepareRenderedGeminiChange(rendered)
	if err != nil {
		return nil, err
	}
	return append(changes, documentChange), nil
}
