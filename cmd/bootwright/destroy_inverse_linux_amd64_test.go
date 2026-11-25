//go:build linux && amd64

package main

import (
	"context"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// preparedExample freezes its apply plan only within the scope its disk
// discovery preparation names: planned whole, its installation groups name no
// Machine yet.
const preparedExample = "lab-anysize"

// removalArms are the relaxations a removal may be planned under, each the
// machines whose blocks the composed capabilities relax or abandon.
func removalArms(resolver lifecycle.CapabilityResolver, applied reconciliation.Plan) map[string]map[string]string {
	arms := map[string]map[string]string{"plain": nil}
	for _, block := range applied.Blocks {
		capability, ok := resolver.Resolve(block.Kind, block.Implementation)
		if !ok {
			continue
		}
		if relaxer, relaxes := capability.(lifecycle.RemovalRelaxer); relaxes && relaxer.RelaxesMachine(block) {
			for _, method := range []string{lifecycle.EraseRetain, lifecycle.EraseMetadataClear} {
				if arms[method] == nil {
					arms[method] = map[string]string{}
				}
				arms[method][block.Object] = method
			}
		}
		if abandoner, abandons := capability.(lifecycle.RemovalAbandoner); abandons && abandoner.AbandonsMachine(block) {
			if arms[lifecycle.Abandon] == nil {
				arms[lifecycle.Abandon] = map[string]string{}
			}
			arms[lifecycle.Abandon][block.Object] = lifecycle.Abandon
		}
	}
	return arms
}

// TestEveryExampleRemovalStepStatesItsInverse is the step-pairing guard: every
// example's apply plan, removed whole through this build's composition, plainly
// and under each erase relaxation and abandonment its capabilities offer (no
// example yet plans an abandonable block, so that arm runs only once one does),
// freezes a removal whose every step states an effect, what it keeps, the steps
// it leaves with or why it has nothing to take back, and whose frozen
// leaves-with answer is the one the removal phases compute from it.
func TestEveryExampleRemovalStepStatesItsInverse(t *testing.T) {
	t.Parallel()
	resolver := buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{})
	retired, exercised := 0, map[string]bool{}
	for _, planned := range plannedExamples(t) {
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
		if planned.name == preparedExample {
			if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != "records.plan-invalid" || !strings.Contains(reported[0].Message, "bounded unique ordered Machine set") {
				t.Fatalf("%s planned whole refuses %v, not its unprepared installation groups", planned.name, reported)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", planned.name, diagnostics.Of(err))
		}
		for arm, relaxations := range removalArms(resolver, applied) {
			removal, computed, err := lifecycle.RemovalOf(context.Background(), resolver, applied, relaxations)
			if err != nil {
				t.Errorf("%s: the %s removal refuses: %v", planned.name, arm, diagnostics.Of(err))
				continue
			}
			exercised[arm] = true
			frozen := map[string][]string{}
			for _, block := range removal.Blocks {
				if len(block.Impacts) == 0 && !block.Inverse.Stated() {
					t.Errorf("%s: the %s removal step %s (%s) states no inverse", planned.name, arm, block.ID, block.Implementation)
				}
				if block.Inverse != nil && len(block.Inverse.LeavesWith) != 0 {
					frozen[block.ID] = block.Inverse.LeavesWith
				}
			}
			if !maps.EqualFunc(frozen, computed, slices.Equal[[]string]) {
				t.Errorf("%s: the %s removal froze leaves-with %v, and its phases compute %v", planned.name, arm, frozen, computed)
			}
			if arm == "plain" {
				retired += len(frozen)
			}
		}
	}
	if retired == 0 {
		t.Fatal("no example removal freezes a step that leaves with its hosts, so the guard compares nothing")
	}
	for _, arm := range []string{"plain", lifecycle.EraseRetain, lifecycle.EraseMetadataClear} {
		if !exercised[arm] {
			t.Errorf("no example plans its removal under %s", arm)
		}
	}
}
