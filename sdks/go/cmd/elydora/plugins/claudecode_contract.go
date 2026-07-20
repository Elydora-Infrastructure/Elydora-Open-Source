package plugins

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"reflect"
	"sort"
)

const (
	claudeAgentKey           = "claudecode"
	claudeConfigFile         = "settings.json"
	claudeGuardScript        = "guard.js"
	claudeAuditScript        = "hook.js"
	claudeHookTimeout        = float64(10)
	claudeGuardStatusMessage = "Checking Elydora agent state"
	claudeAuditStatusMessage = "Recording Elydora tool use"
)

var claudeHookEvents = stringSet(
	"SessionStart", "Setup", "InstructionsLoaded", "UserPromptSubmit",
	"UserPromptExpansion", "MessageDisplay", "PreToolUse", "PermissionRequest",
	"PostToolUse", "PostToolUseFailure", "PostToolBatch", "PermissionDenied",
	"Notification", "SubagentStart", "SubagentStop", "TaskCreated",
	"TaskCompleted", "Stop", "StopFailure", "TeammateIdle", "ConfigChange",
	"CwdChanged", "FileChanged", "WorktreeCreate", "WorktreeRemove",
	"PreCompact", "PostCompact", "SessionEnd", "Elicitation",
	"ElicitationResult",
)

var claudeHandlerKeys = map[string]map[string]struct{}{
	"command": stringSet(
		"if", "once", "statusMessage", "timeout", "args", "async",
		"asyncRewake", "command", "rewakeMessage", "rewakeSummary",
		"shell", "type",
	),
	"prompt": stringSet(
		"if", "once", "statusMessage", "timeout", "continueOnBlock", "model",
		"prompt", "type",
	),
	"agent": stringSet(
		"if", "once", "statusMessage", "timeout", "model", "prompt", "type",
	),
	"http": stringSet(
		"if", "once", "statusMessage", "timeout", "allowedEnvVars", "headers",
		"type", "url",
	),
	"mcp_tool": stringSet(
		"if", "once", "statusMessage", "timeout", "input", "server", "tool",
		"type",
	),
}

type claudeGroup struct {
	object   map[string]any
	handlers []map[string]any
}

type claudeHooks map[string][]claudeGroup

type claudeDocument struct {
	exists        bool
	filePath      string
	root          map[string]any
	hooks         claudeHooks
	hooksDisabled bool
	raw           []byte
}

type claudeRenderedDocument struct {
	document *claudeDocument
	changed  bool
	next     []byte
	remove   bool
}

type claudeRuntimeContract struct {
	agentID   string
	guardPath string
	auditPath string
}

func cloneClaudeObject(value map[string]any) map[string]any {
	clone := make(map[string]any, len(value))
	for key, item := range value {
		clone[key] = item
	}
	return clone
}

func cloneClaudeHooks(source claudeHooks) claudeHooks {
	clone := make(claudeHooks, len(source))
	for event, groups := range source {
		clone[event] = append([]claudeGroup(nil), groups...)
	}
	return clone
}

func validateClaudeKnownKeys(
	value map[string]any,
	allowed map[string]struct{},
	label string,
) error {
	fields := make([]string, 0, len(value))
	for field := range value {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	for _, field := range fields {
		if _, ok := allowed[field]; !ok {
			return fmt.Errorf(`%s contains unsupported field %q`, label, field)
		}
	}
	return nil
}

func requireClaudeString(value map[string]any, field, label string) (string, error) {
	text, ok := value[field].(string)
	if !ok || text == "" {
		return "", fmt.Errorf(`%s field %q must be a non-empty string`, label, field)
	}
	return text, nil
}

func validateClaudeOptionalString(value map[string]any, field, label string) error {
	if item, exists := value[field]; exists {
		if _, ok := item.(string); !ok {
			return fmt.Errorf(`%s field %q must be a string`, label, field)
		}
	}
	return nil
}

func validateClaudeOptionalBoolean(value map[string]any, field, label string) error {
	if item, exists := value[field]; exists {
		if _, ok := item.(bool); !ok {
			return fmt.Errorf(`%s field %q must be a boolean`, label, field)
		}
	}
	return nil
}

func validateClaudeCommonFields(value map[string]any, label string) error {
	for _, field := range []string{"if", "statusMessage"} {
		if err := validateClaudeOptionalString(value, field, label); err != nil {
			return err
		}
	}
	if err := validateClaudeOptionalBoolean(value, "once", label); err != nil {
		return err
	}
	if item, exists := value["timeout"]; exists {
		timeout, ok := item.(float64)
		if !ok || math.IsNaN(timeout) || math.IsInf(timeout, 0) || timeout <= 0 {
			return fmt.Errorf("%s timeout must be a positive finite number", label)
		}
	}
	return nil
}

func validateClaudeStringArray(value any, field, label string, nonEmpty bool) error {
	items, ok := value.([]any)
	if !ok {
		return fmt.Errorf(`%s field %q must be an array of strings`, label, field)
	}
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			return fmt.Errorf(`%s field %q must be an array of strings`, label, field)
		}
		if nonEmpty && text == "" {
			return fmt.Errorf(
				`%s field %q must contain non-empty strings`,
				label,
				field,
			)
		}
	}
	return nil
}

func validateClaudeHTTPHandler(handler map[string]any, label string) error {
	rawURL, err := requireClaudeString(handler, "url", label)
	if err != nil {
		return err
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf(`%s field "url" must be a valid URL`, label)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return fmt.Errorf(`%s field "url" must use HTTP or HTTPS`, label)
	}
	if headers, exists := handler["headers"]; exists {
		object, ok := headers.(map[string]any)
		if !ok {
			return fmt.Errorf(`%s field "headers" must map names to strings`, label)
		}
		for _, item := range object {
			if _, ok := item.(string); !ok {
				return fmt.Errorf(`%s field "headers" must map names to strings`, label)
			}
		}
	}
	if allowed, exists := handler["allowedEnvVars"]; exists {
		return validateClaudeStringArray(allowed, "allowedEnvVars", label, true)
	}
	return nil
}

func validateClaudeHandler(
	value any,
	event string,
	groupIndex int,
	handlerIndex int,
) (map[string]any, error) {
	label := fmt.Sprintf(
		"Claude Code settings handler hooks.%s[%d].hooks[%d]",
		event,
		groupIndex,
		handlerIndex,
	)
	handler, ok := value.(map[string]any)
	if !ok || handler == nil {
		return nil, fmt.Errorf("%s must be an object", label)
	}
	handlerType, ok := handler["type"].(string)
	allowed, supported := claudeHandlerKeys[handlerType]
	if !ok || !supported {
		return nil, fmt.Errorf(`%s has unsupported type %q`, label, fmt.Sprint(handler["type"]))
	}
	if err := validateClaudeKnownKeys(handler, allowed, label); err != nil {
		return nil, err
	}
	if err := validateClaudeCommonFields(handler, label); err != nil {
		return nil, err
	}
	switch handlerType {
	case "command":
		if _, err := requireClaudeString(handler, "command", label); err != nil {
			return nil, err
		}
		if args, exists := handler["args"]; exists {
			if err := validateClaudeStringArray(args, "args", label, false); err != nil {
				return nil, err
			}
		}
		for _, field := range []string{"async", "asyncRewake"} {
			if err := validateClaudeOptionalBoolean(handler, field, label); err != nil {
				return nil, err
			}
		}
		for _, field := range []string{"rewakeMessage", "rewakeSummary"} {
			if _, exists := handler[field]; exists {
				if _, err := requireClaudeString(handler, field, label); err != nil {
					return nil, err
				}
			}
		}
		if shell, exists := handler["shell"]; exists &&
			shell != "bash" && shell != "powershell" {
			return nil, fmt.Errorf(
				`%s field "shell" must be "bash" or "powershell"`,
				label,
			)
		}
	case "prompt", "agent":
		if _, err := requireClaudeString(handler, "prompt", label); err != nil {
			return nil, err
		}
		if err := validateClaudeOptionalString(handler, "model", label); err != nil {
			return nil, err
		}
		if handlerType == "prompt" {
			if err := validateClaudeOptionalBoolean(
				handler,
				"continueOnBlock",
				label,
			); err != nil {
				return nil, err
			}
		}
	case "http":
		if err := validateClaudeHTTPHandler(handler, label); err != nil {
			return nil, err
		}
	case "mcp_tool":
		for _, field := range []string{"server", "tool"} {
			if _, err := requireClaudeString(handler, field, label); err != nil {
				return nil, err
			}
		}
		if input, exists := handler["input"]; exists {
			if _, ok := input.(map[string]any); !ok {
				return nil, fmt.Errorf(`%s field "input" must be an object`, label)
			}
		}
	}
	return cloneClaudeObject(handler), nil
}

func validateClaudeGroup(value any, event string, groupIndex int) (claudeGroup, error) {
	label := fmt.Sprintf("Claude Code settings matcher group hooks.%s[%d]", event, groupIndex)
	object, ok := value.(map[string]any)
	if !ok || object == nil {
		return claudeGroup{}, fmt.Errorf("%s must be an object", label)
	}
	if err := validateClaudeKnownKeys(
		object,
		stringSet("hooks", "matcher"),
		label,
	); err != nil {
		return claudeGroup{}, err
	}
	if matcher, exists := object["matcher"]; exists {
		if _, ok := matcher.(string); !ok {
			return claudeGroup{}, fmt.Errorf("%s matcher must be a string", label)
		}
	}
	values, ok := object["hooks"].([]any)
	if !ok {
		return claudeGroup{}, fmt.Errorf("%s must contain a hooks array", label)
	}
	handlers := make([]map[string]any, 0, len(values))
	for handlerIndex, value := range values {
		handler, err := validateClaudeHandler(value, event, groupIndex, handlerIndex)
		if err != nil {
			return claudeGroup{}, err
		}
		handlers = append(handlers, handler)
	}
	return claudeGroup{object: cloneClaudeObject(object), handlers: handlers}, nil
}

func readClaudeHooks(root map[string]any) (claudeHooks, error) {
	value, exists := root["hooks"]
	if !exists {
		return claudeHooks{}, nil
	}
	object, ok := value.(map[string]any)
	if !ok || object == nil {
		return nil, fmt.Errorf(`Claude Code settings field "hooks" must be an object`)
	}
	hooks := make(claudeHooks, len(object))
	for event, groupsValue := range object {
		if _, supported := claudeHookEvents[event]; !supported {
			return nil, fmt.Errorf(
				`Claude Code settings contains unsupported hook event %q`,
				event,
			)
		}
		values, ok := groupsValue.([]any)
		if !ok {
			return nil, fmt.Errorf(
				`Claude Code settings field "hooks.%s" must be an array`,
				event,
			)
		}
		groups := make([]claudeGroup, 0, len(values))
		for index, value := range values {
			group, err := validateClaudeGroup(value, event, index)
			if err != nil {
				return nil, err
			}
			groups = append(groups, group)
		}
		hooks[event] = groups
	}
	return hooks, nil
}

func parseClaudeDocument(filePath string, raw []byte) (*claudeDocument, error) {
	label := fmt.Sprintf("Claude Code user settings at %s", filePath)
	root, err := decodeStrictJSONObject(raw, label)
	if err != nil {
		return nil, err
	}
	disabled := false
	if value, exists := root["disableAllHooks"]; exists {
		var ok bool
		disabled, ok = value.(bool)
		if !ok {
			return nil, fmt.Errorf(
				`Claude Code settings field "disableAllHooks" must be a boolean`,
			)
		}
	}
	hooks, err := readClaudeHooks(root)
	if err != nil {
		return nil, err
	}
	return &claudeDocument{
		exists: true, filePath: filePath, root: root, hooks: hooks,
		hooksDisabled: disabled, raw: append([]byte(nil), raw...),
	}, nil
}

func createClaudeDocument(filePath string) *claudeDocument {
	return &claudeDocument{
		filePath: filePath,
		root:     map[string]any{},
		hooks:    claudeHooks{},
	}
}

func renderClaudeHooks(hooks claudeHooks) map[string]any {
	result := make(map[string]any, len(hooks))
	for event, groups := range hooks {
		values := make([]any, 0, len(groups))
		for _, group := range groups {
			object := cloneClaudeObject(group.object)
			handlers := make([]any, 0, len(group.handlers))
			for _, handler := range group.handlers {
				handlers = append(handlers, handler)
			}
			object["hooks"] = handlers
			values = append(values, object)
		}
		result[event] = values
	}
	return result
}

func renderClaudeDocument(
	document *claudeDocument,
	hooks claudeHooks,
) (*claudeRenderedDocument, error) {
	if !document.exists && len(hooks) == 0 {
		return &claudeRenderedDocument{document: document}, nil
	}
	if reflect.DeepEqual(hooks, document.hooks) {
		return &claudeRenderedDocument{document: document}, nil
	}
	if len(hooks) == 0 && entirelyManagedClaudeDocument(document) {
		return &claudeRenderedDocument{
			document: document, changed: true, remove: true,
		}, nil
	}
	root := cloneClaudeObject(document.root)
	if len(hooks) == 0 {
		delete(root, "hooks")
	} else {
		root["hooks"] = renderClaudeHooks(hooks)
	}
	next, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode Claude Code user settings: %w", err)
	}
	next = append(next, '\n')
	if _, err := parseClaudeDocument(document.filePath, next); err != nil {
		return nil, fmt.Errorf("validate rendered Claude Code user settings: %w", err)
	}
	return &claudeRenderedDocument{
		document: document,
		changed:  !document.exists || !bytes.Equal(next, document.raw),
		next:     next,
	}, nil
}
