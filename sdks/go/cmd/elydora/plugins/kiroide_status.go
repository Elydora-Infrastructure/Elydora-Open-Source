package plugins

import (
	"path/filepath"
	"runtime"
)

func kiroIdeExpectedScripts(agentID string, _ map[string]any) ([]byte, []byte) {
	return []byte(generateGuardScript(kiroIdeAgentKey, agentID, "", false, "")),
		[]byte(buildHookScriptWithOutput(kiroIdeAgentKey, agentID, "", false, true))
}

func kiroIdeRuntimeFileMode(snapshot *managedFileSnapshot, expected uint32) bool {
	if runtime.GOOS == "windows" {
		return true
	}
	return uint32(snapshot.mode.Perm()) == expected
}

// kiroIdeRuntimeModesMatch requires the private modes the installer writes.
func kiroIdeRuntimeModesMatch(contract managedRuntimeContract) (bool, error) {
	agentDirectory := filepath.Dir(contract.guardPath)
	for _, item := range []struct {
		path, label string
		limit       int64
		mode        uint32
	}{
		{filepath.Join(agentDirectory, "config.json"), "Elydora runtime config", maxRuntimeConfigBytes, 0600},
		{filepath.Join(agentDirectory, "private.key"), "Elydora private key", maxProtectedSecretBytes, 0600},
		{contract.guardPath, "Elydora guard runtime", maxManagedSourceBytes, 0700},
		{contract.auditPath, "Elydora audit runtime", maxManagedSourceBytes, 0700},
	} {
		snapshot, err := readManagedFile(item.path, item.label, item.limit)
		if err != nil || snapshot == nil {
			return false, err
		}
		if !kiroIdeRuntimeFileMode(snapshot, item.mode) {
			return false, nil
		}
	}
	return true, nil
}

func kiroIdeRuntimeFilesExist(contracts []managedRuntimeContract) (bool, error) {
	for _, contract := range contracts {
		exists, err := managedRuntimeContractExists(
			contract, kiroIdeAgentKey, "Kiro IDE", kiroIdeExpectedScripts,
		)
		if err != nil {
			return false, err
		}
		if !exists {
			continue
		}
		modes, err := kiroIdeRuntimeModesMatch(contract)
		if err != nil || modes {
			return modes, err
		}
	}
	return false, nil
}
