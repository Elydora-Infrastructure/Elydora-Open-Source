package plugins

import "fmt"

type augmentRuntimePaths struct {
	managedRuntimePaths
	guardWrapperPath string
	auditWrapperPath string
}

func augmentWrapperArtifacts(agentDirectory string) []managedArtifact {
	wrappers := resolveAugmentWrapperPaths(agentDirectory)
	return []managedArtifact{
		{wrappers.guard, "Auggie guard wrapper", maxManagedSourceBytes},
		{wrappers.audit, "Auggie audit wrapper", maxManagedSourceBytes},
	}
}

func augmentAgentPaths(config InstallConfig) (*augmentRuntimePaths, error) {
	if err := validateManagedInstallConfig(config, augmentAgentKey, "Augment Code CLI"); err != nil {
		return nil, err
	}
	paths, err := resolveManagedRuntimePaths(config, augmentGuardScript, augmentAuditScript)
	if err != nil {
		return nil, err
	}
	wrappers := resolveAugmentWrapperPaths(paths.agentDirectory)
	return &augmentRuntimePaths{
		managedRuntimePaths: *paths,
		guardWrapperPath:    wrappers.guard,
		auditWrapperPath:    wrappers.audit,
	}, nil
}

func validateAugmentRuntimeIdentity(agentDirectory, agentID string) error {
	return validateManagedRuntimeIdentity(
		agentDirectory, agentID, augmentAgentKey, "Auggie",
		augmentWrapperArtifacts(agentDirectory)...,
	)
}

func preflightAugmentInstallation(
	config InstallConfig,
	document *augmentDocument,
) (*augmentRuntimePaths, string, error) {
	if document == nil || document.configPath == "" {
		return nil, "", fmt.Errorf("Augment Code CLI installation requires a user settings document")
	}
	paths, err := augmentAgentPaths(config)
	if err != nil {
		return nil, "", err
	}
	if err := validateAugmentRuntimeIdentity(paths.agentDirectory, config.AgentID); err != nil {
		return nil, "", err
	}
	nodePath, err := resolveAbsoluteNodeRuntime("Auggie")
	if err != nil {
		return nil, "", err
	}
	if err := validateAugmentMatchers(document.hooks, nodePath); err != nil {
		return nil, "", err
	}
	return paths, nodePath, nil
}

func prepareAugmentInstallationChanges(
	config InstallConfig,
	paths *augmentRuntimePaths,
	nodePath string,
	rendered *augmentRenderedDocument,
) ([]*fileChange, error) {
	if paths == nil || rendered == nil {
		return nil, fmt.Errorf("prepared Auggie installation is required")
	}
	if err := validateAugmentRuntimeIdentity(paths.agentDirectory, config.AgentID); err != nil {
		return nil, err
	}
	changes, err := managedRuntimeFileChanges(
		config,
		&paths.managedRuntimePaths,
		augmentAgentKey,
		generateGuardScript(augmentAgentKey, config.AgentID, "", false, ""),
		buildHookScriptWithOutput(augmentAgentKey, config.AgentID, "", false, true),
	)
	if err != nil {
		return nil, err
	}
	for _, item := range []struct {
		path, label string
		content     []byte
	}{
		{paths.guardWrapperPath, "Auggie guard wrapper", buildAugmentWrapper(nodePath, paths.guardPath)},
		{paths.auditWrapperPath, "Auggie audit wrapper", buildAugmentWrapper(nodePath, paths.auditPath)},
	} {
		change, err := prepareFileChange(item.path, item.label, item.content, 0700)
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}
	documentChange, err := prepareRenderedAugmentChange(rendered)
	if err != nil {
		return nil, err
	}
	return append(changes, documentChange), nil
}
