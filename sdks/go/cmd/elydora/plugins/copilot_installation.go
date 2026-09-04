package plugins

import (
	"fmt"
)

type preparedCopilotInstallation struct {
	changes       []*fileChange
	preconditions []filePrecondition
	paths         *managedRuntimePaths
}

func preflightCopilotInstallation(
	config InstallConfig,
	sources *copilotSources,
) (*managedRuntimePaths, string, error) {
	if sources == nil || sources.user == nil || sources.user.filePath == "" {
		return nil, "", fmt.Errorf(
			"GitHub Copilot CLI installation requires a user hook path",
		)
	}
	if err := requireCopilotHooksEnabled(sources); err != nil {
		return nil, "", err
	}
	if err := validateManagedInstallConfig(config, copilotAgentKey, "GitHub Copilot CLI"); err != nil {
		return nil, "", err
	}
	paths, err := resolveManagedRuntimePaths(config, copilotGuardScript, copilotAuditScript)
	if err != nil {
		return nil, "", err
	}
	if err := validateManagedRuntimeIdentity(
		paths.agentDirectory, config.AgentID, copilotAgentKey, "Copilot",
	); err != nil {
		return nil, "", err
	}
	nodePath, err := resolveAbsoluteNodeRuntime("GitHub Copilot CLI")
	if err != nil {
		return nil, "", err
	}
	hookSources := []copilotHooks{sources.user.hooks}
	if sources.legacy != nil {
		hookSources = append(hookSources, sources.legacy.hooks)
	}
	if err := validateCopilotJavaScriptMatchers(hookSources, nodePath); err != nil {
		return nil, "", err
	}
	return paths, nodePath, nil
}

func validateCopilotRenderedSet(
	sources *copilotSources,
	rendered []*copilotRenderedDocument,
) error {
	expected := []*copilotDocument{sources.user}
	if sources.legacy != nil {
		expected = append(expected, sources.legacy)
	}
	if len(rendered) != len(expected) {
		return fmt.Errorf("GitHub Copilot rendered source set is incomplete")
	}
	for _, document := range expected {
		matches := 0
		for _, item := range rendered {
			if item != nil && item.document != nil &&
				sameManagedPath(item.document.filePath, document.filePath) {
				matches++
			}
		}
		if matches != 1 {
			return fmt.Errorf(
				"GitHub Copilot rendered source set contains unexpected paths",
			)
		}
	}
	return nil
}

func copilotInstallationPreconditions(
	sources *copilotSources,
	rendered []*copilotRenderedDocument,
) []filePrecondition {
	result := make([]filePrecondition, 0, len(sources.settingsPreconditions)+len(rendered))
	for _, item := range sources.settingsPreconditions {
		result = append(result, filePrecondition{
			filePath: item.filePath, label: item.label,
			snapshot: item.snapshot, maximumSize: maxManagedSourceBytes,
		})
	}
	for _, item := range rendered {
		if item != nil && !item.changed {
			result = append(result, filePrecondition{
				filePath:    item.document.filePath,
				label:       "GitHub Copilot hook source",
				snapshot:    item.document.snapshot,
				maximumSize: maxManagedSourceBytes,
			})
		}
	}
	return result
}

func prepareCopilotInstallation(
	config InstallConfig,
	sources *copilotSources,
	rendered []*copilotRenderedDocument,
) (*preparedCopilotInstallation, error) {
	paths, _, err := preflightCopilotInstallation(config, sources)
	if err != nil {
		return nil, err
	}
	if err := validateCopilotRenderedSet(sources, rendered); err != nil {
		return nil, err
	}
	guardScript, auditScript := copilotExpectedScripts(config.AgentID, nil)
	changes, err := managedRuntimeFileChanges(
		config, paths, copilotAgentKey, string(guardScript), string(auditScript),
	)
	if err != nil {
		return nil, err
	}
	for _, document := range rendered {
		change, changeErr := prepareRenderedCopilotChange(document)
		if changeErr != nil {
			return nil, changeErr
		}
		changes = append(changes, change)
	}
	return &preparedCopilotInstallation{
		changes: changes, paths: paths,
		preconditions: copilotInstallationPreconditions(sources, rendered),
	}, nil
}

func commitCopilotInstallation(
	prepared *preparedCopilotInstallation,
	rename renameFunc,
) error {
	if prepared == nil || prepared.paths == nil {
		return fmt.Errorf("prepared GitHub Copilot installation is required")
	}
	return writeCopilotChanges(
		prepared.changes,
		"Install GitHub Copilot hooks",
		rename,
		prepared.paths.runtimeRoot,
		prepared.paths.agentDirectory,
		prepared.preconditions,
	)
}

func copilotExpectedScripts(agentID string, _ map[string]any) ([]byte, []byte) {
	return []byte(generateGuardScript(copilotAgentKey, agentID, "", false, "")),
		[]byte(buildHookScriptWithOutput(copilotAgentKey, agentID, "", false, true))
}
