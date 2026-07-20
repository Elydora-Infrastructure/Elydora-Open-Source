package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var qwenHomeEnvironmentKeys = [...]string{"QWEN_HOME", "QWEN_RUNTIME_DIR"}

type qwenDisableControl struct {
	disabled bool
	source   *qwenDocument
}

type qwenSources struct {
	qwenHome         string
	systemDefaults   *qwenDocument
	user             *qwenDocument
	workspace        *qwenDocument
	system           *qwenDocument
	workspaceActive  bool
	workspaceTrusted bool
	disableControl   qwenDisableControl
	preconditions    []filePrecondition
}

type qwenRoutingResult struct {
	qwenHome      string
	preconditions []filePrecondition
}

func defaultQwenHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".qwen"), nil
}

func resolveQwenStoragePath(value string) (string, error) {
	resolved := value
	if value == "~" || len(value) >= 2 && value[0] == '~' &&
		(value[1] == '/' || value[1] == '\\') {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		resolved = home
		if value != "~" {
			for _, segment := range qwenPathSeparatorPattern.Split(value[2:], -1) {
				if segment != "" {
					resolved = filepath.Join(resolved, segment)
				}
			}
		}
	}
	if filepath.IsAbs(resolved) {
		return filepath.Clean(resolved), nil
	}
	absolute, err := filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("resolve Qwen Code storage path %q: %w", value, err)
	}
	return absolute, nil
}

func qwenComparisonPath(filePath string) string {
	resolved, err := filepath.Abs(filePath)
	if err != nil {
		resolved = filepath.Clean(filePath)
	}
	if runtime.GOOS == "windows" {
		return strings.ToLower(resolved)
	}
	return resolved
}

func resolveQwenRouting() (*qwenRoutingResult, error) {
	values := map[string]string{}
	owned := map[string]bool{}
	for _, key := range qwenHomeEnvironmentKeys {
		value, present := os.LookupEnv(key)
		values[key] = value
		owned[key] = present
	}
	if values["QWEN_HOME"] != "" && values["QWEN_RUNTIME_DIR"] != "" {
		home, err := resolveQwenStoragePath(values["QWEN_HOME"])
		return &qwenRoutingResult{qwenHome: home}, err
	}
	defaultHome, err := defaultQwenHome()
	if err != nil {
		return nil, err
	}
	initialHome := values["QWEN_HOME"]
	initialDirectory := defaultHome
	if initialHome != "" {
		initialDirectory, err = resolveQwenStoragePath(initialHome)
		if err != nil {
			return nil, err
		}
	}
	candidates := []string{filepath.Join(initialDirectory, ".env")}
	if initialHome == "" {
		candidates = append(candidates, filepath.Join(filepath.Dir(initialDirectory), ".env"))
	}
	preconditions := make([]filePrecondition, 0, 3)
	visited := map[string]bool{}
	readCandidate := func(filePath string) error {
		resolved, resolveErr := filepath.Abs(filePath)
		if resolveErr != nil {
			return fmt.Errorf("resolve Qwen Code home environment path: %w", resolveErr)
		}
		key := qwenComparisonPath(resolved)
		if visited[key] {
			return nil
		}
		visited[key] = true
		snapshot, readErr := readManagedFile(
			resolved,
			"Qwen Code home environment",
			maxManagedSourceBytes,
		)
		if readErr != nil {
			return readErr
		}
		preconditions = append(preconditions, filePrecondition{
			filePath: resolved, label: "Qwen Code home environment",
			snapshot: snapshot, maximumSize: maxManagedSourceBytes,
		})
		if snapshot == nil {
			return nil
		}
		parsed := parseDotenv(snapshot.contents)
		for _, envKey := range qwenHomeEnvironmentKeys {
			value := parsed[envKey]
			if value != "" && !owned[envKey] {
				values[envKey] = value
				owned[envKey] = true
			}
		}
		return nil
	}
	for _, candidate := range candidates {
		if err := readCandidate(candidate); err != nil {
			return nil, err
		}
	}
	discoveredHome := values["QWEN_HOME"]
	if discoveredHome != "" && discoveredHome != initialHome {
		discoveredDirectory, resolveErr := resolveQwenStoragePath(discoveredHome)
		if resolveErr != nil {
			return nil, resolveErr
		}
		if !sameQwenPath(discoveredDirectory, initialDirectory) {
			if err := readCandidate(filepath.Join(discoveredDirectory, ".env")); err != nil {
				return nil, err
			}
		}
	}
	resolvedHome := defaultHome
	if values["QWEN_HOME"] != "" {
		resolvedHome, err = resolveQwenStoragePath(values["QWEN_HOME"])
		if err != nil {
			return nil, err
		}
	}
	return &qwenRoutingResult{
		qwenHome:      resolvedHome,
		preconditions: preconditions,
	}, nil
}

func resolveQwenHome() (string, error) {
	routing, err := resolveQwenRouting()
	if err != nil {
		return "", err
	}
	return routing.qwenHome, nil
}

func qwenSystemSettingsPath() (string, error) {
	if configured := os.Getenv("QWEN_CODE_SYSTEM_SETTINGS_PATH"); configured != "" {
		return filepath.Abs(configured)
	}
	switch runtime.GOOS {
	case "darwin":
		return "/Library/Application Support/QwenCode/settings.json", nil
	case "windows":
		return `C:\ProgramData\qwen-code\settings.json`, nil
	default:
		return "/etc/qwen-code/settings.json", nil
	}
}

func qwenSystemDefaultsPath(systemPath string) (string, error) {
	if configured := os.Getenv("QWEN_CODE_SYSTEM_DEFAULTS_PATH"); configured != "" {
		return filepath.Abs(configured)
	}
	return filepath.Join(filepath.Dir(systemPath), "system-defaults.json"), nil
}

func readQwenDocument(kind, filePath string) (*qwenDocument, error) {
	snapshot, err := readManagedFile(filePath, qwenSourceLabel(kind), maxManagedSourceBytes)
	if err != nil {
		return nil, err
	}
	if snapshot == nil {
		return createQwenDocument(kind, filePath)
	}
	return parseQwenDocument(kind, true, filePath, snapshot.contents, snapshot)
}

func canonicalQwenPath(filePath string) string {
	canonical, err := filepath.EvalSymlinks(filePath)
	if err == nil {
		return canonical
	}
	absolute, absErr := filepath.Abs(filePath)
	if absErr == nil {
		return absolute
	}
	return filepath.Clean(filePath)
}

func qwenPathWithin(child, parent string) bool {
	relative, err := filepath.Rel(qwenComparisonPath(parent), qwenComparisonPath(child))
	if err != nil {
		return false
	}
	return relative == "." ||
		(relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) &&
			!filepath.IsAbs(relative))
}

func qwenWorkspaceTrust(
	system, user *qwenDocument,
	qwenHome, workspacePath string,
) (bool, *filePrecondition, error) {
	var enabled *bool
	if user != nil && user.folderTrustEnabled != nil {
		enabled = user.folderTrustEnabled
	} else if system != nil {
		enabled = system.folderTrustEnabled
	}
	if enabled == nil || !*enabled {
		return true, nil, nil
	}
	filePath := os.Getenv("QWEN_CODE_TRUSTED_FOLDERS_PATH")
	var err error
	if filePath != "" {
		filePath, err = filepath.Abs(filePath)
		if err != nil {
			return false, nil, fmt.Errorf("resolve Qwen Code trusted folders path: %w", err)
		}
	} else {
		filePath = filepath.Join(qwenHome, "trustedFolders.json")
	}
	snapshot, err := readManagedFile(
		filePath,
		"Qwen Code trusted folders",
		maxManagedSourceBytes,
	)
	if err != nil {
		return false, nil, err
	}
	precondition := &filePrecondition{
		filePath: filePath, label: "Qwen Code trusted folders",
		snapshot: snapshot, maximumSize: maxManagedSourceBytes,
	}
	if snapshot == nil {
		return true, precondition, nil
	}
	rules, err := decodeJSONCObject(
		snapshot.contents,
		fmt.Sprintf("Qwen Code trusted folders at %s", filePath),
		false,
	)
	if err != nil {
		return false, nil, err
	}
	for rulePath, level := range rules {
		if level != "TRUST_FOLDER" && level != "TRUST_PARENT" && level != "DO_NOT_TRUST" {
			return false, nil, fmt.Errorf(
				"Qwen Code trusted folders has invalid trust level for %q",
				rulePath,
			)
		}
	}
	workspace := canonicalQwenPath(workspacePath)
	for rulePath, level := range rules {
		canonicalRule := canonicalQwenPath(rulePath)
		trustRoot := canonicalRule
		if level == "TRUST_PARENT" {
			trustRoot = filepath.Dir(canonicalRule)
		}
		if (level == "TRUST_FOLDER" || level == "TRUST_PARENT") &&
			qwenPathWithin(workspace, trustRoot) {
			return true, precondition, nil
		}
	}
	for rulePath, level := range rules {
		if level == "DO_NOT_TRUST" &&
			qwenComparisonPath(workspace) == qwenComparisonPath(canonicalQwenPath(rulePath)) {
			return false, precondition, nil
		}
	}
	return true, precondition, nil
}

func effectiveQwenDisable(
	systemDefaults, user, workspace, system *qwenDocument,
	useWorkspace bool,
) qwenDisableControl {
	control := qwenDisableControl{}
	documents := []*qwenDocument{systemDefaults, user}
	if useWorkspace {
		documents = append(documents, workspace)
	}
	documents = append(documents, system)
	for _, document := range documents {
		if document == nil || document.disableAllHooks == nil {
			continue
		}
		control.disabled = *document.disableAllHooks
		control.source = document
	}
	return control
}

func qwenSourcePrecondition(document *qwenDocument) filePrecondition {
	return filePrecondition{
		filePath:    document.filePath,
		label:       qwenDocumentLabel(document),
		snapshot:    document.snapshot,
		maximumSize: maxManagedSourceBytes,
	}
}

func deduplicateQwenPreconditions(values []filePrecondition) []filePrecondition {
	result := make([]filePrecondition, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		key := qwenComparisonPath(value.filePath)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, value)
	}
	return result
}

func readQwenSources() (*qwenSources, error) {
	routing, err := resolveQwenRouting()
	if err != nil {
		return nil, err
	}
	systemPath, err := qwenSystemSettingsPath()
	if err != nil {
		return nil, err
	}
	defaultsPath, err := qwenSystemDefaultsPath(systemPath)
	if err != nil {
		return nil, err
	}
	workspaceDirectory, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("resolve Qwen Code workspace: %w", err)
	}
	workspacePath := filepath.Join(workspaceDirectory, ".qwen", "settings.json")
	system, err := readQwenDocument(qwenSystemKind, systemPath)
	if err != nil {
		return nil, err
	}
	systemDefaults, err := readQwenDocument(qwenSystemDefaultsKind, defaultsPath)
	if err != nil {
		return nil, err
	}
	user, err := readQwenDocument(
		qwenUserKind,
		filepath.Join(routing.qwenHome, "settings.json"),
	)
	if err != nil {
		return nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}
	canonicalWorkspace := canonicalQwenPath(workspaceDirectory)
	workspaceActive := qwenComparisonPath(canonicalWorkspace) !=
		qwenComparisonPath(canonicalQwenPath(home))
	workspace, err := createQwenDocument(qwenWorkspaceKind, workspacePath)
	if err != nil {
		return nil, err
	}
	if workspaceActive {
		workspace, err = readQwenDocument(qwenWorkspaceKind, workspacePath)
		if err != nil {
			return nil, err
		}
	}
	workspaceTrusted := false
	var trustPrecondition *filePrecondition
	if workspaceActive {
		workspaceTrusted, trustPrecondition, err = qwenWorkspaceTrust(
			system,
			user,
			routing.qwenHome,
			canonicalWorkspace,
		)
		if err != nil {
			return nil, err
		}
	}
	if err := validateQwenJavaScriptMatchers([]qwenHookSettings{
		systemDefaults.hooks,
		user.hooks,
		workspace.hooks,
		system.hooks,
	}); err != nil {
		return nil, err
	}
	preconditions := append([]filePrecondition(nil), routing.preconditions...)
	preconditions = append(
		preconditions,
		qwenSourcePrecondition(systemDefaults),
		qwenSourcePrecondition(user),
	)
	if workspaceActive {
		preconditions = append(preconditions, qwenSourcePrecondition(workspace))
	}
	preconditions = append(preconditions, qwenSourcePrecondition(system))
	if trustPrecondition != nil {
		preconditions = append(preconditions, *trustPrecondition)
	}
	return &qwenSources{
		qwenHome:         routing.qwenHome,
		systemDefaults:   systemDefaults,
		user:             user,
		workspace:        workspace,
		system:           system,
		workspaceActive:  workspaceActive,
		workspaceTrusted: workspaceTrusted,
		disableControl: effectiveQwenDisable(
			systemDefaults,
			user,
			workspace,
			system,
			workspaceActive && workspaceTrusted,
		),
		preconditions: deduplicateQwenPreconditions(preconditions),
	}, nil
}

func requireQwenHooksEnabled(sources *qwenSources) error {
	if sources == nil {
		return fmt.Errorf("Qwen Code settings sources are required")
	}
	if !sources.disableControl.disabled {
		return nil
	}
	location := "effective settings"
	if sources.disableControl.source != nil {
		location = fmt.Sprintf(
			"%s at %s",
			qwenDocumentLabel(sources.disableControl.source),
			sources.disableControl.source.filePath,
		)
	}
	return fmt.Errorf(
		"Qwen Code hooks are disabled by disableAllHooks in %s",
		location,
	)
}
