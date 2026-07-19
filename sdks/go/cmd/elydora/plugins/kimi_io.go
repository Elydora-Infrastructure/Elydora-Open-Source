package plugins

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

func kimiPathExists(path, label string) (bool, error) {
	// #nosec G703 -- KIMI_CODE_HOME is an explicit local-user configuration path.
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if isMissingKimiPath(err) {
		return false, nil
	}
	return false, fmt.Errorf("inspect %s at %s: %w", label, path, err)
}

func legacyKimiCLIOnPath() (bool, error) {
	_, err := exec.LookPath("kimi-cli")
	if err == nil {
		return true, nil
	}
	if errors.Is(err, exec.ErrNotFound) {
		return false, nil
	}
	return false, fmt.Errorf("inspect kimi-cli executable on PATH: %w", err)
}

func resolveKimiContracts() ([]kimiContract, error) {
	home, err := kimiHomeDirectory()
	if err != nil {
		return nil, err
	}
	explicitHome := os.Getenv("KIMI_CODE_HOME")
	kimiHome := explicitHome
	if kimiHome == "" {
		kimiHome = filepath.Join(home, ".kimi-code")
	}
	modern := kimiContract{
		runtimeName: "Kimi Code",
		label:       "Kimi Code hooks config",
		configPath:  filepath.Join(kimiHome, "config.toml"),
		events:      kimiModernEvents,
	}
	legacy := kimiContract{
		runtimeName: "kimi-cli",
		label:       "kimi-cli legacy hooks config",
		configPath:  filepath.Join(home, ".kimi", "config.toml"),
		events:      kimiSharedEvents,
	}
	modernDetected := explicitHome != ""
	if !modernDetected {
		modernDetected, err = kimiPathExists(kimiHome, "Kimi Code home")
		if err != nil {
			return nil, err
		}
	}
	legacyDetected, err := kimiPathExists(legacy.configPath, legacy.label)
	if err != nil {
		return nil, err
	}
	if !legacyDetected {
		legacyDetected, err = legacyKimiCLIOnPath()
		if err != nil {
			return nil, err
		}
	}
	if legacyDetected && !modernDetected {
		return []kimiContract{legacy}, nil
	}
	if legacyDetected {
		return []kimiContract{modern, legacy}, nil
	}
	return []kimiContract{modern}, nil
}

func readKimiConfig(contract kimiContract) (kimiDocument, error) {
	raw, err := os.ReadFile(contract.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return parseKimiDocument(contract, nil, false)
		}
		return kimiDocument{}, fmt.Errorf("read %s at %s: %w", contract.label, contract.configPath, err)
	}
	return parseKimiDocument(contract, raw, true)
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

func writeKimiConfig(document kimiDocument, raw []byte) error {
	if err := writeHookFileAtomic(document.contract.configPath, raw, 0600); err != nil {
		return fmt.Errorf("write %s at %s: %w", document.contract.label, document.contract.configPath, err)
	}
	return nil
}

func removeKimiConfig(document kimiDocument) error {
	if err := removeHookFile(document.contract.configPath, document.contract.label); err != nil {
		return err
	}
	return nil
}

func buildKimiCommand(runtimePath, scriptPath string) string {
	if runtime.GOOS == "windows" {
		return quoteWindowsArgument(runtimePath) + " " + quoteWindowsArgument(scriptPath)
	}
	return quotePOSIXArgument(runtimePath) + " " + quotePOSIXArgument(scriptPath)
}

func buildKimiHook(event, command string) kimiHook {
	timeout := kimiHookTimeoutSeconds
	return kimiHook{event: event, command: command, timeout: &timeout}
}

func kimiRuntimeNames(documents []kimiDocument) string {
	names := make([]string, 0, len(documents))
	for _, document := range documents {
		names = append(names, document.contract.runtimeName)
	}
	return strings.Join(names, " and ")
}
