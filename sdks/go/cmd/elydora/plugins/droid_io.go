package plugins

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func prepareRenderedDroidChange(rendered *droidRenderedDocument) (*fileChange, error) {
	if rendered == nil || !rendered.changed {
		return nil, nil
	}
	label := "Factory Droid hooks"
	if rendered.document.kind == "legacy" {
		label = "Factory Droid legacy hooks"
	} else if rendered.document.kind == "settings" {
		label = "Factory Droid settings"
	}
	return prepareSourceChange(
		rendered.document.filePath,
		label,
		rendered.document.raw,
		rendered.document.exists,
		rendered.next,
		os.FileMode(0600),
		rendered.remove,
	)
}

func droidRuntimeFilesExist(contracts []droidRuntimeContract) (bool, error) {
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
		if !ok || config["agent_name"] != droidAgentKey || !sameDroidAgentID(agentID, contract.agentID) {
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
