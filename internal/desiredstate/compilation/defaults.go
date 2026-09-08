package compilation

import (
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
)

func inherit(value, fallback api.Value, shape *api.Shape, path string, root bool, recipient, environment *objectRecord) api.Value {
	if !fallback.Present() {
		return value
	}
	if !value.Present() {
		markInherited(recipient, environment, fallback, path)
		return fallback
	}
	if shape == nil {
		return value
	}
	shape = shapeForValue(shape, value)
	if value.Type() != api.Mapping || fallback.Type() != api.Mapping || shape.Atomic || shape.Open || shape.KindDefaults || value.Len() == 0 && !root {
		return value
	}
	blocked := map[string]bool{}
	selected := []string{}
	hasSelection := false
	if shape.Discriminator != "" && value.Has(shape.Discriminator) {
		selected = shape.ArmValues[value.Get(shape.Discriminator).Text()]
		hasSelection = true
	}
	for _, arm := range shape.Arms {
		if value.Has(arm) && !(slices.Contains(shape.InertArms, arm) && unpopulated(value.Get(arm))) {
			selected = append(selected, arm)
			hasSelection = true
		}
	}
	if hasSelection {
		for _, arm := range shape.Arms {
			if !slices.Contains(selected, arm) {
				blocked[arm] = true
			}
		}
		if shape.Discriminator != "" && !value.Has(shape.Discriminator) && fallback.Has(shape.Discriminator) {
			for _, arm := range shape.ArmValues[fallback.Get(shape.Discriminator).Text()] {
				if !slices.Contains(selected, arm) {
					blocked[shape.Discriminator] = true
				}
			}
		}
	}
	for _, condition := range shape.Suppress {
		if value.Get(strings.Split(condition.Field, ".")...).Equal(condition.Value) {
			for _, name := range condition.Fields {
				fallback = withoutPath(fallback, strings.Split(name, "."))
			}
		}
	}
	result := value
	for _, f := range shape.Fields {
		if blocked[f.Name] || !fallback.Has(f.Name) {
			continue
		}
		child := inherit(value.Get(f.Name), fallback.Get(f.Name), f.Shape, path+"."+f.Name, false, recipient, environment)
		result = result.With(f.Name, child)
	}
	return result
}

func withoutPath(value api.Value, path []string) api.Value {
	if len(path) == 0 || !value.Has(path[0]) {
		return value
	}
	if len(path) == 1 {
		return value.Without(path[0])
	}
	return value.With(path[0], withoutPath(value.Get(path[0]), path[1:]))
}

func markInherited(recipient, environment *objectRecord, value api.Value, path string) {
	if recipient == nil || environment == nil {
		return
	}
	suffix := strings.TrimPrefix(path, "$.spec")
	origin := "$.spec.defaults." + string(recipient.object.Kind()) + suffix
	if location, ok := environment.location(origin); ok {
		recipient.inherited[path] = location
	}
	recipient.inheritedEnvironment = environment
}

func builtInDefaults(value api.Value, shape *api.Shape) api.Value {
	if shape == nil || !value.Present() {
		return value
	}
	shape = shapeForValue(shape, value)
	if shape.KindDefaults || shape.Open {
		return value
	}
	if value.Type() == api.Mapping {
		for _, field := range shape.Fields {
			child := value.Get(field.Name)
			if !child.Present() && field.Default.Present() {
				child = field.Default
			}
			if child.Present() {
				value = value.With(field.Name, builtInDefaults(child, field.Shape))
			}
		}
	}
	if value.Type() == api.Sequence {
		items := value.Items()
		for i, item := range items {
			items[i] = builtInDefaults(item, shape.Element)
		}
		value = api.ListValue(items...)
	}
	return value
}

type expansionBudget struct{ nodes int }

func (budget *expansionBudget) admit(record *objectRecord, diagnostics *diagnostics) bool {
	var visit func(api.Value, int) bool
	visit = func(value api.Value, depth int) bool {
		if diagnostics.stopped() {
			return false
		}
		if depth > desiredstate.MaxDepth {
			diagnostics.add(desiredstate.Diagnostic{Severity: "error", Code: "input.limit", Message: "expanded representation depth exceeds the ceiling of 64"})
			return false
		}
		budget.nodes++
		if budget.nodes > desiredstate.MaxNodes {
			diagnostics.add(desiredstate.Diagnostic{Severity: "error", Code: "input.limit", Message: "expanded representation nodes exceed the ceiling of 1000000"})
			return false
		}
		for _, field := range value.Fields() {
			if !visit(api.StringValue(field.Name), depth+1) || !visit(field.Value, depth+1) {
				return false
			}
		}
		for _, item := range value.Items() {
			if !visit(item, depth+1) {
				return false
			}
		}
		return true
	}
	budget.nodes++
	return visit(record.object.Value(), 1)
}

func withinExpansionBudget(records []*objectRecord, diagnostics *diagnostics) bool {
	budget := expansionBudget{}
	for _, record := range records {
		if !budget.admit(record, diagnostics) {
			return false
		}
	}
	return true
}
