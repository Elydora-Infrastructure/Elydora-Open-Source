package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

const copilotRegexValidationTimeout = 10 * time.Second

var copilotSupportedEvents = stringSet(
	"agentStop",
	"Stop",
	"errorOccurred",
	"ErrorOccurred",
	"notification",
	"Notification",
	"permissionRequest",
	"PermissionRequest",
	"postToolUse",
	"PostToolUse",
	"postToolUseFailure",
	"PostToolUseFailure",
	"preCompact",
	"PreCompact",
	"preToolUse",
	"PreToolUse",
	"sessionEnd",
	"SessionEnd",
	"sessionStart",
	"SessionStart",
	"subagentStart",
	"SubagentStart",
	"subagentStop",
	"SubagentStop",
	"userPromptSubmitted",
	"UserPromptSubmit",
	"userPromptTransformed",
)

const copilotRegexValidator = `import fs from "node:fs";
const entries = JSON.parse(fs.readFileSync(0, "utf8"));
for (const entry of entries) {
  try {
    new RegExp("^(?:" + entry.pattern + ")$");
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    process.stderr.write(entry.label + ": " + message);
    process.exit(1);
  }
}
`

type copilotMatcherEntry struct {
	Label   string `json:"label"`
	Pattern string `json:"pattern"`
}

func copilotFieldLabel(label, field string) string {
	return fmt.Sprintf(`%s field %q`, label, field)
}

func requireCopilotString(value any, label string) (string, error) {
	text, ok := value.(string)
	if !ok || text == "" {
		return "", fmt.Errorf("%s must be a non-empty string", label)
	}
	return text, nil
}

func validateCopilotOptionalString(
	handler map[string]any,
	field string,
	label string,
) error {
	value, exists := handler[field]
	if exists {
		if _, ok := value.(string); !ok {
			return fmt.Errorf("%s must be a string", copilotFieldLabel(label, field))
		}
	}
	return nil
}

func validateCopilotTimeouts(handler map[string]any, label string) error {
	for _, field := range []string{"timeout", "timeoutSec"} {
		value, exists := handler[field]
		if !exists {
			continue
		}
		number, ok := value.(float64)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) || number <= 0 {
			return fmt.Errorf(
				"%s must be a positive number",
				copilotFieldLabel(label, field),
			)
		}
	}
	return nil
}

func validateCopilotMatcher(
	handler map[string]any,
	label string,
) error {
	value, exists := handler["matcher"]
	if !exists {
		return nil
	}
	_, err := requireCopilotString(value, copilotFieldLabel(label, "matcher"))
	return err
}

func validateCopilotStringMap(value any, label string) error {
	object, ok := value.(map[string]any)
	if !ok || object == nil {
		return fmt.Errorf("%s must be an object", label)
	}
	for key, item := range object {
		if _, err := requireCopilotString(item, label+"."+key); err != nil {
			return err
		}
	}
	return nil
}

func validateCopilotCommand(
	handler map[string]any,
	label string,
) error {
	hasCommand := false
	for _, field := range []string{"bash", "powershell", "command"} {
		if _, exists := handler[field]; exists {
			hasCommand = true
		}
	}
	if !hasCommand {
		return fmt.Errorf("%s must define bash, powershell, or command", label)
	}
	for _, field := range []string{"bash", "powershell", "command", "cwd"} {
		if err := validateCopilotOptionalString(handler, field, label); err != nil {
			return err
		}
	}
	if value, exists := handler["env"]; exists {
		if err := validateCopilotStringMap(value, copilotFieldLabel(label, "env")); err != nil {
			return err
		}
	}
	if err := validateCopilotTimeouts(handler, label); err != nil {
		return err
	}
	return validateCopilotMatcher(handler, label)
}

func copilotLoopbackHost(hostname string) bool {
	return hostname == "localhost" || hostname == "::1" ||
		strings.HasPrefix(hostname, "127.")
}

func validateCopilotHTTP(
	handler map[string]any,
	label string,
) error {
	rawURL, err := requireCopilotString(
		handler["url"],
		copilotFieldLabel(label, "url"),
	)
	if err != nil {
		return err
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" || parsed.Hostname() == "" {
		return fmt.Errorf("%s is invalid", copilotFieldLabel(label, "url"))
	}
	localhostAllowed := os.Getenv("COPILOT_HOOK_ALLOW_LOCALHOST") == "1" &&
		parsed.Scheme == "http" && copilotLoopbackHost(parsed.Hostname())
	if parsed.Scheme != "https" && !localhostAllowed {
		return fmt.Errorf("%s must use HTTPS", copilotFieldLabel(label, "url"))
	}
	if value, exists := handler["headers"]; exists {
		if err := validateCopilotStringMap(value, copilotFieldLabel(label, "headers")); err != nil {
			return err
		}
	}
	if value, exists := handler["allowedEnvVars"]; exists {
		items, ok := value.([]any)
		if !ok {
			return fmt.Errorf(
				"%s must be an array of strings",
				copilotFieldLabel(label, "allowedEnvVars"),
			)
		}
		for _, item := range items {
			if text, ok := item.(string); !ok || text == "" {
				return fmt.Errorf(
					"%s must be an array of strings",
					copilotFieldLabel(label, "allowedEnvVars"),
				)
			}
		}
	}
	if err := validateCopilotTimeouts(handler, label); err != nil {
		return err
	}
	return validateCopilotMatcher(handler, label)
}

func validateCopilotPrompt(
	handler map[string]any,
	event string,
	label string,
) error {
	if event != "sessionStart" && event != "SessionStart" {
		return fmt.Errorf("%s prompt hooks are supported only for sessionStart", label)
	}
	_, err := requireCopilotString(handler["prompt"], copilotFieldLabel(label, "prompt"))
	return err
}

func validateCopilotHandler(
	handler map[string]any,
	event string,
	label string,
) error {
	handlerType := "command"
	if value, exists := handler["type"]; exists {
		var ok bool
		handlerType, ok = value.(string)
		if !ok {
			return fmt.Errorf("%s is unsupported", copilotFieldLabel(label, "type"))
		}
	}
	switch handlerType {
	case "command":
		return validateCopilotCommand(handler, label)
	case "http":
		return validateCopilotHTTP(handler, label)
	case "prompt":
		return validateCopilotPrompt(handler, event, label)
	default:
		return fmt.Errorf("%s is unsupported", copilotFieldLabel(label, "type"))
	}
}

func validateCopilotHooks(value any, label string) (copilotHooks, error) {
	object, ok := value.(map[string]any)
	if !ok || object == nil {
		return nil, fmt.Errorf(`%s field "hooks" must be an object`, label)
	}
	hooks := make(copilotHooks, len(object))
	for event, candidate := range object {
		if _, supported := copilotSupportedEvents[event]; !supported {
			return nil, fmt.Errorf("%s hook event %q is unsupported", label, event)
		}
		values, ok := candidate.([]any)
		if !ok {
			return nil, fmt.Errorf(`%s field "hooks.%s" must be an array`, label, event)
		}
		handlers := make([]map[string]any, 0, len(values))
		for index, value := range values {
			handler, ok := value.(map[string]any)
			itemLabel := fmt.Sprintf("%s handler hooks.%s[%d]", label, event, index)
			if !ok || handler == nil {
				return nil, fmt.Errorf("%s must be an object", itemLabel)
			}
			if err := validateCopilotHandler(handler, event, itemLabel); err != nil {
				return nil, err
			}
			handlers = append(handlers, cloneCopilotObject(handler))
		}
		hooks[event] = handlers
	}
	return hooks, nil
}

func copilotMatcherEntries(hooks copilotHooks) []copilotMatcherEntry {
	entries := make([]copilotMatcherEntry, 0)
	wildcardEvents := stringSet(
		"preToolUse", "PreToolUse", "permissionRequest", "PermissionRequest",
	)
	for event, handlers := range hooks {
		for index, handler := range handlers {
			matcher, ok := handler["matcher"].(string)
			if !ok {
				continue
			}
			if _, wildcardEvent := wildcardEvents[event]; wildcardEvent &&
				(matcher == "*" || matcher == "**") {
				continue
			}
			entries = append(entries, copilotMatcherEntry{
				Label:   fmt.Sprintf("GitHub Copilot hooks.%s[%d] matcher", event, index),
				Pattern: matcher,
			})
		}
	}
	return entries
}

func validateCopilotJavaScriptMatchers(
	sources []copilotHooks,
	nodePath string,
) error {
	entries := make([]copilotMatcherEntry, 0)
	for _, hooks := range sources {
		entries = append(entries, copilotMatcherEntries(hooks)...)
	}
	if len(entries) == 0 {
		return nil
	}
	input, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("encode GitHub Copilot matchers: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), copilotRegexValidationTimeout)
	defer cancel()
	command := exec.CommandContext( // #nosec G204 -- nodePath is resolved through exec.LookPath.
		ctx,
		nodePath,
		"--input-type=module",
		"--eval",
		copilotRegexValidator,
	)
	command.Stdin = bytes.NewReader(input)
	output, runErr := command.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf(
			"GitHub Copilot matcher validation timed out after %s",
			copilotRegexValidationTimeout,
		)
	}
	if runErr != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = runErr.Error()
		}
		return fmt.Errorf(
			"GitHub Copilot matcher must be a valid JavaScript regular expression: %s",
			message,
		)
	}
	return nil
}
