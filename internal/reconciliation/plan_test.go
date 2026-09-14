package reconciliation

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func definition(id string, dependencies ...string) BlockDefinition {
	slices.Sort(dependencies)
	return BlockDefinition{
		ID:             id,
		Description:    "serve artifacts for " + id,
		Stage:          StageInfraComponents,
		Dependencies:   dependencies,
		Impacts:        []string{"create-container-unit"},
		Groups:         []Group{{ID: "start-service", Description: "start the service", Machines: []string{"controller"}}},
		Kind:           "ArtifactServer",
		Object:         id,
		Implementation: "artifact-server-nginx-v1",
		ContentDigest:  strings.Repeat("a", 64),
		Request:        json.RawMessage(`{"name":"` + id + `"}`),
	}
}

func TestPlanOrderAndDigestIgnoreInputOrder(t *testing.T) {
	forward := []BlockDefinition{definition("alpha"), definition("bravo", "alpha"), definition("charlie", "alpha")}
	reversed := []BlockDefinition{definition("charlie", "alpha"), definition("bravo", "alpha"), definition("alpha")}
	first, err := NewPlan(Apply, forward)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewPlan(Apply, reversed)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"alpha", "bravo", "charlie"}
	for _, plan := range []Plan{first, second} {
		var got []string
		for _, block := range plan.Blocks {
			got = append(got, block.ID)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("plan order = %v, want %v", got, want)
		}
	}
	firstDigest, err := first.Digest()
	if err != nil {
		t.Fatal(err)
	}
	secondDigest, err := second.Digest()
	if err != nil || firstDigest != secondDigest {
		t.Fatalf("plan digest depends on input order: %s vs %s (%v)", firstDigest, secondDigest, err)
	}
	if len(firstDigest) != 64 {
		t.Fatalf("plan digest = %q", firstDigest)
	}
}

func TestPlanDigestChangesWithEveryFrozenField(t *testing.T) {
	base, err := NewPlan(Apply, []BlockDefinition{definition("alpha")})
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := base.Digest()
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(BlockDefinition) BlockDefinition{
		"description":    func(d BlockDefinition) BlockDefinition { d.Description = "other"; return d },
		"impacts":        func(d BlockDefinition) BlockDefinition { d.Impacts = []string{"open-listener 1"}; return d },
		"groups":         func(d BlockDefinition) BlockDefinition { d.Groups[0].Machines = []string{"other"}; return d },
		"implementation": func(d BlockDefinition) BlockDefinition { d.Implementation = "other-v1"; return d },
		"contentDigest":  func(d BlockDefinition) BlockDefinition { d.ContentDigest = strings.Repeat("b", 64); return d },
		"request":        func(d BlockDefinition) BlockDefinition { d.Request = json.RawMessage(`{"name":"other"}`); return d },
	} {
		t.Run(name, func(t *testing.T) {
			plan, err := NewPlan(Apply, []BlockDefinition{mutate(definition("alpha"))})
			if err != nil {
				t.Fatal(err)
			}
			digest, err := plan.Digest()
			if err != nil || digest == baseline {
				t.Fatalf("%s does not participate in the plan digest", name)
			}
		})
	}
	destroy, err := NewPlan(Destroy, []BlockDefinition{definition("alpha")})
	if err != nil {
		t.Fatal(err)
	}
	if digest, err := destroy.Digest(); err != nil || digest == baseline {
		t.Fatal("the verb does not participate in the plan digest")
	}
}

func TestPlanRefusesInvalidGraphs(t *testing.T) {
	cyclic := []BlockDefinition{definition("alpha", "bravo"), definition("bravo", "alpha")}
	duplicate := []BlockDefinition{definition("alpha"), definition("alpha")}
	unknown := []BlockDefinition{definition("alpha", "missing")}
	selfish := []BlockDefinition{definition("alpha", "alpha")}
	for name, definitions := range map[string][]BlockDefinition{
		"cycle": cyclic, "duplicate": duplicate, "unknown dependency": unknown, "self dependency": selfish,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewPlan(Apply, definitions); err == nil {
				t.Fatal("invalid graph produced a plan")
			}
		})
	}
}

func TestPlanRefusesUnsafeBlockContent(t *testing.T) {
	for name, mutate := range map[string]func(BlockDefinition) BlockDefinition{
		"empty identity":        func(d BlockDefinition) BlockDefinition { d.ID = ""; return d },
		"uppercase identity":    func(d BlockDefinition) BlockDefinition { d.ID = "Alpha"; return d },
		"trailing dash":         func(d BlockDefinition) BlockDefinition { d.ID = "alpha-"; return d },
		"control description":   func(d BlockDefinition) BlockDefinition { d.Description = "a\nb"; return d },
		"escape description":    func(d BlockDefinition) BlockDefinition { d.Description = "a\x1b[31mb"; return d },
		"empty description":     func(d BlockDefinition) BlockDefinition { d.Description = ""; return d },
		"short digest":          func(d BlockDefinition) BlockDefinition { d.ContentDigest = "abc"; return d },
		"uppercase digest":      func(d BlockDefinition) BlockDefinition { d.ContentDigest = strings.Repeat("A", 64); return d },
		"missing kind":          func(d BlockDefinition) BlockDefinition { d.Kind = ""; return d },
		"empty group machines":  func(d BlockDefinition) BlockDefinition { d.Groups[0].Machines = nil; return d },
		"unsorted machines":     func(d BlockDefinition) BlockDefinition { d.Groups[0].Machines = []string{"b", "a"}; return d },
		"duplicate group":       func(d BlockDefinition) BlockDefinition { d.Groups = append(d.Groups, d.Groups[0]); return d },
		"unsorted dependencies": func(d BlockDefinition) BlockDefinition { d.Dependencies = []string{"b", "a"}; return d },
		"request array":         func(d BlockDefinition) BlockDefinition { d.Request = json.RawMessage(`[]`); return d },
		"request trailing":      func(d BlockDefinition) BlockDefinition { d.Request = json.RawMessage(`{} {}`); return d },
		"request spaced":        func(d BlockDefinition) BlockDefinition { d.Request = json.RawMessage(`{ "a": 1 }`); return d },
		"request unordered":     func(d BlockDefinition) BlockDefinition { d.Request = json.RawMessage(`{"b":1,"a":2}`); return d },
		"request empty":         func(d BlockDefinition) BlockDefinition { d.Request = nil; return d },
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewPlan(Apply, []BlockDefinition{mutate(definition("alpha"))}); err == nil {
				t.Fatal("unsafe block content produced a plan")
			}
		})
	}
}

func TestPlanRefusesUnknownVerbAndOversizedGraph(t *testing.T) {
	if _, err := NewPlan("reconcile", []BlockDefinition{definition("alpha")}); err == nil {
		t.Fatal("an unknown verb produced a plan")
	}
	definitions := make([]BlockDefinition, 0, MaxBlocks+1)
	for index := range MaxBlocks + 1 {
		definitions = append(definitions, definition("block-"+ordinal(index)))
	}
	if _, err := NewPlan(Apply, definitions); err == nil {
		t.Fatal("an oversized graph produced a plan")
	}
}

func ordinal(value int) string {
	digits := "0123456789"
	if value == 0 {
		return "0"
	}
	var out []byte
	for value > 0 {
		out = append([]byte{digits[value%10]}, out...)
		value /= 10
	}
	return string(out)
}

func TestInverseReversesOrderAndPreservesBlocks(t *testing.T) {
	plan, err := NewPlan(Apply, []BlockDefinition{definition("alpha"), definition("bravo", "alpha")})
	if err != nil {
		t.Fatal(err)
	}
	inverse := plan.Inverse()
	if inverse.Verb != Destroy || len(inverse.Blocks) != 2 || inverse.Blocks[0].ID != "bravo" || inverse.Blocks[1].ID != "alpha" {
		t.Fatalf("inverse = %+v", inverse)
	}
	if inverse.Blocks[0].RequestDigest != plan.Blocks[1].RequestDigest {
		t.Fatal("inverse changed a frozen request digest")
	}
}

func TestPlanValuesAreCopiedAtTheBoundary(t *testing.T) {
	source := definition("alpha")
	plan, err := NewPlan(Apply, []BlockDefinition{source})
	if err != nil {
		t.Fatal(err)
	}
	source.Impacts[0] = "mutated"
	source.Groups[0].Machines[0] = "mutated"
	source.Request[2] = 'X'
	if plan.Blocks[0].Impacts[0] == "mutated" || plan.Blocks[0].Groups[0].Machines[0] == "mutated" {
		t.Fatal("the plan aliases its caller's mutable collections")
	}
	block, _ := plan.Block("alpha")
	block.Impacts[0] = "mutated"
	if plan.Blocks[0].Impacts[0] == "mutated" {
		t.Fatal("Block returns an alias of the frozen plan")
	}
}
