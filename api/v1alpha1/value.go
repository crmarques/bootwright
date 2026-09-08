package v1alpha1

import (
	"math/big"
	"slices"
	"strconv"
)

type ValueType uint8

const (
	Absent ValueType = iota
	String
	Boolean
	Integer
	Number
	Mapping
	Sequence
)

type FieldValue struct {
	Name  string
	Value Value
}

type Value struct {
	typeOf  ValueType
	text    string
	boolean bool
	fields  []FieldValue
	items   []Value
}

func StringValue(s string) Value { return Value{typeOf: String, text: s} }
func BoolValue(b bool) Value     { return Value{typeOf: Boolean, boolean: b} }
func IntegerValue(decimal string) Value {
	n, ok := new(big.Int).SetString(decimal, 10)
	if !ok {
		return Value{}
	}
	return Value{typeOf: Integer, text: n.String()}
}
func NumberValue(decimal string) Value { return Value{typeOf: Number, text: decimal} }
func MapValue(fields ...FieldValue) Value {
	return Value{typeOf: Mapping, fields: slices.Clone(fields)}
}
func ListValue(items ...Value) Value { return Value{typeOf: Sequence, items: slices.Clone(items)} }
func (v Value) Type() ValueType      { return v.typeOf }
func (v Value) Present() bool        { return v.typeOf != Absent }
func (v Value) Text() string         { return v.text }
func (v Value) Bool() bool           { return v.boolean }
func (v Value) Int64() (int64, bool) {
	n, e := strconv.ParseInt(v.text, 10, 64)
	return n, e == nil && v.typeOf == Integer
}
func (v Value) Uint64() (uint64, bool) {
	n, e := strconv.ParseUint(v.text, 10, 64)
	return n, e == nil && v.typeOf == Integer
}
func (v Value) Float64() (float64, bool) {
	n, e := strconv.ParseFloat(v.text, 64)
	return n, e == nil && (v.typeOf == Integer || v.typeOf == Number)
}
func (v Value) Fields() []FieldValue { return slices.Clone(v.fields) }
func (v Value) Items() []Value       { return slices.Clone(v.items) }
func (v Value) Len() int {
	if v.typeOf == Mapping {
		return len(v.fields)
	}
	return len(v.items)
}
func (v Value) Get(path ...string) Value {
	for _, name := range path {
		found := Value{}
		for _, f := range v.fields {
			if f.Name == name {
				found = f.Value
				break
			}
		}
		v = found
	}
	return v
}
func (v Value) Has(path ...string) bool { return v.Get(path...).Present() }
func (v Value) With(name string, value Value) Value {
	fields := slices.Clone(v.fields)
	for i, f := range fields {
		if f.Name == name {
			fields[i].Value = value
			return MapValue(fields...)
		}
	}
	return MapValue(append(fields, FieldValue{Name: name, Value: value})...)
}
func (v Value) Without(name string) Value {
	fields := make([]FieldValue, 0, len(v.fields))
	for _, f := range v.fields {
		if f.Name != name {
			fields = append(fields, f)
		}
	}
	return MapValue(fields...)
}
func (v Value) WithPath(value Value, path ...string) Value {
	if len(path) == 0 {
		return value
	}
	return v.With(path[0], v.Get(path[0]).WithPath(value, path[1:]...))
}
func (v Value) Default(name string, value Value) Value {
	if v.Has(name) {
		return v
	}
	return v.With(name, value)
}
func (v Value) Strings() []string {
	out := make([]string, 0, len(v.items))
	for _, item := range v.items {
		out = append(out, item.Text())
	}
	return out
}
func StringList(items ...string) Value {
	out := make([]Value, len(items))
	for i, s := range items {
		out[i] = StringValue(s)
	}
	return ListValue(out...)
}
func (v Value) Equal(other Value) bool {
	if v.typeOf != other.typeOf || v.text != other.text || v.boolean != other.boolean || len(v.fields) != len(other.fields) || len(v.items) != len(other.items) {
		return false
	}
	for _, f := range v.fields {
		if !f.Value.Equal(other.Get(f.Name)) {
			return false
		}
	}
	for i, x := range v.items {
		if !x.Equal(other.items[i]) {
			return false
		}
	}
	return true
}
