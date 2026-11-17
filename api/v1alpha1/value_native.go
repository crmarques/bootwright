package v1alpha1

import (
	"encoding/json"
	"errors"
)

type NumberStyle int

const (
	NumberText NumberStyle = iota
	NumberBinary
	NumberInteger
)

type NativeOptions struct {
	Numbers          NumberStyle
	RefuseAbsent     bool
	RefuseDuplicates bool
}

func (v Value) Native(style NumberStyle) any {
	native, _ := v.NativeChecked(NativeOptions{Numbers: style})
	return native
}

func (v Value) NativeChecked(options NativeOptions) (any, error) {
	switch v.Type() {
	case Mapping:
		result := map[string]any{}
		for _, field := range v.Fields() {
			if _, exists := result[field.Name]; exists && options.RefuseDuplicates {
				return nil, errors.New("duplicate object field " + field.Name)
			}
			value, err := field.Value.NativeChecked(options)
			if err != nil {
				return nil, err
			}
			result[field.Name] = value
		}
		return result, nil
	case Sequence:
		result := make([]any, 0, v.Len())
		for _, item := range v.Items() {
			value, err := item.NativeChecked(options)
			if err != nil {
				return nil, err
			}
			result = append(result, value)
		}
		return result, nil
	case Boolean:
		return v.Bool(), nil
	case Integer, Number:
		return v.nativeNumber(options.Numbers)
	case Absent:
		if options.RefuseAbsent {
			return nil, errors.New("absent object value")
		}
		return nil, nil
	}
	return v.Text(), nil
}

func (v Value) nativeNumber(style NumberStyle) (any, error) {
	switch style {
	case NumberText:
		return json.Number(v.Text()), nil
	case NumberInteger:
		if number, ok := v.Int64(); ok && v.Type() == Integer {
			return number, nil
		}
		return nil, errors.New("native number is not an integer")
	}
	if v.Type() == Integer {
		if number, ok := v.Int64(); ok {
			return number, nil
		}
	} else if number, ok := v.Float64(); ok {
		return number, nil
	}
	return v.Text(), nil
}
