package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

func grokHomeDirectory() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return home, nil
}

func grokRuntimeRoot() (string, error) {
	home, err := grokHomeDirectory()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".elydora"), nil
}

func grokConfigPath() (string, error) {
	home, err := grokHomeDirectory()
	if err != nil {
		return "", err
	}
	grokHome := os.Getenv("GROK_HOME")
	if grokHome == "" {
		grokHome = filepath.Join(home, ".grok")
	}
	// #nosec G703 -- GROK_HOME is an explicit local-user configuration path.
	return filepath.Join(grokHome, "hooks", grokConfigFile), nil
}

func readGrokConfig() (grokDocument, error) {
	configPath, err := grokConfigPath()
	if err != nil {
		return grokDocument{}, err
	}
	root, exists, err := readHookJSONObject(configPath, "Grok hooks config")
	if err != nil {
		return grokDocument{}, err
	}
	hooks, err := readGrokHooks(root)
	if err != nil {
		return grokDocument{}, err
	}
	return grokDocument{exists: exists, configPath: configPath, root: root, hooks: hooks}, nil
}

func quoteGrokWindowsArgument(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}

func buildGrokCommand(runtimePath, scriptPath string) string {
	if runtime.GOOS == "windows" {
		return quoteGrokWindowsArgument(runtimePath) + " " + quoteGrokWindowsArgument(scriptPath)
	}
	return quotePOSIXArgument(runtimePath) + " " + quotePOSIXArgument(scriptPath)
}

func buildGrokHandler(command string) map[string]any {
	return map[string]any{
		"type":    "command",
		"command": command,
		"timeout": grokHookTimeout,
	}
}

func buildGrokGroup(handler map[string]any) grokGroup {
	return grokGroup{object: map[string]any{}, handlers: []map[string]any{handler}}
}

func managedGrokIDs(groups []grokGroup, scriptName, runtimeRoot string) map[string]string {
	result := map[string]string{}
	for _, group := range groups {
		if _, hasMatcher := group.object["matcher"]; hasMatcher {
			continue
		}
		for _, handler := range group.handlers {
			agentID, managed := managedGrokAgentID(handler, scriptName, runtimeRoot)
			if managed {
				key := agentID
				if runtime.GOOS == "windows" {
					key = strings.ToLower(key)
				}
				result[key] = agentID
			}
		}
	}
	return result
}

func grokRuntimeContracts(hooks grokHooks, runtimeRoot string) []grokRuntimeContract {
	guards := managedGrokIDs(hooks["PreToolUse"], grokGuardScript, runtimeRoot)
	audits := managedGrokIDs(hooks["PostToolUse"], grokAuditScript, runtimeRoot)
	keys := make([]string, 0, len(guards))
	for key := range guards {
		if _, exists := audits[key]; exists {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	contracts := make([]grokRuntimeContract, 0, len(keys))
	for _, key := range keys {
		agentID := guards[key]
		contracts = append(contracts, grokRuntimeContract{
			agentID:   agentID,
			guardPath: filepath.Join(runtimeRoot, agentID, grokGuardScript),
			auditPath: filepath.Join(runtimeRoot, agentID, grokAuditScript),
		})
	}
	return contracts
}

func grokRuntimeScriptsExist(contracts []grokRuntimeContract, runtimeRoot string) (bool, error) {
	entries, err := os.ReadDir(runtimeRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read Elydora runtime directory at %s: %w", runtimeRoot, err)
	}
	for _, contract := range contracts {
		var entryName string
		for _, entry := range entries {
			if entry.IsDir() && sameGrokAgentID(entry.Name(), contract.agentID) {
				entryName = entry.Name()
				break
			}
		}
		if entryName == "" {
			continue
		}
		agentDirectory := filepath.Join(runtimeRoot, entryName)
		guardPath := filepath.Join(agentDirectory, grokGuardScript)
		auditPath := filepath.Join(agentDirectory, grokAuditScript)
		if !sameGrokPath(guardPath, contract.guardPath) ||
			!sameGrokPath(auditPath, contract.auditPath) {
			continue
		}
		configPath := filepath.Join(agentDirectory, "config.json")
		config, exists, err := readHookJSONObject(configPath, "Elydora runtime config")
		if err != nil {
			return false, err
		}
		if !exists {
			continue
		}
		agentName, ok := config["agent_name"].(string)
		if !ok {
			return false, fmt.Errorf(
				`Elydora runtime config at %s field "agent_name" must be a string`,
				configPath,
			)
		}
		if agentName != grokAgentKey {
			continue
		}
		guardExists, err := regularFileExists(
			guardPath, "Elydora guard runtime",
		)
		if err != nil {
			return false, err
		}
		auditExists, err := regularFileExists(
			auditPath, "Elydora audit runtime",
		)
		if err != nil {
			return false, err
		}
		return guardExists && auditExists, nil
	}
	return false, nil
}
