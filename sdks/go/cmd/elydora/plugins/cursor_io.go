package plugins

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	info, err := os.Lstat(filePath)
	if errors.Is(err, os.ErrNotExist) {
		return createCursorDocument(filePath), nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect Cursor user hooks at %s: %w", filePath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("Cursor user hooks path is not a physical file: %s", filePath)
	}
	raw, err := os.ReadFile(filePath) // #nosec G304 -- filePath is the fixed Cursor user configuration path.
	if err != nil {
		return nil, fmt.Errorf("read Cursor user hooks at %s: %w", filePath, err)
	}
	return parseCursorDocument(filePath, raw, runtimeRoot)
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

func cursorPhysicalFileExists(path, label string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect %s at %s: %w", label, path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false, fmt.Errorf("%s path is not a physical file: %s", label, path)
	}
	return true, nil
}

func cursorPhysicalDirectoryExists(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect Elydora runtime directory at %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false, fmt.Errorf("Elydora runtime path is not a physical directory: %s", path)
	}
	return true, nil
}

func readCursorRuntimeConfig(path string) (map[string]any, bool, error) {
	exists, err := cursorPhysicalFileExists(path, "Elydora runtime config")
	if err != nil || !exists {
		return nil, exists, err
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- path is confined to one validated Elydora agent directory.
	if err != nil {
		return nil, true, fmt.Errorf("read Elydora runtime config at %s: %w", path, err)
	}
	label := fmt.Sprintf("Elydora runtime config at %s", path)
	if !json.Valid(raw) {
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, true, fmt.Errorf("parse %s: %w", label, err)
		}
	}
	config, err := decodeJSONCObject(raw, label, false)
	if err != nil {
		return nil, true, err
	}
	return config, true, nil
}

func validateCursorRuntimeIdentity(path, agentID string) (bool, error) {
	config, exists, err := readCursorRuntimeConfig(path)
	if err != nil || !exists {
		return exists, err
	}
	configuredID, ok := config["agent_id"].(string)
	if !ok || config["agent_name"] != cursorAgentKey ||
		!sameCursorAgentID(configuredID, agentID) {
		return true, fmt.Errorf(
			"Elydora runtime config identity does not match Cursor agent %s: %s",
			agentID,
			path,
		)
	}
	return true, nil
}

func preflightCursorRuntime(agentDirectory, agentID string) error {
	exists, err := cursorPhysicalDirectoryExists(agentDirectory)
	if err != nil || !exists {
		return err
	}
	identityExists, err := validateCursorRuntimeIdentity(
		filepath.Join(agentDirectory, "config.json"), agentID,
	)
	if err != nil {
		return err
	}
	runtimeExists := false
	for _, item := range []struct{ path, label string }{
		{filepath.Join(agentDirectory, cursorGuardScript), "Elydora guard runtime"},
		{filepath.Join(agentDirectory, cursorAuditScript), "Elydora audit runtime"},
		{filepath.Join(agentDirectory, "private.key"), "Elydora private key"},
	} {
		exists, err := cursorPhysicalFileExists(item.path, item.label)
		if err != nil {
			return err
		}
		runtimeExists = runtimeExists || exists
	}
	if runtimeExists && !identityExists {
		return fmt.Errorf(
			"Elydora runtime identity cannot be verified without config.json: %s",
			agentDirectory,
		)
	}
	return nil
}

func prepareCursorInstallationChanges(
	config InstallConfig,
	agentDirectory string,
	rendered *cursorRenderedDocument,
) ([]*fileChange, error) {
	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = "https://api.elydora.com"
	}
	runtimeConfig, err := json.MarshalIndent(agentRuntimeConfig{
		OrgID: config.OrgID, AgentID: config.AgentID, KID: config.KID,
		BaseURL: baseURL, Token: config.Token, AgentName: cursorAgentKey,
	}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode Elydora runtime config: %w", err)
	}
	runtimeConfig = append(runtimeConfig, '\n')
	if len(runtimeConfig) > maxRuntimeConfigBytes {
		return nil, fmt.Errorf(
			"Elydora runtime config exceeds %d bytes after JSON encoding",
			maxRuntimeConfigBytes,
		)
	}
	items := []struct {
		path, label string
		content     []byte
		mode        os.FileMode
	}{
		{
			filepath.Join(agentDirectory, cursorGuardScript), "Elydora guard runtime",
			[]byte(generateGuardScript(
				cursorAgentKey,
				config.AgentID,
				`{"permission":"allow"}`+"\n",
				true,
				"cursor",
			)), 0700,
		},
		{filepath.Join(agentDirectory, "config.json"), "Elydora runtime config", runtimeConfig, 0600},
		{filepath.Join(agentDirectory, "private.key"), "Elydora private key", []byte(config.PrivateKey), 0600},
		{
			filepath.Join(agentDirectory, cursorAuditScript), "Elydora audit runtime",
			[]byte(buildHookScriptWithOutput(
				cursorAgentKey,
				config.AgentID,
				"{}\n",
				true,
				true,
			)), 0700,
		},
	}
	changes := make([]*fileChange, 0, len(items)+1)
	for _, item := range items {
		if err := validateRuntimeFileTarget(item.path, item.label); err != nil {
			return nil, err
		}
		change, err := prepareFileChange(item.path, item.label, item.content, item.mode)
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}
	documentChange, err := prepareRenderedCursorChange(rendered)
	if err != nil {
		return nil, err
	}
	return append(changes, documentChange), nil
}

func ensureCursorDirectory(path, label string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return fmt.Errorf("create %s directory at %s: %w", label, path, err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect %s directory at %s: %w", label, path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%s directory is not a physical directory: %s", label, path)
	}
	return nil
}

func writeCursorChanges(
	changes []*fileChange,
	label string,
	rename renameFunc,
	runtimeRoot, agentDirectory string,
) error {
	hasChanges := false
	for _, change := range changes {
		if change != nil {
			hasChanges = true
			break
		}
	}
	if !hasChanges {
		return nil
	}
	if agentDirectory != "" {
		if err := EnsurePrivateDirectory(runtimeRoot); err != nil {
			return err
		}
		if err := EnsurePrivateDirectory(agentDirectory); err != nil {
			return err
		}
	}
	directories := map[string]string{}
	for _, change := range changes {
		if change == nil {
			continue
		}
		directory := filepath.Dir(change.filePath)
		key := filepath.Clean(directory)
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		directories[key] = directory
	}
	for _, directory := range directories {
		if err := ensureCursorDirectory(directory, label); err != nil {
			return err
		}
	}
	return writeChanges(changes, label, rename)
}

func cursorRuntimeFilesExist(contracts []cursorRuntimeContract) (bool, error) {
	for _, contract := range contracts {
		agentDirectory := filepath.Dir(contract.guardPath)
		directoryExists, err := cursorPhysicalDirectoryExists(agentDirectory)
		if err != nil {
			return false, err
		}
		if !directoryExists {
			continue
		}
		config, exists, err := readCursorRuntimeConfig(filepath.Join(agentDirectory, "config.json"))
		if err != nil {
			return false, err
		}
		configuredID, idOK := config["agent_id"].(string)
		if !exists || !idOK || config["agent_name"] != cursorAgentKey ||
			!sameCursorAgentID(configuredID, contract.agentID) {
			continue
		}
		complete := true
		for _, item := range []struct{ path, label string }{
			{contract.guardPath, "Elydora guard runtime"},
			{contract.auditPath, "Elydora audit runtime"},
			{filepath.Join(agentDirectory, "private.key"), "Elydora private key"},
		} {
			exists, err := cursorPhysicalFileExists(item.path, item.label)
			if err != nil {
				return false, err
			}
			complete = complete && exists
		}
		if complete {
			return true, nil
		}
	}
	return false, nil
}
