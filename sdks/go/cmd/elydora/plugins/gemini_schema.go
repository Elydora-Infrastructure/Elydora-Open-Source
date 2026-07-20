package plugins

import (
	"fmt"
	"math"
)

var geminiKnownEvents = map[string]bool{
	"BeforeTool": true, "AfterTool": true, "BeforeAgent": true,
	"Notification": true, "AfterAgent": true, "SessionStart": true,
	"SessionEnd": true, "PreCompress": true, "BeforeModel": true,
	"AfterModel": true, "BeforeToolSelection": true,
}

type geminiHooks map[string][]any

type geminiHookControls struct {
	enabled  bool
	disabled []string
}

func optionalGeminiString(value map[string]any, field, label string) error {
	if item, exists := value[field]; exists {
		if _, ok := item.(string); !ok {
			return fmt.Errorf(`%s field %q must be a string`, label, field)
		}
	}
	return nil
}

func validateGeminiHandler(
	value any,
	event string,
	groupIndex int,
	handlerIndex int,
) error {
	label := fmt.Sprintf(
		"Gemini CLI settings handler hooks.%s[%d].hooks[%d]",
		event,
		groupIndex,
		handlerIndex,
	)
	handler, ok := value.(map[string]any)
	if !ok || handler == nil {
		return fmt.Errorf("%s must be an object", label)
	}
	if handler["type"] != "command" {
		return fmt.Errorf(
			`%s has unsupported type %q`,
			label,
			fmt.Sprint(handler["type"]),
		)
	}
	for _, field := range []string{"name", "description", "source"} {
		if err := optionalGeminiString(handler, field, label); err != nil {
			return err
		}
	}
	if timeout, exists := handler["timeout"]; exists {
		number, ok := timeout.(float64)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) || number < 0 {
			return fmt.Errorf(
				"%s timeout must be a non-negative finite number",
				label,
			)
		}
	}
	if environment, exists := handler["env"]; exists {
		entries, ok := environment.(map[string]any)
		if !ok {
			return fmt.Errorf("%s env must map names to strings", label)
		}
		for _, entry := range entries {
			if _, ok := entry.(string); !ok {
				return fmt.Errorf("%s env must map names to strings", label)
			}
		}
	}
	command, ok := handler["command"].(string)
	if !ok || command == "" {
		return fmt.Errorf("%s requires a non-empty command", label)
	}
	return nil
}

func validateGeminiGroup(value any, event string, groupIndex int) error {
	label := fmt.Sprintf(
		"Gemini CLI settings group hooks.%s[%d]",
		event,
		groupIndex,
	)
	group, ok := value.(map[string]any)
	if !ok || group == nil {
		return fmt.Errorf("%s must be an object", label)
	}
	if matcher, exists := group["matcher"]; exists {
		if _, ok := matcher.(string); !ok {
			return fmt.Errorf("%s matcher must be a string", label)
		}
	}
	if sequential, exists := group["sequential"]; exists {
		if _, ok := sequential.(bool); !ok {
			return fmt.Errorf("%s sequential must be a boolean", label)
		}
	}
	handlers, ok := group["hooks"].([]any)
	if !ok {
		return fmt.Errorf("%s must contain a hooks array", label)
	}
	for handlerIndex, handler := range handlers {
		if err := validateGeminiHandler(
			handler,
			event,
			groupIndex,
			handlerIndex,
		); err != nil {
			return err
		}
	}
	return nil
}

func readGeminiHooks(root map[string]any) (geminiHooks, error) {
	value, exists := root["hooks"]
	if !exists {
		return geminiHooks{}, nil
	}
	object, ok := value.(map[string]any)
	if !ok || object == nil {
		return nil, fmt.Errorf(`Gemini CLI settings field "hooks" must be an object`)
	}
	hooks := make(geminiHooks, len(object))
	for event, groupsValue := range object {
		groups, ok := groupsValue.([]any)
		if !ok {
			return nil, fmt.Errorf(
				`Gemini CLI settings field "hooks.%s" must be an array`,
				event,
			)
		}
		if geminiKnownEvents[event] {
			for groupIndex, group := range groups {
				if err := validateGeminiGroup(group, event, groupIndex); err != nil {
					return nil, err
				}
			}
		}
		hooks[event] = append([]any(nil), groups...)
	}
	return hooks, nil
}

func readGeminiHookControls(root map[string]any) (geminiHookControls, error) {
	value, exists := root["hooksConfig"]
	if !exists {
		return geminiHookControls{enabled: true}, nil
	}
	object, ok := value.(map[string]any)
	if !ok || object == nil {
		return geminiHookControls{}, fmt.Errorf(
			`Gemini CLI settings field "hooksConfig" must be an object`,
		)
	}
	for field := range object {
		if field != "enabled" && field != "disabled" && field != "notifications" {
			return geminiHookControls{}, fmt.Errorf(
				`Gemini CLI settings field "hooksConfig" contains unsupported field %q`,
				field,
			)
		}
	}
	enabled := true
	if value, exists := object["enabled"]; exists {
		flag, ok := value.(bool)
		if !ok {
			return geminiHookControls{}, fmt.Errorf(
				`Gemini CLI settings field "hooksConfig.enabled" must be a boolean`,
			)
		}
		enabled = flag
	}
	if value, exists := object["notifications"]; exists {
		if _, ok := value.(bool); !ok {
			return geminiHookControls{}, fmt.Errorf(
				`Gemini CLI settings field "hooksConfig.notifications" must be a boolean`,
			)
		}
	}
	disabled := []string{}
	if value, exists := object["disabled"]; exists {
		items, ok := value.([]any)
		if !ok {
			return geminiHookControls{}, fmt.Errorf(
				`Gemini CLI settings field "hooksConfig.disabled" must be an array of strings`,
			)
		}
		for _, item := range items {
			entry, ok := item.(string)
			if !ok {
				return geminiHookControls{}, fmt.Errorf(
					`Gemini CLI settings field "hooksConfig.disabled" must be an array of strings`,
				)
			}
			disabled = append(disabled, entry)
		}
	}
	return geminiHookControls{enabled: enabled, disabled: disabled}, nil
}
