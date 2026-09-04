package plugins

import "path/filepath"

func clineRuntimeFilesExist(contract *clineRuntimeContract) (bool, error) {
	if !sameManagedPath(contract.guardPath, filepath.Join(contract.agentDirectory, clineGuardScript)) {
		return false, nil
	}
	return managedRuntimeContractExists(
		managedRuntimeContract{contract.agentID, contract.guardPath, contract.auditPath},
		clineAgentKey,
		"Cline",
		clineExpectedScripts,
	)
}
