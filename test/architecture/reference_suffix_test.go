package architecture_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// referenceSuffixExceptions are the reference-bearing fields specs/api.md
// (References) names as deliberate exceptions: inventory selections and the
// data-pool record's name.
var referenceSuffixExceptions = []string{
	"CustomPlaybook.spec.target.clusters",
	"CustomPlaybook.spec.target.machines",
	"ClusterAddon.spec.steps[].target.static.clusters",
	"ClusterAddon.spec.steps[].target.static.machines",
	"CephFilesystem.spec.dataPoolRefs[].name",
}

func referenceShape(shape *api.Shape) bool {
	if shape == nil {
		return false
	}
	if len(shape.Reference) > 0 || shape.Category != "" || shape.Nested != "" {
		return true
	}
	return slices.ContainsFunc(shape.Alternatives, referenceShape)
}

func referenceList(shape *api.Shape) bool {
	return shape != nil && shape.Type == api.Sequence && referenceShape(shape.Element)
}

func referenceSuffixViolations(path string, shape *api.Shape, exceptions []string, used map[string]bool, depth int) []string {
	if shape == nil || depth > 32 {
		return nil
	}
	var found []string
	for _, field := range shape.Fields {
		at := path + "." + field.Name
		refers := referenceShape(field.Shape) || referenceList(field.Shape)
		suffixed := strings.HasSuffix(field.Name, "Ref") || strings.HasSuffix(field.Name, "Refs")
		switch {
		case slices.Contains(exceptions, at):
			used[at] = true
		case suffixed && !refers:
			found = append(found, at+": the field ends in Ref but its shape is no typed reference; declare it with ref, a nested reference helper or external")
		case refers && !suffixed:
			found = append(found, at+": the field holds a reference but its name does not end in Ref or Refs")
		}
		found = append(found, referenceSuffixViolations(at, field.Shape, exceptions, used, depth+1)...)
	}
	found = append(found, referenceSuffixViolations(path+"[]", shape.Element, exceptions, used, depth+1)...)
	for _, alternative := range shape.Alternatives {
		found = append(found, referenceSuffixViolations(path, alternative, exceptions, used, depth+1)...)
	}
	return found
}

// A field whose value names a Bootwright object, an entry nested in its owner,
// a native service or an external object ends in Ref or Refs, and a field so
// named is a typed reference (specs/api.md, References and Naming new names).
func TestEveryReferenceFieldEndsInRefAndIsTyped(t *testing.T) {
	used := map[string]bool{}
	for _, kind := range api.Kinds() {
		for _, violation := range referenceSuffixViolations(string(kind)+".spec", api.Schema(kind), referenceSuffixExceptions, used, 0) {
			t.Error(violation)
		}
	}
	for _, exception := range referenceSuffixExceptions {
		if !used[exception] {
			t.Errorf("referenceSuffixExceptions lists %s, which holds no reference: remove it", exception)
		}
	}
	text := &api.Shape{Type: api.String, MinLength: 1}
	object := &api.Shape{Type: api.String, MinLength: 1, Reference: []api.Kind{api.Machine}}
	for name, violator := range map[string]*api.Shape{
		"an untyped Ref field":    {Type: api.Mapping, Fields: []api.Field{{Name: "nodeRef", Shape: text}}},
		"an unsuffixed reference": {Type: api.Mapping, Fields: []api.Field{{Name: "machine", Shape: object}}},
	} {
		if got := referenceSuffixViolations("violator.spec", violator, nil, map[string]bool{}, 0); len(got) != 1 {
			t.Errorf("the reference rule reports %s as %v", name, fmt.Sprint(got))
		}
	}
}
