package plugins

func droidRuntimeFilesExist(contracts []managedRuntimeContract) (bool, error) {
	return managedRuntimeFilesExist(contracts, droidAgentKey, "Factory Droid", droidExpectedScripts)
}
