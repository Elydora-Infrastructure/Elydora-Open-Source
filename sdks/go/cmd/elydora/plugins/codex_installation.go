package plugins

import "path/filepath"

func preflightCodexInstallation(
	config InstallConfig,
	hooksPath string,
) (*managedRuntimePaths, string, error) {
	if err := validateManagedInstallConfig(config, codexAgentKey, "Codex"); err != nil {
		return nil, "", err
	}
	paths, err := resolveManagedRuntimePaths(config, codexGuardScript, codexAuditScript)
	if err != nil {
		return nil, "", err
	}
	if _, err := managedPhysicalDirectoryExists(
		filepath.Dir(hooksPath), "Codex hooks directory",
	); err != nil {
		return nil, "", err
	}
	if err := validateManagedRuntimeIdentity(
		paths.agentDirectory, config.AgentID, codexAgentKey, "Codex",
	); err != nil {
		return nil, "", err
	}
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		return nil, "", err
	}
	return paths, nodePath, nil
}

func prepareCodexInstallationChanges(
	config InstallConfig,
	paths *managedRuntimePaths,
	rendered *codexRenderedDocument,
) ([]*fileChange, error) {
	changes, err := managedRuntimeFileChanges(
		config,
		paths,
		codexAgentKey,
		generateGuardScript(codexAgentKey, config.AgentID, "", false, ""),
		buildHookScriptWithOutput(codexAgentKey, config.AgentID, "", false, true),
	)
	if err != nil {
		return nil, err
	}
	documentChange, err := prepareRenderedCodexChange(rendered)
	if err != nil {
		return nil, err
	}
	return append(changes, documentChange), nil
}
