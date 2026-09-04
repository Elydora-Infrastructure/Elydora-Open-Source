package plugins

import "fmt"

type preparedQwenInstallation struct {
	changes       []*fileChange
	preconditions []filePrecondition
	paths         *managedRuntimePaths
}

func preflightQwenInstallation(
	config InstallConfig,
	sources *qwenSources,
) (*managedRuntimePaths, string, error) {
	if sources == nil || sources.user == nil || sources.user.filePath == "" {
		return nil, "", fmt.Errorf("Qwen Code installation requires user settings")
	}
	if err := requireQwenHooksEnabled(sources); err != nil {
		return nil, "", err
	}
	if err := validateManagedInstallConfig(config, qwenAgentKey, "Qwen Code"); err != nil {
		return nil, "", err
	}
	paths, err := resolveManagedRuntimePaths(config, qwenGuardScript, qwenAuditScript)
	if err != nil {
		return nil, "", err
	}
	if err := validateManagedRuntimeIdentity(
		paths.agentDirectory, config.AgentID, qwenAgentKey, "Qwen Code",
	); err != nil {
		return nil, "", err
	}
	nodePath, err := resolveAbsoluteNodeRuntime("Qwen Code")
	if err != nil {
		return nil, "", err
	}
	return paths, nodePath, nil
}

func qwenExpectedScripts(agentID string, _ map[string]any) ([]byte, []byte) {
	return []byte(generateGuardScript(qwenAgentKey, agentID, "", false, "")),
		[]byte(buildHookScriptWithOutput(qwenAgentKey, agentID, "", false, true))
}

func prepareRenderedQwenChange(
	rendered *qwenRenderedDocument,
) (*fileChange, error) {
	if rendered == nil || !rendered.changed {
		return nil, nil
	}
	return prepareSnapshotSourceChange(
		rendered.document.filePath,
		qwenDocumentLabel(rendered.document),
		rendered.document.snapshot,
		rendered.next,
		0600,
		rendered.remove,
	)
}

func qwenReadOnlyPreconditions(
	sources *qwenSources,
	changedPath string,
) []filePrecondition {
	if sources == nil {
		return nil
	}
	result := make([]filePrecondition, 0, len(sources.preconditions))
	for _, condition := range sources.preconditions {
		if changedPath != "" && sameManagedPath(condition.filePath, changedPath) {
			continue
		}
		result = append(result, condition)
	}
	return result
}

func prepareQwenInstallation(
	config InstallConfig,
	sources *qwenSources,
	rendered *qwenRenderedDocument,
) (*preparedQwenInstallation, error) {
	paths, _, err := preflightQwenInstallation(config, sources)
	if err != nil {
		return nil, err
	}
	if rendered == nil || rendered.document == nil ||
		!sameManagedPath(rendered.document.filePath, sources.user.filePath) {
		return nil, fmt.Errorf("Qwen Code rendered user settings are required")
	}
	guardScript, auditScript := qwenExpectedScripts(config.AgentID, nil)
	changes, err := managedRuntimeFileChanges(
		config, paths, qwenAgentKey, string(guardScript), string(auditScript),
	)
	if err != nil {
		return nil, err
	}
	settingsChange, err := prepareRenderedQwenChange(rendered)
	if err != nil {
		return nil, err
	}
	changes = append(changes, settingsChange)
	changedPath := ""
	if rendered.changed {
		changedPath = rendered.document.filePath
	}
	return &preparedQwenInstallation{
		changes:       changes,
		paths:         paths,
		preconditions: qwenReadOnlyPreconditions(sources, changedPath),
	}, nil
}

func commitQwenInstallation(
	prepared *preparedQwenInstallation,
	rename renameFunc,
) error {
	if prepared == nil || prepared.paths == nil {
		return fmt.Errorf("prepared Qwen Code installation is required")
	}
	if err := EnsurePrivateDirectory(prepared.paths.runtimeRoot); err != nil {
		return err
	}
	if err := EnsurePrivateDirectory(prepared.paths.agentDirectory); err != nil {
		return err
	}
	return writeChanges(
		prepared.changes,
		"Install Qwen Code hooks",
		rename,
		prepared.preconditions...,
	)
}

func prepareQwenUninstall(
	sources *qwenSources,
	rendered *qwenRenderedDocument,
) (*fileChange, []filePrecondition, error) {
	change, err := prepareRenderedQwenChange(rendered)
	if err != nil {
		return nil, nil, err
	}
	changedPath := ""
	if rendered != nil && rendered.changed {
		changedPath = rendered.document.filePath
	}
	return change, qwenReadOnlyPreconditions(sources, changedPath), nil
}

func commitQwenUninstall(
	change *fileChange,
	preconditions []filePrecondition,
	rename renameFunc,
) error {
	return writeChanges(
		[]*fileChange{change},
		"Uninstall Qwen Code hooks",
		rename,
		preconditions...,
	)
}
