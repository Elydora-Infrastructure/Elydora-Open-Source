package plugins

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type droidDocument struct {
	kind              string
	filePath          string
	exists            bool
	raw               []byte
	hooks             droidHookSettings
	basePath          []any
	hasHooksContainer bool
	ownedFile         bool
}

type droidSources struct {
	rootPath string
	primary  *droidDocument
	settings *droidDocument
}

type droidInstallationTargets struct {
	targets     map[string]*droidDocument
	createdRoot *droidDocument
}

type droidRenderedDocument struct {
	document *droidDocument
	changed  bool
	next     []byte
	remove   bool
}

func droidFactoryPaths() (string, string, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", "", fmt.Errorf("resolve home directory: %w", err)
	}
	directory := filepath.Join(home, ".factory")
	return filepath.Join(directory, "hooks.json"),
		filepath.Join(directory, "hooks", "hooks.json"),
		filepath.Join(directory, "settings.json"), nil
}

func readDroidSources() (*droidSources, error) {
	rootPath, legacyPath, settingsPath, err := droidFactoryPaths()
	if err != nil {
		return nil, err
	}
	rootRaw, rootExists, err := readOptionalFile(rootPath, "Factory Droid hooks")
	if err != nil {
		return nil, err
	}
	var primary *droidDocument
	if rootExists {
		primary, err = parseDroidDocument(true, rootPath, "hooks", rootRaw)
		if err != nil {
			return nil, err
		}
	} else {
		legacyRaw, legacyExists, readErr := readOptionalFile(legacyPath, "Factory Droid legacy hooks")
		if readErr != nil {
			return nil, readErr
		}
		if legacyExists {
			primary, err = parseDroidDocument(true, legacyPath, "legacy", legacyRaw)
			if err != nil {
				return nil, err
			}
		}
	}
	settingsRaw, settingsExists, err := readOptionalFile(settingsPath, "Factory Droid settings")
	if err != nil {
		return nil, err
	}
	var settings *droidDocument
	if settingsExists {
		settings, err = parseDroidDocument(true, settingsPath, "settings", settingsRaw)
	} else {
		settings, err = parseDroidDocument(false, settingsPath, "settings", []byte("{}\n"))
	}
	if err != nil {
		return nil, err
	}
	return &droidSources{rootPath: rootPath, primary: primary, settings: settings}, nil
}

func parseDroidDocument(exists bool, filePath, kind string, raw []byte) (*droidDocument, error) {
	label := droidDocumentLabel(kind, filePath)
	root, err := decodeJSONCObject(raw, label, true)
	if err != nil {
		return nil, err
	}
	document := &droidDocument{
		kind: kind, filePath: filePath, exists: exists, raw: append([]byte(nil), raw...),
	}
	if kind == "settings" {
		hooksValue, hasHooks := root["hooks"]
		document.basePath = []any{"hooks"}
		document.hasHooksContainer = hasHooks
		if !hasHooks {
			document.hooks = droidHookSettings{}
			return document, nil
		}
		document.hooks, err = readDroidHookSettings(hooksValue, label+` field "hooks"`)
		return document, err
	}
	document.hooks, err = readDroidHookSettings(root, label)
	document.hasHooksContainer = true
	document.ownedFile = bytes.HasPrefix(raw, []byte(droidOwnedFileMarker))
	return document, err
}

func droidDocumentLabel(kind, filePath string) string {
	switch kind {
	case "settings":
		return fmt.Sprintf("Factory Droid settings at %s", filePath)
	case "legacy":
		return fmt.Sprintf("Factory Droid legacy hooks at %s", filePath)
	default:
		return fmt.Sprintf("Factory Droid hooks at %s", filePath)
	}
}

func createOwnedDroidDocument(filePath string) (*droidDocument, error) {
	return parseDroidDocument(
		false, filePath, "hooks", []byte(droidOwnedFileMarker+"\n{}\n"),
	)
}

func selectDroidInstallationTargets(sources *droidSources) (*droidInstallationTargets, error) {
	selection := &droidInstallationTargets{targets: make(map[string]*droidDocument, len(droidToolEvents))}
	for _, event := range droidToolEvents {
		switch {
		case sources.primary != nil && hasDroidHookField(sources.primary.hooks, event):
			selection.targets[event] = sources.primary
		case sources.settings.hasHooksContainer && hasDroidHookField(sources.settings.hooks, event):
			selection.targets[event] = sources.settings
		case sources.primary != nil:
			selection.targets[event] = sources.primary
		case sources.settings.hasHooksContainer:
			selection.targets[event] = sources.settings
		default:
			if selection.createdRoot == nil {
				created, err := createOwnedDroidDocument(sources.rootPath)
				if err != nil {
					return nil, err
				}
				selection.createdRoot = created
			}
			selection.targets[event] = selection.createdRoot
		}
	}
	return selection, nil
}

func hasDroidHookField(settings droidHookSettings, key string) bool {
	_, exists := settings[key]
	return exists
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
	document *droidDocument,
	targets map[string]*droidDocument,
	groups map[string]map[string]any,
) map[string]map[string]any {
	additions := map[string]map[string]any{}
	for _, event := range droidToolEvents {
		if sameDroidPath(targets[event].filePath, document.filePath) {
			additions[event] = groups[event]
		}
	}
	return additions
}

func renderDroidDocument(
	document *droidDocument,
	agentID, runtimeRoot string,
	additions map[string]map[string]any,
) (*droidRenderedDocument, error) {
	editor, err := newJSONCEditor(document.raw, droidDocumentLabel(document.kind, document.filePath), true)
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
	for _, removal := range removals {
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
	current, err := parseDroidDocument(document.exists, document.filePath, document.kind, editor.pack())
	if err != nil {
		return nil, err
	}
	if document.ownedFile {
		for _, event := range droidToolEvents {
			groups, exists := current.hooks[event].([]any)
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
		current, err = parseDroidDocument(document.exists, document.filePath, document.kind, editor.pack())
		if err != nil {
			return nil, err
		}
		eventPath := appendDroidPath(document.basePath, event)
		if hasDroidHookField(current.hooks, event) {
			err = editor.appendArray(eventPath, group)
		} else {
			err = editor.addProperty(document.basePath, event, []any{group})
		}
		if err != nil {
			return nil, err
		}
	}
	next := editor.pack()
	nextDocument, err := parseDroidDocument(document.exists, document.filePath, document.kind, next)
	if err != nil {
		return nil, err
	}
	remove := len(additions) == 0 && document.ownedFile && document.kind != "settings" && len(nextDocument.hooks) == 0
	return &droidRenderedDocument{
		document: document, changed: remove || !bytes.Equal(next, document.raw), next: next, remove: remove,
	}, nil
}

func appendDroidPath(base []any, parts ...any) []any {
	path := make([]any, 0, len(base)+len(parts))
	path = append(path, base...)
	return append(path, parts...)
}

func displayDroidConfigPath(sources *droidSources) string {
	if sources.primary != nil {
		return sources.primary.filePath
	}
	if sources.settings.exists && sources.settings.hasHooksContainer {
		return sources.settings.filePath
	}
	return sources.rootPath
}
