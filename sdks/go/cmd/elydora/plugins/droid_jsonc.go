package plugins

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/tailscale/hujson"
)

type droidJSONCEditor struct {
	value  hujson.Value
	eol    string
	indent string
}

func standardizeDroidJSONC(source []byte) ([]byte, error) {
	value, err := parseDroidJSONC(source, "JSONC source")
	if err != nil {
		return nil, err
	}
	value.Standardize()
	return value.Pack(), nil
}

func parseDroidJSONC(source []byte, label string) (hujson.Value, error) {
	value, err := hujson.Parse(source)
	if err != nil {
		return hujson.Value{}, fmt.Errorf("parse %s: %w", label, err)
	}
	if err := rejectDroidDuplicateKeys(&value, label, "$"); err != nil {
		return hujson.Value{}, err
	}
	return value, nil
}

func rejectDroidDuplicateKeys(value *hujson.Value, label, location string) error {
	switch current := value.Value.(type) {
	case *hujson.Object:
		seen := make(map[string]bool, len(current.Members))
		for index := range current.Members {
			member := &current.Members[index]
			name := member.Name.Value.(hujson.Literal).String()
			if seen[name] {
				return fmt.Errorf("%s contains duplicate key %q at %s", label, name, location)
			}
			seen[name] = true
			if err := rejectDroidDuplicateKeys(&member.Value, label, location+"/"+name); err != nil {
				return err
			}
		}
	case *hujson.Array:
		for index := range current.Elements {
			if err := rejectDroidDuplicateKeys(
				&current.Elements[index], label, location+"/"+strconv.Itoa(index),
			); err != nil {
				return err
			}
		}
	}
	return nil
}

func decodeDroidJSONCObject(source []byte, label string) (map[string]any, error) {
	value, err := parseDroidJSONC(source, label)
	if err != nil {
		return nil, err
	}
	if _, ok := value.Value.(*hujson.Object); !ok {
		return nil, fmt.Errorf("%s must contain a JSON object", label)
	}
	standard := value.Clone()
	standard.Standardize()
	var object map[string]any
	if err := json.Unmarshal(standard.Pack(), &object); err != nil {
		return nil, fmt.Errorf("decode %s: %w", label, err)
	}
	return object, nil
}

func newDroidJSONCEditor(source []byte, label string) (*droidJSONCEditor, error) {
	value, err := parseDroidJSONC(source, label)
	if err != nil {
		return nil, err
	}
	return &droidJSONCEditor{
		value: value, eol: detectDroidEOL(source), indent: detectDroidIndent(source),
	}, nil
}

func detectDroidEOL(source []byte) string {
	if bytes.Contains(source, []byte("\r\n")) {
		return "\r\n"
	}
	return "\n"
}

func detectDroidIndent(source []byte) string {
	normalized := strings.ReplaceAll(string(source), "\r\n", "\n")
	for _, line := range strings.Split(normalized, "\n") {
		trimmed := strings.TrimLeft(line, "\t ")
		if !strings.HasPrefix(trimmed, `"`) || trimmed == line {
			continue
		}
		return line[:len(line)-len(trimmed)]
	}
	return "  "
}

func (editor *droidJSONCEditor) pack() []byte {
	return editor.value.Pack()
}

func (editor *droidJSONCEditor) remove(path []any) error {
	operation := []map[string]any{{"op": "remove", "path": droidJSONPointer(path)}}
	patch, err := json.Marshal(operation)
	if err != nil {
		return fmt.Errorf("encode Factory Droid JSONC removal: %w", err)
	}
	if err := editor.value.Patch(patch); err != nil {
		return fmt.Errorf("apply Factory Droid JSONC removal at %s: %w", droidJSONPointer(path), err)
	}
	return nil
}

func (editor *droidJSONCEditor) addProperty(objectPath []any, name string, value any) error {
	container, err := findDroidJSONCValue(&editor.value, objectPath)
	if err != nil {
		return err
	}
	object, ok := container.Value.(*hujson.Object)
	if !ok {
		return fmt.Errorf("Factory Droid JSONC path %s must be an object", droidJSONPointer(objectPath))
	}
	for _, member := range object.Members {
		if member.Name.Value.(hujson.Literal).String() == name {
			return fmt.Errorf("Factory Droid JSONC property %q already exists", name)
		}
	}
	propertyDepth := len(objectPath) + 1
	formatted, err := editor.formattedValue(value, propertyDepth)
	if err != nil {
		return err
	}
	trailingComma := len(object.Members) > 0 &&
		object.Members[len(object.Members)-1].Value.AfterExtra != nil
	leading := editor.newLeadingExtra(object.AfterExtra, propertyDepth)
	object.AfterExtra = []byte(editor.eol + strings.Repeat(editor.indent, len(objectPath)))
	member := hujson.ObjectMember{
		Name:  hujson.Value{BeforeExtra: leading, Value: hujson.String(name)},
		Value: hujson.Value{BeforeExtra: []byte(" "), Value: formatted.Value},
	}
	if trailingComma {
		member.Value.AfterExtra = []byte{}
	}
	object.Members = append(object.Members, member)
	return nil
}

func (editor *droidJSONCEditor) appendArray(arrayPath []any, value any) error {
	container, err := findDroidJSONCValue(&editor.value, arrayPath)
	if err != nil {
		return err
	}
	array, ok := container.Value.(*hujson.Array)
	if !ok {
		return fmt.Errorf("Factory Droid JSONC path %s must be an array", droidJSONPointer(arrayPath))
	}
	arrayDepth := len(arrayPath)
	formatted, err := editor.formattedValue(value, arrayDepth+1)
	if err != nil {
		return err
	}
	trailingComma := len(array.Elements) > 0 && array.Elements[len(array.Elements)-1].AfterExtra != nil
	formatted.BeforeExtra = editor.newLeadingExtra(array.AfterExtra, arrayDepth+1)
	if trailingComma {
		formatted.AfterExtra = []byte{}
	} else {
		formatted.AfterExtra = nil
	}
	array.AfterExtra = []byte(editor.eol + strings.Repeat(editor.indent, arrayDepth))
	array.Elements = append(array.Elements, formatted)
	return nil
}

func (editor *droidJSONCEditor) formattedValue(value any, depth int) (hujson.Value, error) {
	prefix := strings.Repeat(editor.indent, depth)
	encoded, err := json.MarshalIndent(value, prefix, editor.indent)
	if err != nil {
		return hujson.Value{}, fmt.Errorf("encode Factory Droid hook value: %w", err)
	}
	if editor.eol != "\n" {
		encoded = bytes.ReplaceAll(encoded, []byte("\n"), []byte(editor.eol))
	}
	parsed, err := hujson.Parse(encoded)
	if err != nil {
		return hujson.Value{}, fmt.Errorf("parse generated Factory Droid hook value: %w", err)
	}
	return parsed, nil
}

func (editor *droidJSONCEditor) newLeadingExtra(current hujson.Extra, depth int) hujson.Extra {
	standard := []byte(editor.eol + strings.Repeat(editor.indent, depth))
	if bytes.Contains(current, []byte("//")) || bytes.Contains(current, []byte("/*")) {
		combined := append(hujson.Extra(nil), current...)
		return append(combined, standard...)
	}
	return standard
}

func findDroidJSONCValue(root *hujson.Value, path []any) (*hujson.Value, error) {
	current := root
	for _, part := range path {
		switch key := part.(type) {
		case string:
			object, ok := current.Value.(*hujson.Object)
			if !ok {
				return nil, fmt.Errorf("Factory Droid JSONC path %s must traverse an object", droidJSONPointer(path))
			}
			found := false
			for index := range object.Members {
				if object.Members[index].Name.Value.(hujson.Literal).String() == key {
					current = &object.Members[index].Value
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("Factory Droid JSONC path %s does not exist", droidJSONPointer(path))
			}
		case int:
			array, ok := current.Value.(*hujson.Array)
			if !ok || key < 0 || key >= len(array.Elements) {
				return nil, fmt.Errorf("Factory Droid JSONC array path %s is out of range", droidJSONPointer(path))
			}
			current = &array.Elements[key]
		default:
			return nil, fmt.Errorf("unsupported Factory Droid JSONC path part %T", part)
		}
	}
	return current, nil
}

func droidJSONPointer(path []any) string {
	var pointer strings.Builder
	for _, part := range path {
		pointer.WriteByte('/')
		switch value := part.(type) {
		case string:
			value = strings.ReplaceAll(value, "~", "~0")
			pointer.WriteString(strings.ReplaceAll(value, "/", "~1"))
		case int:
			pointer.WriteString(strconv.Itoa(value))
		}
	}
	return pointer.String()
}
