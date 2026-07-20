package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type copilotSettingsLayer struct {
	filePath string
	label    string
	jsonc    bool
}

type copilotDirectoryLocation struct {
	path  string
	label string
}

type copilotPaths struct {
	copilotHome        string
	userHooksDirectory string
	userHookPath       string
	legacyHookPath     string
	settingsLayers     []copilotSettingsLayer
	directories        []copilotDirectoryLocation
}

type parsedCopilotSettings struct {
	layer    copilotSettingsLayer
	disabled *bool
	snapshot *managedFileSnapshot
}

func resolveCopilotPaths() (*copilotPaths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}
	override := os.Getenv("COPILOT_HOME")
	copilotHome := override
	if strings.TrimSpace(override) == "" {
		copilotHome = filepath.Join(home, ".copilot")
	} else if !filepath.IsAbs(copilotHome) {
		copilotHome, err = filepath.Abs(copilotHome)
		if err != nil {
			return nil, fmt.Errorf("resolve COPILOT_HOME at %s: %w", override, err)
		}
	}
	project, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("resolve working directory: %w", err)
	}
	github := filepath.Join(project, ".github")
	githubCopilot := filepath.Join(github, "copilot")
	githubHooks := filepath.Join(github, "hooks")
	claude := filepath.Join(project, ".claude")
	userHooks := filepath.Join(copilotHome, "hooks")
	return &copilotPaths{
		copilotHome:        copilotHome,
		userHooksDirectory: userHooks,
		userHookPath:       filepath.Join(userHooks, copilotConfigFile),
		legacyHookPath:     filepath.Join(githubHooks, "hooks.json"),
		settingsLayers: []copilotSettingsLayer{
			{filepath.Join(copilotHome, "config.json"), "legacy Copilot user config", false},
			{filepath.Join(copilotHome, "settings.json"), "Copilot user settings", true},
			{filepath.Join(claude, "settings.json"), "Claude repository settings", true},
			{filepath.Join(claude, "settings.local.json"), "Claude local settings", true},
			{filepath.Join(githubCopilot, "settings.json"), "Copilot repository settings", true},
			{filepath.Join(githubCopilot, "settings.local.json"), "Copilot local settings", true},
		},
		directories: []copilotDirectoryLocation{
			{project, "Copilot working directory"},
			{copilotHome, "COPILOT_HOME"},
			{userHooks, "Copilot user hooks directory"},
			{github, "GitHub configuration directory"},
			{githubHooks, "GitHub repository hooks directory"},
			{githubCopilot, "Copilot repository settings directory"},
			{claude, "Claude repository settings directory"},
		},
	}, nil
}

func inspectCopilotDirectories(locations []copilotDirectoryLocation) error {
	for _, location := range locations {
		if _, err := managedPhysicalDirectoryExists(location.path, location.label); err != nil {
			return err
		}
	}
	return nil
}

func readCopilotDocument(filePath, label string) (*copilotDocument, error) {
	snapshot, err := readManagedFile(filePath, label, maxManagedSourceBytes)
	if err != nil || snapshot == nil {
		return nil, err
	}
	return parseCopilotDocument(filePath, snapshot, label)
}

func parseCopilotSettings(
	snapshot *managedFileSnapshot,
	layer copilotSettingsLayer,
) (map[string]any, error) {
	if len(strings.TrimSpace(string(snapshot.contents))) == 0 {
		return map[string]any{}, nil
	}
	label := fmt.Sprintf("%s at %s", layer.label, layer.filePath)
	if layer.jsonc {
		return decodeJSONCObject(snapshot.contents, label, true)
	}
	return decodeStrictJSONObject(snapshot.contents, label)
}

func readCopilotSettingsLayer(
	layer copilotSettingsLayer,
) (parsedCopilotSettings, error) {
	snapshot, err := readManagedFile(
		layer.filePath,
		layer.label,
		maxManagedSourceBytes,
	)
	if err != nil || snapshot == nil {
		return parsedCopilotSettings{layer: layer}, err
	}
	root, err := parseCopilotSettings(snapshot, layer)
	if err != nil {
		return parsedCopilotSettings{}, err
	}
	result := parsedCopilotSettings{layer: layer, snapshot: snapshot}
	if value, exists := root["disableAllHooks"]; exists {
		disabled, ok := value.(bool)
		if !ok {
			return parsedCopilotSettings{}, fmt.Errorf(
				`%s at %s field "disableAllHooks" must be a boolean`,
				layer.label,
				layer.filePath,
			)
		}
		result.disabled = &disabled
	}
	return result, nil
}

func effectiveCopilotDisabledSource(layers []parsedCopilotSettings) string {
	disabledBy := ""
	for _, layer := range layers {
		if layer.disabled == nil {
			continue
		}
		if *layer.disabled {
			disabledBy = fmt.Sprintf("%s at %s", layer.layer.label, layer.layer.filePath)
		} else {
			disabledBy = ""
		}
	}
	return disabledBy
}

func readCopilotSources() (*copilotSources, *copilotPaths, error) {
	paths, err := resolveCopilotPaths()
	if err != nil {
		return nil, nil, err
	}
	if err := inspectCopilotDirectories(paths.directories); err != nil {
		return nil, nil, err
	}
	user, err := readCopilotDocument(paths.userHookPath, "GitHub Copilot user hooks")
	if err != nil {
		return nil, nil, err
	}
	legacy, err := readCopilotDocument(
		paths.legacyHookPath,
		"GitHub Copilot legacy project hooks",
	)
	if err != nil {
		return nil, nil, err
	}
	settings := make([]parsedCopilotSettings, 0, len(paths.settingsLayers))
	for _, layer := range paths.settingsLayers {
		parsed, readErr := readCopilotSettingsLayer(layer)
		if readErr != nil {
			return nil, nil, readErr
		}
		settings = append(settings, parsed)
	}
	if user == nil {
		user = createCopilotDocument(paths.userHookPath)
	}
	disabledBy := effectiveCopilotDisabledSource(settings)
	if user.hooksDisabled {
		disabledBy = fmt.Sprintf("GitHub Copilot user hooks at %s", paths.userHookPath)
	}
	preconditions := make([]copilotSourcePrecondition, 0, len(settings))
	for _, item := range settings {
		preconditions = append(preconditions, copilotSourcePrecondition{
			filePath: item.layer.filePath,
			label:    item.layer.label,
			snapshot: item.snapshot,
		})
	}
	return &copilotSources{
		user: user, legacy: legacy, disabledBy: disabledBy,
		settingsPreconditions: preconditions,
	}, paths, nil
}

func requireCopilotHooksEnabled(sources *copilotSources) error {
	if sources.disabledBy == "" {
		return nil
	}
	return fmt.Errorf(
		"GitHub Copilot hooks are disabled by %s; set disableAllHooks to false before installation",
		sources.disabledBy,
	)
}

func prepareRenderedCopilotChange(
	rendered *copilotRenderedDocument,
) (*fileChange, error) {
	if rendered == nil || !rendered.changed {
		return nil, nil
	}
	return prepareSnapshotSourceChange(
		rendered.document.filePath,
		"GitHub Copilot hook source",
		rendered.document.snapshot,
		rendered.next,
		0600,
		rendered.remove,
	)
}

func writeCopilotChanges(
	changes []*fileChange,
	label string,
	rename renameFunc,
	runtimeRoot string,
	agentDirectory string,
	preconditions []filePrecondition,
) error {
	hasChanges := false
	for _, change := range changes {
		if change != nil {
			hasChanges = true
			break
		}
	}
	if !hasChanges {
		return writeChanges(changes, label, rename, preconditions...)
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
