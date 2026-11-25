package lifecycle

import (
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

// TestTheOwnedLineageReadsEachRemovalStepsSettledEffect builds the fold's
// members as production does over a completed removal that abandoned one step:
// the abandoned step reads abandoned and stays owned, the removed ones read
// absent and taken back, so a removal that left its effect in place is never
// read as one that removed it.
func TestTheOwnedLineageReadsEachRemovalStepsSettledEffect(t *testing.T) {
	t.Parallel()
	h := unprovedEnrollment(t)
	if _, err := h.service.Destroy(t.Context(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true, Relaxations: abandoning("enrollment")}); err != nil {
		t.Fatal(err)
	}
	store := h.service.store(h.workspace.view())
	index, err := store.Index(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	removal, err := store.ReadOperation(t.Context(), index.Current)
	if err != nil || removal.Verb != reconciliation.Destroy || removal.Source == "" {
		t.Fatalf("the index names %+v (%v)", removal, err)
	}
	plan, err := store.ReadPlan(t.Context(), removal.ID)
	if err != nil {
		t.Fatal(err)
	}
	states, err := store.BlockStates(t.Context(), removal.ID, plan)
	if err != nil {
		t.Fatal(err)
	}
	held, err := ownedLineage(t.Context(), store, lineageMember{operation: removal, plan: plan, states: states})
	if err != nil || len(held) != 2 || !held[1].whole {
		t.Fatalf("the owned lineage reads %d members (%v)", len(held), err)
	}
	if effect := held[1].effects["enrollment"]; effect != reconciliation.EffectNoEffect {
		t.Fatalf("the abandoned step's settled effect reads %q", effect)
	}
	owned := held.ownership()
	for id, want := range map[string]reconciliation.Life{"enrollment": reconciliation.LifeAbandoned, "provider": reconciliation.LifeAbsent, "artifact": reconciliation.LifeAbsent} {
		holding, found := owned.Holding(id)
		if !found || holding.Holder != 1 || holding.Life != want || owned.TakenBack(holding) != (want == reconciliation.LifeAbsent) {
			t.Errorf("%s reads %v %+v, want %s", id, found, holding, want)
		}
	}
	if _, _, found := held.owners().of("provider"); found {
		t.Error("a step the removal took back still routes to its apply")
	}
	if owner, _, found := held.owners().of("enrollment"); !found || owner.operation.ID != removal.Source {
		t.Errorf("the abandoned step routes to %+v (%v), want its apply", owner.operation, found)
	}
}
