package reconciliation

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

func withPrerequisites(id string, dependencies []string, prerequisites ...string) BlockDefinition {
	d := definition(id, dependencies...)
	d.Prerequisites = prerequisites
	return d
}

func planOf(t *testing.T, definitions ...BlockDefinition) Plan {
	t.Helper()
	plan, err := NewPlan(Apply, definitions)
	if err != nil {
		t.Fatalf("planning: %v", diagnostics.Of(err))
	}
	return plan
}

func TestAPlanWithoutPrerequisitesEncodesAndDigestsAsBefore(t *testing.T) {
	plan := planOf(t, definition("alpha"), definition("bravo", "alpha"))
	encoded, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "prerequisites") {
		t.Fatalf("a plan without prerequisites encodes the member: %s", encoded)
	}
	for _, block := range plan.Blocks {
		if block.Prerequisites != nil {
			t.Fatalf("%s carries %#v, not nil", block.ID, block.Prerequisites)
		}
	}
	explicit := definition("bravo", "alpha")
	explicit.Prerequisites = []string{}
	if same := planOf(t, definition("alpha"), explicit); !reflect.DeepEqual(same, plan) {
		t.Fatalf("an empty prerequisite list froze differently: %+v", same)
	}
	before, err := plan.Digest()
	if err != nil {
		t.Fatal(err)
	}
	const pinned = "2ecae48ccee7d734f4a577dd3ed99993a23a445a3cc9f79e94db4c35e28d2006"
	if before != pinned {
		t.Fatalf("the digest of a plan without prerequisites moved to %s", before)
	}
}

func TestPrerequisitesAreHashedAndExcludedFromSchedulingAndDependencies(t *testing.T) {
	plain := planOf(t, definition("alpha"))
	added := planOf(t, withPrerequisites("alpha", nil, "root"))
	if plainDigest, addedDigest := mustDigest(t, plain), mustDigest(t, added); plainDigest == addedDigest {
		t.Fatal("a prerequisite did not move the plan digest")
	}
	other := planOf(t, withPrerequisites("alpha", nil, "other"))
	if mustDigest(t, added) == mustDigest(t, other) {
		t.Fatal("a different prerequisite did not move the plan digest")
	}
	chained := planOf(t, withPrerequisites("alpha", nil, "root"), withPrerequisites("bravo", []string{"alpha"}, "root", "zulu"))
	if got := chained.Blocks[0].Dependencies; len(got) != 0 {
		t.Fatalf("a prerequisite became a dependency: %v", got)
	}
	if got := ScheduleOf(chained); got.Count != 2 || got.Waves["alpha"] != 0 || got.Waves["bravo"] != 1 {
		t.Fatalf("prerequisites changed the schedule: %+v", got)
	}
	if got := ids(Ready(chained, map[string]BlockState{})); !slices.Equal(got, []string{"alpha"}) {
		t.Fatalf("ready = %v, want only alpha", got)
	}
	states := map[string]BlockState{"alpha": BlockDone}
	if got := ids(Ready(chained, states)); !slices.Equal(got, []string{"bravo"}) {
		t.Fatalf("ready after alpha = %v", got)
	}
}

func mustDigest(t *testing.T, plan Plan) string {
	t.Helper()
	digest, err := plan.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

func TestPlanRefusesMalformedPrerequisites(t *testing.T) {
	for name, test := range map[string]struct {
		verb        Verb
		definitions []BlockDefinition
	}{
		"a block of its own plan":   {Apply, []BlockDefinition{definition("alpha"), withPrerequisites("bravo", nil, "alpha")}},
		"itself":                    {Apply, []BlockDefinition{withPrerequisites("alpha", nil, "alpha")}},
		"an unsorted set":           {Apply, []BlockDefinition{withPrerequisites("alpha", nil, "zulu", "root")}},
		"a repeated identity":       {Apply, []BlockDefinition{withPrerequisites("alpha", nil, "root", "root")}},
		"an unsafe identity":        {Apply, []BlockDefinition{withPrerequisites("alpha", nil, "Not Safe")}},
		"an empty identity":         {Apply, []BlockDefinition{withPrerequisites("alpha", nil, "")}},
		"a removal plan":            {Destroy, []BlockDefinition{withPrerequisites("alpha", nil, "root")}},
		"more than a plan can hold": {Apply, []BlockDefinition{withPrerequisites("alpha", nil, manyPrerequisites()...)}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := NewPlan(test.verb, test.definitions)
			if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != "lifecycle.state" {
				t.Fatalf("planning read as %+v", reported)
			}
		})
	}
}

func manyPrerequisites() []string {
	out := make([]string, 0, MaxBlocks+1)
	for index := range MaxBlocks + 1 {
		out = append(out, "p"+ordinal(index))
	}
	slices.Sort(out)
	return out
}

func TestInverseRefusesAPlanThatCarriesPrerequisites(t *testing.T) {
	plan := planOf(t, withPrerequisites("alpha", nil, "root"))
	_, err := plan.Inverse()
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != "lifecycle.state" ||
		reported[0].Message != "lifecycle plan with prerequisites cannot be inverted without the plans that hold them" {
		t.Fatalf("inverse read as %+v", reported)
	}
	root := planOf(t, definition("root"))
	whole, err := Lineage(root, plan)
	if err != nil {
		t.Fatal(err)
	}
	inverse, err := whole.Inverse()
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(inverse.Blocks); !slices.Equal(got, []string{"alpha", "root"}) {
		t.Fatalf("the lineage's removal order = %v, want the addition before its predecessor", got)
	}
}

func TestRetainKeepsThePrerequisitesOfTheBlocksItKeeps(t *testing.T) {
	plan := planOf(t, withPrerequisites("alpha", nil, "root"), definition("bravo"))
	kept, err := plan.Retain([]string{"alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if block, _ := kept.Block("alpha"); !slices.Equal(block.Prerequisites, []string{"root"}) {
		t.Fatalf("retained alpha carries %v", block.Prerequisites)
	}
}

func chainOf() []BlockDefinition {
	return []BlockDefinition{
		definition("net"), definition("machine", "net"), definition("cluster", "machine"),
		definition("instance-a", "cluster"), definition("instance-b", "cluster", "instance-a"),
		definition("instance-c", "instance-b"), definition("loose"),
	}
}

func TestLineageOfAnAdditionRestoresTheFreshPlan(t *testing.T) {
	fresh := planOf(t, chainOf()...)
	rootBlocks := []string{"net", "machine", "cluster"}
	root, err := fresh.Retain(rootBlocks)
	if err != nil {
		t.Fatal(err)
	}
	added, err := Additive(fresh, root)
	if err != nil {
		t.Fatalf("adding: %v", diagnostics.Of(err))
	}
	if got := ids(added.Blocks); slices.Contains(got, "cluster") || len(got) != 4 {
		t.Fatalf("the addition holds %v", got)
	}
	instance, _ := added.Block("instance-a")
	if !slices.Equal(instance.Prerequisites, []string{"cluster"}) || len(instance.Dependencies) != 0 {
		t.Fatalf("instance-a carries prerequisites %v and dependencies %v", instance.Prerequisites, instance.Dependencies)
	}
	second, _ := added.Block("instance-b")
	if !slices.Equal(second.Prerequisites, []string{"cluster"}) || !slices.Equal(second.Dependencies, []string{"instance-a"}) {
		t.Fatalf("instance-b carries prerequisites %v and dependencies %v", second.Prerequisites, second.Dependencies)
	}
	loose, _ := added.Block("loose")
	if loose.Prerequisites != nil {
		t.Fatalf("a block that waits for no lineage block carries %v", loose.Prerequisites)
	}
	joined, err := Lineage(root, added)
	if err != nil {
		t.Fatalf("joining: %v", diagnostics.Of(err))
	}
	if !reflect.DeepEqual(joined, fresh) || mustDigest(t, joined) != mustDigest(t, fresh) {
		t.Fatalf("the lineage is not the fresh plan:\n%+v\n%+v", joined, fresh)
	}
}

func TestLineageOfTwoAdditionsRestoresTheFreshPlan(t *testing.T) {
	fresh := planOf(t, chainOf()...)
	root, err := fresh.Retain([]string{"net", "machine", "cluster"})
	if err != nil {
		t.Fatal(err)
	}
	firstFresh, err := fresh.Retain([]string{"net", "machine", "cluster", "instance-a", "loose"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := Additive(firstFresh, root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Additive(fresh, root, first)
	if err != nil {
		t.Fatalf("adding twice: %v", diagnostics.Of(err))
	}
	if got := ids(second.Blocks); !slices.Equal(got, []string{"instance-b", "instance-c"}) {
		t.Fatalf("the second addition holds %v", got)
	}
	if block, _ := second.Block("instance-b"); !slices.Equal(block.Prerequisites, []string{"cluster", "instance-a"}) || len(block.Dependencies) != 0 {
		t.Fatalf("instance-b carries prerequisites %v and dependencies %v", block.Prerequisites, block.Dependencies)
	}
	joined, err := Lineage(root, first, second)
	if err != nil {
		t.Fatalf("joining: %v", diagnostics.Of(err))
	}
	if !reflect.DeepEqual(joined, fresh) {
		t.Fatalf("the lineage is not the fresh plan:\n%+v\n%+v", joined, fresh)
	}
}

func TestAdditiveRefusesWhatWouldRewriteTheLineage(t *testing.T) {
	root := planOf(t, definition("net"), definition("cluster", "net"))
	edit := func(change func(*BlockDefinition)) []BlockDefinition {
		cluster := definition("cluster", "net")
		change(&cluster)
		return []BlockDefinition{definition("net"), cluster, definition("instance", "cluster")}
	}
	for name, test := range map[string]struct {
		fresh   []BlockDefinition
		message string
	}{
		"a changed request": {edit(func(d *BlockDefinition) { d.Request = json.RawMessage(`{"name":"cluster","x":1}`) }),
			"lifecycle block cluster would be frozen differently from the plan it was frozen in"},
		"a changed dependency set": {edit(func(d *BlockDefinition) { d.Dependencies = nil }),
			"lifecycle block cluster would be frozen differently from the plan it was frozen in"},
		"a dropped block": {[]BlockDefinition{definition("net"), definition("instance", "net")},
			"lifecycle block cluster would be frozen differently from the plan it was frozen in"},
		"nothing added": {[]BlockDefinition{definition("net"), definition("cluster", "net")},
			"the plan adds no block to the lineage"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Additive(planOf(t, test.fresh...), root)
			if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Message != test.message {
				t.Fatalf("adding read as %+v", reported)
			}
		})
	}
	same := edit(func(d *BlockDefinition) { d.Description = "described anew" })
	if _, err := Additive(planOf(t, same...), root); err != nil {
		t.Fatalf("a description is not a different freeze: %v", diagnostics.Of(err))
	}
	if _, err := Additive(planOf(t, same...)); err == nil {
		t.Fatal("an addition to no plan was admitted")
	}
	if _, err := Additive(planOf(t, same...), mustDestroy(t, root)); err == nil {
		t.Fatal("an addition to a removal was admitted")
	}
	carrying := planOf(t, definition("net"), definition("cluster", "net"), withPrerequisites("instance", nil, "elsewhere"))
	if _, err := Additive(carrying, root); err == nil {
		t.Fatal("an addition from a plan that already carries prerequisites was admitted")
	}
}

func mustDestroy(t *testing.T, plan Plan) Plan {
	t.Helper()
	inverse, err := plan.Inverse()
	if err != nil {
		t.Fatal(err)
	}
	return inverse
}

func TestLineageRefusesWhatIsNotOneLineage(t *testing.T) {
	root := planOf(t, definition("net"), definition("cluster", "net"))
	addition := planOf(t, withPrerequisites("instance", nil, "cluster"))
	unheld := planOf(t, withPrerequisites("instance", nil, "ghost"))
	for name, test := range map[string]struct {
		plans   []Plan
		message string
	}{
		"no plan":                  {nil, "a lineage needs at least one plan"},
		"a repeated identity":      {[]Plan{root, planOf(t, definition("cluster"))}, "lifecycle lineage repeats the block cluster"},
		"the same plan twice":      {[]Plan{root, root}, "lifecycle lineage repeats the block net"},
		"a prerequisite not held":  {[]Plan{root, unheld}, "lifecycle block instance has the prerequisite ghost, which no earlier plan holds"},
		"an addition before root":  {[]Plan{addition, root}, "lifecycle block instance has the prerequisite cluster, which no earlier plan holds"},
		"an addition alone":        {[]Plan{addition}, "lifecycle block instance has the prerequisite cluster, which no earlier plan holds"},
		"a removal":                {[]Plan{root, mustDestroy(t, root)}, "only apply plans make a lineage"},
		"a prerequisite of itself": {[]Plan{{Verb: Apply, Blocks: []Block{{BlockDefinition: withPrerequisites("loop", nil, "loop")}}}}, "lifecycle block loop has the prerequisite loop, which no earlier plan holds"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Lineage(test.plans...)
			if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Message != test.message {
				t.Fatalf("joining read as %+v", reported)
			}
		})
	}
	if _, err := Lineage(root, addition); err != nil {
		t.Fatalf("a held prerequisite refused: %v", diagnostics.Of(err))
	}
}

func TestAnAddedBlockKeepsItsAddedDependenciesAndMovesTheLineageOnesToPrerequisites(t *testing.T) {
	root := planOf(t, definition("net"), definition("cluster", "net"))
	fresh := planOf(t, definition("net"), definition("cluster", "net"), definition("sidecar", "cluster"), definition("instance", "cluster", "sidecar"))
	added, err := Additive(fresh, root)
	if err != nil {
		t.Fatalf("adding: %v", diagnostics.Of(err))
	}
	instance, _ := added.Block("instance")
	if !slices.Equal(instance.Dependencies, []string{"sidecar"}) || !slices.Equal(instance.Prerequisites, []string{"cluster"}) {
		t.Fatalf("instance carries dependencies %v and prerequisites %v", instance.Dependencies, instance.Prerequisites)
	}
	sidecar, _ := added.Block("sidecar")
	if len(sidecar.Dependencies) != 0 || !slices.Equal(sidecar.Prerequisites, []string{"cluster"}) {
		t.Fatalf("sidecar carries dependencies %v and prerequisites %v", sidecar.Dependencies, sidecar.Prerequisites)
	}
	if got := ids(Ready(added, map[string]BlockState{})); !slices.Equal(got, []string{"sidecar"}) {
		t.Fatalf("ready = %v, want only the sidecar: the instance waits for it, not for the lineage", got)
	}
	joined, err := Lineage(root, added)
	if err != nil || !reflect.DeepEqual(joined, fresh) {
		t.Fatalf("the lineage is not the fresh plan (%v):\n%+v\n%+v", diagnostics.Of(err), joined, fresh)
	}
}

func TestAnAdditionAcrossATwoPlanLineageHoldsPrerequisitesInBothPlans(t *testing.T) {
	root := planOf(t, definition("net"), definition("cluster", "net"))
	first := mustAdd(t, planOf(t, definition("net"), definition("cluster", "net"), definition("alpha", "cluster")), root)
	fresh := planOf(t, definition("net"), definition("cluster", "net"), definition("alpha", "cluster"),
		definition("bravo", "cluster", "alpha"), definition("charlie", "alpha", "bravo"), definition("delta", "net"))
	second, err := Additive(fresh, root, first)
	if err != nil {
		t.Fatalf("adding: %v", diagnostics.Of(err))
	}
	if got := ids(second.Blocks); !slices.Equal(got, []string{"bravo", "delta", "charlie"}) {
		t.Fatalf("the addition holds %v", got)
	}
	bravo, _ := second.Block("bravo")
	if len(bravo.Dependencies) != 0 || !slices.Equal(bravo.Prerequisites, []string{"alpha", "cluster"}) {
		t.Fatalf("bravo carries dependencies %v and prerequisites %v, want both plans' blocks as prerequisites", bravo.Dependencies, bravo.Prerequisites)
	}
	charlie, _ := second.Block("charlie")
	if !slices.Equal(charlie.Dependencies, []string{"bravo"}) || !slices.Equal(charlie.Prerequisites, []string{"alpha"}) {
		t.Fatalf("charlie carries dependencies %v and prerequisites %v", charlie.Dependencies, charlie.Prerequisites)
	}
	delta, _ := second.Block("delta")
	if len(delta.Dependencies) != 0 || !slices.Equal(delta.Prerequisites, []string{"net"}) {
		t.Fatalf("delta carries dependencies %v and prerequisites %v", delta.Dependencies, delta.Prerequisites)
	}
	joined, err := Lineage(root, first, second)
	if err != nil || !reflect.DeepEqual(joined, fresh) {
		t.Fatalf("the lineage is not the fresh plan (%v):\n%+v\n%+v", diagnostics.Of(err), joined, fresh)
	}
}

func mustAdd(t *testing.T, fresh Plan, owned ...Plan) Plan {
	t.Helper()
	added, err := Additive(fresh, owned...)
	if err != nil {
		t.Fatalf("adding: %v", diagnostics.Of(err))
	}
	return added
}

func TestALineageBlockDifferingOnlyInImpactsOrGroupTextIsAcceptedAndKeepsItsFrozenText(t *testing.T) {
	root := planOf(t, definition("net"), definition("cluster", "net"))
	reworded := definition("cluster", "net")
	reworded.Impacts = []string{"create-container-unit", "restart-the-service"}
	reworded.Groups = []Group{{ID: "start-service", Description: "bring the service up", Machines: []string{"controller"}}}
	fresh := planOf(t, definition("net"), reworded, definition("instance", "cluster"))
	added, err := Additive(fresh, root)
	if err != nil {
		t.Fatalf("a text-only difference refused: %v", diagnostics.Of(err))
	}
	if got := ids(added.Blocks); !slices.Equal(got, []string{"instance"}) {
		t.Fatalf("the addition holds %v, not only the added block", got)
	}
	joined, err := Lineage(root, added)
	if err != nil {
		t.Fatal(err)
	}
	cluster, _ := joined.Block("cluster")
	frozen, _ := root.Block("cluster")
	if !reflect.DeepEqual(cluster.Impacts, frozen.Impacts) || !reflect.DeepEqual(cluster.Groups, frozen.Groups) {
		t.Fatalf("the lineage reworded a frozen block: %+v", cluster)
	}
	changed := definition("cluster", "net")
	changed.Groups = []Group{{ID: "start-service", Description: "start the service", Machines: []string{"other"}}}
	if _, err := Additive(planOf(t, definition("net"), changed, definition("instance", "cluster")), root); err == nil {
		t.Fatal("a group on other machines is a different freeze but was admitted")
	}
}
