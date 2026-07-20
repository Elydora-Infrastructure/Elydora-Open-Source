package plugins

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func copilotConfigPaths() (string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("resolve home directory: %w", err)
	}
	copilotHome := strings.TrimSpace(os.Getenv("COPILOT_HOME"))
	if copilotHome == "" {
		copilotHome = filepath.Join(home, ".copilot")
	} else if !filepath.IsAbs(copilotHome) {
		copilotHome, err = filepath.Abs(copilotHome)
		if err != nil {
			return "", "", fmt.Errorf("resolve COPILOT_HOME: %w", err)
		}
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		return "", "", fmt.Errorf("resolve working directory: %w", err)
	}
	return filepath.Join(copilotHome, "hooks", copilotConfigFile),
		filepath.Join(workingDirectory, ".github", "hooks", "hooks.json"), nil
}

func readCopilotHooks(value any, label string) (copilotHooks, error) {
	if value == nil {
		return nil, fmt.Errorf(`%s field "hooks" must be an object`, label)
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf(`%s field "hooks" must be an object`, label)
	}
	hooks := make(copilotHooks, len(object))
	for event, handlerValue := range object {
		values, ok := handlerValue.([]any)
		if !ok {
			return nil, fmt.Errorf(`%s field "hooks.%s" must be an array`, label, event)
		}
		handlers := make([]map[string]any, 0, len(values))
		for index, value := range values {
			handler, ok := value.(map[string]any)
			if !ok || handler == nil {
				return nil, fmt.Errorf(`%s handler hooks.%s[%d] must be an object`, label, event, index)
			}
			handlers = append(handlers, handler)
		}
		hooks[event] = handlers
	}
	return hooks, nil
}

func parseCopilotDocument(
	filePath string,
	raw []byte,
	label string,
) (*copilotDocument, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("parse %s at %s: %w", label, filePath, err)
	}
	root, ok := value.(map[string]any)
	if !ok || root == nil {
		return nil, fmt.Errorf("%s at %s must contain a JSON object", label, filePath)
	}
	if root["version"] != float64(1) {
		return nil, fmt.Errorf("%s at %s must declare version 1", label, filePath)
	}
	hooks := copilotHooks{}
	if hooksValue, exists := root["hooks"]; exists {
		var err error
		hooks, err = readCopilotHooks(hooksValue, label)
		if err != nil {
			return nil, err
		}
	}
	disabled := false
	if value, exists := root["disableAllHooks"]; exists {
		var valid bool
		disabled, valid = value.(bool)
		if !valid {
			return nil, fmt.Errorf(`%s field "disableAllHooks" must be a boolean`, label)
		}
	}
	return &copilotDocument{
		exists: true, filePath: filePath, root: root,
		hooks: hooks, raw: append([]byte(nil), raw...), disabled: disabled,
	}, nil
}

func readCopilotDocument(filePath, label string) (*copilotDocument, error) {
	info, err := os.Lstat(filePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect %s at %s: %w", label, filePath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s path is not a physical file: %s", label, filePath)
	}
	raw, err := os.ReadFile(filePath) // #nosec G304 -- filePath is a validated provider configuration path.
	if err != nil {
		return nil, fmt.Errorf("read %s at %s: %w", label, filePath, err)
	}
	return parseCopilotDocument(filePath, raw, label)
}

func readCopilotSources() (*copilotSources, error) {
	userPath, legacyPath, err := copilotConfigPaths()
	if err != nil {
		return nil, err
	}
	user, err := readCopilotDocument(userPath, "GitHub Copilot user hooks")
	if err != nil {
		return nil, err
	}
	legacy, err := readCopilotDocument(legacyPath, "GitHub Copilot project hooks")
	if err != nil {
		return nil, err
	}
	if user == nil {
		user = &copilotDocument{
			filePath: userPath,
			root:     map[string]any{},
			hooks:    copilotHooks{},
		}
	}
	return &copilotSources{user: user, legacy: legacy}, nil
}

func prepareRenderedCopilotChange(
	rendered *copilotRenderedDocument,
) (*fileChange, error) {
	if rendered == nil || !rendered.changed {
		return nil, nil
	}
	return prepareSourceChange(
		rendered.document.filePath,
		"GitHub Copilot hook source",
		rendered.document.raw,
		rendered.document.exists,
		rendered.next,
		0600,
		rendered.remove,
	)
}

func prepareCopilotInstallationChanges(
	config InstallConfig,
	agentDirectory string,
	auditPath string,
	rendered []*copilotRenderedDocument,
) ([]*fileChange, error) {
	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = "https://api.elydora.com"
	}
	runtimeConfig, err := json.MarshalIndent(agentRuntimeConfig{
		OrgID: config.OrgID, AgentID: config.AgentID, KID: config.KID,
		BaseURL: baseURL, Token: config.Token, AgentName: copilotAgentKey,
	}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode Elydora runtime config: %w", err)
	}
	runtimeConfig = append(runtimeConfig, '\n')
	if len(runtimeConfig) > maxRuntimeConfigBytes {
		return nil, fmt.Errorf(
			"Elydora runtime config exceeds %d bytes after JSON encoding",
			maxRuntimeConfigBytes,
		)
	}
	items := []struct {
		path, label string
		content     []byte
		mode        os.FileMode
	}{
		{filepath.Join(agentDirectory, "config.json"), "Elydora runtime config", runtimeConfig, 0600},
		{filepath.Join(agentDirectory, "private.key"), "Elydora private key", []byte(config.PrivateKey), 0600},
		{auditPath, "Elydora audit runtime", []byte(buildHookScript(copilotAgentKey, config.AgentID)), 0700},
	}
	changes := make([]*fileChange, 0, len(items)+len(rendered))
	for _, item := range items {
		if err := validateRuntimeFileTarget(item.path, item.label); err != nil {
			return nil, err
		}
		change, err := prepareFileChange(item.path, item.label, item.content, item.mode)
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}
	for _, document := range rendered {
		change, err := prepareRenderedCopilotChange(document)
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}
	return changes, nil
}

func activeCopilotContracts(
	sources *copilotSources,
	runtimeRoot string,
) []copilotRuntimeContract {
	contracts := make([]copilotRuntimeContract, 0)
	for _, document := range []*copilotDocument{sources.user, sources.legacy} {
		if document == nil || document.disabled {
			continue
		}
		contracts = append(contracts, copilotRuntimeContracts(document.hooks, runtimeRoot)...)
	}
	unique := map[string]copilotRuntimeContract{}
	for _, contract := range contracts {
		key := contract.agentID
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		unique[key] = contract
	}
	result := make([]copilotRuntimeContract, 0, len(unique))
	for _, contract := range unique {
		result = append(result, contract)
	}
	return result
}

func configuredCopilotPath(
	sources *copilotSources,
	runtimeRoot string,
) string {
	if !sources.user.disabled && len(copilotRuntimeContracts(sources.user.hooks, runtimeRoot)) > 0 {
		return sources.user.filePath
	}
	if sources.legacy != nil && !sources.legacy.disabled &&
		len(copilotRuntimeContracts(sources.legacy.hooks, runtimeRoot)) > 0 {
		return sources.legacy.filePath
	}
	return sources.user.filePath
}

func copilotPhysicalFileExists(path, label string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect %s at %s: %w", label, path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false, fmt.Errorf("%s path is not a physical file: %s", label, path)
	}
	return true, nil
}

func copilotRuntimeFilesExist(contracts []copilotRuntimeContract) (bool, error) {
	for _, contract := range contracts {
		configPath := filepath.Join(filepath.Dir(contract.guardPath), "config.json")
		configExists, err := copilotPhysicalFileExists(configPath, "Elydora runtime config")
		if err != nil {
			return false, err
		}
		if !configExists {
			continue
		}
		raw, exists, err := readOptionalFile(configPath, "Elydora runtime config")
		if err != nil {
			return false, err
		}
		if !exists {
			continue
		}
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return false, fmt.Errorf("parse Elydora runtime config at %s: %w", configPath, err)
		}
		config, ok := value.(map[string]any)
		if !ok || config == nil {
			return false, fmt.Errorf("Elydora runtime config at %s must contain a JSON object", configPath)
		}
		agentID, ok := config["agent_id"].(string)
		if !ok || config["agent_name"] != copilotAgentKey ||
			!sameCopilotAgentID(agentID, contract.agentID) {
			continue
		}
		guardExists, err := copilotPhysicalFileExists(contract.guardPath, "Elydora guard runtime")
		if err != nil {
			return false, err
		}
		auditExists, err := copilotPhysicalFileExists(contract.auditPath, "Elydora audit runtime")
		if err != nil {
			return false, err
		}
		if guardExists && auditExists {
			return true, nil
		}
	}
	return false, nil
}
