package architecture_test

import (
	"fmt"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// unionViolations holds one shape tree to the union rule of specs/api.md
// (Unions): an alternative with its own configuration is a presence arm with
// no sibling discriminator; an enum selects a variant only when no variant
// has a block of its own, so no enum value names a sibling block; and
// omission selects at most one stated arm, the arm whose default is shown in
// effective output.
func unionViolations(path string, shape *api.Shape, seen map[*api.Shape]bool) []string {
	if shape == nil || seen[shape] {
		return nil
	}
	seen[shape] = true
	var found []string
	blocks := map[string]bool{}
	for _, field := range shape.Fields {
		if field.Shape != nil && field.Shape.Type == api.Mapping {
			blocks[field.Name] = true
		}
	}
	if len(shape.Arms) > 0 {
		var defaulted []string
		for _, field := range shape.Fields {
			if field.Name == "type" && !slices.Contains(shape.Arms, "type") {
				found = append(found, path+": a presence union carries a type discriminator beside its arms")
			}
			if slices.Contains(shape.Arms, field.Name) && field.Default.Present() {
				defaulted = append(defaulted, field.Name)
			}
		}
		if len(defaulted) > 1 {
			found = append(found, fmt.Sprintf("%s: omission selects more than one arm %v", path, defaulted))
		}
		if len(defaulted) == 1 && shape.AllowEmpty {
			found = append(found, path+": an arm is selected by omission, so an empty union is never effective")
		}
	}
	for _, field := range shape.Fields {
		if field.Shape == nil {
			continue
		}
		for _, value := range field.Shape.Enums {
			if blocks[value] {
				found = append(found, path+"."+field.Name+": enum value "+value+" names the sibling block "+path+"."+value+"; make the variant a presence arm")
			}
		}
		found = append(found, unionViolations(path+"."+field.Name, field.Shape, seen)...)
	}
	found = append(found, unionViolations(path+"[]", shape.Element, seen)...)
	for index, alternative := range shape.Alternatives {
		found = append(found, unionViolations(fmt.Sprintf("%s<%d>", path, index), alternative, seen)...)
	}
	return found
}

// Every registered schema follows the one union rule, and the rule fires on a
// shape that breaks each of its clauses.
func TestEveryUnionFollowsTheOneRule(t *testing.T) {
	seen := map[*api.Shape]bool{}
	for _, kind := range api.Kinds() {
		for _, violation := range unionViolations(string(kind)+".spec", api.Schema(kind), seen) {
			t.Error(violation)
		}
	}
	text := &api.Shape{Type: api.String}
	block := &api.Shape{Type: api.Mapping}
	for name, violator := range map[string]*api.Shape{
		"a discriminator beside presence arms": {Type: api.Mapping, Arms: []string{"a", "b"},
			Fields: []api.Field{{Name: "type", Shape: text}, {Name: "a", Shape: block}, {Name: "b", Shape: block}}},
		"an enum value naming a sibling block": {Type: api.Mapping,
			Fields: []api.Field{{Name: "distribution", Shape: &api.Shape{Type: api.String, Enums: []string{"a", "b"}}}, {Name: "b", Shape: block}}},
		"two arms selected by omission": {Type: api.Mapping, Arms: []string{"a", "b"},
			Fields: []api.Field{{Name: "a", Shape: block, Default: api.MapValue()}, {Name: "b", Shape: block, Default: api.MapValue()}}},
		"an empty union with a defaulted arm": {Type: api.Mapping, Arms: []string{"a", "b"}, AllowEmpty: true,
			Fields: []api.Field{{Name: "a", Shape: block, Default: api.MapValue()}, {Name: "b", Shape: block}}},
	} {
		if len(unionViolations("violator", violator, map[*api.Shape]bool{})) == 0 {
			t.Errorf("the union rule admits %s", name)
		}
	}
}
