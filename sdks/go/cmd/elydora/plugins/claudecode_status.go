package plugins

func claudeRuntimeFilesExist(contracts []managedRuntimeContract) (bool, error) {
	return managedRuntimeFilesExist(contracts, claudeAgentKey, "Claude Code", nil)
}
