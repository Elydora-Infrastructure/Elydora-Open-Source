package plugins

import (
	"bytes"
	"fmt"
	"sort"
)

type qwenDocument struct {
	filePath          string
	exists            bool
	raw               []byte
	root              map[string]any
	hooks             qwenHookSettings
	hasHooksContainer bool
	hooksDisabled     bool
	ownedFile         bool
}

type qwenRenderedDocument struct {
	document *qwenDocument
	changed  bool
	next     []byte
	remove   bool
}

func qwenDocumentLabel(filePath string) string {
	return fmt.Sprintf("Qwen Code settings at %s", filePath)
}

func parseQwenDocument(exists bool, filePath string, raw []byte) (*qwenDocument, error) {
	label := qwenDocumentLabel(filePath)
	root, err := decodeJSONCObject(raw, label, false)
	if err != nil {
		return nil, err
	}
	disabled := false
	if value, hasFlag := root["disableAllHooks"]; hasFlag {
		flag, ok := value.(bool)
		if !ok {
			return nil, fmt.Errorf(`%s field "disableAllHooks" must be a boolean`, label)
		}
		disabled = flag
	}
	hooks := qwenHookSettings{}
	hooksValue, hasHooks := root["hooks"]
	if hasHooks {
		hooks, err = readQwenHookSettings(hooksValue, label+` field "hooks"`)
		if err != nil {
			return nil, err
		}
	}
	return &qwenDocument{
		filePath: filePath, exists: exists, raw: append([]byte(nil), raw...), root: root,
		hooks: hooks, hasHooksContainer: hasHooks, hooksDisabled: disabled,
		ownedFile: bytes.HasPrefix(raw, []byte(qwenOwnedFileMarker)),
	}, nil
}

func createOwnedQwenDocument(filePath string) (*qwenDocument, error) {
	return parseQwenDocument(false, filePath, []byte(qwenOwnedFileMarker+"\n{}\n"))
}

func renderQwenDocument(
	document *qwenDocument,
	agentID, runtimeRoot string,
	additions map[string]map[string]any,
) (*qwenRenderedDocument, error) {
	label := qwenDocumentLabel(document.filePath)
	editor, err := newJSONCEditor(document.raw, label, false)
	if err != nil {
		return nil, err
	}
	removals := managedQwenRemovals(document.hooks, agentID, runtimeRoot)
	for _, event := range qwenToolEvents {
		eventRemovals := make([]qwenManagedRemoval, 0)
		for _, removal := range removals {
			if removal.event == event {
				eventRemovals = append(eventRemovals, removal)
			}
		}
		sort.Slice(eventRemovals, func(left, right int) bool {
			return eventRemovals[left].groupIndex > eventRemovals[right].groupIndex
		})
		for _, removal := range eventRemovals {
			groupPath := []any{"hooks", event, removal.groupIndex}
			if removal.removeGroup {
				if err := editor.remove(groupPath); err != nil {
					return nil, err
				}
				continue
			}
			sort.Sort(sort.Reverse(sort.IntSlice(removal.handlerIndexes)))
			for _, handlerIndex := range removal.handlerIndexes {
				if err := editor.remove(appendQwenPath(groupPath, "hooks", handlerIndex)); err != nil {
					return nil, err
				}
			}
		}
		if len(eventRemovals) > 0 {
			current, parseErr := parseQwenDocument(document.exists, document.filePath, editor.pack())
			if parseErr != nil {
				return nil, parseErr
			}
			if groups, exists := current.hooks[event].([]any); exists && len(groups) == 0 {
				if err := editor.remove([]any{"hooks", event}); err != nil {
					return nil, err
				}
			}
		}
	}
	current, err := parseQwenDocument(document.exists, document.filePath, editor.pack())
	if err != nil {
		return nil, err
	}
	if current.hasHooksContainer && len(current.hooks) == 0 {
		if err := editor.remove([]any{"hooks"}); err != nil {
			return nil, err
		}
	}
	for _, event := range qwenToolEvents {
		group, exists := additions[event]
		if !exists {
			continue
		}
		current, err = parseQwenDocument(document.exists, document.filePath, editor.pack())
		if err != nil {
			return nil, err
		}
		switch {
		case !current.hasHooksContainer:
			err = editor.addProperty(nil, "hooks", map[string]any{event: []any{group}})
		case current.hooks[event] != nil:
			err = editor.appendArray([]any{"hooks", event}, group)
		default:
			err = editor.addProperty([]any{"hooks"}, event, []any{group})
		}
		if err != nil {
			return nil, err
		}
	}
	next := editor.pack()
	nextDocument, err := parseQwenDocument(document.exists, document.filePath, next)
	if err != nil {
		return nil, err
	}
	remove := len(additions) == 0 && document.ownedFile && len(nextDocument.root) == 0
	return &qwenRenderedDocument{
		document: document, changed: remove || !bytes.Equal(next, document.raw), next: next, remove: remove,
	}, nil
}

func appendQwenPath(base []any, parts ...any) []any {
	path := make([]any, 0, len(base)+len(parts))
	path = append(path, base...)
	return append(path, parts...)
}
