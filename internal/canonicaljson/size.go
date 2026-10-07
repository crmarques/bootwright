package canonicaljson

import (
	"encoding"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"
)

var (
	rawMessage    = reflect.TypeFor[json.RawMessage]()
	jsonMarshaler = reflect.TypeFor[json.Marshaler]()
	textMarshaler = reflect.TypeFor[encoding.TextMarshaler]()
)

// Size predicts the exact length encoding/json would give value, without a
// line feed, before anything allocates the encoding. It refuses a length past
// limit, nesting past depth and any shape whose encoding it cannot predict
// exactly: a map, a float, a byte slice, an embedded or unexported field, a
// tag option other than omitempty, or a type that encodes itself.
func Size(value any, limit, depth int) (int, bool) {
	if limit < 0 || depth < 0 {
		return 0, false
	}
	predictor := sizePredictor{limit: limit, depth: depth}
	if !predictor.value(reflect.ValueOf(value), 0) {
		return 0, false
	}
	return predictor.count, true
}

type sizePredictor struct {
	limit, depth, count int
}

func (p *sizePredictor) add(n int) bool {
	if n < 0 || n > p.limit-p.count {
		return false
	}
	p.count += n
	return true
}

func (p *sizePredictor) value(v reflect.Value, depth int) bool {
	if depth > p.depth {
		return false
	}
	if !v.IsValid() {
		return p.add(4)
	}
	if v.Type() == rawMessage {
		return p.raw(v.Bytes())
	}
	if encodesItself(v.Type()) {
		return false
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return p.add(4)
		}
		return p.value(v.Elem(), depth)
	case reflect.Bool:
		if v.Bool() {
			return p.add(4)
		}
		return p.add(5)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		var scratch [24]byte
		return p.add(len(strconv.AppendInt(scratch[:0], v.Int(), 10)))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		var scratch [24]byte
		return p.add(len(strconv.AppendUint(scratch[:0], v.Uint(), 10)))
	case reflect.String:
		return p.str(v.String())
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return false
		}
		if v.IsNil() {
			return p.add(4)
		}
		return p.sequence(v, depth)
	case reflect.Array:
		return p.sequence(v, depth)
	case reflect.Struct:
		return p.structure(v, depth)
	}
	return false
}

func encodesItself(t reflect.Type) bool {
	pointer := reflect.PointerTo(t)
	return t.Implements(jsonMarshaler) || t.Implements(textMarshaler) || pointer.Implements(jsonMarshaler) || pointer.Implements(textMarshaler)
}

func (p *sizePredictor) sequence(v reflect.Value, depth int) bool {
	if !p.add(2) {
		return false
	}
	for index := 0; index < v.Len(); index++ {
		if index > 0 && !p.add(1) {
			return false
		}
		if !p.value(v.Index(index), depth+1) {
			return false
		}
	}
	return true
}

func (p *sizePredictor) structure(v reflect.Value, depth int) bool {
	if !p.add(2) {
		return false
	}
	written := 0
	for index := 0; index < v.NumField(); index++ {
		field := v.Type().Field(index)
		if field.Anonymous || !field.IsExported() {
			return false
		}
		tag := field.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, option, _ := strings.Cut(tag, ",")
		item := v.Field(index)
		switch option {
		case "":
		case "omitempty":
			empty, predictable := emptyValue(item)
			if !predictable {
				return false
			}
			if empty {
				continue
			}
		default:
			return false
		}
		if name == "" {
			name = field.Name
		}
		if written > 0 && !p.add(1) {
			return false
		}
		written++
		if !p.str(name) || !p.add(1) || !p.value(item, depth+1) {
			return false
		}
	}
	return true
}

func emptyValue(v reflect.Value) (bool, bool) {
	switch v.Kind() {
	case reflect.Bool:
		return !v.Bool(), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0, true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0, true
	case reflect.String, reflect.Slice:
		return v.Len() == 0, true
	case reflect.Pointer:
		return v.IsNil(), true
	}
	return false, false
}

func (p *sizePredictor) str(value string) bool {
	if len(value) > p.limit-p.count {
		return false
	}
	size := 2
	for index := 0; index < len(value); {
		c := value[index]
		if c < utf8.RuneSelf {
			switch {
			case c == '"' || c == '\\' || c == '\b' || c == '\f' || c == '\n' || c == '\r' || c == '\t':
				size += 2
			case c < 0x20 || c == '<' || c == '>' || c == '&':
				size += 6
			default:
				size++
			}
			index++
			continue
		}
		r, width := utf8.DecodeRuneInString(value[index:])
		if r == utf8.RuneError && width == 1 || r == '\u2028' || r == '\u2029' {
			size += 6
		} else {
			size += width
		}
		index += width
	}
	return p.add(size)
}

// raw counts a raw value as encoding/json writes it: compacted, with <, >, &
// and the two line separators escaped. A raw value that is not valid JSON,
// including an absent one, refuses, because what it would write is no record.
func (p *sizePredictor) raw(raw []byte) bool {
	if !json.Valid(raw) {
		return false
	}
	quoted, escaped := false, false
	for index := 0; index < len(raw); {
		c := raw[index]
		size, width := 1, 1
		switch {
		case !quoted && (c == ' ' || c == '\t' || c == '\n' || c == '\r'):
			size = 0
		case c == '<' || c == '>' || c == '&':
			size = 6
		case c == 0xE2 && index+2 < len(raw) && raw[index+1] == 0x80 && raw[index+2]&^1 == 0xA8:
			size, width = 6, 3
		}
		if !p.add(size) {
			return false
		}
		switch {
		case escaped:
			escaped = false
		case quoted && c == '\\':
			escaped = true
		case c == '"':
			quoted = !quoted
		}
		index += width
	}
	return true
}
