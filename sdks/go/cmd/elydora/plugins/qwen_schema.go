package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"strings"
	"time"
)

var qwenEventNames = map[string]bool{
	"PreToolUse": true, "PostToolUse": true, "PostToolUseFailure": true,
	"PostToolBatch": true, "Notification": true, "UserPromptSubmit": true,
	"UserPromptExpansion": true, "SessionStart": true, "Stop": true,
	"MessageDisplay": true, "SubagentStart": true, "SubagentStop": true,
	"PreCompact": true, "PostCompact": true, "SessionEnd": true,
	"PermissionRequest": true, "PermissionDenied": true, "StopFailure": true,
	"TodoCreated": true, "TodoCompleted": true, "InstructionsLoaded": true,
}

var qwenConfigFields = map[string]bool{
	"enabled": true, "disabled": true, "notifications": true,
}

type qwenRegexEntry struct {
	Label   string `json:"label"`
	Pattern string `json:"pattern"`
}

func readQwenHookSettings(value any, label string) (qwenHookSettings, error) {
	object, ok := value.(map[string]any)
	if !ok || object == nil {
		return nil, fmt.Errorf("%s must contain a JSON object", label)
	}
	settings := make(qwenHookSettings, len(object))
	for key, item := range object {
		if qwenConfigFields[key] {
			settings[key] = item
			continue
		}
		if !qwenEventNames[key] {
			return nil, fmt.Errorf(`%s contains unsupported field %q`, label, key)
		}
		groups, ok := item.([]any)
		if !ok {
			return nil, fmt.Errorf(`%s field %q must be an array`, label, key)
		}
		for groupIndex, group := range groups {
			if err := validateQwenGroup(group, label, key, groupIndex); err != nil {
				return nil, err
			}
		}
		settings[key] = item
	}
	return settings, nil
}

func validateQwenGroup(value any, label, event string, groupIndex int) error {
	location := fmt.Sprintf(`%s field %q[%d]`, label, event, groupIndex)
	group, ok := value.(map[string]any)
	if !ok || group == nil {
		return fmt.Errorf("%s must be an object", location)
	}
	if matcher, exists := group["matcher"]; exists {
		if _, ok := matcher.(string); !ok {
			return fmt.Errorf("%s matcher must be a string", location)
		}
	}
	if sequential, exists := group["sequential"]; exists {
		if _, ok := sequential.(bool); !ok {
			return fmt.Errorf("%s sequential must be a boolean", location)
		}
	}
	handlers, ok := group["hooks"].([]any)
	if !ok {
		return fmt.Errorf("%s must contain a hooks array", location)
	}
	for handlerIndex, handler := range handlers {
		if err := validateQwenHandler(handler, location, handlerIndex); err != nil {
			return err
		}
	}
	return nil
}

func validateQwenHandler(value any, groupLabel string, handlerIndex int) error {
	location := fmt.Sprintf("%s.hooks[%d]", groupLabel, handlerIndex)
	handler, ok := value.(map[string]any)
	if !ok || handler == nil {
		return fmt.Errorf("%s must be an object", location)
	}
	kind, _ := handler["type"].(string)
	if kind != "command" && kind != "http" && kind != "prompt" {
		return fmt.Errorf(`%s type must be "command", "http", or "prompt"`, location)
	}
	if timeout, exists := handler["timeout"]; exists {
		number, ok := timeout.(float64)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) || number < 0 {
			return fmt.Errorf("%s timeout must be a non-negative finite number", location)
		}
	}
	for _, key := range []string{"name", "description", "statusMessage", "source"} {
		if err := optionalQwenString(handler, key, location); err != nil {
			return err
		}
	}
	switch kind {
	case "command":
		command, ok := handler["command"].(string)
		if !ok || command == "" {
			return fmt.Errorf("%s command must be a non-empty string", location)
		}
		if err := optionalQwenStringMap(handler, "env", location); err != nil {
			return err
		}
		if err := optionalQwenBoolean(handler, "async", location); err != nil {
			return err
		}
		if shell, exists := handler["shell"]; exists && shell != "bash" && shell != "powershell" {
			return fmt.Errorf(`%s shell must be "bash" or "powershell"`, location)
		}
	case "http":
		url, ok := handler["url"].(string)
		if !ok || url == "" {
			return fmt.Errorf("%s url must be a non-empty string", location)
		}
		if err := optionalQwenStringMap(handler, "headers", location); err != nil {
			return err
		}
		if err := optionalQwenBoolean(handler, "once", location); err != nil {
			return err
		}
		if err := optionalQwenString(handler, "if", location); err != nil {
			return err
		}
		if allowed, exists := handler["allowedEnvVars"]; exists {
			values, ok := allowed.([]any)
			if !ok || !allQwenStrings(values) {
				return fmt.Errorf("%s allowedEnvVars must be an array of strings", location)
			}
		}
	case "prompt":
		prompt, ok := handler["prompt"].(string)
		if !ok || prompt == "" {
			return fmt.Errorf("%s prompt must be a non-empty string", location)
		}
		if err := optionalQwenString(handler, "model", location); err != nil {
			return err
		}
	}
	return nil
}

func optionalQwenString(value map[string]any, key, label string) error {
	if item, exists := value[key]; exists {
		if _, ok := item.(string); !ok {
			return fmt.Errorf(`%s field %q must be a string`, label, key)
		}
	}
	return nil
}

func optionalQwenBoolean(value map[string]any, key, label string) error {
	if item, exists := value[key]; exists {
		if _, ok := item.(bool); !ok {
			return fmt.Errorf(`%s field %q must be a boolean`, label, key)
		}
	}
	return nil
}

func optionalQwenStringMap(value map[string]any, key, label string) error {
	item, exists := value[key]
	if !exists {
		return nil
	}
	entries, ok := item.(map[string]any)
	if !ok {
		return fmt.Errorf(`%s field %q must contain string values`, label, key)
	}
	for _, entry := range entries {
		if _, ok := entry.(string); !ok {
			return fmt.Errorf(`%s field %q must contain string values`, label, key)
		}
	}
	return nil
}

func allQwenStrings(values []any) bool {
	for _, value := range values {
		if _, ok := value.(string); !ok {
			return false
		}
	}
	return true
}

func validateQwenRegexes(nodePath string, settings qwenHookSettings) error {
	entries := make([]qwenRegexEntry, 0)
	for event, value := range settings {
		if qwenConfigFields[event] {
			continue
		}
		for index, groupValue := range value.([]any) {
			group := groupValue.(map[string]any)
			matcher, ok := group["matcher"].(string)
			if ok && strings.TrimSpace(matcher) != "" && strings.TrimSpace(matcher) != "*" {
				entries = append(entries, qwenRegexEntry{
					Label: fmt.Sprintf(`Qwen Code hooks field %q[%d] matcher`, event, index), Pattern: matcher,
				})
			}
		}
	}
	if len(entries) == 0 {
		return nil
	}
	payload, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("encode Qwen Code regular expressions: %w", err)
	}
	validator := `import fs from 'node:fs';
const entries = JSON.parse(fs.readFileSync(0, 'utf8'));
for (const entry of entries) {
  try { new RegExp(entry.pattern); }
  catch (error) {
    process.stderr.write(entry.label + ': ' + (error instanceof Error ? error.message : String(error)));
    process.exit(1);
  }
}`
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// #nosec G204 -- nodePath is resolved by exec.LookPath and every argument is fixed.
	command := exec.CommandContext(ctx, nodePath, "--input-type=module", "--eval", validator)
	command.Stdin = bytes.NewReader(payload)
	output, runErr := command.CombinedOutput()
	if ctx.Err() != nil {
		return fmt.Errorf("Qwen Code regular expression validation timed out: %w", ctx.Err())
	}
	if runErr != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = runErr.Error()
		}
		return fmt.Errorf("Qwen Code matcher must be a valid JavaScript regular expression: %s", message)
	}
	return nil
}
