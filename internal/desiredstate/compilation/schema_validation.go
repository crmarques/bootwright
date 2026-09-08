package compilation

import (
	"math/big"
	"slices"
	"strconv"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func validateShape(record *objectRecord, value api.Value, shape *api.Shape, path string, partial, references bool, catalog api.Catalog, diagnostics *diagnostics) {
	if shape == nil || diagnostics.stopped() || !value.Present() {
		return
	}
	shape = shapeForValue(shape, value)
	issue := func(code, message string) {
		diagnostics.issue(record, api.Issue{Code: code, Field: path, Message: message})
	}
	if shape.Type != api.Absent && shape.Type != value.Type() && !(shape.Type == api.Number && value.Type() == api.Integer) {
		return
	}
	if shape.MinLength > 0 {
		length := value.Len()
		if value.Type() == api.String {
			length = len(value.Text())
		}
		if length < shape.MinLength {
			issue("api.value", "value must not be empty")
		}
	}
	if value.Type() == api.String {
		if len(shape.Enums) > 0 && !slices.Contains(shape.Enums, value.Text()) {
			issue("api.value", "value is not one of the permitted choices")
		}
		if shape.Rule != "" && !api.ValidLexical(shape.Rule, value.Text()) {
			issue("api.value", "value does not match the required "+shape.Rule+" grammar")
		}
		if len(shape.Reference) > 0 && value.Text() != "" {
			if !api.ValidLexical("name", value.Text()) {
				issue("api.value", "reference must be a DNS label")
			} else if references {
				matches := 0
				var target api.Object
				for _, kind := range shape.Reference {
					for _, candidate := range catalog.OfKind(kind) {
						if candidate.Name() == value.Text() {
							matches++
							target = candidate
						}
					}
				}
				if matches != 1 {
					issue("api.reference", "reference must resolve to exactly one object of the required kind")
				} else if target.Kind() == api.Secret && len(shape.SecretTypes) > 0 && !slices.Contains(shape.SecretTypes, target.Spec().Get("type").Text()) {
					issue("api.reference", "referenced Secret has an incompatible declared type")
				}
			}
		}
	}
	if value.Type() == api.Integer || value.Type() == api.Number {
		n, ok := new(big.Rat).SetString(value.Text())
		if ok {
			for _, bound := range []struct {
				text    string
				minimum bool
			}{{shape.Minimum, true}, {shape.Maximum, false}} {
				if bound.text != "" {
					b, valid := new(big.Rat).SetString(bound.text)
					if valid && (bound.minimum && n.Cmp(b) < 0 || !bound.minimum && n.Cmp(b) > 0) {
						issue("api.value", "number is outside the permitted range")
					}
				}
			}
		}
	}
	if value.Type() == api.Mapping {
		if shape.KindDefaults {
			for _, f := range value.Fields() {
				validateShape(record, f.Value, api.Schema(api.Kind(f.Name)), path+"."+f.Name, true, false, catalog, diagnostics)
			}
			return
		}
		if shape.Open || shape.Type == api.Absent {
			if shape.Element != nil {
				for _, f := range value.Fields() {
					validateShape(record, f.Value, shape.Element, path, partial, references, catalog, diagnostics)
				}
			}
			return
		}
		if len(shape.Arms) > 0 {
			count := 0
			allowed, selected := shape.ArmValues[value.Get(shape.Discriminator).Text()]
			for _, arm := range shape.Arms {
				if value.Has(arm) && !(slices.Contains(shape.InertArms, arm) && unpopulated(value.Get(arm))) {
					count++
					if selected && !slices.Contains(allowed, arm) {
						diagnostics.issue(record, api.Issue{Code: "api.invariant", Field: path + "." + arm, Message: "configuration arm does not match the selected discriminator"})
					}
				}
			}
			if count > 1 || count == 0 && !partial && !shape.AllowEmpty && shape.Discriminator == "" {
				issue("api.invariant", "exactly one implementation arm is required")
			}
		}
		for _, field := range shape.Fields {
			if diagnostics.stopped() {
				break
			}
			childPath := path + "." + field.Name
			if field.Required && !partial && !value.Has(field.Name) {
				diagnostics.issue(record, api.Issue{Code: "api.required", Field: childPath, Message: "required field is absent"})
				continue
			}
			validateShape(record, value.Get(field.Name), field.Shape, childPath, partial, references, catalog, diagnostics)
		}
	}
	if value.Type() == api.Sequence {
		seen := map[string]bool{}
		for i, item := range value.Items() {
			if diagnostics.stopped() {
				break
			}
			childPath := path + "[" + strconv.Itoa(i) + "]"
			key := ""
			if shape.NameKey != "" {
				key = item.Get(shape.NameKey).Text()
			} else if shape.Unique {
				key = valueKey(item)
			}
			if key != "" {
				if seen[key] {
					diagnostics.issue(record, api.Issue{Code: "api.duplicate", Field: childPath, Message: "collection entries must be unique"})
				}
				seen[key] = true
			}
			validateShape(record, item, shape.Element, childPath, partial, references, catalog, diagnostics)
		}
	}
}

func shapeForValue(shape *api.Shape, value api.Value) *api.Shape {
	for _, alternative := range shape.Alternatives {
		if alternative.Type == value.Type() || alternative.Type == api.Number && value.Type() == api.Integer {
			return alternative
		}
	}
	return shape
}

func unpopulated(value api.Value) bool {
	switch value.Type() {
	case api.Absent:
		return true
	case api.Mapping:
		for _, f := range value.Fields() {
			if !unpopulated(f.Value) {
				return false
			}
		}
		return true
	case api.Integer, api.Number:
		n, ok := new(big.Rat).SetString(value.Text())
		return ok && n.Sign() == 0
	case api.String:
		return value.Text() == ""
	case api.Boolean:
		return !value.Bool()
	case api.Sequence:
		return value.Len() == 0
	}
	return false
}

func valueKey(value api.Value) string {
	switch value.Type() {
	case api.String:
		return "s:" + value.Text()
	case api.Integer, api.Number:
		return "n:" + value.Text()
	case api.Boolean:
		return "b:" + strconv.FormatBool(value.Bool())
	case api.Mapping:
		fields := value.Fields()
		slices.SortFunc(fields, func(a, b api.FieldValue) int {
			if a.Name < b.Name {
				return -1
			}
			if a.Name > b.Name {
				return 1
			}
			return 0
		})
		key := "m:"
		for _, f := range fields {
			key += strconv.Quote(f.Name) + ":" + valueKey(f.Value) + ","
		}
		return key
	case api.Sequence:
		key := "l:"
		for _, item := range value.Items() {
			key += valueKey(item) + ","
		}
		return key
	}
	return "absent"
}
