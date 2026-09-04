package plugins

func geminiRuntimeFilesExist(contracts []managedRuntimeContract) (bool, error) {
	return managedRuntimeFilesExist(contracts, geminiAgentKey, "Gemini CLI", nil)
}
