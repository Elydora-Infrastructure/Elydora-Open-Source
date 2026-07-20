package plugins

import "fmt"

func validateClineHookDirectories(paths clineHookPaths) error {
	if _, err := managedPhysicalDirectoryExists(
		paths.clineDirectory,
		"Cline configuration directory",
	); err != nil {
		return err
	}
	_, err := managedPhysicalDirectoryExists(
		paths.hooksDirectory,
		"Cline hooks directory",
	)
	return err
}

func readClineHookFile(filePath string) (clineHookFile, error) {
	snapshot, err := readManagedFile(filePath, "Cline hook", maxManagedSourceBytes)
	if err != nil {
		return clineHookFile{}, err
	}
	if snapshot == nil {
		return clineHookFile{filePath: filePath}, nil
	}
	source := string(snapshot.contents)
	metadata, err := parseClineMetadata(filePath, source)
	if err != nil {
		return clineHookFile{}, err
	}
	return clineHookFile{
		exists: true, filePath: filePath, source: source, metadata: metadata,
	}, nil
}

func readClineHookPair() (
	clineHookPaths,
	clineHookFile,
	clineHookFile,
	error,
) {
	paths, err := resolveClineHookFiles()
	if err != nil {
		return clineHookPaths{}, clineHookFile{}, clineHookFile{}, err
	}
	if err := validateClineHookDirectories(paths); err != nil {
		return paths, clineHookFile{}, clineHookFile{}, err
	}
	guard, err := readClineHookFile(paths.guardPath)
	if err != nil {
		return paths, clineHookFile{}, clineHookFile{}, err
	}
	audit, err := readClineHookFile(paths.auditPath)
	if err != nil {
		return paths, clineHookFile{}, clineHookFile{}, err
	}
	return paths, guard, audit, nil
}

func requireAvailableClineHook(file clineHookFile) error {
	if file.exists && file.metadata == nil {
		return fmt.Errorf(
			"Cline hook at %s already exists and is owned by another integration",
			file.filePath,
		)
	}
	return nil
}

func writeClineHookChanges(
	changes []*fileChange,
	label string,
	rename renameFunc,
	paths clineHookPaths,
) error {
	if !hasClineChanges(changes) {
		return nil
	}
	if err := ensureManagedDirectory(
		paths.clineDirectory,
		"Cline configuration directory",
	); err != nil {
		return err
	}
	if err := ensureManagedDirectory(paths.hooksDirectory, "Cline hooks directory"); err != nil {
		return err
	}
	return writeChanges(changes, label, rename)
}

func writeClineChanges(
	changes []*fileChange,
	label string,
	rename renameFunc,
	paths *clineRuntimePaths,
) error {
	if !hasClineChanges(changes) {
		return nil
	}
	if err := EnsurePrivateDirectory(paths.runtimeRoot); err != nil {
		return err
	}
	if err := EnsurePrivateDirectory(paths.agentDirectory); err != nil {
		return err
	}
	return writeClineHookChanges(changes, label, rename, paths.hooks)
}

func hasClineChanges(changes []*fileChange) bool {
	for _, change := range changes {
		if change != nil {
			return true
		}
	}
	return false
}

func prepareClineUninstallChanges(
	files []clineHookFile,
	agentID string,
) ([]*fileChange, error) {
	changes := make([]*fileChange, 0, len(files))
	for _, file := range files {
		if file.metadata == nil ||
			(agentID != "" && !sameClineAgentID(file.metadata.AgentID, agentID)) {
			continue
		}
		if err := assertClineWrapperIntegrity(file); err != nil {
			return nil, err
		}
		change, err := prepareSourceChange(
			file.filePath,
			fmt.Sprintf("Cline %s hook", file.metadata.Kind),
			[]byte(file.source),
			file.exists,
			nil,
			0700,
			true,
		)
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}
	return changes, nil
}
