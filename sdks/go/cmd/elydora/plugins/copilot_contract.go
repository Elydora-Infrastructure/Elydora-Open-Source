package plugins

import (
	"encoding/json"
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

type copilotHooks map[string][]map[string]any

type copilotDocument struct {
	exists   bool
	filePath string
	root     map[string]any
	hooks    copilotHooks
	raw      []byte
	disabled bool
}

type copilotSources struct {
	user   *copilotDocument
	legacy *copilotDocument
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

func cloneCopilotHooks(source copilotHooks) copilotHooks {
	clone := make(copilotHooks, len(source))
	for event, handlers := range source {
		clone[event] = append([]map[string]any(nil), handlers...)
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

func copilotManagedAgentID(
	handler map[string]any,
	scriptName string,
	runtimeRoot string,
) (string, bool) {
	scriptPath, managed := copilotManagedScriptPath(handler)
	if !managed || !filepath.IsAbs(scriptPath) || filepath.Base(scriptPath) != scriptName {
		return "", false
	}
	agentDirectory := filepath.Dir(scriptPath)
	if !sameCopilotPath(filepath.Dir(agentDirectory), runtimeRoot) {
		return "", false
	}
	agentID := filepath.Base(agentDirectory)
	return agentID, agentID != "" && agentID != "." && agentID != ".."
}

func removeManagedCopilotHooks(
	hooks copilotHooks,
	runtimeRoot string,
	agentID string,
) copilotHooks {
	result := cloneCopilotHooks(hooks)
	for _, contract := range []struct{ event, script string }{
		{"preToolUse", copilotGuardScript},
		{"postToolUse", copilotAuditScript},
	} {
		kept := make([]map[string]any, 0, len(result[contract.event]))
		for _, handler := range result[contract.event] {
			managedID, managed := copilotManagedAgentID(handler, contract.script, runtimeRoot)
			if managed && (agentID == "" || sameCopilotAgentID(managedID, agentID)) {
				continue
			}
			kept = append(kept, handler)
		}
		if len(kept) == 0 {
			delete(result, contract.event)
		} else {
			result[contract.event] = kept
		}
	}
	return result
}

func renderCopilotDocument(
	document *copilotDocument,
	hooks copilotHooks,
) (*copilotRenderedDocument, error) {
	if !document.exists && len(hooks) == 0 {
		return &copilotRenderedDocument{document: document}, nil
	}
	if document.exists && len(hooks) == 0 {
		owned := true
		for key := range document.root {
			if key != "version" && key != "hooks" {
				owned = false
				break
			}
		}
		if owned {
			return &copilotRenderedDocument{document: document, changed: true, remove: true}, nil
		}
	}
	root := make(map[string]any, len(document.root)+1)
	for key, value := range document.root {
		root[key] = value
	}
	root["version"] = float64(1)
	if len(hooks) == 0 {
		delete(root, "hooks")
	} else {
		root["hooks"] = hooks
	}
	next, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, err
	}
	next = append(next, '\n')
	return &copilotRenderedDocument{
		document: document,
		changed:  !document.exists || string(next) != string(document.raw),
		next:     next,
	}, nil
}

func copilotRuntimeContracts(
	hooks copilotHooks,
	runtimeRoot string,
) []copilotRuntimeContract {
	guards := managedCopilotIDs(hooks["preToolUse"], copilotGuardScript, runtimeRoot)
	audits := managedCopilotIDs(hooks["postToolUse"], copilotAuditScript, runtimeRoot)
	keys := make([]string, 0, len(guards))
	for key := range guards {
		if _, exists := audits[key]; exists {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	contracts := make([]copilotRuntimeContract, 0, len(keys))
	for _, key := range keys {
		agentID := guards[key]
		agentDirectory := filepath.Join(runtimeRoot, agentID)
		contracts = append(contracts, copilotRuntimeContract{
			agentID:   agentID,
			guardPath: filepath.Join(agentDirectory, copilotGuardScript),
			auditPath: filepath.Join(agentDirectory, copilotAuditScript),
		})
	}
	return contracts
}

func managedCopilotIDs(
	handlers []map[string]any,
	scriptName string,
	runtimeRoot string,
) map[string]string {
	result := map[string]string{}
	for _, handler := range handlers {
		agentID, managed := copilotManagedAgentID(handler, scriptName, runtimeRoot)
		if !managed {
			continue
		}
		key := agentID
		if runtime.GOOS == "windows" {
			key = strings.ToLower(agentID)
		}
		result[key] = agentID
	}
	return result
}
