package plugins

import (
	"bytes"
	"fmt"
	"strings"
)

type kimiRenderedDocument struct {
	document kimiDocument
	next     []byte
}

func preflightKimiInstallation(
	config InstallConfig,
	documents []kimiDocument,
) (*managedRuntimePaths, string, error) {
	if len(documents) == 0 {
		return nil, "", fmt.Errorf("kimi installation requires at least one hook contract")
	}
	if err := validateManagedInstallConfig(config, kimiAgentKey, "kimi"); err != nil {
		return nil, "", err
	}
	paths, err := resolveManagedRuntimePaths(config, kimiGuardScript, kimiAuditScript)
	if err != nil {
		return nil, "", err
	}
	if err := validateManagedRuntimeIdentity(
		paths.agentDirectory, config.AgentID, kimiAgentKey, "Kimi",
	); err != nil {
		return nil, "", err
	}
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		return nil, "", err
	}
	return paths, nodePath, nil
}

func kimiExpectedScripts(agentID string, _ map[string]any) ([]byte, []byte) {
	return []byte(generateGuardScript(kimiAgentKey, agentID, "", false, "")),
		[]byte(buildHookScriptWithOutput(kimiAgentKey, agentID, "", false, true))
}

func renderKimiChange(
	document kimiDocument,
	keep []int,
	additions []kimiHook,
) (kimiRenderedDocument, error) {
	next, err := renderKimiHooks(document, keep, additions)
	if err != nil {
		return kimiRenderedDocument{}, err
	}
	if strings.TrimSpace(string(next)) != "" {
		if _, err := parseKimiDocument(document.contract, next, true); err != nil {
			return kimiRenderedDocument{}, fmt.Errorf(
				"validate rendered %s: %w", document.contract.label, err,
			)
		}
	}
	return kimiRenderedDocument{document: document, next: next}, nil
}

func prepareRenderedKimiChange(rendered kimiRenderedDocument) (*fileChange, error) {
	document := rendered.document
	if document.exists && bytes.Equal(document.raw, rendered.next) {
		return nil, nil
	}
	remove := document.exists && strings.TrimSpace(string(rendered.next)) == ""
	return prepareSourceChange(
		document.contract.configPath,
		document.contract.label,
		document.raw,
		document.exists,
		rendered.next,
		0600,
		remove,
	)
}

func prepareKimiInstallationChanges(
	config InstallConfig,
	paths *managedRuntimePaths,
	rendered []kimiRenderedDocument,
) ([]*fileChange, error) {
	guardScript, auditScript := kimiExpectedScripts(config.AgentID, nil)
	changes, err := managedRuntimeFileChanges(
		config, paths, kimiAgentKey, string(guardScript), string(auditScript),
	)
	if err != nil {
		return nil, err
	}
	documentChanges, err := prepareKimiUninstallChanges(rendered)
	if err != nil {
		return nil, err
	}
	return append(changes, documentChanges...), nil
}

func prepareKimiUninstallChanges(
	rendered []kimiRenderedDocument,
) ([]*fileChange, error) {
	changes := make([]*fileChange, 0, len(rendered))
	for _, document := range rendered {
		change, err := prepareRenderedKimiChange(document)
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}
	return changes, nil
}
