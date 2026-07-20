package plugins

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const (
	copilotAgentKey        = "copilot"
	copilotGuardScript     = "guard.js"
	copilotAuditScript     = "hook.js"
	copilotConfigFile      = "elydora-audit.json"
	copilotHookTimeout     = float64(10)
	copilotLegacyTimeout   = float64(5)
	copilotPOSIXApostrophe = `'"'"'`
)

var copilotManagedEvents = []struct {
	event  string
	script string
}{
	{"preToolUse", copilotGuardScript},
	{"postToolUse", copilotAuditScript},
	{"postToolUseFailure", copilotAuditScript},
}

type copilotHooks map[string][]map[string]any

type copilotDocument struct {
	exists        bool
	filePath      string
	root          map[string]any
	hooks         copilotHooks
	raw           []byte
	snapshot      *managedFileSnapshot
	hooksDisabled bool
}

type copilotSourcePrecondition struct {
	filePath string
	label    string
	snapshot *managedFileSnapshot
}

type copilotSources struct {
	user                  *copilotDocument
	legacy                *copilotDocument
	disabledBy            string
	settingsPreconditions []copilotSourcePrecondition
}

type copilotRenderedDocument struct {
	document *copilotDocument
	changed  bool
	next     []byte
	remove   bool
}

type copilotRuntimeContract struct {
	agentID   string
	guardPath string
	auditPath string
}

type copilotManagedEntry struct {
	agentID    string
	scriptPath string
}

func cloneCopilotObject(value map[string]any) map[string]any {
	clone := make(map[string]any, len(value))
	for key, item := range value {
		clone[key] = item
	}
	return clone
}

func cloneCopilotHooks(source copilotHooks) copilotHooks {
	clone := make(copilotHooks, len(source))
	for event, handlers := range source {
		copied := make([]map[string]any, len(handlers))
		copy(copied, handlers)
		clone[event] = copied
	}
	return clone
}

func sameCopilotPath(left, right string) bool {
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

func sameCopilotAgentID(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func sameCopilotFileName(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func quoteCopilotPowerShell(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func buildCopilotHandler(nodePath, scriptPath string) map[string]any {
	return map[string]any{
		"type":       "command",
		"bash":       quotePOSIXArgument(nodePath) + " " + quotePOSIXArgument(scriptPath),
		"powershell": "& " + quoteCopilotPowerShell(nodePath) + " " + quoteCopilotPowerShell(scriptPath) + "; exit $LASTEXITCODE",
		"timeoutSec": copilotHookTimeout,
	}
}

func readCopilotPOSIXArgument(command string, start int) (string, int, bool) {
	if start >= len(command) || command[start] != '\'' {
		return "", start, false
	}
	var value strings.Builder
	for index := start + 1; index < len(command); {
		if strings.HasPrefix(command[index:], copilotPOSIXApostrophe) {
			value.WriteByte('\'')
			index += len(copilotPOSIXApostrophe)
			continue
		}
		if command[index] == '\'' {
			return value.String(), index + 1, true
		}
		value.WriteByte(command[index])
		index++
	}
	return "", start, false
}

func readCopilotPowerShellArgument(command string, start int) (string, int, bool) {
	if start >= len(command) || command[start] != '\'' {
		return "", start, false
	}
	var value strings.Builder
	for index := start + 1; index < len(command); index++ {
		if command[index] != '\'' {
			value.WriteByte(command[index])
			continue
		}
		if index+1 < len(command) && command[index+1] == '\'' {
			value.WriteByte('\'')
			index++
			continue
		}
		return value.String(), index + 1, true
	}
	return "", start, false
}

func parseCopilotBash(command string) (string, string, bool) {
	executable, next, ok := readCopilotPOSIXArgument(command, 0)
	if !ok || next >= len(command) || command[next] != ' ' {
		return "", "", false
	}
	script, end, ok := readCopilotPOSIXArgument(command, next+1)
	return executable, script, ok && end == len(command)
}

func parseCopilotPowerShell(command string) (string, string, bool) {
	if !strings.HasPrefix(command, "& ") {
		return "", "", false
	}
	executable, next, ok := readCopilotPowerShellArgument(command, 2)
	if !ok || next >= len(command) || command[next] != ' ' {
		return "", "", false
	}
	script, end, ok := readCopilotPowerShellArgument(command, next+1)
	return executable, script, ok && command[end:] == "; exit $LASTEXITCODE"
}

func parseCopilotLegacyCommand(command string) (string, bool) {
	command = strings.TrimSpace(command)
	separator := strings.IndexAny(command, " \t")
	if separator < 0 {
		return "", false
	}
	executable := command[:separator]
	if !strings.EqualFold(executable, "node") && !strings.EqualFold(executable, "node.exe") {
		return "", false
	}
	script := strings.TrimSpace(command[separator:])
	if len(script) >= 2 && ((script[0] == '"' && script[len(script)-1] == '"') ||
		(script[0] == '\'' && script[len(script)-1] == '\'')) {
		script = script[1 : len(script)-1]
	}
	return script, script != ""
}

func isCopilotNodeExecutable(path string) bool {
	name := filepath.Base(path)
	return name == "node" || strings.EqualFold(name, "node.exe")
}

func copilotManagedScriptPath(handler map[string]any) (string, bool) {
	if len(handler) != 4 || handler["type"] != "command" {
		return "", false
	}
	bash, bashOK := handler["bash"].(string)
	powershell, powershellOK := handler["powershell"].(string)
	if !bashOK || !powershellOK {
		return "", false
	}
	if handler["timeoutSec"] == copilotHookTimeout {
		bashNode, bashScript, bashParsed := parseCopilotBash(bash)
		psNode, psScript, psParsed := parseCopilotPowerShell(powershell)
		if bashParsed && psParsed && filepath.IsAbs(bashNode) && filepath.IsAbs(bashScript) &&
			isCopilotNodeExecutable(bashNode) && sameCopilotPath(bashNode, psNode) &&
			sameCopilotPath(bashScript, psScript) {
			return bashScript, true
		}
	}
	if handler["timeoutSec"] != copilotLegacyTimeout || bash != powershell {
		return "", false
	}
	return parseCopilotLegacyCommand(bash)
}

func copilotManagedEntryForHandler(
	handler map[string]any,
	scriptName string,
) (*copilotManagedEntry, error) {
	scriptPath, managed := copilotManagedScriptPath(handler)
	if !managed || !filepath.IsAbs(scriptPath) ||
		!sameCopilotFileName(filepath.Base(scriptPath), scriptName) {
		return nil, nil
	}
	runtimeRoot, err := AgentRuntimeRoot()
	if err != nil {
		return nil, err
	}
	agentDirectory := filepath.Dir(scriptPath)
	if !sameCopilotPath(filepath.Dir(agentDirectory), runtimeRoot) {
		return nil, nil
	}
	agentID := filepath.Base(agentDirectory)
	if agentID == "" || agentID == "." || agentID == ".." {
		return nil, nil
	}
	return &copilotManagedEntry{agentID: agentID, scriptPath: scriptPath}, nil
}

func removeManagedCopilotHooks(
	hooks copilotHooks,
	agentID string,
) (copilotHooks, error) {
	result := cloneCopilotHooks(hooks)
	for _, contract := range copilotManagedEvents {
		kept := make([]map[string]any, 0, len(result[contract.event]))
		for _, handler := range result[contract.event] {
			entry, err := copilotManagedEntryForHandler(handler, contract.script)
			if err != nil {
				return nil, err
			}
			remove := entry != nil &&
				(agentID == "" || sameCopilotAgentID(entry.agentID, agentID))
			if !remove {
				kept = append(kept, handler)
			}
		}
		if len(kept) == 0 {
			delete(result, contract.event)
		} else {
			result[contract.event] = kept
		}
	}
	return result, nil
}

func parseCopilotDocument(
	filePath string,
	snapshot *managedFileSnapshot,
	label string,
) (*copilotDocument, error) {
	if snapshot == nil {
		return nil, fmt.Errorf("%s snapshot is required: %s", label, filePath)
	}
	documentLabel := fmt.Sprintf("%s at %s", label, filePath)
	root, err := decodeStrictJSONObject(snapshot.contents, documentLabel)
	if err != nil {
		return nil, err
	}
	if root["version"] != float64(1) {
		return nil, fmt.Errorf("%s must declare version 1", documentLabel)
	}
	disabled := false
	if value, exists := root["disableAllHooks"]; exists {
		var valid bool
		disabled, valid = value.(bool)
		if !valid {
			return nil, fmt.Errorf(`%s field "disableAllHooks" must be a boolean`, documentLabel)
		}
	}
	hooks := copilotHooks{}
	if value, exists := root["hooks"]; exists {
		hooks, err = validateCopilotHooks(value, documentLabel)
		if err != nil {
			return nil, err
		}
	}
	return &copilotDocument{
		exists: true, filePath: filePath, root: root, hooks: hooks,
		raw: append([]byte(nil), snapshot.contents...), snapshot: snapshot,
		hooksDisabled: disabled,
	}, nil
}

func createCopilotDocument(filePath string) *copilotDocument {
	return &copilotDocument{
		filePath: filePath,
		root:     map[string]any{},
		hooks:    copilotHooks{},
	}
}

func emptyOwnedCopilotDocument(root map[string]any, hooks copilotHooks) bool {
	if len(hooks) != 0 {
		return false
	}
	for key := range root {
		if key != "version" && key != "hooks" {
			return false
		}
	}
	return true
}

func renderCopilotDocument(
	document *copilotDocument,
	hooks copilotHooks,
) (*copilotRenderedDocument, error) {
	if !document.exists && len(hooks) == 0 {
		return &copilotRenderedDocument{document: document}, nil
	}
	if document.exists && emptyOwnedCopilotDocument(document.root, hooks) {
		return &copilotRenderedDocument{
			document: document, changed: true, remove: true,
		}, nil
	}
	root := cloneCopilotObject(document.root)
	root["version"] = float64(1)
	if len(hooks) == 0 {
		delete(root, "hooks")
	} else {
		root["hooks"] = hooks
	}
	next, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode GitHub Copilot hooks: %w", err)
	}
	next = append(next, '\n')
	if _, err := parseCopilotDocument(
		document.filePath,
		&managedFileSnapshot{contents: next},
		"GitHub Copilot rendered hooks",
	); err != nil {
		return nil, fmt.Errorf("validate rendered GitHub Copilot hooks: %w", err)
	}
	return &copilotRenderedDocument{
		document: document,
		changed:  !document.exists || !bytes.Equal(next, document.raw),
		next:     next,
	}, nil
}

func copilotEntryKey(agentID string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(agentID)
	}
	return agentID
}

func copilotManagedEntries(
	handlers []map[string]any,
	scriptName string,
) (map[string][]copilotManagedEntry, error) {
	result := map[string][]copilotManagedEntry{}
	for _, handler := range handlers {
		entry, err := copilotManagedEntryForHandler(handler, scriptName)
		if err != nil {
			return nil, err
		}
		if entry != nil {
			key := copilotEntryKey(entry.agentID)
			result[key] = append(result[key], *entry)
		}
	}
	return result, nil
}

func copilotRuntimeContracts(hooks copilotHooks) ([]copilotRuntimeContract, error) {
	guards, err := copilotManagedEntries(hooks["preToolUse"], copilotGuardScript)
	if err != nil {
		return nil, err
	}
	successes, err := copilotManagedEntries(hooks["postToolUse"], copilotAuditScript)
	if err != nil {
		return nil, err
	}
	failures, err := copilotManagedEntries(
		hooks["postToolUseFailure"], copilotAuditScript,
	)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(guards))
	for key := range guards {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	contracts := make([]copilotRuntimeContract, 0, len(keys))
	for _, key := range keys {
		guard := guards[key]
		success := successes[key]
		failure := failures[key]
		if len(guard) != 1 || len(success) != 1 || len(failure) != 1 ||
			!sameCopilotPath(success[0].scriptPath, failure[0].scriptPath) {
			continue
		}
		contracts = append(contracts, copilotRuntimeContract{
			agentID: guard[0].agentID, guardPath: guard[0].scriptPath,
			auditPath: success[0].scriptPath,
		})
	}
	return contracts, nil
}
