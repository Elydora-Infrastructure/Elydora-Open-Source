package plugins

import (
	"fmt"
	"os"
	"path/filepath"
)

type kiroIdePaths struct {
	homeDirectory  string
	workspaceRoot  string
	kiroDirectory  string
	hooksDirectory string
	configPath     string
	legacyPath     string
}

type kiroIdeSources struct {
	paths              *kiroIdePaths
	document           *kiroIdeDocument
	legacy             *kiroIdeLegacyDocument
	initialDirectories []kiroIdeDirectoryIdentity
}

func resolveKiroIdePaths() (*kiroIdePaths, error) {
	workspace, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("resolve Kiro IDE workspace: %w", err)
	}
	workspace, err = filepath.Abs(workspace)
	if err != nil {
		return nil, fmt.Errorf("resolve Kiro IDE workspace path: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}
	kiroDirectory := filepath.Join(workspace, ".kiro")
	hooksDirectory := filepath.Join(kiroDirectory, "hooks")
	return &kiroIdePaths{
		homeDirectory: home, workspaceRoot: workspace,
		kiroDirectory: kiroDirectory, hooksDirectory: hooksDirectory,
		configPath: filepath.Join(hooksDirectory, kiroIdeConfigFile),
		legacyPath: filepath.Join(home, ".kiro", "hooks", kiroIdeLegacyFile),
	}, nil
}

func inspectKiroIdeWorkspace(paths *kiroIdePaths) error {
	exists, err := managedPhysicalDirectoryExists(
		paths.workspaceRoot,
		"Kiro IDE workspace",
	)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("Kiro IDE workspace is missing: %s", paths.workspaceRoot)
	}
	if _, err := managedPhysicalDirectoryExists(
		paths.kiroDirectory,
		"Kiro IDE configuration directory",
	); err != nil {
		return err
	}
	_, err = managedPhysicalDirectoryExists(
		paths.hooksDirectory,
		"Kiro IDE hooks directory",
	)
	return err
}

func inspectKiroIdeLegacyDirectories(paths *kiroIdePaths) error {
	kiroDirectory := filepath.Dir(filepath.Dir(paths.legacyPath))
	hooksDirectory := filepath.Dir(paths.legacyPath)
	for _, item := range []struct{ path, label string }{
		{kiroDirectory, "legacy Kiro IDE configuration directory"},
		{hooksDirectory, "legacy Kiro IDE hooks directory"},
	} {
		exists, err := managedPhysicalDirectoryExists(item.path, item.label)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("%s is missing: %s", item.label, item.path)
		}
	}
	return nil
}

func readKiroIdeSources() (*kiroIdeSources, error) {
	paths, err := resolveKiroIdePaths()
	if err != nil {
		return nil, err
	}
	initialDirectories := make([]kiroIdeDirectoryIdentity, 0, 5)
	for _, item := range []struct{ path, label string }{
		{paths.workspaceRoot, "Kiro IDE workspace"},
		{paths.kiroDirectory, "Kiro IDE configuration directory"},
		{paths.hooksDirectory, "Kiro IDE hooks directory"},
	} {
		identity, err := snapshotKiroIdeDirectory(item.path, item.label)
		if err != nil {
			return nil, err
		}
		initialDirectories = append(initialDirectories, identity)
	}
	if err := inspectKiroIdeWorkspace(paths); err != nil {
		return nil, err
	}
	workspaceSnapshot, err := readManagedFile(
		paths.configPath,
		"Kiro IDE hooks",
		maxManagedSourceBytes,
	)
	if err != nil {
		return nil, err
	}
	document, err := parseKiroIdeDocument(paths.configPath, workspaceSnapshot)
	if err != nil {
		return nil, err
	}
	legacySnapshot, err := readManagedFile(
		paths.legacyPath,
		"legacy Kiro IDE hook",
		maxManagedSourceBytes,
	)
	if err != nil {
		return nil, err
	}
	if legacySnapshot != nil {
		if err := inspectKiroIdeLegacyDirectories(paths); err != nil {
			return nil, err
		}
		for _, item := range []struct{ path, label string }{
			{
				filepath.Dir(filepath.Dir(paths.legacyPath)),
				"legacy Kiro IDE configuration directory",
			},
			{filepath.Dir(paths.legacyPath), "legacy Kiro IDE hooks directory"},
		} {
			identity, err := snapshotKiroIdeDirectory(item.path, item.label)
			if err != nil {
				return nil, err
			}
			initialDirectories = append(initialDirectories, identity)
		}
		current, err := readManagedFile(
			paths.legacyPath,
			"legacy Kiro IDE hook",
			maxManagedSourceBytes,
		)
		if err != nil {
			return nil, err
		}
		if !sameManagedSnapshot(current, legacySnapshot) {
			return nil, fmt.Errorf(
				"legacy Kiro IDE hook changed while reading: %s",
				paths.legacyPath,
			)
		}
	}
	legacy, err := parseKiroIdeLegacyDocument(paths.legacyPath, legacySnapshot)
	if err != nil {
		return nil, err
	}
	if err := assertKiroIdeDirectoryStates(
		initialDirectories,
		"Kiro IDE source read",
	); err != nil {
		return nil, err
	}
	return &kiroIdeSources{
		paths: paths, document: document, legacy: legacy,
		initialDirectories: initialDirectories,
	}, nil
}

func prepareRenderedKiroIdeChange(
	rendered *kiroIdeRenderedDocument,
) (*fileChange, error) {
	if rendered == nil || !rendered.changed {
		return nil, nil
	}
	return prepareSnapshotSourceChange(
		rendered.document.filePath,
		"Kiro IDE hooks",
		rendered.document.snapshot,
		rendered.next,
		0600,
		rendered.remove,
	)
}
