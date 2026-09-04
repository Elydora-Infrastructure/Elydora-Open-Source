package plugins

func grokRuntimeFilesExist(contracts []managedRuntimeContract) (bool, error) {
	return managedRuntimeFilesExist(contracts, grokAgentKey, "Grok", nil)
}
