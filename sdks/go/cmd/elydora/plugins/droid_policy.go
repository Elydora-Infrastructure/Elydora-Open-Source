package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

type droidPolicyLocation struct {
	filePath string
	label    string
}

type droidPolicyLayer struct {
	filePath              string
	label                 string
	snapshot              *managedFileSnapshot
	hooksDisabled         *bool
	allowManagedHooksOnly *bool
	showHookOutput        *bool
}

type droidPolicyOrigin struct {
	filePath string
	label    string
}

type droidPolicyState struct {
	allowManagedHooksOnlyBy *droidPolicyOrigin
	hooksDisabled           *bool
	hooksDisabledBy         *droidPolicyOrigin
	preconditions           []filePrecondition
}

func defaultDroidManagedSettingsPath() string {
	switch runtime.GOOS {
	case "darwin":
		return "/Library/Application Support/Factory/settings.json"
	case "windows":
		programFiles := os.Getenv("ProgramFiles")
		if programFiles == "" {
			programFiles = `C:\Program Files`
		}
		return filepath.Join(programFiles, "Factory", "settings.json")
	default:
		return "/etc/factory/settings.json"
	}
}

var droidManagedSettingsPath = defaultDroidManagedSettingsPath

func readDroidPolicyLayer(location droidPolicyLocation) (*droidPolicyLayer, error) {
	if _, err := managedPhysicalDirectoryExists(
		filepath.Dir(location.filePath),
		location.label+" directory",
	); err != nil {
		return nil, err
	}
	snapshot, err := readManagedFile(
		location.filePath,
		location.label,
		maxManagedSourceBytes,
	)
	if err != nil {
		return nil, err
	}
	layer := &droidPolicyLayer{
		filePath: location.filePath,
		label:    location.label,
		snapshot: snapshot,
	}
	if snapshot == nil {
		return layer, nil
	}
	root, err := decodeJSONCObject(
		snapshot.contents,
		fmt.Sprintf("%s at %s", location.label, location.filePath),
		true,
	)
	if err != nil {
		return nil, err
	}
	layer.hooksDisabled, err = droidOptionalBoolean(root, "hooksDisabled", location.label)
	if err != nil {
		return nil, err
	}
	layer.allowManagedHooksOnly, err = droidOptionalBoolean(
		root,
		"allowManagedHooksOnly",
		location.label,
	)
	if err != nil {
		return nil, err
	}
	layer.showHookOutput, err = droidOptionalBoolean(root, "showHookOutput", location.label)
	if err != nil {
		return nil, err
	}
	return layer, nil
}

func droidGitRoot(start string) (string, bool, error) {
	current, err := filepath.Abs(start)
	if err != nil {
		return "", false, fmt.Errorf("resolve Factory Droid project directory: %w", err)
	}
	for {
		marker := filepath.Join(current, ".git")
		info, inspectErr := os.Lstat(marker)
		switch {
		case inspectErr == nil:
			if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
				return "", false, fmt.Errorf(
					"Factory Droid project marker is not physical: %s",
					marker,
				)
			}
			return current, true, nil
		case !os.IsNotExist(inspectErr):
			return "", false, fmt.Errorf(
				"inspect Factory Droid project marker at %s: %w",
				marker,
				inspectErr,
			)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false, nil
		}
		current = parent
	}
}

func droidProjectDirectories(root, current string) []string {
	directories := []string{root}
	relative, err := filepath.Rel(root, current)
	if err != nil || relative == "." || relative == "" || filepath.IsAbs(relative) {
		return directories
	}
	if relative == ".." || len(relative) > 3 && relative[:3] == ".."+string(os.PathSeparator) {
		return directories
	}
	directory := root
	for _, segment := range splitDroidPath(relative) {
		directory = filepath.Join(directory, segment)
		directories = append(directories, directory)
	}
	return directories
}

func splitDroidPath(value string) []string {
	parts := make([]string, 0)
	for value != "." && value != "" {
		directory, file := filepath.Split(value)
		if file != "" {
			parts = append([]string{file}, parts...)
		}
		value = filepath.Clean(directory)
		if value == string(os.PathSeparator) {
			break
		}
	}
	return parts
}

func droidProjectPolicyLocations() ([][2]droidPolicyLocation, error) {
	current, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("resolve Factory Droid working directory: %w", err)
	}
	current, err = filepath.Abs(current)
	if err != nil {
		return nil, fmt.Errorf("resolve Factory Droid working directory: %w", err)
	}
	root, found, err := droidGitRoot(current)
	if err != nil {
		return nil, err
	}
	if !found {
		root = current
	}
	directories := droidProjectDirectories(root, current)
	locations := make([][2]droidPolicyLocation, 0, len(directories))
	for index, directory := range directories {
		scope := "project"
		if index > 0 {
			scope = "folder " + directory
		}
		factory := filepath.Join(directory, ".factory")
		locations = append(locations, [2]droidPolicyLocation{
			{filepath.Join(factory, "settings.json"), "Factory Droid " + scope + " settings"},
			{filepath.Join(factory, "settings.local.json"), "Factory Droid " + scope + " local settings"},
		})
	}
	return locations, nil
}

func droidScopeHooksDisabled(
	settings, local *droidPolicyLayer,
) *droidPolicyLayer {
	if local.hooksDisabled != nil {
		return local
	}
	if settings.hooksDisabled != nil {
		return settings
	}
	return nil
}

func readDroidPolicy() (*droidPolicyState, error) {
	managed, err := readDroidPolicyLayer(droidPolicyLocation{
		droidManagedSettingsPath(),
		"Factory Droid system-managed settings",
	})
	if err != nil {
		return nil, err
	}
	locations, err := droidProjectPolicyLocations()
	if err != nil {
		return nil, err
	}
	projectLayers := make([][2]*droidPolicyLayer, 0, len(locations))
	for _, scope := range locations {
		settings, readErr := readDroidPolicyLayer(scope[0])
		if readErr != nil {
			return nil, readErr
		}
		local, readErr := readDroidPolicyLayer(scope[1])
		if readErr != nil {
			return nil, readErr
		}
		projectLayers = append(projectLayers, [2]*droidPolicyLayer{settings, local})
	}
	state := &droidPolicyState{}
	if managed.allowManagedHooksOnly != nil && *managed.allowManagedHooksOnly {
		state.allowManagedHooksOnlyBy = &droidPolicyOrigin{managed.filePath, managed.label}
	}
	selected := managed
	if selected.hooksDisabled == nil {
		selected = nil
		for _, scope := range projectLayers {
			if layer := droidScopeHooksDisabled(scope[0], scope[1]); layer != nil {
				selected = layer
				break
			}
		}
	}
	if selected != nil {
		state.hooksDisabled = selected.hooksDisabled
		state.hooksDisabledBy = &droidPolicyOrigin{selected.filePath, selected.label}
	}
	layers := []*droidPolicyLayer{managed}
	for _, scope := range projectLayers {
		layers = append(layers, scope[0], scope[1])
	}
	state.preconditions = make([]filePrecondition, 0, len(layers))
	for _, layer := range layers {
		state.preconditions = append(state.preconditions, filePrecondition{
			filePath:    layer.filePath,
			label:       layer.label,
			snapshot:    layer.snapshot,
			maximumSize: maxManagedSourceBytes,
		})
	}
	return state, nil
}
