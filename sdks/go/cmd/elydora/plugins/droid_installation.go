package plugins

import (
	"fmt"
)

type preparedDroidInstallation struct {
	changes       []*fileChange
	preconditions []filePrecondition
	paths         *managedRuntimePaths
}

type preparedDroidUninstall struct {
	changes       []*fileChange
	preconditions []filePrecondition
}

func droidSourceLabel(document *droidDocument) string {
	switch document.kind {
	case "settings":
		return "Factory Droid user settings"
	case "local-settings":
		return "Factory Droid local settings"
	case "legacy":
		return "Factory Droid legacy hooks"
	default:
		return "Factory Droid user hooks"
	}
}

func preflightDroidInstallation(
	config InstallConfig,
	sources *droidSources,
) (*managedRuntimePaths, string, error) {
	if sources == nil || sources.root == nil || sources.policy == nil {
		return nil, "", fmt.Errorf("Factory Droid installation sources are required")
	}
	if err := requireDroidHooksEnabled(sources); err != nil {
		return nil, "", err
	}
	hooks := make([]droidHookSettings, 0, len(droidSourceDocuments(sources)))
	for _, document := range droidSourceDocuments(sources) {
		hooks = append(hooks, document.hooks)
	}
	if err := validateDroidRegexes(hooks...); err != nil {
		return nil, "", err
	}
	if err := validateManagedInstallConfig(config, droidAgentKey, "Factory Droid"); err != nil {
		return nil, "", err
	}
	paths, err := resolveManagedRuntimePaths(config, droidGuardScript, droidAuditScript)
	if err != nil {
		return nil, "", err
	}
	if err := validateManagedRuntimeIdentity(
		paths.agentDirectory, config.AgentID, droidAgentKey, "Factory Droid",
	); err != nil {
		return nil, "", err
	}
	nodePath, err := resolveAbsoluteNodeRuntime("Factory Droid")
	if err != nil {
		return nil, "", err
	}
	return paths, nodePath, nil
}

func prepareRenderedDroidChange(
	rendered *droidRenderedDocument,
) (*fileChange, error) {
	if rendered == nil || !rendered.changed {
		return nil, nil
	}
	return prepareSnapshotSourceChange(
		rendered.document.filePath,
		droidSourceLabel(rendered.document),
		rendered.document.snapshot,
		rendered.next,
		0600,
		rendered.remove,
	)
}

func validateDroidRenderedSet(
	sources *droidSources,
	rendered []*droidRenderedDocument,
) error {
	expected := droidInstallationDocuments(sources)
	if len(rendered) != len(expected) {
		return fmt.Errorf("Factory Droid rendered source set is incomplete")
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
			return fmt.Errorf("Factory Droid rendered source set contains unexpected paths")
		}
	}
	return nil
}

func droidSourcePreconditions(
	sources *droidSources,
	changedPaths []string,
) []filePrecondition {
	preconditions := make([]filePrecondition, 0, len(droidSourceDocuments(sources)))
	for _, document := range droidSourceDocuments(sources) {
		changed := false
		for _, path := range changedPaths {
			if sameManagedPath(document.filePath, path) {
				changed = true
				break
			}
		}
		if !changed {
			preconditions = append(preconditions, filePrecondition{
				filePath:    document.filePath,
				label:       droidSourceLabel(document),
				snapshot:    document.snapshot,
				maximumSize: maxManagedSourceBytes,
			})
		}
	}
	return preconditions
}

func prepareDroidInstallation(
	config InstallConfig,
	sources *droidSources,
	rendered []*droidRenderedDocument,
) (*preparedDroidInstallation, error) {
	paths, _, err := preflightDroidInstallation(config, sources)
	if err != nil {
		return nil, err
	}
	if err := validateDroidRenderedSet(sources, rendered); err != nil {
		return nil, err
	}
	guardScript, auditScript := droidExpectedScripts(config.AgentID, nil)
	changes, err := managedRuntimeFileChanges(
		config, paths, droidAgentKey, string(guardScript), string(auditScript),
	)
	if err != nil {
		return nil, err
	}
	changedPaths := make([]string, 0, len(rendered))
	for _, document := range rendered {
		change, changeErr := prepareRenderedDroidChange(document)
		if changeErr != nil {
			return nil, changeErr
		}
		changes = append(changes, change)
		if change != nil {
			changedPaths = append(changedPaths, change.filePath)
		}
	}
	preconditions := droidSourcePreconditions(sources, changedPaths)
	preconditions = append(preconditions, sources.policy.preconditions...)
	return &preparedDroidInstallation{
		changes:       changes,
		preconditions: preconditions,
		paths:         paths,
	}, nil
}

func writeDroidChanges(
	changes []*fileChange,
	label string,
	rename renameFunc,
	runtimeRoot, agentDirectory string,
	preconditions []filePrecondition,
) error {
	if !hasFileChanges(changes) {
		return writeChanges(changes, label, rename, preconditions...)
	}
	if err := assertFilePreconditions(preconditions, label); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	if agentDirectory != "" {
		if err := EnsurePrivateDirectory(runtimeRoot); err != nil {
			return err
		}
		if err := EnsurePrivateDirectory(agentDirectory); err != nil {
			return err
		}
	}
	return writeChanges(changes, label, rename, preconditions...)
}

func commitDroidInstallation(
	prepared *preparedDroidInstallation,
	rename renameFunc,
) error {
	if prepared == nil || prepared.paths == nil {
		return fmt.Errorf("prepared Factory Droid installation is required")
	}
	return writeDroidChanges(
		prepared.changes,
		"Install Factory Droid hooks",
		rename,
		prepared.paths.runtimeRoot,
		prepared.paths.agentDirectory,
		prepared.preconditions,
	)
}

func prepareDroidUninstall(
	rendered []*droidRenderedDocument,
) (*preparedDroidUninstall, error) {
	changes := make([]*fileChange, 0, len(rendered))
	changedPaths := make([]string, 0, len(rendered))
	for _, item := range rendered {
		change, err := prepareRenderedDroidChange(item)
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
		if change != nil {
			changedPaths = append(changedPaths, change.filePath)
		}
	}
	preconditions := make([]filePrecondition, 0, len(rendered))
	for _, item := range rendered {
		changed := false
		for _, path := range changedPaths {
			if sameManagedPath(item.document.filePath, path) {
				changed = true
				break
			}
		}
		if !changed {
			preconditions = append(preconditions, filePrecondition{
				filePath:    item.document.filePath,
				label:       droidSourceLabel(item.document),
				snapshot:    item.document.snapshot,
				maximumSize: maxManagedSourceBytes,
			})
		}
	}
	return &preparedDroidUninstall{changes, preconditions}, nil
}

func commitDroidUninstall(
	prepared *preparedDroidUninstall,
	rename renameFunc,
) error {
	if prepared == nil {
		return fmt.Errorf("prepared Factory Droid uninstall is required")
	}
	return writeDroidChanges(
		prepared.changes,
		"Uninstall Factory Droid hooks",
		rename,
		"",
		"",
		prepared.preconditions,
	)
}

func droidExpectedScripts(agentID string, _ map[string]any) ([]byte, []byte) {
	return []byte(generateGuardScript(droidAgentKey, agentID, "", false, "")),
		[]byte(buildHookScriptWithOutput(droidAgentKey, agentID, "", false, true))
}
