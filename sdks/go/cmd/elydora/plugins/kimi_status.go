package plugins

func kimiRuntimeFilesExist(contracts []kimiRuntimeContract) (bool, error) {
	for _, contract := range contracts {
		exists, err := managedRuntimeContractExists(
			contract.managedRuntimeContract, kimiAgentKey, "Kimi", nil,
		)
		if err != nil || exists {
			return exists, err
		}
	}
	return false, nil
}
