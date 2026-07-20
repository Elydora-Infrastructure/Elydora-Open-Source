package plugins

import (
	"fmt"
	"os"
	"path/filepath"
)

type droidFactoryConfigPaths struct {
	directory       string
	root            string
	legacyDirectory string
	legacy          string
	settings        string
	localSettings   string
}

func droidFactoryPaths() (*droidFactoryConfigPaths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}
	directory := filepath.Join(home, ".factory")
	legacyDirectory := filepath.Join(directory, "hooks")
	return &droidFactoryConfigPaths{
		directory:       directory,
		root:            filepath.Join(directory, "hooks.json"),
		legacyDirectory: legacyDirectory,
		legacy:          filepath.Join(legacyDirectory, "hooks.json"),
		settings:        filepath.Join(directory, "settings.json"),
		localSettings:   filepath.Join(directory, "settings.local.json"),
	}, nil
}

func readDroidDocument(
	filePath, kind, label string,
) (*droidDocument, error) {
	snapshot, err := readManagedFile(filePath, label, maxManagedSourceBytes)
	if err != nil || snapshot == nil {
		return nil, err
	}
	return parseDroidDocument(
		true,
		filePath,
		kind,
		snapshot.contents,
		snapshot,
	)
}

func readDroidSources() (*droidSources, error) {
	paths, err := droidFactoryPaths()
	if err != nil {
		return nil, err
	}
	if _, err := managedPhysicalDirectoryExists(
		paths.directory,
		"Factory Droid user configuration directory",
	); err != nil {
		return nil, err
	}
	if _, err := managedPhysicalDirectoryExists(
		paths.legacyDirectory,
		"Factory Droid legacy hooks directory",
	); err != nil {
		return nil, err
	}
	root, err := readDroidDocument(paths.root, "hooks", "Factory Droid user hooks")
	if err != nil {
		return nil, err
	}
	if root == nil {
		root, err = createOwnedDroidDocument(paths.root)
		if err != nil {
			return nil, err
		}
	}
	legacy, err := readDroidDocument(paths.legacy, "legacy", "Factory Droid legacy hooks")
	if err != nil {
		return nil, err
	}
	if legacy == nil {
		legacy, err = createDroidDocument(paths.legacy, "legacy", []byte("{}\n"))
		if err != nil {
			return nil, err
		}
	}
	settings, err := readDroidDocument(paths.settings, "settings", "Factory Droid user settings")
	if err != nil {
		return nil, err
	}
	if settings == nil {
		settings, err = createDroidDocument(paths.settings, "settings", []byte("{}\n"))
		if err != nil {
			return nil, err
		}
	}
	localSettings, err := readDroidDocument(
		paths.localSettings,
		"local-settings",
		"Factory Droid local settings",
	)
	if err != nil {
		return nil, err
	}
	if localSettings == nil {
		localSettings, err = createDroidDocument(
			paths.localSettings,
			"local-settings",
			[]byte("{}\n"),
		)
		if err != nil {
			return nil, err
		}
	}
	policy, err := readDroidPolicy()
	if err != nil {
		return nil, err
	}
	return &droidSources{
		root:          root,
		legacy:        legacy,
		settings:      settings,
		localSettings: localSettings,
		policy:        policy,
	}, nil
}

func requireDroidHooksEnabled(sources *droidSources) error {
	if blocked := droidHookBlocked(sources); blocked != nil {
		return fmt.Errorf(
			"Factory Droid user hooks are disabled by %s in %s at %s",
			blocked.field,
			blocked.label,
			blocked.filePath,
		)
	}
	return nil
}
