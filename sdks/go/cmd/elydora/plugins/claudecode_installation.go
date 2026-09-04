package plugins

import "fmt"

func preflightClaudeInstallation(
	config InstallConfig,
	document *claudeDocument,
) (*managedRuntimePaths, string, error) {
	if document == nil || document.filePath == "" {
		return nil, "", fmt.Errorf("Claude Code installation requires a user settings document")
	}
	if err := validateManagedInstallConfig(config, claudeAgentKey, "Claude Code"); err != nil {
		return nil, "", err
	}
	paths, err := resolveManagedRuntimePaths(config, claudeGuardScript, claudeAuditScript)
	if err != nil {
		return nil, "", err
	}
	if err := validateManagedRuntimeIdentity(
		paths.agentDirectory, config.AgentID, claudeAgentKey, "Claude Code",
	); err != nil {
		return nil, "", err
	}
	nodePath, err := resolveAbsoluteNodeRuntime("Claude Code")
	if err != nil {
		return nil, "", err
	}
	return paths, nodePath, nil
}

func prepareClaudeInstallationChanges(
	config InstallConfig,
	paths *managedRuntimePaths,
	rendered *claudeRenderedDocument,
) ([]*fileChange, error) {
	changes, err := managedRuntimeFileChanges(
		config,
		paths,
		claudeAgentKey,
		generateGuardScript(claudeAgentKey, config.AgentID, "", false, ""),
		buildHookScriptWithOutput(claudeAgentKey, config.AgentID, "", false, true),
	)
	if err != nil {
		return nil, err
	}
	documentChange, err := prepareRenderedClaudeChange(rendered)
	if err != nil {
		return nil, err
	}
	return append(changes, documentChange), nil
}
