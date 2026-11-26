package reconciliation

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

func definition(id string, dependencies ...string) BlockDefinition {
	slices.Sort(dependencies)
	return BlockDefinition{
		ID:             id,
		Description:    "serve artifacts for " + id,
		Stage:          StageInfrastructureServices,
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

func TestOneClusterCanHoldItsExecutionKeyAndEightBMCOrigins(t *testing.T) {
	block := definition("cluster")
	block.Exclusive = []string{"container-cluster-install"}
	for index := range 8 {
		block.Exclusive = append(block.Exclusive, "bmc-media:bmc-"+string(rune('a'+index))+".example.test:443")
	}
	slices.Sort(block.Exclusive)
	if _, err := NewPlan(Apply, []BlockDefinition{block}); err != nil {
		t.Fatalf("the existing eight-node topology cannot hold its media scope: %v", err)
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

// A relaxation is frozen only on a removal, enters its digest only when set,
// and an unrelaxed removal encodes exactly as it did before the member existed.
func TestOnlyARemovalFreezesARelaxationAndOnlyASetOneEncodes(t *testing.T) {
	relaxed := definition("alpha")
	relaxed.Relaxation = "metadataClear"
	if _, err := NewPlan(Apply, []BlockDefinition{relaxed}); err == nil {
		t.Fatal("an apply froze a relaxation")
	}
	for _, method := range []string{"metadata-clear", "retain ", strings.Repeat("a", 64)} {
		invalid := definition("alpha")
		invalid.Relaxation = method
		if _, err := NewPlan(Destroy, []BlockDefinition{invalid}); err == nil {
			t.Fatalf("a removal froze the relaxation %q", method)
		}
	}
	plain, err := NewPlan(Destroy, []BlockDefinition{definition("alpha")})
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := NewPlan(Destroy, []BlockDefinition{relaxed})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(plain)
	if err != nil || strings.Contains(string(encoded), "relaxation") {
		t.Fatalf("an unrelaxed removal encodes %s (%v)", encoded, err)
	}
	if encoded, err = json.Marshal(frozen); err != nil || !strings.Contains(string(encoded), `"relaxation":"metadataClear"`) {
		t.Fatalf("a relaxed removal encodes %s (%v)", encoded, err)
	}
	var read Plan
	if err := json.Unmarshal(encoded, &read); err != nil || read.Blocks[0].Relaxation != "metadataClear" {
		t.Fatalf("a relaxed removal reads back %+v (%v)", read, err)
	}
	first, err := plain.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if second, err := frozen.Digest(); err != nil || second == first {
		t.Fatal("the relaxation does not participate in the plan digest")
	}
}

// An inverse is frozen only on a removal, names only blocks of its own plan,
// states nothing to take back only beside no effect and kept hosts only beside
// one, is cloned with its block
// and enters the plan digest, never the request digest.
func TestOnlyARemovalFreezesAnInverseAndTheDigestCoversIt(t *testing.T) {
	stated := func(inverse Inverse, impacts ...string) []BlockDefinition {
		alpha, bravo := definition("alpha"), definition("bravo", "alpha")
		alpha.Impacts, alpha.Inverse = append([]string{}, impacts...), &inverse
		return []BlockDefinition{alpha, bravo}
	}
	valid := stated(Inverse{Retains: []string{"the unit of alpha"}, LeavesWith: []string{"bravo"}})
	if _, err := NewPlan(Apply, valid); err == nil {
		t.Fatal("an apply froze an inverse")
	}
	for name, definitions := range map[string][]BlockDefinition{
		"empty":                  stated(Inverse{}),
		"unsafe retains":         stated(Inverse{Retains: []string{"line\nbreak"}}),
		"outside the plan":       stated(Inverse{LeavesWith: []string{"charlie"}}),
		"itself":                 stated(Inverse{LeavesWith: []string{"alpha"}}),
		"unordered":              stated(Inverse{LeavesWith: []string{"bravo", "alpha"}}),
		"repeated":               stated(Inverse{LeavesWith: []string{"bravo", "bravo"}}),
		"nothing beside retains": stated(Inverse{Retains: []string{"the unit"}, Nothing: "nothing"}),
		"nothing beside leaves":  stated(Inverse{LeavesWith: []string{"bravo"}, Nothing: "nothing"}),
		"nothing beside impacts": stated(Inverse{Nothing: "nothing"}, "remove-unit alpha"),
		"unordered kept hosts":   stated(Inverse{KeptHosts: []string{"node-2", "node-1"}}, "remove-unit alpha"),
		"unsafe kept host":       stated(Inverse{KeptHosts: []string{"node/1"}}, "remove-unit alpha"),
		"nothing beside kept":    stated(Inverse{KeptHosts: []string{"node-1"}, Nothing: "nothing"}),
		"kept with no impacts":   stated(Inverse{KeptHosts: []string{"node-1"}}),
		"blanks a host it drops": stated(Inverse{KeptHosts: []string{"node-1"}, Blanks: []string{"node-2"}}, "remove-unit alpha"),
		"blanks without loss":    stated(Inverse{KeptHosts: []string{"node-1"}, Blanks: []string{"node-1"}}, "remove-unit alpha"),
	} {
		if _, err := NewPlan(Destroy, definitions); err == nil {
			t.Errorf("a removal froze the %s inverse", name)
		}
	}
	frozen, err := NewPlan(Destroy, valid)
	if err != nil {
		t.Fatalf("a removal refused a valid inverse: %v", diagnostics.Of(err))
	}
	if _, err := NewPlan(Destroy, stated(Inverse{Nothing: "its apply changed nothing"})); err != nil {
		t.Fatalf("a removal refused a reason to take nothing back: %v", diagnostics.Of(err))
	}
	kept, err := NewPlan(Destroy, stated(Inverse{KeptHosts: []string{"node-1", "node-2"}}, "remove-unit alpha"))
	if encoded, _ := json.Marshal(kept); err != nil || !strings.Contains(string(encoded), `"inverse":{"keptHosts":["node-1","node-2"]}`) {
		t.Fatalf("a removal complete on kept hosts encodes %s (%v)", encoded, diagnostics.Of(err))
	}
	blanked := stated(Inverse{KeptHosts: []string{"node-1", "node-2"}, Blanks: []string{"node-1", "node-2"}}, "remove-unit alpha")
	blanked[0].Consumes = []string{AuthorizationDataLoss}
	if blanks, err := NewPlan(Destroy, blanked); err != nil || !slices.Equal(blanks.Blocks[0].Inverse.Blanks, []string{"node-1", "node-2"}) {
		t.Fatalf("a removal that blanks its kept hosts' root disks froze %+v (%v)", blanks, diagnostics.Of(err))
	}
	plain, err := NewPlan(Destroy, []BlockDefinition{definition("alpha"), definition("bravo", "alpha")})
	if err != nil {
		t.Fatal(err)
	}
	if encoded, err := json.Marshal(plain); err != nil || strings.Contains(string(encoded), "inverse") {
		t.Fatalf("a removal stating no inverse encodes %s (%v)", encoded, err)
	}
	encoded, err := json.Marshal(frozen)
	if err != nil || !strings.Contains(string(encoded), `"inverse":{"retains":["the unit of alpha"],"leavesWith":["bravo"]}`) {
		t.Fatalf("a removal stating its inverse encodes %s (%v)", encoded, err)
	}
	block, _ := frozen.Block("alpha")
	block.Inverse.Retains[0], block.Inverse.LeavesWith[0] = "changed", "changed"
	if again, _ := frozen.Block("alpha"); again.Inverse.Retains[0] != "the unit of alpha" || again.Inverse.LeavesWith[0] != "bravo" {
		t.Fatal("a block's inverse aliases the frozen plan")
	}
	first, _ := plain.Digest()
	if second, err := frozen.Digest(); err != nil || second == first {
		t.Fatal("the inverse does not participate in the plan digest")
	}
	for index := range plain.Blocks {
		if plain.Blocks[index].RequestDigest != frozen.Blocks[index].RequestDigest {
			t.Fatal("the inverse moves a request digest")
		}
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

// A cycle refuses naming its blocks, so a nested provider whose host waits
// on its own child is told which references close the loop.
func TestPlanRefusesACycleByNamingItsBlocks(t *testing.T) {
	_, err := NewPlan(Apply, []BlockDefinition{definition("addon-gitops-host", "machine-child-0"), definition("cluster-install-host"),
		definition("machine-child-0", "substrate-host-child"), definition("substrate-host-child", "addon-gitops-host", "cluster-install-host")})
	want := "addon-gitops-host -> machine-child-0 -> substrate-host-child -> addon-gitops-host"
	if found := diagnostics.Of(err); len(found) != 1 || !strings.Contains(found[0].Message, want) {
		t.Fatalf("the cycle was refused as %v, want it named %q", found, want)
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

// The plan is written wave by wave, keeping step numbers stable independently
// of ready-work priority. A block that waits for nothing sits
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
	schedule := ScheduleOf(plan, 0)
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
	if schedule := ScheduleOf(wide, 0); schedule.Count != 2 || schedule.Widest != 3 {
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
	if wave := ScheduleOf(deep, 0).Waves["charlie"]; wave != 2 {
		t.Fatalf("deepest dependency wave = %d, want 2", wave)
	}
}

func TestScheduleWidthCountsOnlyWhatClaimsAndThrottlesAdmitTogether(t *testing.T) {
	var shared, throttled, mixed []BlockDefinition
	for _, name := range []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel", "india", "juliet", "kilo", "lima"} {
		one := definition(name)
		one.Exclusive = []string{"ceph/example"}
		shared = append(shared, one)
		two := definition(name)
		two.Throttle = "ceph/example"
		throttled = append(throttled, two)
		three := definition(name)
		three.Throttle = "ceph/example"
		if name < "d" {
			three.Exclusive = []string{"ceph/example"}
		}
		mixed = append(mixed, three)
	}
	for _, test := range []struct {
		name        string
		definitions []BlockDefinition
		throttle    int
		widest      int
	}{
		{"one shared claim", shared, 4, 1},
		{"one throttle", throttled, 4, 4},
		{"an unbounded throttle", throttled, 0, 12},
		{"claims inside a throttle", mixed, 8, 8},
		{"claims inside a narrow throttle", mixed, 2, 2},
	} {
		plan, err := NewPlan(Apply, test.definitions)
		if err != nil {
			t.Fatal(err)
		}
		if schedule := ScheduleOf(plan, test.throttle); schedule.Count != 1 || schedule.Widest != test.widest {
			t.Errorf("%s: %d waves, widest %d, want 1 and %d", test.name, schedule.Count, schedule.Widest, test.widest)
		}
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
