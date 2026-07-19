package plugins

import (
	"fmt"
	"os"
	"path/filepath"
)

func kimiRuntimeScriptsExist(contracts []kimiRuntimeContract) (bool, error) {
	home, err := kimiHomeDirectory()
	if err != nil {
		return false, err
	}
	root := filepath.Join(home, ".elydora")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read Elydora runtime directory at %s: %w", root, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		agentDir := filepath.Join(root, entry.Name())
		guardPath := filepath.Join(agentDir, kimiGuardScript)
		auditPath := filepath.Join(agentDir, kimiAuditScript)
		if !kimiContractsReferenceRuntime(contracts, guardPath, auditPath) {
			continue
		}
		configPath := filepath.Join(agentDir, "config.json")
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
		if agentName != kimiAgentKey {
			continue
		}
		guardExists, err := regularFileExists(guardPath, "Elydora guard runtime")
		if err != nil {
			return false, err
		}
		auditExists, err := regularFileExists(auditPath, "Elydora audit runtime")
		if err != nil {
			return false, err
		}
		return guardExists && auditExists, nil
	}
	return false, nil
}

func kimiContractsReferenceRuntime(contracts []kimiRuntimeContract, guardPath, auditPath string) bool {
	for _, contract := range contracts {
		if kimiCommandEndsWithPath(contract.guard, guardPath) &&
			kimiCommandEndsWithPath(contract.audit, auditPath) {
			return true
		}
	}
	return false
}
