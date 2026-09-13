package reconciliation

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func staged(id string, stage Stage, dependencies ...string) BlockDefinition {
	block := definition(id, dependencies...)
	block.Stage = stage
	return block
}

// nestedPlan is the shape that makes stages a graph rather than strata: a
// KubeVirt substrate cannot exist before the hosting cluster's virtualization
// add-on, so a substrate block waits on an add-on block.
func nestedPlan(t *testing.T) Plan {
	t.Helper()
	plan, err := NewPlan(Apply, []BlockDefinition{
		staged("artifacts", StageInfraComponents),
		staged("provider-metal", StageSubstrates),
		staged("host-node", StageMachines, "provider-metal"),
		staged("host-cluster", StageClusters, "host-node"),
		staged("host-virtualization", StageAddOns, "host-cluster"),
		staged("provider-kubevirt", StageSubstrates, "host-virtualization"),
		staged("hub-node", StageMachines, "provider-kubevirt"),
		staged("hub-cluster", StageClusters, "hub-node"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func ids(blocks []Block) []string {
	out := make([]string, 0, len(blocks))
	for _, block := range blocks {
		out = append(out, block.ID)
	}
	return out
}

func TestStageNamesAreClosedAndCanonical(t *testing.T) {
	selection, err := ParseStages([]string{"clusters", "infra-components", "clusters"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(selection.Names(), []string{"infra-components", "clusters"}) {
		t.Fatalf("selection = %v", selection.Names())
	}
	if _, err := ParseStages([]string{"storage"}); err == nil {
		t.Fatal("an unknown stage was accepted")
	}
	empty := StageSelection(nil)
	for _, stage := range Stages() {
		if !empty.Selects(stage) {
			t.Fatalf("an empty selection excluded %s", stage)
		}
	}
	if selection.Selects(StageMachines) {
		t.Fatal("a selection admitted a stage it does not name")
	}
}

func TestPlanRefusesABlockWithoutARecognizedStage(t *testing.T) {
	for name, stage := range map[string]Stage{"absent": "", "invented": "storage"} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewPlan(Apply, []BlockDefinition{staged("alpha", stage)}); err == nil {
				t.Fatal("a block with no recognized stage was frozen")
			}
		})
	}
}

// The stage is frozen with the block, so moving a block to another stage is a
// different plan and cannot be continued as the same operation.
func TestPlanDigestCoversTheStage(t *testing.T) {
	first, err := NewPlan(Apply, []BlockDefinition{staged("alpha", StageInfraComponents)})
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewPlan(Apply, []BlockDefinition{staged("alpha", StageSubstrates)})
	if err != nil {
		t.Fatal(err)
	}
	before, err := first.Digest()
	if err != nil {
		t.Fatal(err)
	}
	after, err := second.Digest()
	if err != nil || before == after {
		t.Fatalf("digests = %s and %s (%v)", before, after, err)
	}
	if !strings.Contains(string(mustEncode(t, first.Blocks[0])), `"stage":"infra-components"`) {
		t.Fatal("the frozen block does not carry its stage")
	}
}

func mustEncode(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestReadyFollowsDependenciesAndStartableFiltersByStage(t *testing.T) {
	plan := nestedPlan(t)
	states := map[string]BlockState{}
	if got := ids(Ready(plan, states)); !slices.Equal(got, []string{"artifacts", "provider-metal"}) {
		t.Fatalf("ready = %v", got)
	}
	substrates, err := ParseStages([]string{"substrates"})
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(Startable(plan, states, substrates)); !slices.Equal(got, []string{"provider-metal"}) {
		t.Fatalf("startable = %v", got)
	}
	states["provider-metal"] = BlockDone
	if got := ids(Startable(plan, states, substrates)); len(got) != 0 {
		t.Fatalf("the kubevirt substrate started before its add-on: %v", got)
	}
	for _, id := range []string{"artifacts", "host-node", "host-cluster", "host-virtualization"} {
		states[id] = BlockDone
	}
	if got := ids(Startable(plan, states, substrates)); !slices.Equal(got, []string{"provider-kubevirt"}) {
		t.Fatalf("startable after the add-on = %v", got)
	}
}

func TestDeferralsExplainEveryPendingBlock(t *testing.T) {
	plan := nestedPlan(t)
	selection, err := ParseStages([]string{"substrates"})
	if err != nil {
		t.Fatal(err)
	}
	deferrals := Deferrals(plan, map[string]BlockState{}, selection)
	if deferrals["artifacts"].Reason != DeferredNotSelected || deferrals["artifacts"].Stage != StageInfraComponents {
		t.Fatalf("artifacts deferral = %+v", deferrals["artifacts"])
	}
	if _, waiting := deferrals["provider-metal"]; waiting {
		t.Fatal("a startable block was reported as deferred")
	}
	kubevirt := deferrals["provider-kubevirt"]
	if kubevirt.Reason != DeferredWaiting || kubevirt.Block != "host-virtualization" || kubevirt.Stage != StageAddOns {
		t.Fatalf("kubevirt deferral = %+v", kubevirt)
	}
}

// A removal covers what an apply completed, so a paused operation destroys
// exactly its done blocks and nothing it never started.
func TestDoneSubsetKeepsFrozenOrder(t *testing.T) {
	plan := nestedPlan(t)
	states := map[string]BlockState{"artifacts": BlockDone, "provider-metal": BlockDone, "host-node": BlockFailed}
	owned := DoneSubset(plan, states)
	if !slices.Equal(ids(owned.Blocks), []string{"artifacts", "provider-metal"}) {
		t.Fatalf("owned = %v", ids(owned.Blocks))
	}
	if got := ids(owned.Inverse().Blocks); !slices.Equal(got, []string{"provider-metal", "artifacts"}) {
		t.Fatalf("removal order = %v", got)
	}
}
