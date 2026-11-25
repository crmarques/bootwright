//go:build linux && amd64

package main

import (
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// floorScope is a staged destroy's scope over a plan: its steps at or above
// the floor and every step that depends on one.
func floorScope(plan reconciliation.Plan, floor reconciliation.Stage) []string {
	in := map[string]bool{}
	for changed := true; changed; {
		changed = false
		for _, block := range plan.Blocks {
			if !in[block.ID] && (block.Stage.AtOrAbove(floor) || slices.ContainsFunc(block.Dependencies, func(id string) bool { return in[id] })) {
				in[block.ID], changed = true, true
			}
		}
	}
	var scope []string
	for _, block := range plan.Blocks {
		if in[block.ID] {
			scope = append(scope, block.ID)
		}
	}
	return scope
}

// TestEveryExampleRestoresWhatEachFloorTakesBack plans every example whole and,
// for each floor that keeps a step, restores what that floor takes back over
// the unchanged plan: the restore adds exactly the taken-back steps, every kept
// step compares the same, and the fold of the completed apply, the staged
// destroy and the restore merges back into the plan the apply froze.
func TestEveryExampleRestoresWhatEachFloorTakesBack(t *testing.T) {
	t.Parallel()
	resolver := buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{})
	restored := 0
	for _, planned := range plannedExamples(t) {
		if planned.name == preparedExample {
			continue
		}
		definitions := make([]reconciliation.BlockDefinition, 0, len(planned.blocks))
		for _, block := range planned.blocks {
			definitions = append(definitions, block.definition)
		}
		resolved, err := reconciliation.ResolveRequirements(definitions)
		if err != nil {
			t.Fatalf("%s: %v", planned.name, diagnostics.Of(err))
		}
		ordered, err := lifecycle.DependOnController(resolver, resolved)
		if err != nil {
			t.Fatalf("%s: %v", planned.name, diagnostics.Of(err))
		}
		applied, err := reconciliation.NewPlan(reconciliation.Apply, ordered)
		if err != nil {
			t.Fatalf("%s: %v", planned.name, diagnostics.Of(err))
		}
		done := map[string]reconciliation.BlockState{}
		for _, block := range applied.Blocks {
			done[block.ID] = reconciliation.BlockDone
		}
		for _, floor := range []reconciliation.Stage{reconciliation.StageSubstrates, reconciliation.StageMachines, reconciliation.StageClusters, reconciliation.StageAddOns} {
			scope := floorScope(applied, floor)
			if len(scope) == 0 || len(scope) == len(applied.Blocks) {
				continue
			}
			kept := slices.DeleteFunc(slices.Clone(stepIDs(applied)), func(id string) bool { return slices.Contains(scope, id) })
			owned, err := applied.Retain(kept)
			if err != nil {
				t.Fatalf("%s at %s: %v", planned.name, floor, err)
			}
			if first, differs := reconciliation.Compare(owned, applied).First(); differs && first.Change != reconciliation.Added {
				t.Fatalf("%s at %s: the kept step %s compares %s", planned.name, floor, first.Block, first.Change)
			}
			added, err := reconciliation.Restoring(applied, owned)
			sorted := slices.Sorted(slices.Values(scope))
			if err != nil || !slices.Equal(slices.Sorted(slices.Values(stepIDs(added))), sorted) {
				t.Fatalf("%s at %s: the restore adds %v, want %v (%v)", planned.name, floor, stepIDs(added), scope, diagnostics.Of(err))
			}
			merged, err := reconciliation.Fold(
				reconciliation.Member{Operation: "applied", Plan: applied, States: done, Completed: true},
				reconciliation.Member{Operation: "staged", Plan: reconciliation.Plan{Verb: reconciliation.Destroy}, TakenBack: sorted},
				reconciliation.Member{Operation: "restore", Plan: added},
			).Plan()
			if err != nil {
				t.Fatalf("%s at %s: the restored lineage does not merge: %v", planned.name, floor, diagnostics.Of(err))
			}
			if first, differs := reconciliation.Compare(merged, applied).First(); differs {
				t.Fatalf("%s at %s: the restored lineage holds %s %s", planned.name, floor, first.Block, first.Change)
			}
			restored++
		}
	}
	if restored == 0 {
		t.Fatal("no example admitted a floor that keeps a step")
	}
}

func stepIDs(plan reconciliation.Plan) []string {
	ids := make([]string, 0, len(plan.Blocks))
	for _, block := range plan.Blocks {
		ids = append(ids, block.ID)
	}
	return ids
}
