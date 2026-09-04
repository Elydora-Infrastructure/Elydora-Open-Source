package plugins

import (
	"fmt"
	"os"
	"path/filepath"
)

type kiroIdeRuntimePaths struct {
	managedRuntimePaths
	initialDirectories []kiroIdeDirectoryIdentity
}

type kiroIdePreparedTransaction struct {
	operation          string
	paths              *kiroIdePaths
	runtimePaths       *kiroIdeRuntimePaths
	changes            []*fileChange
	preconditions      []filePrecondition
	removeLegacy       bool
	initialDirectories []kiroIdeDirectoryIdentity
}

func preflightKiroIdeInstallation(
	config InstallConfig,
) (*kiroIdeRuntimePaths, string, error) {
	if err := validateManagedInstallConfig(config, kiroIdeAgentKey, "Kiro IDE"); err != nil {
		return nil, "", err
	}
	managed, err := resolveManagedRuntimePaths(config, kiroIdeGuardScript, kiroIdeAuditScript)
	if err != nil {
		return nil, "", err
	}
	paths := &kiroIdeRuntimePaths{managedRuntimePaths: *managed}
	initialDirectories := make([]kiroIdeDirectoryIdentity, 0, 3)
	for _, item := range []struct{ path, label string }{
		{filepath.Dir(paths.runtimeRoot), "home directory"},
		{paths.runtimeRoot, "Elydora runtime directory"},
		{paths.agentDirectory, "Elydora agent runtime directory"},
	} {
		identity, err := snapshotKiroIdeDirectory(item.path, item.label)
		if err != nil {
			return nil, "", err
		}
		initialDirectories = append(initialDirectories, identity)
	}
	if initialDirectories[0].info == nil {
		return nil, "", fmt.Errorf(
			"home directory is missing: %s",
			initialDirectories[0].path,
		)
	}
	if err := validateManagedRuntimeIdentity(
		paths.agentDirectory, config.AgentID, kiroIdeAgentKey, "Kiro IDE",
	); err != nil {
		return nil, "", err
	}
	if err := assertKiroIdeDirectoryStates(
		initialDirectories,
		"Kiro IDE runtime preflight",
	); err != nil {
		return nil, "", err
	}
	paths.initialDirectories = initialDirectories
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		return nil, "", err
	}
	return paths, nodePath, nil
}

func prepareKiroIdeFile(
	filePath string,
	label string,
	contents []byte,
	mode os.FileMode,
	maximumBytes int64,
) (*fileChange, *filePrecondition, error) {
	snapshot, err := readManagedFile(filePath, label, maximumBytes)
	if err != nil {
		return nil, nil, err
	}
	change, err := prepareSnapshotSourceChange(
		filePath,
		label,
		snapshot,
		contents,
		mode,
		false,
	)
	if err != nil {
		return nil, nil, err
	}
	if change != nil {
		return change, nil, nil
	}
	condition := &filePrecondition{
		filePath: filePath, label: label, snapshot: snapshot,
		maximumSize: maximumBytes,
	}
	return nil, condition, nil
}

func appendKiroIdeChange(
	prepared *kiroIdePreparedTransaction,
	change *fileChange,
	condition *filePrecondition,
) {
	if change != nil {
		prepared.changes = append(prepared.changes, change)
	}
	if condition != nil {
		prepared.preconditions = append(prepared.preconditions, *condition)
	}
}

func prepareKiroIdeInstallation(
	config InstallConfig,
	sources *kiroIdeSources,
	paths *kiroIdeRuntimePaths,
	rendered *kiroIdeRenderedDocument,
) (*kiroIdePreparedTransaction, error) {
	if sources == nil || paths == nil || rendered == nil {
		return nil, fmt.Errorf("Kiro IDE installation requires prepared sources")
	}
	runtimeConfig, err := buildManagedRuntimeConfig(config, kiroIdeAgentKey)
	if err != nil {
		return nil, err
	}
	guardScript, auditScript := kiroIdeExpectedScripts(config.AgentID, nil)
	prepared := &kiroIdePreparedTransaction{
		operation: "installation", paths: sources.paths, runtimePaths: paths,
		initialDirectories: append(
			append([]kiroIdeDirectoryIdentity(nil), sources.initialDirectories...),
			paths.initialDirectories...,
		),
	}
	for _, item := range []struct {
		path, label string
		contents    []byte
		mode        os.FileMode
		maximum     int64
	}{
		{paths.guardPath, "Elydora guard runtime", guardScript, 0700, maxManagedSourceBytes},
		{paths.configPath, "Elydora runtime config", runtimeConfig, 0600, maxRuntimeConfigBytes},
		{paths.keyPath, "Elydora private key", []byte(config.PrivateKey), 0600, maxProtectedSecretBytes},
		{paths.auditPath, "Elydora audit runtime", auditScript, 0700, maxManagedSourceBytes},
	} {
		change, condition, err := prepareKiroIdeFile(
			item.path, item.label, item.contents, item.mode, item.maximum,
		)
		if err != nil {
			return nil, err
		}
		appendKiroIdeChange(prepared, change, condition)
	}
	documentChange, err := prepareRenderedKiroIdeChange(rendered)
	if err != nil {
		return nil, err
	}
	if documentChange == nil {
		prepared.preconditions = append(prepared.preconditions, filePrecondition{
			filePath: sources.document.filePath, label: "Kiro IDE hooks",
			snapshot: sources.document.snapshot,
		})
	} else {
		prepared.changes = append(prepared.changes, documentChange)
	}
	removeLegacy := sources.legacy.contract != nil &&
		sameManagedAgentID(sources.legacy.contract.agentID, config.AgentID)
	if removeLegacy {
		legacyChange, err := prepareSnapshotSourceChange(
			sources.legacy.filePath,
			"legacy Kiro IDE hook",
			sources.legacy.snapshot,
			nil,
			0600,
			true,
		)
		if err != nil {
			return nil, err
		}
		if legacyChange == nil {
			return nil, fmt.Errorf(
				"legacy Kiro IDE hook disappeared before migration: %s",
				sources.legacy.filePath,
			)
		}
		prepared.changes = append(prepared.changes, legacyChange)
		prepared.removeLegacy = true
	} else {
		prepared.preconditions = append(prepared.preconditions, filePrecondition{
			filePath: sources.legacy.filePath, label: "legacy Kiro IDE hook",
			snapshot: sources.legacy.snapshot,
		})
	}
	if err := assertKiroIdeDirectoryStates(
		prepared.initialDirectories,
		"Kiro IDE installation preparation",
	); err != nil {
		return nil, err
	}
	return prepared, nil
}

func prepareKiroIdeUninstall(
	sources *kiroIdeSources,
	rendered *kiroIdeRenderedDocument,
	agentID string,
) (*kiroIdePreparedTransaction, error) {
	prepared := &kiroIdePreparedTransaction{
		operation: "uninstall", paths: sources.paths,
		initialDirectories: append(
			[]kiroIdeDirectoryIdentity(nil),
			sources.initialDirectories...,
		),
	}
	documentChange, err := prepareRenderedKiroIdeChange(rendered)
	if err != nil {
		return nil, err
	}
	if documentChange == nil {
		prepared.preconditions = append(prepared.preconditions, filePrecondition{
			filePath: sources.document.filePath, label: "Kiro IDE hooks",
			snapshot: sources.document.snapshot,
		})
	} else {
		prepared.changes = append(prepared.changes, documentChange)
	}
	removeLegacy := sources.legacy.contract != nil &&
		(agentID == "" || sameManagedAgentID(sources.legacy.contract.agentID, agentID))
	if removeLegacy {
		change, err := prepareSnapshotSourceChange(
			sources.legacy.filePath,
			"legacy Kiro IDE hook",
			sources.legacy.snapshot,
			nil,
			0600,
			true,
		)
		if err != nil {
			return nil, err
		}
		if change != nil {
			prepared.changes = append(prepared.changes, change)
			prepared.removeLegacy = true
		}
	} else {
		prepared.preconditions = append(prepared.preconditions, filePrecondition{
			filePath: sources.legacy.filePath, label: "legacy Kiro IDE hook",
			snapshot: sources.legacy.snapshot,
		})
	}
	if err := assertKiroIdeDirectoryStates(
		prepared.initialDirectories,
		"Kiro IDE uninstall preparation",
	); err != nil {
		return nil, err
	}
	return prepared, nil
}

func commitKiroIdeTransaction(
	prepared *kiroIdePreparedTransaction,
	rename renameFunc,
) error {
	if prepared == nil {
		return fmt.Errorf("Kiro IDE transaction is missing")
	}
	operation := "Kiro IDE " + prepared.operation
	if err := assertKiroIdeDirectoryStates(
		prepared.initialDirectories,
		operation,
	); err != nil {
		return err
	}
	workspaceChange := false
	for _, change := range prepared.changes {
		if change != nil && sameManagedPath(change.filePath, prepared.paths.configPath) {
			workspaceChange = true
			break
		}
	}
	directories := make([]kiroIdeDirectoryIdentity, 0, len(prepared.initialDirectories))
	for _, directory := range prepared.initialDirectories {
		if directory.info != nil {
			directories = appendKiroIdeDirectory(directories, directory)
		}
	}
	establish := func(path string, private bool) error {
		if err := assertKiroIdeDirectoryStates(directories, operation); err != nil {
			return err
		}
		initial, err := kiroIdeInitialDirectory(prepared, path)
		if err != nil {
			return err
		}
		current, err := establishKiroIdeDirectory(initial, operation, private)
		if err != nil {
			return err
		}
		directories = appendKiroIdeDirectory(directories, current)
		return assertKiroIdeDirectoryStates(directories, operation)
	}
	if workspaceChange {
		if err := establish(prepared.paths.kiroDirectory, false); err != nil {
			return err
		}
		if err := establish(prepared.paths.hooksDirectory, false); err != nil {
			return err
		}
	}
	if prepared.runtimePaths != nil {
		if err := establish(prepared.runtimePaths.runtimeRoot, true); err != nil {
			return err
		}
		if err := establish(prepared.runtimePaths.agentDirectory, true); err != nil {
			return err
		}
	}
	preconditions := append([]filePrecondition(nil), prepared.preconditions...)
	preconditions = append(preconditions, kiroIdeDirectoryPreconditions(directories)...)
	return writeChangesWithFileOps(
		prepared.changes,
		operation,
		guardedKiroIdeTransactionOps(rename, directories),
		preconditions...,
	)
}

func commitKiroIdeInstallation(
	prepared *kiroIdePreparedTransaction,
	rename renameFunc,
) error {
	return commitKiroIdeTransaction(prepared, rename)
}

func commitKiroIdeUninstall(
	prepared *kiroIdePreparedTransaction,
	rename renameFunc,
) error {
	return commitKiroIdeTransaction(prepared, rename)
}
