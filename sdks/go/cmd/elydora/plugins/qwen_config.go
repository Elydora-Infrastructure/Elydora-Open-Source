package plugins

import (
	"bytes"
	"fmt"
	"sort"
)

const (
	qwenSystemDefaultsKind = "system-defaults"
	qwenUserKind           = "user"
	qwenWorkspaceKind      = "workspace"
	qwenSystemKind         = "system"
)

type qwenDocument struct {
	kind               string
	filePath           string
	exists             bool
	raw                []byte
	snapshot           *managedFileSnapshot
	root               map[string]any
	hooks              qwenHookSettings
	hasHooksContainer  bool
	disableAllHooks    *bool
	folderTrustEnabled *bool
	ownedFile          bool
}

type qwenRenderedDocument struct {
	document *qwenDocument
	changed  bool
	next     []byte
	remove   bool
}

func qwenSourceLabel(kind string) string {
	switch kind {
	case qwenSystemDefaultsKind:
		return "Qwen Code system defaults"
	case qwenWorkspaceKind:
		return "Qwen Code workspace settings"
	case qwenSystemKind:
		return "Qwen Code system override settings"
	default:
		return "Qwen Code user settings"
	}
}

func qwenDocumentLabel(document *qwenDocument) string {
	if document == nil {
		return "Qwen Code settings"
	}
	return qwenSourceLabel(document.kind)
}

func readQwenFolderTrust(
	root map[string]any,
	label string,
) (*bool, error) {
	securityValue, exists := root["security"]
	if !exists {
		return nil, nil
	}
	security, ok := securityValue.(map[string]any)
	if !ok || security == nil {
		return nil, fmt.Errorf(`%s field "security" must be an object`, label)
	}
	folderTrustValue, exists := security["folderTrust"]
	if !exists {
		return nil, nil
	}
	folderTrust, ok := folderTrustValue.(map[string]any)
	if !ok || folderTrust == nil {
		return nil, fmt.Errorf(`%s field "security.folderTrust" must be an object`, label)
	}
	enabledValue, exists := folderTrust["enabled"]
	if !exists {
		return nil, nil
	}
	enabled, ok := enabledValue.(bool)
	if !ok {
		return nil, fmt.Errorf(
			`%s field "security.folderTrust.enabled" must be a boolean`,
			label,
		)
	}
	return &enabled, nil
}

func parseQwenDocument(
	kind string,
	exists bool,
	filePath string,
	raw []byte,
	snapshot *managedFileSnapshot,
) (*qwenDocument, error) {
	label := fmt.Sprintf("%s at %s", qwenSourceLabel(kind), filePath)
	root, err := decodeJSONCObject(raw, label, false)
	if err != nil {
		return nil, err
	}
	var disabled *bool
	if value, hasFlag := root["disableAllHooks"]; hasFlag {
		flag, ok := value.(bool)
		if !ok {
			return nil, fmt.Errorf(`%s field "disableAllHooks" must be a boolean`, label)
		}
		disabled = &flag
	}
	hooks := qwenHookSettings{}
	hooksValue, hasHooks := root["hooks"]
	if hasHooks {
		hooks, err = readQwenHookSettings(hooksValue, label+` field "hooks"`)
		if err != nil {
			return nil, err
		}
	}
	folderTrustEnabled, err := readQwenFolderTrust(root, label)
	if err != nil {
		return nil, err
	}
	return &qwenDocument{
		kind: kind, filePath: filePath, exists: exists,
		raw: append([]byte(nil), raw...), snapshot: snapshot, root: root,
		hooks: hooks, hasHooksContainer: hasHooks,
		disableAllHooks: disabled, folderTrustEnabled: folderTrustEnabled,
		ownedFile: kind == qwenUserKind && bytes.HasPrefix(raw, []byte(qwenOwnedFileMarker)),
	}, nil
}

func createQwenDocument(kind, filePath string) (*qwenDocument, error) {
	raw := []byte("{}\n")
	if kind == qwenUserKind {
		raw = []byte(qwenOwnedFileMarker + "\n{}\n")
	}
	return parseQwenDocument(kind, false, filePath, raw, nil)
}

func renderQwenDocument(
	document *qwenDocument,
	agentID, runtimeRoot string,
	additions map[string]map[string]any,
) (*qwenRenderedDocument, error) {
	if document == nil {
		return nil, fmt.Errorf("Qwen Code user settings are required")
	}
	label := fmt.Sprintf("%s at %s", qwenDocumentLabel(document), document.filePath)
	editor, err := newJSONCEditor(document.raw, label, false)
	if err != nil {
		return nil, err
	}
	removals := managedQwenRemovals(document.hooks, agentID, runtimeRoot)
	for _, event := range qwenManagedEvents {
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
			current, parseErr := parseQwenDocument(
				document.kind,
				document.exists,
				document.filePath,
				editor.pack(),
				document.snapshot,
			)
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
	current, err := parseQwenDocument(
		document.kind,
		document.exists,
		document.filePath,
		editor.pack(),
		document.snapshot,
	)
	if err != nil {
		return nil, err
	}
	if current.hasHooksContainer && len(current.hooks) == 0 {
		if err := editor.remove([]any{"hooks"}); err != nil {
			return nil, err
		}
	}
	for _, event := range qwenManagedEvents {
		group, exists := additions[event]
		if !exists {
			continue
		}
		current, err = parseQwenDocument(
			document.kind,
			document.exists,
			document.filePath,
			editor.pack(),
			document.snapshot,
		)
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
	nextDocument, err := parseQwenDocument(
		document.kind,
		document.exists,
		document.filePath,
		next,
		document.snapshot,
	)
	if err != nil {
		return nil, err
	}
	remove := len(additions) == 0 && document.ownedFile && len(nextDocument.root) == 0
	return &qwenRenderedDocument{
		document: document,
		changed:  remove || !bytes.Equal(next, document.raw),
		next:     next,
		remove:   remove,
	}, nil
}

func appendQwenPath(base []any, parts ...any) []any {
	path := make([]any, 0, len(base)+len(parts))
	path = append(path, base...)
	return append(path, parts...)
}
