package plugins

import (
	"fmt"
	"strings"
)

type kimiMutation struct {
	document kimiDocument
	raw      []byte
	remove   bool
}

// KimiPlugin manages Kimi Code and legacy kimi-cli global lifecycle hooks.
type KimiPlugin struct{}

func (p *KimiPlugin) Install(config InstallConfig) error {
	if config.AgentID == "" {
		return fmt.Errorf("agent ID is required")
	}
	documents, err := readAllKimiConfigs()
	if err != nil {
		return err
	}
	if config.GuardScriptPath == "" {
		return fmt.Errorf("guard script path is required")
	}
	guardExists, err := regularFileExists(config.GuardScriptPath, "Elydora guard runtime")
	if err != nil {
		return err
	}
	if !guardExists {
		return fmt.Errorf("Elydora guard runtime is missing: %s", config.GuardScriptPath)
	}
	hookPath, err := hookScriptPath(config.AgentID)
	if err != nil {
		return err
	}
	if config.HookScript != "" {
		hookPath = config.HookScript
	}
	nodePath, err := resolveNodeRuntime()
	if err != nil {
		return err
	}
	additions := []kimiHook{
		buildKimiHook("PreToolUse", buildKimiCommand(nodePath, config.GuardScriptPath)),
		buildKimiHook("PostToolUse", buildKimiCommand(nodePath, hookPath)),
	}
	mutations := make([]kimiMutation, 0, len(documents))
	for _, document := range documents {
		raw, err := renderKimiHooks(document, keptKimiHookIndices(document.hooks, ""), additions)
		if err != nil {
			return err
		}
		if _, err := parseKimiDocument(document.contract, raw, true); err != nil {
			return fmt.Errorf("validate rendered %s: %w", document.contract.label, err)
		}
		mutations = append(mutations, kimiMutation{document: document, raw: raw})
	}

	runtimeConfig := config
	runtimeConfig.AgentName = kimiAgentKey
	if err := GenerateHookScript(hookPath, runtimeConfig); err != nil {
		return fmt.Errorf("generate hook script: %w", err)
	}
	for _, mutation := range mutations {
		if err := writeKimiConfig(mutation.document, mutation.raw); err != nil {
			return err
		}
	}
	fmt.Printf("%s: global PreToolUse and PostToolUse hooks installed.\n", kimiRuntimeNames(documents))
	return nil
}

func (p *KimiPlugin) Uninstall(agentID string) error {
	documents, err := readAllKimiConfigs()
	if err != nil {
		return err
	}
	mutations := make([]kimiMutation, 0, len(documents))
	for _, document := range documents {
		keep := keptKimiHookIndices(document.hooks, agentID)
		if len(keep) == len(document.hooks) {
			continue
		}
		raw, err := renderKimiHooks(document, keep, nil)
		if err != nil {
			return err
		}
		if _, err := parseKimiDocument(document.contract, raw, true); err != nil {
			return fmt.Errorf("validate rendered %s: %w", document.contract.label, err)
		}
		mutations = append(mutations, kimiMutation{
			document: document,
			raw:      raw,
			remove:   strings.TrimSpace(string(raw)) == "",
		})
	}
	for _, mutation := range mutations {
		if mutation.remove {
			if err := removeKimiConfig(mutation.document); err != nil {
				return err
			}
			continue
		}
		if err := writeKimiConfig(mutation.document, mutation.raw); err != nil {
			return err
		}
	}
	return nil
}

func (p *KimiPlugin) Status() (PluginStatus, error) {
	documents, err := readAllKimiConfigs()
	status := PluginStatus{
		AgentName:   kimiAgentKey,
		DisplayName: "Kimi Code",
	}
	if err != nil {
		return status, err
	}
	status.ConfigPath = documents[0].contract.configPath
	contracts := make([]kimiRuntimeContract, 0, len(documents))
	for _, document := range documents {
		contract := kimiRuntimeForDocument(document)
		if contract == nil {
			continue
		}
		contracts = append(contracts, *contract)
		status.ConfigPath = contract.configPath
	}
	if len(contracts) == 0 {
		return status, nil
	}
	status.HookConfigured = true
	status.HookScriptExists, err = kimiRuntimeScriptsExist(contracts)
	if err != nil {
		return status, err
	}
	status.Installed = status.HookScriptExists
	return status, nil
}
