package plugins

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const augmentRegexValidator = `import fs from "node:fs";
const pattern = fs.readFileSync(0, "utf8");
try {
  new RegExp(pattern);
} catch (error) {
  process.stderr.write(error instanceof Error ? error.message : String(error));
  process.exit(1);
}`

func augmentGuardWrapperName() string {
	if runtime.GOOS == "windows" {
		return "augment-guard.cmd"
	}
	return "augment-guard.sh"
}

func augmentAuditWrapperName() string {
	if runtime.GOOS == "windows" {
		return "augment-hook.cmd"
	}
	return "augment-hook.sh"
}

func augmentHomeDirectory() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return home, nil
}

func augmentRuntimeRoot() (string, error) {
	home, err := augmentHomeDirectory()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".elydora"), nil
}

func augmentConfigPath() (string, error) {
	home, err := augmentHomeDirectory()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".augment", "settings.json"), nil
}

func readAugmentConfig() (augmentDocument, error) {
	configPath, err := augmentConfigPath()
	if err != nil {
		return augmentDocument{}, err
	}
	root, exists, err := readHookJSONObject(configPath, "Auggie settings")
	if err != nil {
		return augmentDocument{}, err
	}
	hooks, err := readAugmentHooks(root)
	if err != nil {
		return augmentDocument{}, err
	}
	return augmentDocument{
		exists:     exists,
		configPath: configPath,
		root:       root,
		hooks:      hooks,
	}, nil
}

func validateAugmentMatchers(hooks augmentHooks, nodePath string) error {
	events := make([]string, 0, len(hooks))
	for event := range hooks {
		events = append(events, event)
	}
	sort.Strings(events)
	for _, event := range events {
		for groupIndex, group := range hooks[event] {
			matcher, exists := group.object["matcher"].(string)
			if !exists {
				continue
			}
			// #nosec G204 -- nodePath is resolved with exec.LookPath and matcher data uses stdin.
			command := exec.Command(
				nodePath, "--input-type=module", "--eval", augmentRegexValidator,
			)
			command.Stdin = strings.NewReader(matcher)
			output, err := command.CombinedOutput()
			if err == nil {
				continue
			}
			message := strings.TrimSpace(string(output))
			if message == "" {
				message = err.Error()
			}
			return fmt.Errorf(
				"Auggie settings group hooks.%s[%d] matcher "+
					"must be a valid JavaScript regular expression: %s",
				event, groupIndex, message,
			)
		}
	}
	return nil
}

func quoteAugmentWindowsCommand(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}

func quoteAugmentBatchArgument(value string) string {
	return `"` + strings.ReplaceAll(value, "%", "%%") + `"`
}

func buildAugmentCommand(wrapperPath string) string {
	if runtime.GOOS == "windows" {
		return quoteAugmentWindowsCommand(wrapperPath)
	}
	return quotePOSIXArgument(wrapperPath)
}

func buildAugmentWrapper(runtimePath, scriptPath string) []byte {
	if runtime.GOOS == "windows" {
		return []byte(
			"@echo off\r\n" +
				quoteAugmentBatchArgument(runtimePath) + " " +
				quoteAugmentBatchArgument(scriptPath) + "\r\n" +
				"exit /b %errorlevel%\r\n",
		)
	}
	return []byte(
		"#!/bin/sh\nexec " +
			quotePOSIXArgument(runtimePath) + " " +
			quotePOSIXArgument(scriptPath) + "\n",
	)
}

func buildAugmentHandler(wrapperPath string) map[string]any {
	return map[string]any{
		"type":    "command",
		"command": buildAugmentCommand(wrapperPath),
		"timeout": augmentHookTimeout,
	}
}

func buildAugmentGroup(handler map[string]any) augmentGroup {
	return augmentGroup{
		object:   map[string]any{"matcher": ".*"},
		handlers: []map[string]any{handler},
	}
}

func resolveAugmentWrapperPaths(runtimeRoot, agentID string) augmentWrapperPaths {
	agentDirectory := filepath.Join(runtimeRoot, agentID)
	return augmentWrapperPaths{
		guard: filepath.Join(agentDirectory, augmentGuardWrapperName()),
		audit: filepath.Join(agentDirectory, augmentAuditWrapperName()),
	}
}

func managedAugmentIDs(
	groups []augmentGroup,
	wrapperName, runtimeRoot string,
) map[string]string {
	result := map[string]string{}
	for _, group := range groups {
		for _, handler := range group.handlers {
			agentID, managed := managedAugmentAgentID(handler, wrapperName, runtimeRoot)
			if managed {
				key := agentID
				if runtime.GOOS == "windows" {
					key = strings.ToLower(key)
				}
				result[key] = agentID
			}
		}
	}
	return result
}

func augmentRuntimeContracts(
	hooks augmentHooks,
	runtimeRoot string,
) []augmentRuntimeContract {
	guards := managedAugmentIDs(
		hooks["PreToolUse"], augmentGuardWrapperName(), runtimeRoot,
	)
	audits := managedAugmentIDs(
		hooks["PostToolUse"], augmentAuditWrapperName(), runtimeRoot,
	)
	keys := make([]string, 0, len(guards))
	for key := range guards {
		if _, exists := audits[key]; exists {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	contracts := make([]augmentRuntimeContract, 0, len(keys))
	for _, key := range keys {
		agentID := guards[key]
		agentDirectory := filepath.Join(runtimeRoot, agentID)
		contracts = append(contracts, augmentRuntimeContract{
			agentID:      agentID,
			guardPath:    filepath.Join(agentDirectory, augmentGuardScript),
			auditPath:    filepath.Join(agentDirectory, augmentAuditScript),
			guardWrapper: filepath.Join(agentDirectory, augmentGuardWrapperName()),
			auditWrapper: filepath.Join(agentDirectory, augmentAuditWrapperName()),
		})
	}
	return contracts
}

func augmentRuntimeFilesExist(
	contracts []augmentRuntimeContract,
	runtimeRoot string,
) (bool, error) {
	entries, err := os.ReadDir(runtimeRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf(
			"read Elydora runtime directory at %s: %w", runtimeRoot, err,
		)
	}
	for _, contract := range contracts {
		var entryName string
		for _, entry := range entries {
			if entry.IsDir() && sameAugmentAgentID(entry.Name(), contract.agentID) {
				entryName = entry.Name()
				break
			}
		}
		if entryName == "" {
			continue
		}
		agentDirectory := filepath.Join(runtimeRoot, entryName)
		configPath := filepath.Join(agentDirectory, "config.json")
		config, exists, err := readHookJSONObject(
			configPath, "Elydora runtime config",
		)
		if err != nil {
			return false, err
		}
		if !exists {
			continue
		}
		agentName, ok := config["agent_name"].(string)
		if !ok {
			return false, fmt.Errorf(
				`Elydora runtime config at %s field "agent_name" must be a string`,
				configPath,
			)
		}
		if agentName != augmentAgentKey {
			continue
		}
		files := []struct {
			path  string
			label string
		}{
			{contract.guardPath, "Elydora guard runtime"},
			{contract.auditPath, "Elydora audit runtime"},
			{contract.guardWrapper, "Auggie guard wrapper"},
			{contract.auditWrapper, "Auggie audit wrapper"},
		}
		complete := true
		for _, file := range files {
			exists, err := regularFileExists(file.path, file.label)
			if err != nil {
				return false, err
			}
			if !exists {
				complete = false
				break
			}
		}
		if complete {
			return true, nil
		}
	}
	return false, nil
}
