package plugins

func qwenRuntimeFilesExist(contracts []managedRuntimeContract) (bool, error) {
	return managedRuntimeFilesExist(contracts, qwenAgentKey, "Qwen Code", qwenExpectedScripts)
}
