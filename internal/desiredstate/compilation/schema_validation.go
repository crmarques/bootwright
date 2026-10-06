package compilation

import (
	"math/big"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func validateShape(record *objectRecord, value api.Value, shape *api.Shape, path string, partial, references bool, catalog api.Catalog, sink *diagnosticSink) {
	if shape == nil || sink.stopped() || !value.Present() {
		return
	}
	shape = shapeForValue(shape, value)
	if shape.Type != api.Absent && shape.Type != value.Type() && !(shape.Type == api.Number && value.Type() == api.Integer) {
		return
	}
	if shape.MinLength > 0 && value.Type() == api.String && len(value.Text()) < shape.MinLength {
		sink.issue(record, api.Issue{Code: "api.value", Field: path, Message: "value must not be empty", Remediation: "set a non-empty value"})
	}
	switch value.Type() {
	case api.String:
		validateString(record, value, shape, path, references, catalog, sink)
	case api.Integer, api.Number:
		validateRange(record, value, shape, path, sink)
	case api.Mapping:
		validateMapping(record, value, shape, path, partial, references, catalog, sink)
	case api.Sequence:
		validateSequence(record, value, shape, path, partial, references, catalog, sink)
	}
}

func validateString(record *objectRecord, value api.Value, shape *api.Shape, path string, references bool, catalog api.Catalog, sink *diagnosticSink) {
	if len(shape.Enums) > 0 && !slices.Contains(shape.Enums, value.Text()) {
		remediation := "use one of the permitted values"
		if at := slices.IndexFunc(shape.Enums, func(choice string) bool { return strings.EqualFold(choice, value.Text()) }); at >= 0 {
			remediation = "use " + shape.Enums[at] + " (values are case-sensitive)"
		}
		sink.issue(record, api.Issue{Code: "api.value", Field: path, Message: "value is not one of the permitted choices: " + boundedList(shape.Enums, ", "), Remediation: remediation})
	}
	if shape.Rule != "" && !api.ValidLexical(shape.Rule, value.Text()) {
		sink.issue(record, api.Issue{Code: "api.value", Field: path, Message: "value does not match the required " + shape.Rule + " grammar", Remediation: "write a value in the " + shape.Rule + " grammar its field documents"})
	}
	if len(shape.Reference) > 0 && value.Text() != "" {
		if !api.ValidLexical("name", value.Text()) {
			sink.issue(record, api.Issue{Code: "api.value", Field: path, Message: "reference must be an object name (a lowercase DNS label)", Remediation: "use the referenced object's metadata.name"})
		} else if references {
			validateReference(record, value.Text(), shape, path, catalog, sink)
		}
	}
}

func validateReference(record *objectRecord, name string, shape *api.Shape, path string, catalog api.Catalog, sink *diagnosticSink) {
	matches := 0
	var target api.Object
	kinds, declarations := []string{}, []string{}
	for _, kind := range shape.Reference {
		kinds, declarations = append(kinds, string(kind)), append(declarations, string(kind)+"/"+name)
		for _, candidate := range catalog.OfKind(kind) {
			if candidate.Name() == name {
				matches++
				target = candidate
			}
		}
	}
	switch {
	case matches == 0:
		sink.issue(record, api.Issue{Code: "api.reference", Field: path, Message: "no " + boundedList(kinds, " or ") + " named " + name + " is in the selected input",
			Remediation: "declare " + boundedList(declarations, " or ") + ", add its file to the selection, or correct the reference"})
	case matches > 1:
		sink.issue(record, api.Issue{Code: "api.reference", Field: path, Message: name + " is ambiguous: it names " + strconv.Itoa(matches) + " " + boundedList(kinds, " or ") + " objects",
			Remediation: "give the targets distinct names"})
	case target.Kind() == api.Secret && len(shape.SecretTypes) > 0:
		declared := target.Spec().Get("type")
		if declared.Type() == api.String && slices.Contains(secretTypes(), declared.Text()) && !slices.Contains(shape.SecretTypes, declared.Text()) {
			sink.issue(record, api.Issue{Code: "api.reference", Field: path, Message: target.Identity() + " is declared as " + declared.Text() + "; this field accepts " + boundedList(shape.SecretTypes, ", "),
				Remediation: "reference a Secret of an accepted type or change " + target.Identity() + "'s type"})
		}
	}
}

func secretTypes() []string {
	if field, ok := api.Schema(api.Secret).Field("type"); ok {
		return field.Shape.Enums
	}
	return nil
}

func validateRange(record *objectRecord, value api.Value, shape *api.Shape, path string, sink *diagnosticSink) {
	n, ok := new(big.Rat).SetString(value.Text())
	if !ok {
		return
	}
	outside := false
	for _, bound := range []struct {
		text    string
		minimum bool
	}{{shape.Minimum, true}, {shape.Maximum, false}} {
		if b, valid := new(big.Rat).SetString(bound.text); bound.text != "" && valid && (bound.minimum && n.Cmp(b) < 0 || !bound.minimum && n.Cmp(b) > 0) {
			outside = true
		}
	}
	if !outside {
		return
	}
	message := "number must be at most " + shape.Maximum
	switch {
	case shape.Minimum != "" && shape.Maximum != "":
		message = "number must be between " + shape.Minimum + " and " + shape.Maximum
	case shape.Minimum != "":
		message = "number must be at least " + shape.Minimum
	}
	sink.issue(record, api.Issue{Code: "api.value", Field: path, Message: message, Remediation: "use a value in that range"})
}

func validateMapping(record *objectRecord, value api.Value, shape *api.Shape, path string, partial, references bool, catalog api.Catalog, sink *diagnosticSink) {
	if shape.KindDefaults {
		for _, f := range value.Fields() {
			validateShape(record, f.Value, api.Schema(api.Kind(f.Name)), path+"."+f.Name, true, false, catalog, sink)
		}
		return
	}
	if shape.Open || shape.Type == api.Absent {
		if shape.Element != nil {
			for _, f := range value.Fields() {
				validateShape(record, f.Value, shape.Element, path, partial, references, catalog, sink)
			}
		}
		return
	}
	if len(shape.Arms) > 0 {
		validateArms(record, value, shape, path, partial, sink)
	}
	for _, field := range shape.Fields {
		if sink.stopped() {
			break
		}
		childPath := path + "." + field.Name
		if field.Required && !partial && !value.Has(field.Name) {
			sink.issue(record, api.Issue{Code: "api.required", Field: childPath, Message: "required field is absent", Remediation: "add " + field.Name + " (" + shapeTypeName(field.Shape) + ")"})
			continue
		}
		validateEntries(record, value.Get(field.Name), field, childPath, partial, sink)
		validateShape(record, value.Get(field.Name), field.Shape, childPath, partial, references, catalog, sink)
	}
}

func validateArms(record *objectRecord, value api.Value, shape *api.Shape, path string, partial bool, sink *diagnosticSink) {
	count := 0
	discriminator := value.Get(shape.Discriminator).Text()
	allowed, selected := shape.ArmValues[discriminator]
	for _, arm := range shape.Arms {
		if value.Has(arm) && !(slices.Contains(shape.InertArms, arm) && unpopulated(value.Get(arm))) {
			count++
			if selected && !slices.Contains(allowed, arm) {
				sink.issue(record, api.Issue{Code: "api.invariant", Field: path + "." + arm, Message: arm + " does not apply when " + shape.Discriminator + " is " + discriminator,
					Remediation: "remove " + arm + " or change " + shape.Discriminator})
			}
		}
	}
	if count > 1 || count == 0 && !partial && !shape.AllowEmpty && shape.Discriminator == "" {
		message, remediation := "set exactly one of "+boundedList(shape.Arms, ", "), "keep one of them and remove the others"
		if shape.AllowEmpty {
			message = "set at most one of " + boundedList(shape.Arms, ", ")
		}
		if count == 0 {
			remediation = "add one of " + boundedList(shape.Arms, ", ")
		}
		sink.issue(record, api.Issue{Code: "api.invariant", Field: path, Message: message, Remediation: remediation})
	}
}

func validateEntries(record *objectRecord, value api.Value, field api.Field, path string, partial bool, sink *diagnosticSink) {
	if field.Shape == nil || value.Type() != api.Sequence {
		return
	}
	if shape := shapeForValue(field.Shape, value); shape.Type != api.Sequence || value.Len() >= shape.MinLength {
		return
	}
	remediation := "add at least one entry, or omit the field"
	if field.Required && !partial {
		remediation = "add at least one entry"
	}
	sink.issue(record, api.Issue{Code: "api.value", Field: path, Message: "list must contain at least one entry", Remediation: remediation})
}

func validateSequence(record *objectRecord, value api.Value, shape *api.Shape, path string, partial, references bool, catalog api.Catalog, sink *diagnosticSink) {
	seen := map[string]bool{}
	for i, item := range value.Items() {
		if sink.stopped() {
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
				message := "entry repeats an earlier entry"
				if shape.NameKey != "" && api.ValidLexical("name", key) {
					message += " named " + key
				}
				sink.issue(record, api.Issue{Code: "api.duplicate", Field: childPath, Message: message, Remediation: "remove or rename the repeated entry"})
			}
			seen[key] = true
		}
		validateShape(record, item, shape.Element, childPath, partial, references, catalog, sink)
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
