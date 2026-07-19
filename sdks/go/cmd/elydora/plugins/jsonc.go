package plugins

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/tailscale/hujson"
)

type jsoncEditor struct {
	value  hujson.Value
	eol    string
	indent string
	label  string
}

func standardizeJSONC(source []byte, label string, allowTrailingCommas bool) ([]byte, error) {
	value, err := parseJSONC(source, label, allowTrailingCommas)
	if err != nil {
		return nil, err
	}
	value.Standardize()
	return value.Pack(), nil
}

func parseJSONC(source []byte, label string, allowTrailingCommas bool) (hujson.Value, error) {
	value, err := hujson.Parse(source)
	if err != nil {
		return hujson.Value{}, fmt.Errorf("parse %s: %w", label, err)
	}
	if !allowTrailingCommas {
		if err := rejectJSONCTrailingCommas(&value, label, "$"); err != nil {
			return hujson.Value{}, err
		}
	}
	if err := rejectDuplicateJSONCKeys(&value, label, "$"); err != nil {
		return hujson.Value{}, err
	}
	return value, nil
}

func rejectJSONCTrailingCommas(value *hujson.Value, label, location string) error {
	switch current := value.Value.(type) {
	case *hujson.Object:
		if len(current.Members) > 0 && current.Members[len(current.Members)-1].Value.AfterExtra != nil {
			return fmt.Errorf("%s contains a trailing comma at %s", label, location)
		}
		for index := range current.Members {
			member := &current.Members[index]
			name := member.Name.Value.(hujson.Literal).String()
			if err := rejectJSONCTrailingCommas(&member.Value, label, location+"/"+name); err != nil {
				return err
			}
		}
	case *hujson.Array:
		if len(current.Elements) > 0 && current.Elements[len(current.Elements)-1].AfterExtra != nil {
			return fmt.Errorf("%s contains a trailing comma at %s", label, location)
		}
		for index := range current.Elements {
			if err := rejectJSONCTrailingCommas(
				&current.Elements[index], label, location+"/"+strconv.Itoa(index),
			); err != nil {
				return err
			}
		}
	}
	return nil
}

func rejectDuplicateJSONCKeys(value *hujson.Value, label, location string) error {
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
			if err := rejectDuplicateJSONCKeys(&member.Value, label, location+"/"+name); err != nil {
				return err
			}
		}
	case *hujson.Array:
		for index := range current.Elements {
			if err := rejectDuplicateJSONCKeys(
				&current.Elements[index], label, location+"/"+strconv.Itoa(index),
			); err != nil {
				return err
			}
		}
	}
	return nil
}

func decodeJSONCObject(source []byte, label string, allowTrailingCommas bool) (map[string]any, error) {
	value, err := parseJSONC(source, label, allowTrailingCommas)
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

func newJSONCEditor(source []byte, label string, allowTrailingCommas bool) (*jsoncEditor, error) {
	value, err := parseJSONC(source, label, allowTrailingCommas)
	if err != nil {
		return nil, err
	}
	return &jsoncEditor{
		value: value, eol: detectJSONCEOL(source), indent: detectJSONCIndent(source), label: label,
	}, nil
}

func detectJSONCEOL(source []byte) string {
	if bytes.Contains(source, []byte("\r\n")) {
		return "\r\n"
	}
	return "\n"
}

func detectJSONCIndent(source []byte) string {
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

func (editor *jsoncEditor) pack() []byte {
	return editor.value.Pack()
}

func (editor *jsoncEditor) remove(path []any) error {
	pointer := jsonPointer(path)
	operation := []map[string]any{{"op": "remove", "path": pointer}}
	patch, err := json.Marshal(operation)
	if err != nil {
		return fmt.Errorf("encode %s removal: %w", editor.label, err)
	}
	if err := editor.value.Patch(patch); err != nil {
		return fmt.Errorf("apply %s removal at %s: %w", editor.label, pointer, err)
	}
	return nil
}

func (editor *jsoncEditor) addProperty(objectPath []any, name string, value any) error {
	container, err := findJSONCValue(&editor.value, objectPath, editor.label)
	if err != nil {
		return err
	}
	object, ok := container.Value.(*hujson.Object)
	if !ok {
		return fmt.Errorf("%s path %s must be an object", editor.label, jsonPointer(objectPath))
	}
	for _, member := range object.Members {
		if member.Name.Value.(hujson.Literal).String() == name {
			return fmt.Errorf("%s property %q already exists", editor.label, name)
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

func (editor *jsoncEditor) appendArray(arrayPath []any, value any) error {
	container, err := findJSONCValue(&editor.value, arrayPath, editor.label)
	if err != nil {
		return err
	}
	array, ok := container.Value.(*hujson.Array)
	if !ok {
		return fmt.Errorf("%s path %s must be an array", editor.label, jsonPointer(arrayPath))
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

func (editor *jsoncEditor) formattedValue(value any, depth int) (hujson.Value, error) {
	prefix := strings.Repeat(editor.indent, depth)
	encoded, err := json.MarshalIndent(value, prefix, editor.indent)
	if err != nil {
		return hujson.Value{}, fmt.Errorf("encode value for %s: %w", editor.label, err)
	}
	if editor.eol != "\n" {
		encoded = bytes.ReplaceAll(encoded, []byte("\n"), []byte(editor.eol))
	}
	parsed, err := hujson.Parse(encoded)
	if err != nil {
		return hujson.Value{}, fmt.Errorf("parse generated value for %s: %w", editor.label, err)
	}
	return parsed, nil
}

func (editor *jsoncEditor) newLeadingExtra(current hujson.Extra, depth int) hujson.Extra {
	standard := []byte(editor.eol + strings.Repeat(editor.indent, depth))
	if bytes.Contains(current, []byte("//")) || bytes.Contains(current, []byte("/*")) {
		combined := append(hujson.Extra(nil), current...)
		return append(combined, standard...)
	}
	return standard
}

func findJSONCValue(root *hujson.Value, path []any, label string) (*hujson.Value, error) {
	current := root
	for _, part := range path {
		switch key := part.(type) {
		case string:
			object, ok := current.Value.(*hujson.Object)
			if !ok {
				return nil, fmt.Errorf("%s path %s must traverse an object", label, jsonPointer(path))
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
				return nil, fmt.Errorf("%s path %s does not exist", label, jsonPointer(path))
			}
		case int:
			array, ok := current.Value.(*hujson.Array)
			if !ok || key < 0 || key >= len(array.Elements) {
				return nil, fmt.Errorf("%s array path %s is out of range", label, jsonPointer(path))
			}
			current = &array.Elements[key]
		default:
			return nil, fmt.Errorf("unsupported %s path part %T", label, part)
		}
	}
	return current, nil
}

func jsonPointer(path []any) string {
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
