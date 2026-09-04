package plugins

import "fmt"

type clineRuntimePaths struct {
	managedRuntimePaths
	hooks clineHookPaths
}

func clineAgentPaths(config InstallConfig, hooks clineHookPaths) (*clineRuntimePaths, error) {
	if err := validateManagedInstallConfig(config, clineAgentKey, "Cline"); err != nil {
		return nil, err
	}
	paths, err := resolveManagedRuntimePaths(config, clineGuardScript, clineAuditScript)
	if err != nil {
		return nil, err
	}
	return &clineRuntimePaths{managedRuntimePaths: *paths, hooks: hooks}, nil
}

func preflightClineInstallation(
	config InstallConfig,
	hooks clineHookPaths,
) (*clineRuntimePaths, error) {
	paths, err := clineAgentPaths(config, hooks)
	if err != nil {
		return nil, err
	}
	if err := validateManagedRuntimeIdentity(
		paths.agentDirectory, config.AgentID, clineAgentKey, "Cline",
	); err != nil {
		return nil, err
	}
	if _, err := resolveAbsoluteNodeRuntime("Cline"); err != nil {
		return nil, err
	}
	return paths, nil
}

func clineExpectedScripts(agentID string, _ map[string]any) ([]byte, []byte) {
	return []byte(generateGuardScript(clineAgentKey, agentID, "", false, "")),
		[]byte(buildHookScriptWithOutput(clineAgentKey, agentID, "", false, true))
}

func prepareClineInstallationChanges(
	config InstallConfig,
	paths *clineRuntimePaths,
	guardFile clineHookFile,
	auditFile clineHookFile,
) ([]*fileChange, error) {
	if paths == nil {
		return nil, fmt.Errorf("prepared Cline installation is required")
	}
	if err := validateManagedRuntimeIdentity(
		paths.agentDirectory, config.AgentID, clineAgentKey, "Cline",
	); err != nil {
		return nil, err
	}
	guardMetadata, err := buildClineMetadata("guard", config.AgentID, paths.guardPath)
	if err != nil {
		return nil, fmt.Errorf("build Cline guard metadata: %w", err)
	}
	auditMetadata, err := buildClineMetadata("audit", config.AgentID, paths.auditPath)
	if err != nil {
		return nil, fmt.Errorf("build Cline audit metadata: %w", err)
	}
	guardWrapper, err := buildClineWrapper(guardMetadata)
	if err != nil {
		return nil, fmt.Errorf("build Cline guard wrapper: %w", err)
	}
	auditWrapper, err := buildClineWrapper(auditMetadata)
	if err != nil {
		return nil, fmt.Errorf("build Cline audit wrapper: %w", err)
	}
	if _, err := clineContractForFiles(
		clineHookFile{exists: true, filePath: guardFile.filePath, source: guardWrapper, metadata: &guardMetadata},
		clineHookFile{exists: true, filePath: auditFile.filePath, source: auditWrapper, metadata: &auditMetadata},
	); err != nil {
		return nil, err
	}
	guardScript, auditScript := clineExpectedScripts(config.AgentID, nil)
	changes, err := managedRuntimeFileChanges(
		config, &paths.managedRuntimePaths, clineAgentKey, string(guardScript), string(auditScript),
	)
	if err != nil {
		return nil, err
	}
	for _, item := range []struct {
		file   clineHookFile
		label  string
		source string
	}{
		{guardFile, "Cline PreToolUse hook", guardWrapper},
		{auditFile, "Cline PostToolUse hook", auditWrapper},
	} {
		change, err := prepareSourceChange(
			item.file.filePath,
			item.label,
			[]byte(item.file.source),
			item.file.exists,
			[]byte(item.source),
			0700,
			false,
		)
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}
	return changes, nil
}
