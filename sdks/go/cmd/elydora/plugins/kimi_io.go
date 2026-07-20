package plugins

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func kimiHomeDirectory() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return home, nil
}

func isMissingKimiPath(err error) bool {
	return os.IsNotExist(err) || errors.Is(err, syscall.ENOTDIR)
}

func kimiEntryExists(path, label string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if isMissingKimiPath(err) {
		return false, nil
	}
	return false, fmt.Errorf("inspect %s at %s: %w", label, path, err)
}

func stableKimiContract(configPath string) kimiContract {
	return kimiContract{
		generation: "stable", runtimeName: "Kimi Code",
		label: "Kimi Code hooks config", directoryLabel: "Kimi Code home directory",
		configPath: configPath, events: kimiModernEvents,
	}
}

func legacyKimiContract(configPath string) kimiContract {
	return kimiContract{
		generation: "legacy", runtimeName: "kimi-cli",
		label:          "kimi-cli legacy hooks config",
		directoryLabel: "kimi-cli legacy home directory",
		configPath:     configPath, events: kimiSharedEvents,
	}
}

func resolveKimiContracts() ([]kimiContract, error) {
	home, err := kimiHomeDirectory()
	if err != nil {
		return nil, err
	}
	configuredHome := os.Getenv("KIMI_CODE_HOME")
	stableHome := filepath.Join(home, ".kimi-code")
	explicitHome := configuredHome != ""
	if explicitHome {
		stableHome, err = filepath.Abs(configuredHome)
		if err != nil {
			return nil, fmt.Errorf("resolve KIMI_CODE_HOME at %s: %w", configuredHome, err)
		}
	}
	legacyHome := filepath.Join(home, ".kimi")
	stable := stableKimiContract(filepath.Join(stableHome, "config.toml"))
	legacy := legacyKimiContract(filepath.Join(legacyHome, "config.toml"))
	if sameKimiPath(stable.configPath, legacy.configPath) {
		return []kimiContract{stable}, nil
	}

	stableDetected := explicitHome
	if !stableDetected {
		stableDetected, err = kimiEntryExists(stableHome, "Kimi Code home")
		if err != nil {
			return nil, err
		}
	}
	legacyDetected, err := kimiEntryExists(legacyHome, "kimi-cli legacy home")
	if err != nil {
		return nil, err
	}
	if legacyDetected && !stableDetected {
		return []kimiContract{legacy}, nil
	}
	if legacyDetected {
		return []kimiContract{stable, legacy}, nil
	}
	return []kimiContract{stable}, nil
}

func readKimiConfig(contract kimiContract) (kimiDocument, error) {
	directory := filepath.Dir(contract.configPath)
	if _, err := managedPhysicalDirectoryExists(directory, contract.directoryLabel); err != nil {
		return kimiDocument{}, err
	}
	snapshot, err := readManagedFile(
		contract.configPath,
		contract.label,
		maxManagedSourceBytes,
	)
	if err != nil {
		return kimiDocument{}, err
	}
	if snapshot == nil {
		return parseKimiDocument(contract, nil, false)
	}
	return parseKimiDocument(contract, snapshot.contents, true)
}

func readAllKimiConfigs() ([]kimiDocument, error) {
	contracts, err := resolveKimiContracts()
	if err != nil {
		return nil, err
	}
	documents := make([]kimiDocument, 0, len(contracts))
	for _, contract := range contracts {
		document, err := readKimiConfig(contract)
		if err != nil {
			return nil, err
		}
		documents = append(documents, document)
	}
	return documents, nil
}

func kimiRuntimeNames(documents []kimiDocument) string {
	names := make([]string, 0, len(documents))
	for _, document := range documents {
		names = append(names, document.contract.runtimeName)
	}
	return strings.Join(names, " and ")
}
