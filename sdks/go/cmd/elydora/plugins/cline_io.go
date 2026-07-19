package plugins

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type clinePendingWrite struct {
	state  clineHookFile
	source string
}

type clineStagedFile struct {
	state         clineHookFile
	temporaryPath string
}

type clineRenameFunc func(source, destination string) error

func readClineHookFile(filePath string) (clineHookFile, error) {
	// #nosec G304 -- filePath is resolved from Cline's native hook directory.
	raw, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return clineHookFile{filePath: filePath}, nil
		}
		return clineHookFile{}, fmt.Errorf("read Cline hook at %s: %w", filePath, err)
	}
	source := string(raw)
	metadata, err := parseClineMetadata(filePath, source)
	if err != nil {
		return clineHookFile{}, err
	}
	return clineHookFile{
		exists:   true,
		filePath: filePath,
		source:   source,
		metadata: metadata,
	}, nil
}

func requireAvailableClineHook(file clineHookFile) error {
	if file.exists && file.metadata == nil {
		return fmt.Errorf(
			"Cline hook at %s already exists and is owned by another integration",
			file.filePath,
		)
	}
	return nil
}

func removeClineTemporary(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove temporary Cline hook at %s: %w", path, err)
	}
	return nil
}

func failClineStage(temporary *os.File, path string, cause error) error {
	closeErr := temporary.Close()
	removeErr := removeClineTemporary(path)
	return errors.Join(cause, closeErr, removeErr)
}

func stageClineHook(write clinePendingWrite) (clineStagedFile, error) {
	directory := filepath.Dir(write.state.filePath)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return clineStagedFile{}, fmt.Errorf("create Cline hooks directory at %s: %w", directory, err)
	}
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(write.state.filePath)+".*.tmp")
	if err != nil {
		return clineStagedFile{}, fmt.Errorf("stage Cline hook at %s: %w", write.state.filePath, err)
	}
	temporaryPath := temporary.Name()
	if err := temporary.Chmod(0700); err != nil {
		return clineStagedFile{}, failClineStage(
			temporary,
			temporaryPath,
			fmt.Errorf("set permissions for %s: %w", temporaryPath, err),
		)
	}
	written, err := temporary.WriteString(write.source)
	if err != nil {
		return clineStagedFile{}, failClineStage(
			temporary,
			temporaryPath,
			fmt.Errorf("write temporary Cline hook for %s: %w", write.state.filePath, err),
		)
	}
	if written != len(write.source) {
		return clineStagedFile{}, failClineStage(
			temporary,
			temporaryPath,
			fmt.Errorf("write temporary Cline hook for %s: %w", write.state.filePath, io.ErrShortWrite),
		)
	}
	if err := temporary.Sync(); err != nil {
		return clineStagedFile{}, failClineStage(
			temporary,
			temporaryPath,
			fmt.Errorf("sync temporary Cline hook for %s: %w", write.state.filePath, err),
		)
	}
	if err := temporary.Close(); err != nil {
		return clineStagedFile{}, errors.Join(
			fmt.Errorf("close temporary Cline hook for %s: %w", write.state.filePath, err),
			removeClineTemporary(temporaryPath),
		)
	}
	return clineStagedFile{state: write.state, temporaryPath: temporaryPath}, nil
}

func rollbackClineHook(state clineHookFile) error {
	if state.exists {
		return writeHookFileAtomic(state.filePath, []byte(state.source), 0700)
	}
	return removeHookFile(state.filePath, "Cline hook rollback")
}

func writeClineHookPair(guard, audit clinePendingWrite) error {
	return writeClineHookPairWithRename(guard, audit, os.Rename)
}

func writeClineHookPairWithRename(
	guard, audit clinePendingWrite,
	rename clineRenameFunc,
) error {
	writes := []clinePendingWrite{guard, audit}
	staged := make([]clineStagedFile, 0, len(writes))
	for _, write := range writes {
		file, err := stageClineHook(write)
		if err != nil {
			failures := []error{fmt.Errorf("stage Cline hook pair: %w", err)}
			for _, item := range staged {
				if cleanupErr := removeClineTemporary(item.temporaryPath); cleanupErr != nil {
					failures = append(failures, cleanupErr)
				}
			}
			return errors.Join(failures...)
		}
		staged = append(staged, file)
	}

	committed := 0
	for index, item := range staged {
		if err := rename(item.temporaryPath, item.state.filePath); err != nil {
			failures := []error{
				fmt.Errorf("write Cline hook pair at %s: %w", item.state.filePath, err),
			}
			for rollbackIndex := committed - 1; rollbackIndex >= 0; rollbackIndex-- {
				if rollbackErr := rollbackClineHook(staged[rollbackIndex].state); rollbackErr != nil {
					failures = append(failures, fmt.Errorf(
						"restore Cline hook at %s: %w",
						staged[rollbackIndex].state.filePath,
						rollbackErr,
					))
				}
			}
			for cleanupIndex := index; cleanupIndex < len(staged); cleanupIndex++ {
				if cleanupErr := removeClineTemporary(staged[cleanupIndex].temporaryPath); cleanupErr != nil {
					failures = append(failures, cleanupErr)
				}
			}
			return errors.Join(failures...)
		}
		committed++
	}
	return nil
}

func removeOwnedClineHooks(files []clineHookFile, agentID string) error {
	for _, file := range files {
		if file.metadata == nil {
			continue
		}
		if agentID != "" && !sameClineAgentID(file.metadata.AgentID, agentID) {
			continue
		}
		if err := removeHookFile(file.filePath, "Cline hook"); err != nil {
			return err
		}
	}
	return nil
}

func clineRuntimeFilesExist(contract *clineRuntimeContract) (bool, error) {
	configPath := filepath.Join(contract.agentDirectory, "config.json")
	config, exists, err := readHookJSONObject(configPath, "Elydora runtime config")
	if err != nil || !exists {
		return false, err
	}
	agentID, ok := config["agent_id"].(string)
	if config["agent_name"] != clineAgentKey || !ok || !sameClineAgentID(agentID, contract.agentID) {
		return false, nil
	}
	guardExists, err := regularFileExists(contract.guardPath, "Elydora guard runtime")
	if err != nil {
		return false, err
	}
	auditExists, err := regularFileExists(contract.auditPath, "Elydora audit runtime")
	if err != nil {
		return false, err
	}
	return guardExists && auditExists, nil
}
