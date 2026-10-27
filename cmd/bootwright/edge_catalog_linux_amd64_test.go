//go:build linux && amd64

package main

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const reconciliationSpec = "../../specs/state-reconciliation.md"

// edgeRow is one row of the reference-edge catalog: the requiring block's
// family and kind, the required kind and family, and the field paths on the
// requiring object that name the target, when the row names it by fields.
type edgeRow struct {
	family, kind, target, targetFamily string
	paths                              []string
}

func (r edgeRow) key() string {
	return r.family + " " + r.kind + " -> " + r.target + " " + r.targetFamily
}

var edgeCode = regexp.MustCompile("`([^`]+)`")

func edgeCatalog(t *testing.T) []edgeRow {
	t.Helper()
	var rows []edgeRow
	for _, cells := range markdownTable(t, reconciliationSpec, "### Reference edges", []string{"Block", "Requires", "Named by"}) {
		block, requires := edgeCode.FindAllStringSubmatch(cells["Block"], -1), edgeCode.FindAllStringSubmatch(cells["Requires"], -1)
		if len(block) != 2 || len(requires) < 1 || len(requires) > 2 {
			t.Fatalf("edge row %v names no family, kind and target", cells)
		}
		row := edgeRow{family: block[0][1], kind: block[1][1], target: requires[0][1]}
		if len(requires) == 2 {
			row.targetFamily = requires[1][1]
		}
		for _, match := range edgeCode.FindAllStringSubmatch(cells["Named by"], -1) {
			if strings.HasPrefix(match[1], "spec.") {
				row.paths = append(row.paths, match[1])
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// edgeTargets reads the names one field path holds on an object: a path
// segment ending in [] walks a list, and a record where a name is expected
// yields its name field.
func edgeTargets(value api.Value, segments []string) []string {
	if len(segments) == 0 {
		switch value.Type() {
		case api.String:
			return []string{value.Text()}
		case api.Mapping:
			return []string{value.Get("name").Text()}
		}
		return nil
	}
	field, list := strings.CutSuffix(segments[0], "[]")
	next := value.Get(field)
	if !list {
		return edgeTargets(next, segments[1:])
	}
	var names []string
	for _, item := range next.Items() {
		names = append(names, edgeTargets(item, segments[1:])...)
	}
	return names
}

type familyBlock struct {
	family     string
	definition reconciliation.BlockDefinition
}

// TestEveryRequirementMatchesTheEdgeCatalog plans every example through this
// build's composition and holds each capability's requirements to the
// reference-edge catalog (specs/state-reconciliation.md, Reference edges):
// a requirement no row names fails, and so does a target a row's field path
// names on the requiring object, realized by some block, that the family's
// blocks for that object neither require nor depend on.
func TestEveryRequirementMatchesTheEdgeCatalog(t *testing.T) {
	rows := edgeCatalog(t)
	catalog := map[string]edgeRow{}
	for _, row := range rows {
		if _, repeated := catalog[row.key()]; repeated {
			t.Errorf("the edge catalog repeats %s", row.key())
		}
		catalog[row.key()] = row
	}
	resolver := buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{})
	names := []string{}
	for name := range exampleDirectories(t) {
		if name != refusedExample {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	used := map[string]bool{}
	for _, name := range names {
		state, _ := compileAcceptance(t, exampleDirectory(t, name))
		input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: name}, State: state, Controller: "controller"}
		var blocks []familyBlock
		for _, binding := range resolver.Bindings() {
			capability, _ := resolver.Resolve(binding.Kind, binding.Implementation)
			contribution, err := capability.Plan(context.Background(), input)
			if err != nil {
				continue
			}
			for _, definition := range contribution.Definitions {
				blocks = append(blocks, familyBlock{binding.Family, definition})
			}
		}
		realizers := func(kind, object, family string) []string {
			var ids []string
			for _, block := range blocks {
				if block.definition.Kind == kind && block.definition.Object == object && (family == "" || block.family == family) {
					ids = append(ids, block.definition.ID)
				}
			}
			return ids
		}
		for _, block := range blocks {
			for _, requirement := range block.definition.Requires {
				key := edgeRow{family: block.family, kind: block.definition.Kind, target: requirement.Kind, targetFamily: requirement.Family}.key()
				if _, found := catalog[key]; !found {
					t.Errorf("%s: %s %s/%s requires %s/%s, which no edge catalog row names (%s)", name, block.family, block.definition.Kind, block.definition.Object, requirement.Kind, requirement.Object, key)
				}
				used[key] = true
			}
		}
		for _, row := range rows {
			if len(row.paths) == 0 {
				continue
			}
			for _, object := range state.Effective().OfKind(api.Kind(row.kind)) {
				var family []familyBlock
				for _, block := range blocks {
					if block.family == row.family && block.definition.Kind == row.kind && block.definition.Object == object.Name() {
						family = append(family, block)
					}
				}
				if len(family) == 0 {
					continue
				}
				for _, path := range row.paths {
					for _, target := range edgeTargets(object.Spec(), strings.Split(strings.TrimPrefix(path, "spec."), ".")) {
						ids := realizers(row.target, target, row.targetFamily)
						if target == "" || len(ids) == 0 {
							continue
						}
						if !edgeSatisfied(family, row, target, ids) {
							t.Errorf("%s: %s %s/%s omits its %s requirement on %s/%s named by %s", name, row.family, row.kind, object.Name(), row.target, row.target, target, path)
						}
					}
				}
			}
		}
	}
	for _, row := range rows {
		if !used[row.key()] {
			t.Errorf("no example plans the edge catalog row %s; drop it or add an example that needs it", row.key())
		}
	}
}

func edgeSatisfied(family []familyBlock, row edgeRow, target string, realizers []string) bool {
	for _, block := range family {
		if slices.Contains(block.definition.Requires, reconciliation.ObjectRef{Kind: row.target, Object: target, Family: row.targetFamily}) {
			return true
		}
		for _, id := range realizers {
			if id != block.definition.ID && slices.Contains(block.definition.Dependencies, id) {
				return true
			}
		}
	}
	return false
}
