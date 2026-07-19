package plugins

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

var qwenPathSeparatorPattern = regexp.MustCompile(`[/\\]+`)

func defaultQwenHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".qwen"), nil
}

func resolveQwenStoragePath(value string) (string, error) {
	resolved := value
	if value == "~" || len(value) >= 2 && value[0] == '~' && (value[1] == '/' || value[1] == '\\') {
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

func qwenHomeFromEnvFile(filePath string) (string, bool, error) {
	raw, exists, err := readOptionalFile(filePath, "Qwen home environment")
	if err != nil || !exists {
		return "", false, err
	}
	value := parseDotenv(raw)["QWEN_HOME"]
	if value == "" {
		return "", false, nil
	}
	resolved, err := resolveQwenStoragePath(value)
	return resolved, err == nil, err
}

func resolveQwenHome() (string, error) {
	defaultHome, err := defaultQwenHome()
	if err != nil {
		return "", err
	}
	value, explicit := os.LookupEnv("QWEN_HOME")
	initialHome := defaultHome
	if value != "" {
		initialHome, err = resolveQwenStoragePath(value)
		if err != nil {
			return "", err
		}
	}
	if explicit {
		return initialHome, nil
	}
	for _, candidate := range []string{
		filepath.Join(initialHome, ".env"), filepath.Join(filepath.Dir(initialHome), ".env"),
	} {
		if discovered, exists, readErr := qwenHomeFromEnvFile(candidate); readErr != nil {
			return "", readErr
		} else if exists {
			return discovered, nil
		}
	}
	return initialHome, nil
}

func readQwenDocument() (*qwenDocument, error) {
	home, err := resolveQwenHome()
	if err != nil {
		return nil, err
	}
	configPath := filepath.Join(home, "settings.json")
	raw, exists, err := readOptionalFile(configPath, "Qwen Code settings")
	if err != nil {
		return nil, err
	}
	if !exists {
		return createOwnedQwenDocument(configPath)
	}
	return parseQwenDocument(true, configPath, raw)
}

func prepareRenderedQwenChange(rendered *qwenRenderedDocument) (*fileChange, error) {
	if rendered == nil || !rendered.changed {
		return nil, nil
	}
	return prepareSourceChange(
		rendered.document.filePath,
		"Qwen Code settings",
		rendered.document.raw,
		rendered.document.exists,
		rendered.next,
		os.FileMode(0600),
		rendered.remove,
	)
}

func qwenRuntimeFilesExist(contracts []qwenRuntimeContract) (bool, error) {
	for _, contract := range contracts {
		configPath := filepath.Join(filepath.Dir(contract.guardPath), "config.json")
		raw, exists, err := readOptionalFile(configPath, "Elydora runtime config")
		if err != nil {
			return false, err
		}
		if !exists {
			continue
		}
		var config map[string]any
		if err := json.Unmarshal(raw, &config); err != nil {
			return false, fmt.Errorf("parse Elydora runtime config at %s: %w", configPath, err)
		}
		agentID, ok := config["agent_id"].(string)
		if !ok || config["agent_name"] != qwenAgentKey || !sameQwenAgentID(agentID, contract.agentID) {
			continue
		}
		guardExists, err := regularFileExists(contract.guardPath, "Elydora guard runtime")
		if err != nil {
			return false, err
		}
		auditExists, err := regularFileExists(contract.auditPath, "Elydora audit runtime")
		if err != nil {
			return false, err
		}
		if guardExists && auditExists {
			return true, nil
		}
	}
	return false, nil
}
