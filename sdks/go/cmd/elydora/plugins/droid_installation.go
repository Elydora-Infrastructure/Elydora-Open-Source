package plugins

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type droidRuntimePaths struct {
	runtimeRoot    string
	agentDirectory string
	guardPath      string
	auditPath      string
}

type preparedDroidInstallation struct {
	changes       []*fileChange
	preconditions []filePrecondition
	paths         *droidRuntimePaths
}

type preparedDroidUninstall struct {
	changes       []*fileChange
	preconditions []filePrecondition
}

func droidSourceLabel(document *droidDocument) string {
	switch document.kind {
	case "settings":
		return "Factory Droid user settings"
	case "local-settings":
		return "Factory Droid local settings"
	case "legacy":
		return "Factory Droid legacy hooks"
	default:
		return "Factory Droid user hooks"
	}
}

func validateDroidInstallConfig(config InstallConfig) error {
	for _, field := range []struct{ name, value string }{
		{"agent name", config.AgentName},
		{"organization ID", config.OrgID},
		{"agent ID", config.AgentID},
		{"key ID", config.KID},
		{"private key", config.PrivateKey},
		{"base URL", config.BaseURL},
		{"guard script path", config.GuardScriptPath},
	} {
		if field.value == "" {
			return fmt.Errorf("%s is required", field.name)
		}
	}
	if config.AgentName != droidAgentKey {
		return fmt.Errorf(
			"Factory Droid installation requires agent name %s",
			droidAgentKey,
		)
	}
	if strings.TrimSpace(config.OrgID) == "" {
		return fmt.Errorf("organization ID is required")
	}
	if strings.TrimSpace(config.KID) == "" {
		return fmt.Errorf("key ID is required")
	}
	if config.Token != "" && strings.TrimSpace(config.Token) == "" {
		return fmt.Errorf("token must contain a non-whitespace value when provided")
	}
	if err := validateManagedPrivateKey(config.PrivateKey); err != nil {
		return err
	}
	return validateManagedBaseURL(config.BaseURL)
}

func droidAgentPaths(config InstallConfig) (*droidRuntimePaths, error) {
	if err := validateDroidInstallConfig(config); err != nil {
		return nil, err
	}
	runtimeRoot, err := AgentRuntimeRoot()
	if err != nil {
		return nil, err
	}
	agentDirectory, err := ResolveAgentRuntimeDirectory(config.AgentID)
	if err != nil {
		return nil, err
	}
	paths := &droidRuntimePaths{
		runtimeRoot:    runtimeRoot,
		agentDirectory: agentDirectory,
		guardPath:      filepath.Join(agentDirectory, droidGuardScript),
		auditPath:      filepath.Join(agentDirectory, droidAuditScript),
	}
	if !filepath.IsAbs(config.GuardScriptPath) ||
		!sameDroidPath(config.GuardScriptPath, paths.guardPath) {
		return nil, fmt.Errorf(
			"Elydora guard runtime must use the managed agent directory: %s",
			paths.guardPath,
		)
	}
	if config.HookScript != "" && (!filepath.IsAbs(config.HookScript) ||
		!sameDroidPath(config.HookScript, paths.auditPath)) {
		return nil, fmt.Errorf(
			"Elydora audit runtime must use the managed agent directory: %s",
			paths.auditPath,
		)
	}
	return paths, nil
}

func preflightDroidInstallation(
	config InstallConfig,
	sources *droidSources,
) (*droidRuntimePaths, string, error) {
	if sources == nil || sources.root == nil || sources.policy == nil {
		return nil, "", fmt.Errorf("Factory Droid installation sources are required")
	}
	if err := requireDroidHooksEnabled(sources); err != nil {
		return nil, "", err
	}
	hooks := make([]droidHookSettings, 0, len(droidSourceDocuments(sources)))
	for _, document := range droidSourceDocuments(sources) {
		hooks = append(hooks, document.hooks)
	}
	if err := validateDroidRegexes(hooks...); err != nil {
		return nil, "", err
	}
	paths, err := droidAgentPaths(config)
	if err != nil {
		return nil, "", err
	}
	if err := validateDroidRuntimeIdentity(paths.agentDirectory, config.AgentID); err != nil {
		return nil, "", err
	}
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		return nil, "", err
	}
	if !filepath.IsAbs(nodePath) || !isDroidNodeExecutable(nodePath) {
		return nil, "", fmt.Errorf(
			"Factory Droid hooks require an absolute Node.js executable path",
		)
	}
	return paths, nodePath, nil
}

func buildDroidRuntimeConfig(config InstallConfig) ([]byte, error) {
	value := map[string]any{
		"org_id":     config.OrgID,
		"agent_id":   config.AgentID,
		"kid":        config.KID,
		"base_url":   config.BaseURL,
		"agent_name": droidAgentKey,
	}
	if config.Token != "" {
		value["token"] = config.Token
	}
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode Elydora runtime config: %w", err)
	}
	encoded = append(encoded, '\n')
	if len(encoded) > maxRuntimeConfigBytes {
		return nil, fmt.Errorf(
			"Elydora runtime config exceeds %d bytes after JSON encoding",
			maxRuntimeConfigBytes,
		)
	}
	return encoded, nil
}

func prepareRenderedDroidChange(
	rendered *droidRenderedDocument,
) (*fileChange, error) {
	if rendered == nil || !rendered.changed {
		return nil, nil
	}
	return prepareSnapshotSourceChange(
		rendered.document.filePath,
		droidSourceLabel(rendered.document),
		rendered.document.snapshot,
		rendered.next,
		0600,
		rendered.remove,
	)
}

func validateDroidRenderedSet(
	sources *droidSources,
	rendered []*droidRenderedDocument,
) error {
	expected := droidInstallationDocuments(sources)
	if len(rendered) != len(expected) {
		return fmt.Errorf("Factory Droid rendered source set is incomplete")
	}
	for _, document := range expected {
		matches := 0
		for _, item := range rendered {
			if item != nil && item.document != nil &&
				sameDroidPath(item.document.filePath, document.filePath) {
				matches++
			}
		}
		if matches != 1 {
			return fmt.Errorf("Factory Droid rendered source set contains unexpected paths")
		}
	}
	return nil
}

func droidSourcePreconditions(
	sources *droidSources,
	changedPaths []string,
) []filePrecondition {
	preconditions := make([]filePrecondition, 0, len(droidSourceDocuments(sources)))
	for _, document := range droidSourceDocuments(sources) {
		changed := false
		for _, path := range changedPaths {
			if sameDroidPath(document.filePath, path) {
				changed = true
				break
			}
		}
		if !changed {
			preconditions = append(preconditions, filePrecondition{
				filePath:    document.filePath,
				label:       droidSourceLabel(document),
				snapshot:    document.snapshot,
				maximumSize: maxManagedSourceBytes,
			})
		}
	}
	return preconditions
}

func prepareDroidInstallation(
	config InstallConfig,
	sources *droidSources,
	rendered []*droidRenderedDocument,
) (*preparedDroidInstallation, error) {
	paths, _, err := preflightDroidInstallation(config, sources)
	if err != nil {
		return nil, err
	}
	if err := validateDroidRenderedSet(sources, rendered); err != nil {
		return nil, err
	}
	runtimeConfig, err := buildDroidRuntimeConfig(config)
	if err != nil {
		return nil, err
	}
	items := []struct {
		path, label string
		content     []byte
		mode        os.FileMode
	}{
		{
			paths.guardPath,
			"Elydora guard runtime",
			[]byte(generateGuardScript(droidAgentKey, config.AgentID, "", false, "")),
			0700,
		},
		{filepath.Join(paths.agentDirectory, "config.json"), "Elydora runtime config", runtimeConfig, 0600},
		{filepath.Join(paths.agentDirectory, "private.key"), "Elydora private key", []byte(config.PrivateKey), 0600},
		{
			paths.auditPath,
			"Elydora audit runtime",
			[]byte(buildHookScriptWithOutput(droidAgentKey, config.AgentID, "", false, true)),
			0700,
		},
	}
	changes := make([]*fileChange, 0, len(items)+len(rendered))
	for _, item := range items {
		change, changeErr := prepareFileChange(item.path, item.label, item.content, item.mode)
		if changeErr != nil {
			return nil, changeErr
		}
		changes = append(changes, change)
	}
	changedPaths := make([]string, 0, len(rendered))
	for _, document := range rendered {
		change, changeErr := prepareRenderedDroidChange(document)
		if changeErr != nil {
			return nil, changeErr
		}
		changes = append(changes, change)
		if change != nil {
			changedPaths = append(changedPaths, change.filePath)
		}
	}
	preconditions := droidSourcePreconditions(sources, changedPaths)
	preconditions = append(preconditions, sources.policy.preconditions...)
	return &preparedDroidInstallation{
		changes:       changes,
		preconditions: preconditions,
		paths:         paths,
	}, nil
}

func writeDroidChanges(
	changes []*fileChange,
	label string,
	rename renameFunc,
	runtimeRoot, agentDirectory string,
	preconditions []filePrecondition,
) error {
	hasChanges := false
	for _, change := range changes {
		if change != nil {
			hasChanges = true
			break
		}
	}
	if !hasChanges {
		return writeChanges(changes, label, rename, preconditions...)
	}
	if err := assertFilePreconditions(preconditions, label); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	if agentDirectory != "" {
		if err := EnsurePrivateDirectory(runtimeRoot); err != nil {
			return err
		}
		if err := EnsurePrivateDirectory(agentDirectory); err != nil {
			return err
		}
	}
	return writeChanges(changes, label, rename, preconditions...)
}

func commitDroidInstallation(
	prepared *preparedDroidInstallation,
	rename renameFunc,
) error {
	if prepared == nil || prepared.paths == nil {
		return fmt.Errorf("prepared Factory Droid installation is required")
	}
	return writeDroidChanges(
		prepared.changes,
		"Install Factory Droid hooks",
		rename,
		prepared.paths.runtimeRoot,
		prepared.paths.agentDirectory,
		prepared.preconditions,
	)
}

func prepareDroidUninstall(
	rendered []*droidRenderedDocument,
) (*preparedDroidUninstall, error) {
	changes := make([]*fileChange, 0, len(rendered))
	changedPaths := make([]string, 0, len(rendered))
	for _, item := range rendered {
		change, err := prepareRenderedDroidChange(item)
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
		if change != nil {
			changedPaths = append(changedPaths, change.filePath)
		}
	}
	preconditions := make([]filePrecondition, 0, len(rendered))
	for _, item := range rendered {
		changed := false
		for _, path := range changedPaths {
			if sameDroidPath(item.document.filePath, path) {
				changed = true
				break
			}
		}
		if !changed {
			preconditions = append(preconditions, filePrecondition{
				filePath:    item.document.filePath,
				label:       droidSourceLabel(item.document),
				snapshot:    item.document.snapshot,
				maximumSize: maxManagedSourceBytes,
			})
		}
	}
	return &preparedDroidUninstall{changes, preconditions}, nil
}

func commitDroidUninstall(
	prepared *preparedDroidUninstall,
	rename renameFunc,
) error {
	if prepared == nil {
		return fmt.Errorf("prepared Factory Droid uninstall is required")
	}
	return writeDroidChanges(
		prepared.changes,
		"Uninstall Factory Droid hooks",
		rename,
		"",
		"",
		prepared.preconditions,
	)
}
