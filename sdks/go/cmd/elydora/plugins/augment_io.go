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

func augmentConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".augment", "settings.json"), nil
}

func readAugmentDocument() (*augmentDocument, error) {
	configPath, err := augmentConfigPath()
	if err != nil {
		return nil, err
	}
	if _, err := managedPhysicalDirectoryExists(
		filepath.Dir(configPath),
		"Auggie configuration directory",
	); err != nil {
		return nil, err
	}
	snapshot, err := readManagedFile(
		configPath,
		"Auggie user settings",
		maxManagedSourceBytes,
	)
	if err != nil {
		return nil, err
	}
	if snapshot == nil {
		return createAugmentDocument(configPath), nil
	}
	return parseAugmentDocument(configPath, snapshot.contents)
}

func prepareRenderedAugmentChange(
	rendered *augmentRenderedDocument,
) (*fileChange, error) {
	if rendered == nil || !rendered.changed {
		return nil, nil
	}
	return prepareSourceChange(
		rendered.document.configPath,
		"Auggie user settings",
		rendered.document.raw,
		rendered.document.exists,
		rendered.next,
		0600,
		rendered.remove,
	)
}

func writeAugmentChanges(
	changes []*fileChange,
	label string,
	rename renameFunc,
	runtimeRoot string,
	agentDirectory string,
	settingsDirectory string,
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
	if settingsDirectory != "" {
		if err := ensureManagedDirectory(
			settingsDirectory,
			"Auggie configuration directory",
		); err != nil {
			return err
		}
	}
	return writeChanges(changes, label, rename)
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
				event,
				groupIndex,
				message,
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

func resolveAugmentWrapperPaths(agentDirectory string) augmentWrapperPaths {
	return augmentWrapperPaths{
		guard: filepath.Join(agentDirectory, augmentGuardWrapperName()),
		audit: filepath.Join(agentDirectory, augmentAuditWrapperName()),
	}
}

func requireAugmentAbsoluteNode(nodePath string) error {
	if !filepath.IsAbs(nodePath) || !isClaudeNodeExecutable(nodePath) {
		return fmt.Errorf("Auggie hooks require an absolute Node.js executable path")
	}
	return nil
}
