package plugins

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

type kimiByteRange struct {
	start int
	end   int
}

type kimiTomlLayout struct {
	hookTables     []kimiByteRange
	inlineValue    *kimiByteRange
	inlineElements []kimiByteRange
}

type kimiDocument struct {
	contract kimiContract
	raw      []byte
	hooks    []kimiHook
	layout   kimiTomlLayout
}

func parseKimiDocument(contract kimiContract, raw []byte, exists bool) (kimiDocument, error) {
	if !exists {
		return kimiDocument{contract: contract, raw: []byte{}, hooks: []kimiHook{}}, nil
	}
	root := map[string]any{}
	if err := toml.Unmarshal(raw, &root); err != nil {
		return kimiDocument{}, fmt.Errorf(
			"parse %s at %s: %w",
			contract.label,
			contract.configPath,
			err,
		)
	}
	hooks, err := kimiHooks(root, contract)
	if err != nil {
		return kimiDocument{}, err
	}
	layout, err := scanKimiTomlLayout(raw)
	if err != nil {
		return kimiDocument{}, fmt.Errorf(
			"inspect %s layout at %s: %w",
			contract.label,
			contract.configPath,
			err,
		)
	}
	if len(hooks) > 0 && len(layout.hookTables) == 0 && layout.inlineValue == nil {
		return kimiDocument{}, fmt.Errorf("%s hooks layout could not be located", contract.label)
	}
	if len(layout.hookTables) > 0 && len(layout.hookTables) != len(hooks) {
		return kimiDocument{}, fmt.Errorf(
			"%s hook layout count %d does not match decoded hook count %d",
			contract.label,
			len(layout.hookTables),
			len(hooks),
		)
	}
	if layout.inlineValue != nil && len(layout.inlineElements) != len(hooks) {
		return kimiDocument{}, fmt.Errorf(
			"%s inline hook layout count %d does not match decoded hook count %d",
			contract.label,
			len(layout.inlineElements),
			len(hooks),
		)
	}
	return kimiDocument{
		contract: contract,
		raw:      append([]byte(nil), raw...),
		hooks:    hooks,
		layout:   layout,
	}, nil
}

func scanKimiTomlLayout(raw []byte) (kimiTomlLayout, error) {
	parser := unstable.Parser{KeepComments: true}
	parser.Reset(raw)
	var tableStarts, hookStarts []int
	var inlineValue *kimiByteRange
	var inlineElements []kimiByteRange
	contextIsRoot := true

	for parser.NextExpression() {
		expression := parser.Expression()
		switch expression.Kind {
		case unstable.Table, unstable.ArrayTable:
			keys := kimiTomlKeys(expression)
			keyRange, err := kimiFirstKeyRange(expression)
			if err != nil {
				return kimiTomlLayout{}, err
			}
			start := kimiLineStart(raw, int(keyRange.Offset))
			tableStarts = append(tableStarts, start)
			contextIsRoot = false
			if expression.Kind == unstable.ArrayTable && len(keys) == 1 && keys[0] == "hooks" {
				hookStarts = append(hookStarts, start)
			}
		case unstable.KeyValue:
			keys := kimiTomlKeys(expression)
			if !contextIsRoot || len(keys) != 1 || keys[0] != "hooks" {
				continue
			}
			value := expression.Value()
			if value.Kind != unstable.Array {
				continue
			}
			rangeValue, err := kimiArrayValueRange(raw, expression)
			if err != nil {
				return kimiTomlLayout{}, err
			}
			inlineValue = &rangeValue
			inlineElements, err = kimiInlineElementRanges(raw, value)
			if err != nil {
				return kimiTomlLayout{}, err
			}
		}
	}
	if err := parser.Error(); err != nil {
		return kimiTomlLayout{}, err
	}
	if len(hookStarts) > 0 && inlineValue != nil {
		return kimiTomlLayout{}, fmt.Errorf("hooks use conflicting TOML layouts")
	}

	sort.Ints(tableStarts)
	tables := make([]kimiByteRange, 0, len(hookStarts))
	for _, start := range hookStarts {
		end := len(raw)
		for _, candidate := range tableStarts {
			if candidate > start {
				end = candidate
				break
			}
		}
		tables = append(tables, kimiByteRange{start: start, end: end})
	}
	return kimiTomlLayout{
		hookTables:     tables,
		inlineValue:    inlineValue,
		inlineElements: inlineElements,
	}, nil
}

func kimiArrayValueRange(raw []byte, expression *unstable.Node) (kimiByteRange, error) {
	keyRange, err := kimiFirstKeyRange(expression)
	if err != nil {
		return kimiByteRange{}, err
	}
	start := int(keyRange.Offset + keyRange.Length)
	end := int(expression.Raw.Offset + expression.Raw.Length)
	if start < 0 || start > end || end > len(raw) {
		return kimiByteRange{}, fmt.Errorf("hooks array range is invalid")
	}
	equals := bytes.IndexByte(raw[start:end], '=')
	if equals < 0 {
		return kimiByteRange{}, fmt.Errorf("hooks array assignment has no equals sign")
	}
	start += equals + 1
	for start < end && (raw[start] == ' ' || raw[start] == '\t') {
		start++
	}
	if start >= end || raw[start] != '[' {
		return kimiByteRange{}, fmt.Errorf("hooks array value has no opening bracket")
	}
	return kimiByteRange{start: start, end: end}, nil
}

func kimiInlineElementRanges(raw []byte, array *unstable.Node) ([]kimiByteRange, error) {
	ranges := []kimiByteRange{}
	children := array.Children()
	for children.Next() {
		child := children.Node()
		if child.Kind != unstable.InlineTable {
			continue
		}
		start := int(child.Raw.Offset)
		end, err := kimiInlineTableEnd(raw, start)
		if err != nil {
			return nil, err
		}
		ranges = append(ranges, kimiByteRange{start: start, end: end})
	}
	return ranges, nil
}

func kimiInlineTableEnd(raw []byte, start int) (int, error) {
	if start < 0 || start >= len(raw) || raw[start] != '{' {
		return 0, fmt.Errorf("inline hook table has no opening brace")
	}
	depth := 0
	for index := start; index < len(raw); {
		switch raw[index] {
		case '"', '\'':
			end, err := kimiTomlStringEnd(raw, index)
			if err != nil {
				return 0, err
			}
			index = end
		case '#':
			if newline := bytes.IndexByte(raw[index:], '\n'); newline >= 0 {
				index += newline + 1
			} else {
				return 0, fmt.Errorf("inline hook table comment reaches end of document")
			}
		case '{':
			depth++
			index++
		case '}':
			depth--
			index++
			if depth == 0 {
				return index, nil
			}
		default:
			index++
		}
	}
	return 0, fmt.Errorf("inline hook table has no closing brace")
}

func kimiTomlStringEnd(raw []byte, start int) (int, error) {
	delimiter := raw[start]
	multiline := start+2 < len(raw) && raw[start+1] == delimiter && raw[start+2] == delimiter
	index := start + 1
	if multiline {
		index = start + 3
	}
	for index < len(raw) {
		if delimiter == '"' && raw[index] == '\\' {
			index += 2
			continue
		}
		if raw[index] != delimiter {
			index++
			continue
		}
		if !multiline {
			return index + 1, nil
		}
		runEnd := index
		for runEnd < len(raw) && raw[runEnd] == delimiter {
			runEnd++
		}
		if runEnd-index >= 3 {
			return runEnd, nil
		}
		index = runEnd
	}
	return 0, fmt.Errorf("inline hook table contains an unterminated string")
}

func kimiTomlKeys(node *unstable.Node) []string {
	keys := []string{}
	iterator := node.Key()
	for iterator.Next() {
		keys = append(keys, string(iterator.Node().Data))
	}
	return keys
}

func kimiFirstKeyRange(node *unstable.Node) (unstable.Range, error) {
	iterator := node.Key()
	if !iterator.Next() {
		return unstable.Range{}, fmt.Errorf("TOML expression has no key")
	}
	return iterator.Node().Raw, nil
}

func kimiLineStart(raw []byte, offset int) int {
	if offset <= 0 {
		return 0
	}
	lineBreak := bytes.LastIndexByte(raw[:offset], '\n')
	if lineBreak < 0 {
		return 0
	}
	return lineBreak + 1
}

func renderKimiHooks(document kimiDocument, keepIndices []int, additions []kimiHook) ([]byte, error) {
	keep := make(map[int]struct{}, len(keepIndices))
	for _, index := range keepIndices {
		if index < 0 || index >= len(document.hooks) {
			return nil, fmt.Errorf("%s hook index %d is out of range", document.contract.label, index)
		}
		keep[index] = struct{}{}
	}

	if document.layout.inlineValue != nil {
		return renderKimiInlineDocument(document, keep, additions)
	}

	base := document.raw
	if len(document.layout.hookTables) > 0 {
		var result bytes.Buffer
		cursor := 0
		for index, block := range document.layout.hookTables {
			if _, exists := keep[index]; exists {
				continue
			}
			result.Write(base[cursor:block.start])
			cursor = block.end
		}
		result.Write(base[cursor:])
		base = result.Bytes()
	}
	if len(additions) == 0 {
		return append([]byte(nil), base...), nil
	}
	return appendKimiHookTables(base, additions, kimiNewline(document.raw)), nil
}

func renderKimiInlineDocument(
	document kimiDocument,
	keep map[int]struct{},
	additions []kimiHook,
) ([]byte, error) {
	valueRange := *document.layout.inlineValue
	value := document.raw[valueRange.start:valueRange.end]
	if len(value) < 2 || value[0] != '[' || value[len(value)-1] != ']' {
		return nil, fmt.Errorf("%s inline hooks value is malformed", document.contract.label)
	}
	local := make([]kimiByteRange, len(document.layout.inlineElements))
	for index, element := range document.layout.inlineElements {
		local[index] = kimiByteRange{
			start: element.start - valueRange.start,
			end:   element.end - valueRange.start,
		}
	}

	var rendered bytes.Buffer
	rendered.WriteByte('[')
	contentStart := 1
	pending := []byte{}
	if len(local) > 0 {
		pending = append(pending, value[contentStart:local[0].start]...)
	} else {
		pending = append(pending, value[contentStart:len(value)-1]...)
	}
	emitted := 0
	for index, element := range local {
		if _, exists := keep[index]; exists {
			if emitted > 0 {
				rendered.WriteByte(',')
			}
			rendered.Write(kimiGapWithoutCommas(pending))
			rendered.Write(value[element.start:element.end])
			pending = pending[:0]
			emitted++
		}
		next := len(value) - 1
		if index+1 < len(local) {
			next = local[index+1].start
		}
		pending = append(pending, value[element.end:next]...)
	}
	for _, hook := range additions {
		if emitted > 0 {
			rendered.WriteByte(',')
		}
		if len(pending) > 0 {
			rendered.Write(kimiGapWithoutCommas(pending))
			pending = pending[:0]
		} else if emitted > 0 {
			rendered.WriteByte(' ')
		}
		rendered.WriteString(renderKimiInlineTable(hook))
		emitted++
	}
	rendered.Write(kimiGapWithoutCommas(pending))
	rendered.WriteByte(']')

	result := make([]byte, 0, len(document.raw)-len(value)+rendered.Len())
	result = append(result, document.raw[:valueRange.start]...)
	result = append(result, rendered.Bytes()...)
	result = append(result, document.raw[valueRange.end:]...)
	return result, nil
}

func kimiGapWithoutCommas(gap []byte) []byte {
	result := make([]byte, 0, len(gap))
	inComment := false
	for _, character := range gap {
		if inComment {
			result = append(result, character)
			if character == '\n' {
				inComment = false
			}
			continue
		}
		if character == '#' {
			inComment = true
			result = append(result, character)
			continue
		}
		if character != ',' {
			result = append(result, character)
		}
	}
	return result
}

func appendKimiHookTables(base []byte, hooks []kimiHook, newline string) []byte {
	result := append([]byte(nil), base...)
	if len(result) > 0 {
		if !bytes.HasSuffix(result, []byte(newline)) {
			result = append(result, newline...)
		}
		if !bytes.HasSuffix(result, []byte(newline+newline)) {
			result = append(result, newline...)
		}
	}
	for index, hook := range hooks {
		if index > 0 {
			result = append(result, newline...)
		}
		result = append(result, renderKimiHookTable(hook, newline)...)
	}
	return result
}

func renderKimiHookTable(hook kimiHook, newline string) []byte {
	var result strings.Builder
	result.WriteString("[[hooks]]")
	result.WriteString(newline)
	result.WriteString("event = ")
	result.WriteString(strconv.Quote(hook.event))
	result.WriteString(newline)
	if hook.matcher != nil {
		result.WriteString("matcher = ")
		result.WriteString(strconv.Quote(*hook.matcher))
		result.WriteString(newline)
	}
	result.WriteString("command = ")
	result.WriteString(strconv.Quote(hook.command))
	result.WriteString(newline)
	if hook.timeout != nil {
		result.WriteString("timeout = ")
		result.WriteString(strconv.FormatInt(*hook.timeout, 10))
		result.WriteString(newline)
	}
	return []byte(result.String())
}

func renderKimiInlineTable(hook kimiHook) string {
	fields := []string{"event = " + strconv.Quote(hook.event)}
	if hook.matcher != nil {
		fields = append(fields, "matcher = "+strconv.Quote(*hook.matcher))
	}
	fields = append(fields, "command = "+strconv.Quote(hook.command))
	if hook.timeout != nil {
		fields = append(fields, "timeout = "+strconv.FormatInt(*hook.timeout, 10))
	}
	return "{ " + strings.Join(fields, ", ") + " }"
}

func kimiNewline(raw []byte) string {
	if bytes.Contains(raw, []byte("\r\n")) {
		return "\r\n"
	}
	return "\n"
}
