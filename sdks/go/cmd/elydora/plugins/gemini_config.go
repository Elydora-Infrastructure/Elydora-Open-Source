package plugins

import (
	"bytes"
	"fmt"
	"reflect"
	"sort"
)

type geminiDocument struct {
	filePath          string
	exists            bool
	raw               []byte
	root              map[string]any
	hooks             geminiHooks
	hookControls      geminiHookControls
	hasHooksContainer bool
	ownedFile         bool
}

type geminiRenderedDocument struct {
	document *geminiDocument
	changed  bool
	next     []byte
	remove   bool
}

func geminiDocumentLabel(filePath string) string {
	return fmt.Sprintf("Gemini CLI user settings at %s", filePath)
}

func parseGeminiDocument(
	exists bool,
	filePath string,
	raw []byte,
) (*geminiDocument, error) {
	label := geminiDocumentLabel(filePath)
	root, err := decodeJSONCObject(raw, label, false)
	if err != nil {
		return nil, err
	}
	hooks, err := readGeminiHooks(root)
	if err != nil {
		return nil, err
	}
	controls, err := readGeminiHookControls(root)
	if err != nil {
		return nil, err
	}
	_, hasHooks := root["hooks"]
	return &geminiDocument{
		filePath: filePath, exists: exists, raw: append([]byte(nil), raw...),
		root: root, hooks: hooks, hookControls: controls,
		hasHooksContainer: hasHooks,
		ownedFile:         bytes.HasPrefix(raw, []byte(geminiOwnedFileMarker)),
	}, nil
}

func createGeminiDocument(filePath string) (*geminiDocument, error) {
	return parseGeminiDocument(
		false,
		filePath,
		[]byte(geminiOwnedFileMarker+"\n{}\n"),
	)
}

func geminiAlreadyInstalled(
	document *geminiDocument,
	additions map[string]map[string]any,
) (bool, error) {
	if len(additions) != len(geminiManagedEvents) {
		return false, nil
	}
	removals, err := managedGeminiRemovals(document.hooks, "")
	if err != nil {
		return false, err
	}
	for _, event := range geminiManagedEvents {
		expected, exists := additions[event]
		if !exists {
			return false, nil
		}
		removalCount := 0
		removeGroup := false
		for _, removal := range removals {
			if removal.event == event {
				removalCount++
				removeGroup = removal.removeGroup
			}
		}
		exactCount := 0
		for _, group := range document.hooks[event] {
			if reflect.DeepEqual(group, expected) {
				exactCount++
			}
		}
		if removalCount != 1 || !removeGroup || exactCount != 1 {
			return false, nil
		}
	}
	return true, nil
}

func renderGeminiDocument(
	document *geminiDocument,
	agentID string,
	additions map[string]map[string]any,
) (*geminiRenderedDocument, error) {
	if agentID == "" && len(additions) > 0 {
		installed, err := geminiAlreadyInstalled(document, additions)
		if err != nil {
			return nil, err
		}
		if installed {
			return &geminiRenderedDocument{document: document}, nil
		}
	}
	label := geminiDocumentLabel(document.filePath)
	editor, err := newJSONCEditor(document.raw, label, false)
	if err != nil {
		return nil, err
	}
	removals, err := managedGeminiRemovals(document.hooks, agentID)
	if err != nil {
		return nil, err
	}
	for _, event := range geminiManagedEvents {
		eventRemovals := make([]geminiManagedRemoval, 0)
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
				if err := editor.remove(
					appendGeminiPath(groupPath, "hooks", handlerIndex),
				); err != nil {
					return nil, err
				}
			}
		}
		if len(eventRemovals) > 0 {
			current, parseErr := parseGeminiDocument(
				document.exists,
				document.filePath,
				editor.pack(),
			)
			if parseErr != nil {
				return nil, parseErr
			}
			if groups, exists := current.hooks[event]; exists && len(groups) == 0 {
				if err := editor.remove([]any{"hooks", event}); err != nil {
					return nil, err
				}
			}
		}
	}
	current, err := parseGeminiDocument(
		document.exists,
		document.filePath,
		editor.pack(),
	)
	if err != nil {
		return nil, err
	}
	if current.hasHooksContainer && len(current.hooks) == 0 {
		if err := editor.remove([]any{"hooks"}); err != nil {
			return nil, err
		}
	}
	for _, event := range geminiManagedEvents {
		group, exists := additions[event]
		if !exists {
			continue
		}
		current, err = parseGeminiDocument(
			document.exists,
			document.filePath,
			editor.pack(),
		)
		if err != nil {
			return nil, err
		}
		_, eventExists := current.hooks[event]
		switch {
		case !current.hasHooksContainer:
			err = editor.addProperty(nil, "hooks", map[string]any{event: []any{group}})
		case eventExists:
			err = editor.appendArray([]any{"hooks", event}, group)
		default:
			err = editor.addProperty([]any{"hooks"}, event, []any{group})
		}
		if err != nil {
			return nil, err
		}
	}
	next := editor.pack()
	nextDocument, err := parseGeminiDocument(
		document.exists,
		document.filePath,
		next,
	)
	if err != nil {
		return nil, err
	}
	remove := len(additions) == 0 && document.ownedFile && len(nextDocument.root) == 0
	return &geminiRenderedDocument{
		document: document, changed: remove || !bytes.Equal(next, document.raw),
		next: next, remove: remove,
	}, nil
}

func appendGeminiPath(base []any, parts ...any) []any {
	path := make([]any, 0, len(base)+len(parts))
	path = append(path, base...)
	return append(path, parts...)
}
