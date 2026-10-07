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
	inverse, err := plan.Inverse()
	if err != nil {
		t.Fatal(err)
	}
	if inverse.Verb != Destroy || len(inverse.Blocks) != 2 || inverse.Blocks[0].ID != "bravo" || inverse.Blocks[1].ID != "alpha" {
		t.Fatalf("inverse = %+v", inverse)
	}
	if inverse.Blocks[0].RequestDigest != plan.Blocks[1].RequestDigest {
		t.Fatal("inverse changed a frozen request digest")
	}
}

// Order alone does not remove dependents first: the executor starts the blocks
// whose dependencies are done, so the edges have to turn around with it.
func TestInverseTurnsEveryDependencyIntoItsDependent(t *testing.T) {
	plan, err := NewPlan(Apply, []BlockDefinition{
		definition("alpha"), definition("bravo", "alpha"), definition("charlie", "alpha"),
	})
	if err != nil {
		t.Fatal(err)
	}
	inverse, err := plan.Inverse()
	if err != nil {
		t.Fatal(err)
	}
	alpha, _ := inverse.Block("alpha")
	if !slices.Equal(alpha.Dependencies, []string{"bravo", "charlie"}) {
		t.Fatalf("alpha waits on %v", alpha.Dependencies)
	}
	bravo, _ := inverse.Block("bravo")
	if len(bravo.Dependencies) != 0 {
		t.Fatalf("bravo waits on %v", bravo.Dependencies)
	}
	if got := ids(Ready(inverse, map[string]BlockState{})); !slices.Equal(got, []string{"bravo", "charlie"}) {
		t.Fatalf("a removal started %v before every dependent was gone", got)
	}
	if got := ids(inverse.Blocks); !slices.Equal(got, []string{"bravo", "charlie", "alpha"}) {
		t.Fatalf("removal order = %v", got)
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

// The plan is written wave by wave, so the numbered list an operator confirms
// is the order the work is started in. A block that waits for nothing sits
// with every other block that waits for nothing, however deep its own
// dependents go, and one arbitrary sequential walk of the graph no longer
// decides where it is printed.
func TestPlanOrderGroupsBlocksByTheWaveTheyCanStartIn(t *testing.T) {
	plan, err := NewPlan(Apply, []BlockDefinition{
		definition("alpha"),
		definition("bravo", "alpha"),
		definition("zulu"),
		definition("charlie", "bravo"),
		definition("yankee", "zulu"),
	})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, block := range plan.Blocks {
		got = append(got, block.ID)
	}
	want := []string{"alpha", "zulu", "bravo", "yankee", "charlie"}
	if !slices.Equal(got, want) {
		t.Fatalf("plan order = %v, want %v", got, want)
	}
	schedule := ScheduleOf(plan)
	if schedule.Count != 3 || schedule.Widest != 2 {
		t.Fatalf("schedule = %d waves, widest %d", schedule.Count, schedule.Widest)
	}
	if schedule.Waves["alpha"] != 0 || schedule.Waves["bravo"] != 1 || schedule.Waves["charlie"] != 2 {
		t.Fatalf("waves = %v", schedule.Waves)
	}
	// The first wave counts too: a schedule that measured only the blocks with
	// dependencies would report a plan of four roots as one step at a time.
	wide, err := NewPlan(Apply, []BlockDefinition{
		definition("alpha"), definition("bravo"), definition("charlie"),
		definition("delta", "alpha"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if schedule := ScheduleOf(wide); schedule.Count != 2 || schedule.Widest != 3 {
		t.Fatalf("schedule = %d waves, widest %d, want 2 and 3", schedule.Count, schedule.Widest)
	}
	// A block sits one wave past the deepest block it waits for, never past
	// the first, so a long chain never overlaps what follows it.
	deep, err := NewPlan(Apply, []BlockDefinition{
		definition("alpha"),
		definition("bravo", "alpha"),
		definition("charlie", "alpha", "bravo"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if wave := ScheduleOf(deep).Waves["charlie"]; wave != 2 {
		t.Fatalf("deepest dependency wave = %d, want 2", wave)
	}
}

// Every block sits after each block it waits for, whatever else the order
// groups together, because the order is what continuation and removal replay.
func TestPlanOrderNeverPrecedesADependency(t *testing.T) {
	plan, err := NewPlan(Apply, []BlockDefinition{
		definition("alpha"), definition("bravo", "alpha"), definition("charlie", "bravo"),
		definition("delta"), definition("echo", "delta", "charlie"),
	})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for index, block := range plan.Blocks {
		for _, dependency := range block.Dependencies {
			position, ordered := seen[dependency]
			if !ordered || position >= index {
				t.Fatalf("%s precedes its dependency %s", block.ID, dependency)
			}
		}
		seen[block.ID] = index
	}
}

// A block may name host resources it does not share while it runs. The plan
// freezes them with everything else it froze, so what a removal must not run
// beside is the same set its apply declared.
func TestExclusiveResourcesAreFrozenAndBounded(t *testing.T) {
	block := definition("alpha")
	block.Exclusive = []string{"path:/srv/tree"}
	plan, err := NewPlan(Apply, []BlockDefinition{block, definition("bravo")})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(plan.Blocks[0].Exclusive, []string{"path:/srv/tree"}) {
		t.Fatalf("exclusive = %v", plan.Blocks[0].Exclusive)
	}
	inverse, err := plan.Inverse()
	if err != nil {
		t.Fatal(err)
	}
	for _, removed := range inverse.Blocks {
		if removed.ID != "alpha" {
			continue
		}
		if !slices.Equal(removed.Exclusive, []string{"path:/srv/tree"}) {
			t.Fatalf("a removal dropped what its apply would not share: %v", removed.Exclusive)
		}
	}
	unordered := definition("charlie")
	unordered.Exclusive = []string{"path:/srv/b", "path:/srv/a"}
	if _, err := NewPlan(Apply, []BlockDefinition{unordered}); err == nil {
		t.Fatal("an unordered exclusive set was frozen")
	}
	repeated := definition("delta")
	repeated.Exclusive = []string{"path:/srv/a", "path:/srv/a"}
	if _, err := NewPlan(Apply, []BlockDefinition{repeated}); err == nil {
		t.Fatal("a repeated exclusive resource was frozen")
	}
	oversized := definition("echo")
	for index := range MaxExclusive + 1 {
		oversized.Exclusive = append(oversized.Exclusive, "path:/srv/"+string(rune('a'+index)))
	}
	if _, err := NewPlan(Apply, []BlockDefinition{oversized}); err == nil {
		t.Fatal("an unbounded exclusive set was frozen")
	}
}

// A list a block always carries has one encoding when it is empty, so a
// frozen plan never reads null for it, however the plan was built.
func TestAPlanEncodesEveryUnsetListAsEmpty(t *testing.T) {
	bare := definition("bare")
	bare.Dependencies, bare.Impacts, bare.Consumes, bare.Groups = nil, nil, nil, nil
	plan, err := NewPlan(Apply, []BlockDefinition{bare})
	if err != nil {
		t.Fatal(err)
	}
	inverse, err := plan.Inverse()
	if err != nil {
		t.Fatal(err)
	}
	retained, err := plan.Retain([]string{"bare"})
	if err != nil {
		t.Fatal(err)
	}
	for name, built := range map[string]Plan{"the plan": plan, "its inverse": inverse, "its retained part": retained} {
		data, err := json.Marshal(built)
		if err != nil {
			t.Fatal(err)
		}
		for _, member := range []string{`"dependencies":[]`, `"impacts":[]`, `"consumes":[]`, `"groups":[]`} {
			if !strings.Contains(string(data), member) {
				t.Errorf("%s does not encode %s: %s", name, member, data)
			}
		}
		if strings.Contains(string(data), "null") {
			t.Errorf("%s encodes null: %s", name, data)
		}
		block, found := built.Block("bare")
		if !found || block.Dependencies == nil || block.Impacts == nil || block.Consumes == nil || block.Groups == nil {
			t.Errorf("%s hands out a nil list: %+v", name, block)
		}
	}
}
