package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
)

func (f *lineageFixture) failHead(t *testing.T, block string) {
	t.Helper()
	ctx := context.Background()
	definition, _ := f.headPlan.Block(block)
	number, err := f.store.StartAttempt(ctx, f.head.ID, block, f.h.service.ranUnder(definition))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.CompleteAttempt(ctx, f.head.ID, block, number, reconciliation.OutcomeFailed, reconciliation.EffectNoEffect, reconciliation.BlockFailed, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	f.head.State = reconciliation.OperationFailed
	if err := f.store.UpdateOperation(ctx, f.head); err != nil {
		t.Fatal(err)
	}
}

func TestAFailedAdditiveHeadReopensTheRootsBindingForStatus(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "producer-a")
	f := newLineageFixture(t, h, consumerDefinition())
	f.failHead(t, "consumer")
	reopened, err := frozenReopenOf(ctx, f.store)
	if err != nil || reopened.binding != f.root.Bindings[0] || reopened.binding == f.head.Bindings[0] {
		t.Fatalf("a failed additive head reopens %q (%v), want the root's %q", reopened.binding, err, f.root.Bindings[0])
	}
}

func TestADestroyOverAnAdditiveHeadThatStartedNothingTakesBackTheRootAndReleasesBothBindings(t *testing.T) {
	h := newHarness(t, "producer-a")
	f := newLineageFixture(t, h, consumerDefinition())
	if _, err := destroy(h); err != nil {
		t.Fatalf("destroy: %v", diagnostics.Of(err))
	}
	if !slices.Equal(h.capability.destroys, []string{"producer-a"}) {
		t.Fatalf("destroyed %v, want only the root's block", h.capability.destroys)
	}
	removal, _ := durableOperation(t, h)
	if removal.Verb != reconciliation.Destroy || removal.Source != f.head.ID || removal.State != reconciliation.OperationDone {
		t.Fatalf("the removal is %+v", removal)
	}
	released := slices.Clone(h.binder.released)
	slices.Sort(released)
	want := slices.Concat(f.root.Bindings, f.head.Bindings)
	slices.Sort(want)
	if !slices.Equal(released, want) {
		t.Fatalf("released %v, want %v", released, want)
	}
}

func TestADestroyOverAFailedAdditiveHeadThatStartedABlockTakesBackBothOperations(t *testing.T) {
	h := newHarness(t, "producer-a")
	f := newLineageFixture(t, h, consumerDefinition())
	f.failHead(t, "consumer")
	if _, err := destroy(h); err != nil {
		t.Fatalf("destroy: %v", diagnostics.Of(err))
	}
	if !slices.Equal(h.capability.destroys, []string{"consumer", "producer-a"}) {
		t.Fatalf("destroyed %v, want the started addition before the root's block", h.capability.destroys)
	}
	released := slices.Clone(h.binder.released)
	slices.Sort(released)
	want := slices.Concat(f.root.Bindings, f.head.Bindings)
	slices.Sort(want)
	if !slices.Equal(released, want) {
		t.Fatalf("released %v, want %v", released, want)
	}
}

func TestACrashBetweenTheReleasesOfALineageRemovalIsFinalizedByTheNextDestroy(t *testing.T) {
	h, f := completedLineage(t)
	releases := 0
	h.binder.kill = func(point string) error {
		if point != "release secret binding" {
			return nil
		}
		if releases++; releases == 2 {
			return errors.New("crash between the releases")
		}
		return nil
	}
	if _, err := destroy(h); err == nil {
		t.Fatal("the crash between the releases reported success")
	}
	removal, _ := durableOperation(t, h)
	if removal.Verb != reconciliation.Destroy || removal.State != reconciliation.OperationDone || bytes.Equal(h.workspace.evidence, killPristine(t)) {
		t.Fatalf("the removal is %s %s with evidence %q, want a done record beside unfinished evidence", removal.Verb, removal.State, h.workspace.evidence)
	}
	h.binder.kill = nil
	result, err := destroy(h)
	if err != nil || !result.Settled {
		t.Fatalf("the finalizing destroy = %+v (%v)", result, diagnostics.Of(err))
	}
	for _, binding := range slices.Concat(f.root.Bindings, f.head.Bindings) {
		if !slices.Contains(h.binder.released, binding) {
			t.Fatalf("released %v, want %s released by the finalization", h.binder.released, binding)
		}
	}
	if !bytes.Equal(h.workspace.evidence, killPristine(t)) {
		t.Fatalf("evidence = %q, want pristine", h.workspace.evidence)
	}
}

func TestALineageRemovalRebindFailedAndReplacedReleasesTheReboundBinding(t *testing.T) {
	alpha := definition("alpha")
	alpha.Request = json.RawMessage(`{"credentialsRef":"bmc","name":"alpha"}`)
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{alpha})
	h.capability.secrets = []string{"bmc"}
	h.capability.access = map[string][]string{"alpha": {"bmc"}}
	h.binder.material = map[string]secrets.Material{"bmc": password("first")}
	h.binder.versions = map[string]string{"bmc": "ver-1"}
	h.binder.versionMaterial = map[string]secrets.Material{"ver-1": password("first"), "ver-2": password("second")}
	consumer := definition("consumer")
	consumer.Dependencies = []string{"alpha"}
	f := newLineageFixture(t, h, consumer)
	f.finish(t, "consumer", `{}`)
	f.complete(t)
	h.binder.versions["bmc"] = "ver-2"
	seen := presentedTo(h)
	if err := destroyWith(h); err == nil {
		t.Fatal("a removal presenting the rotated-out password completed")
	}
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeFailed}}
	if err := destroyWith(h, RebindAuthorization+"bmc"); err == nil {
		t.Fatal("the seeded failure of the rebound removal did not fire")
	}
	rebound, _ := durableOperation(t, h)
	if rebound.Verb != reconciliation.Destroy || rebound.State != reconciliation.OperationFailed || len(rebound.Bindings) != 1 || slices.Contains(f.root.Bindings, rebound.Bindings[0]) {
		t.Fatalf("the rebound removal is %+v", rebound)
	}
	if err := destroyWith(h); err != nil {
		t.Fatalf("the replacement: %v", err)
	}
	for _, binding := range slices.Concat(f.root.Bindings, f.head.Bindings, rebound.Bindings) {
		if !slices.Contains(h.binder.released, binding) {
			t.Fatalf("released %v, want %s released by the replacement", h.binder.released, binding)
		}
	}
	if got := *seen; got[len(got)-1] != "second" {
		t.Fatalf("the replacement presented %q", got)
	}
}
