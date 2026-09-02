package plugins

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

type kiroIdeRuntimeReference struct {
	agentID    string
	scriptPath string
}

func sameKiroIdePath(left, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if absolute, err := filepath.Abs(left); err == nil {
		left = absolute
	}
	if absolute, err := filepath.Abs(right); err == nil {
		right = absolute
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func sameKiroIdeAgentID(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func sameKiroIdeFileName(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func buildKiroIdeCommand(runtimePath, scriptPath string) (string, error) {
	if !filepath.IsAbs(runtimePath) || !filepath.IsAbs(scriptPath) {
		return "", fmt.Errorf(
			"Kiro IDE hook commands require absolute runtime and script paths",
		)
	}
	if runtime.GOOS == "windows" {
		return codexWindowsCommand(runtimePath, scriptPath), nil
	}
	return quotePOSIXArgument(runtimePath) + " " + quotePOSIXArgument(scriptPath), nil
}

func parseKiroIdeCommand(command string) (string, string, bool) {
	if runtime.GOOS == "windows" {
		return parseCodexWindowsCommand(command)
	}
	return parseCodexPOSIXCommand(command)
}

func kiroIdeRuntimeReferenceForCommand(
	command string,
	scriptName string,
) (*kiroIdeRuntimeReference, error) {
	runtimePath, scriptPath, ok := parseKiroIdeCommand(command)
	if !ok || !filepath.IsAbs(runtimePath) || !filepath.IsAbs(scriptPath) ||
		!isCodexNodeExecutable(runtimePath) ||
		!sameKiroIdeFileName(filepath.Base(scriptPath), scriptName) {
		return nil, nil
	}
	agentDirectory := filepath.Dir(scriptPath)
	runtimeRoot, err := AgentRuntimeRoot()
	if err != nil {
		return nil, err
	}
	if !sameKiroIdePath(filepath.Dir(agentDirectory), runtimeRoot) {
		return nil, nil
	}
	agentID := filepath.Base(agentDirectory)
	if agentID == "" || agentID == "." || agentID == ".." {
		return nil, nil
	}
	return &kiroIdeRuntimeReference{agentID: agentID, scriptPath: scriptPath}, nil
}

func currentKiroIdeReference(
	hook map[string]any,
	nodePath string,
) (*kiroIdeRuntimeReference, error) {
	reference, err := managedKiroIdeReference(hook)
	if err != nil || reference == nil {
		return reference, err
	}
	action := hook["action"].(map[string]any)
	command := action["command"].(string)
	expected, err := buildKiroIdeCommand(nodePath, reference.scriptPath)
	if err != nil {
		return nil, err
	}
	if command != expected {
		return nil, nil
	}
	return reference, nil
}

func legacyKiroIdeGoReference(
	command any,
	scriptName string,
) (*kiroIdeRuntimeReference, error) {
	text, ok := command.(string)
	if !ok || !strings.HasPrefix(text, "node ") {
		return nil, nil
	}
	scriptPath := strings.TrimPrefix(text, "node ")
	if !filepath.IsAbs(scriptPath) ||
		!sameKiroIdeFileName(filepath.Base(scriptPath), scriptName) {
		return nil, nil
	}
	agentDirectory := filepath.Dir(scriptPath)
	runtimeRoot, err := AgentRuntimeRoot()
	if err != nil {
		return nil, err
	}
	if !sameKiroIdePath(filepath.Dir(agentDirectory), runtimeRoot) {
		return nil, nil
	}
	agentID := filepath.Base(agentDirectory)
	if agentID == "" || agentID == "." || agentID == ".." {
		return nil, nil
	}
	return &kiroIdeRuntimeReference{agentID: agentID, scriptPath: scriptPath}, nil
}
