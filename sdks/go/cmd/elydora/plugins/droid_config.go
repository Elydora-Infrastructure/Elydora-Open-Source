package plugins

import (
	"bytes"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
)

type droidDocument struct {
	kind              string
	filePath          string
	exists            bool
	raw               []byte
	snapshot          *managedFileSnapshot
	root              map[string]any
	hooks             droidHookSettings
	basePath          []any
	hasHooksContainer bool
	hooksDisabled     *bool
	showHookOutput    *bool
	ownedFile         bool
}

type droidSources struct {
	root          *droidDocument
	legacy        *droidDocument
	settings      *droidDocument
	localSettings *droidDocument
	policy        *droidPolicyState
}

type droidRenderedDocument struct {
	document *droidDocument
	changed  bool
	next     []byte
	remove   bool
}

type droidHookBlock struct {
	field    string
	filePath string
	label    string
}

func parseDroidDocument(
	exists bool,
	filePath, kind string,
	raw []byte,
	snapshot *managedFileSnapshot,
) (*droidDocument, error) {
	label := droidDocumentLabel(kind, filePath)
	root, err := decodeJSONCObject(raw, label, true)
	if err != nil {
		return nil, err
	}
	document := &droidDocument{
		kind: kind, filePath: filePath, exists: exists,
		raw: append([]byte(nil), raw...), snapshot: snapshot, root: root,
		ownedFile: bytes.HasPrefix(raw, []byte(droidOwnedFileMarker)),
	}
	if kind == "settings" || kind == "local-settings" {
		document.basePath = []any{"hooks"}
		document.hooksDisabled, err = droidOptionalBoolean(root, "hooksDisabled", label)
		if err != nil {
			return nil, err
		}
		document.showHookOutput, err = droidOptionalBoolean(root, "showHookOutput", label)
		if err != nil {
			return nil, err
		}
		hooksValue, hasHooks := root["hooks"]
		document.hasHooksContainer = hasHooks
		if !hasHooks {
			document.hooks = droidHookSettings{}
			return document, nil
		}
		document.hooks, err = readDroidHookSettings(hooksValue, label+` field "hooks"`)
		return document, err
	}
	if hooksValue, hasHooks := root["hooks"]; hasHooks {
		document.basePath = []any{"hooks"}
		document.hasHooksContainer = true
		document.hooks, err = readDroidHookSettings(hooksValue, label+` field "hooks"`)
		return document, err
	}
	document.hooksDisabled, err = droidOptionalBoolean(root, "hooksDisabled", label)
	if err != nil {
		return nil, err
	}
	document.showHookOutput, err = droidOptionalBoolean(root, "showHookOutput", label)
	if err != nil {
		return nil, err
	}
	direct := make(map[string]any, len(root))
	for key, value := range root {
		if key != "hooksDisabled" && key != "showHookOutput" {
			direct[key] = value
		}
	}
	document.hooks, err = readDroidHookSettings(direct, label)
	return document, err
}

func droidOptionalBoolean(
	root map[string]any,
	field, label string,
) (*bool, error) {
	value, exists := root[field]
	if !exists {
		return nil, nil
	}
	boolean, ok := value.(bool)
	if !ok {
		return nil, fmt.Errorf(`%s field %q must be a boolean`, label, field)
	}
	return &boolean, nil
}

func droidDocumentLabel(kind, filePath string) string {
	switch kind {
	case "settings":
		return fmt.Sprintf("Factory Droid settings at %s", filePath)
	case "local-settings":
		return fmt.Sprintf("Factory Droid local settings at %s", filePath)
	case "legacy":
		return fmt.Sprintf("Factory Droid legacy hooks at %s", filePath)
	default:
		return fmt.Sprintf("Factory Droid hooks at %s", filePath)
	}
}

func createDroidDocument(filePath, kind string, raw []byte) (*droidDocument, error) {
	return parseDroidDocument(false, filePath, kind, raw, nil)
}

func createOwnedDroidDocument(filePath string) (*droidDocument, error) {
	return createDroidDocument(
		filePath,
		"hooks",
		[]byte(droidOwnedFileMarker+"\n{\n  \"hooks\": {}\n}\n"),
	)
}

func activeDroidDocument(sources *droidSources) *droidDocument {
	switch {
	case sources.root.exists:
		return sources.root
	case sources.legacy.exists:
		return sources.legacy
	case sources.localSettings.hasHooksContainer:
		return sources.localSettings
	case sources.settings.hasHooksContainer:
		return sources.settings
	default:
		return sources.root
	}
}

func effectiveDroidHooks(sources *droidSources) droidHookSettings {
	return activeDroidDocument(sources).hooks
}

func droidHookBlocked(sources *droidSources) *droidHookBlock {
	if sources.policy != nil && sources.policy.allowManagedHooksOnlyBy != nil {
		origin := sources.policy.allowManagedHooksOnlyBy
		return &droidHookBlock{"allowManagedHooksOnly", origin.filePath, origin.label}
	}
	if sources.policy != nil && sources.policy.hooksDisabled != nil {
		if *sources.policy.hooksDisabled {
			origin := sources.policy.hooksDisabledBy
			return &droidHookBlock{"hooksDisabled", origin.filePath, origin.label}
		}
		return nil
	}
	selected := sources.settings
	if sources.localSettings.hooksDisabled != nil {
		selected = sources.localSettings
	}
	if selected.hooksDisabled != nil && *selected.hooksDisabled {
		return &droidHookBlock{
			"hooksDisabled", selected.filePath,
			droidDocumentLabel(selected.kind, selected.filePath),
		}
	}
	active := activeDroidDocument(sources)
	if active.hooksDisabled != nil && *active.hooksDisabled {
		return &droidHookBlock{
			"hooksDisabled", active.filePath,
			droidDocumentLabel(active.kind, active.filePath),
		}
	}
	return nil
}

func droidSourceDocuments(sources *droidSources) []*droidDocument {
	return []*droidDocument{
		sources.root, sources.legacy, sources.settings, sources.localSettings,
	}
}

func droidInstallationDocuments(sources *droidSources) []*droidDocument {
	target := activeDroidDocument(sources)
	candidates := []*droidDocument{target}
	if sources.root.exists {
		candidates = append(candidates, sources.root)
	}
	if sources.legacy.exists {
		candidates = append(candidates, sources.legacy)
	}
	if sources.settings.hasHooksContainer {
		candidates = append(candidates, sources.settings)
	}
	if sources.localSettings.hasHooksContainer {
		candidates = append(candidates, sources.localSettings)
	}
	return uniqueDroidDocuments(candidates...)
}

func uniqueDroidDocuments(documents ...*droidDocument) []*droidDocument {
	result := make([]*droidDocument, 0, len(documents))
	seen := map[string]bool{}
	for _, document := range documents {
		if document == nil {
			continue
		}
		normalized := filepath.Clean(document.filePath)
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		result = append(result, document)
	}
	return result
}

func droidAdditionsFor(
	document, target *droidDocument,
	groups map[string]map[string]any,
) map[string]map[string]any {
	if sameDroidPath(document.filePath, target.filePath) {
		return groups
	}
	return map[string]map[string]any{}
}

func renderDroidDocument(
	document *droidDocument,
	agentID, runtimeRoot string,
	additions map[string]map[string]any,
) (*droidRenderedDocument, error) {
	if len(additions) > 0 && droidHasExactInstallation(document, additions, runtimeRoot) {
		return &droidRenderedDocument{document: document, next: document.raw}, nil
	}
	editor, err := newJSONCEditor(
		document.raw,
		droidDocumentLabel(document.kind, document.filePath),
		true,
	)
	if err != nil {
		return nil, err
	}
	removals := managedDroidRemovals(document.hooks, agentID, runtimeRoot)
	sort.SliceStable(removals, func(left, right int) bool {
		if removals[left].event == removals[right].event {
			return removals[left].groupIndex > removals[right].groupIndex
		}
		return removals[left].event < removals[right].event
	})
	for _, event := range droidToolEvents {
		removedEvent := false
		for _, removal := range removals {
			if removal.event != event {
				continue
			}
			removedEvent = true
			groupPath := appendDroidPath(document.basePath, removal.event, removal.groupIndex)
			if removal.removeGroup {
				if err := editor.remove(groupPath); err != nil {
					return nil, err
				}
				continue
			}
			sort.Sort(sort.Reverse(sort.IntSlice(removal.handlerIndexes)))
			for _, handlerIndex := range removal.handlerIndexes {
				if err := editor.remove(appendDroidPath(groupPath, "hooks", handlerIndex)); err != nil {
					return nil, err
				}
			}
		}
		if removedEvent {
			current, parseErr := droidHooksFromRaw(document, editor.pack())
			if parseErr != nil {
				return nil, parseErr
			}
			groups, exists := current[event].([]any)
			if exists && len(groups) == 0 {
				if err := editor.remove(appendDroidPath(document.basePath, event)); err != nil {
					return nil, err
				}
			}
		}
	}
	for _, event := range droidToolEvents {
		group, exists := additions[event]
		if !exists {
			continue
		}
		current, parseErr := droidHooksFromRaw(document, editor.pack())
		if parseErr != nil {
			return nil, parseErr
		}
		eventPath := appendDroidPath(document.basePath, event)
		if _, exists := current[event]; exists {
			err = editor.appendArray(eventPath, group)
		} else {
			err = editor.addProperty(document.basePath, event, []any{group})
		}
		if err != nil {
			return nil, err
		}
	}
	next := editor.pack()
	nextDocument, err := parseDroidDocument(
		document.exists, document.filePath, document.kind, next, document.snapshot,
	)
	if err != nil {
		return nil, err
	}
	remove := len(additions) == 0 && document.exists && document.ownedFile &&
		droidHookDocumentEmpty(nextDocument)
	return &droidRenderedDocument{
		document: document,
		changed:  remove || !bytes.Equal(next, document.raw),
		next:     next,
		remove:   remove,
	}, nil
}

func droidHasExactInstallation(
	document *droidDocument,
	additions map[string]map[string]any,
	runtimeRoot string,
) bool {
	if len(additions) != len(droidToolEvents) {
		return false
	}
	removals := managedDroidRemovals(document.hooks, "", runtimeRoot)
	if len(removals) != len(droidToolEvents) {
		return false
	}
	for _, event := range droidToolEvents {
		matches := make([]droidManagedRemoval, 0, 1)
		for _, removal := range removals {
			if removal.event == event {
				matches = append(matches, removal)
			}
		}
		if len(matches) != 1 || !matches[0].removeGroup {
			return false
		}
		groups := document.hooks[event].([]any)
		if !reflect.DeepEqual(groups[matches[0].groupIndex], additions[event]) {
			return false
		}
	}
	return true
}

func droidHooksFromRaw(
	document *droidDocument,
	raw []byte,
) (droidHookSettings, error) {
	current, err := parseDroidDocument(
		document.exists, document.filePath, document.kind, raw, document.snapshot,
	)
	if err != nil {
		return nil, err
	}
	return current.hooks, nil
}

func droidHookDocumentEmpty(document *droidDocument) bool {
	if document.kind == "settings" || document.kind == "local-settings" || len(document.hooks) > 0 {
		return false
	}
	for field := range document.root {
		if field != "hooks" {
			return false
		}
	}
	return true
}

func appendDroidPath(base []any, parts ...any) []any {
	path := make([]any, 0, len(base)+len(parts))
	path = append(path, base...)
	return append(path, parts...)
}

func displayDroidConfigPath(sources *droidSources) string {
	return activeDroidDocument(sources).filePath
}
