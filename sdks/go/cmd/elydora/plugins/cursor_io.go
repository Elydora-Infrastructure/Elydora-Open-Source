package plugins

import (
	"fmt"
	"os"
	"path/filepath"
)

func cursorConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".cursor", cursorConfigFile), nil
}

func readCursorDocument() (*cursorDocument, error) {
	filePath, err := cursorConfigPath()
	if err != nil {
		return nil, err
	}
	runtimeRoot, err := AgentRuntimeRoot()
	if err != nil {
		return nil, err
	}
	snapshot, err := readManagedFile(filePath, "Cursor user hooks", maxManagedSourceBytes)
	if err != nil {
		return nil, err
	}
	if snapshot == nil {
		return createCursorDocument(filePath), nil
	}
	return parseCursorDocument(filePath, snapshot.contents, runtimeRoot)
}

func prepareRenderedCursorChange(rendered *cursorRenderedDocument) (*fileChange, error) {
	if rendered == nil || !rendered.changed {
		return nil, nil
	}
	return prepareSourceChange(
		rendered.document.filePath,
		"Cursor user hooks",
		rendered.document.raw,
		rendered.document.exists,
		rendered.next,
		0600,
		rendered.remove,
	)
}

func prepareCursorInstallationChanges(
	config InstallConfig,
	paths *managedRuntimePaths,
	rendered *cursorRenderedDocument,
) ([]*fileChange, error) {
	if config.BaseURL == "" {
		config.BaseURL = "https://api.elydora.com"
	}
	changes, err := managedRuntimeFileChanges(
		config,
		paths,
		cursorAgentKey,
		generateGuardScript(
			cursorAgentKey, config.AgentID, `{"permission":"allow"}`+"\n", true, "cursor",
		),
		buildHookScriptWithOutput(cursorAgentKey, config.AgentID, "{}\n", true, true),
	)
	if err != nil {
		return nil, err
	}
	documentChange, err := prepareRenderedCursorChange(rendered)
	if err != nil {
		return nil, err
	}
	return append(changes, documentChange), nil
}

func cursorRuntimeFilesExist(contracts []managedRuntimeContract) (bool, error) {
	return managedRuntimeFilesPresent(contracts, cursorAgentKey)
}
