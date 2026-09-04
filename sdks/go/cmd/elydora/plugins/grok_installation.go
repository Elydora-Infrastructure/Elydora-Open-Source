package plugins

import "fmt"

func preflightGrokInstallation(
	config InstallConfig,
	document *grokDocument,
) (*managedRuntimePaths, string, error) {
	if document == nil {
		return nil, "", fmt.Errorf("grok installation requires a hook document")
	}
	if err := validateManagedInstallConfig(config, grokAgentKey, "grok"); err != nil {
		return nil, "", err
	}
	paths, err := resolveManagedRuntimePaths(config, grokGuardScript, grokAuditScript)
	if err != nil {
		return nil, "", err
	}
	if err := validateManagedRuntimeIdentity(
		paths.agentDirectory, config.AgentID, grokAgentKey, "Grok",
	); err != nil {
		return nil, "", err
	}
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		return nil, "", err
	}
	return paths, nodePath, nil
}

func grokExpectedScripts(agentID string, _ map[string]any) ([]byte, []byte) {
	return []byte(generateGuardScript(grokAgentKey, agentID, "", false, "grok")),
		[]byte(buildHookScriptWithOutput(grokAgentKey, agentID, "", false, true))
}

func prepareGrokInstallationChanges(
	config InstallConfig,
	paths *managedRuntimePaths,
	rendered *grokRenderedDocument,
) ([]*fileChange, error) {
	guardScript, auditScript := grokExpectedScripts(config.AgentID, nil)
	changes, err := managedRuntimeFileChanges(
		config, paths, grokAgentKey, string(guardScript), string(auditScript),
	)
	if err != nil {
		return nil, err
	}
	documentChange, err := prepareRenderedGrokChange(rendered)
	if err != nil {
		return nil, err
	}
	return append(changes, documentChange), nil
}
