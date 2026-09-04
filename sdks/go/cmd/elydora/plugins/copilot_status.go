package plugins

func copilotRuntimeFilesExist(contracts []managedRuntimeContract) (bool, error) {
	return managedRuntimeFilesExist(contracts, copilotAgentKey, "Copilot", copilotExpectedScripts)
}
