package lifecycle

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
)

// B1870: after a staged destroy rebound the kept Machine's BMC credential and
// a restore, the start-power reading of an additive apply over that lineage
// presents the Machine the rebound version, as every other reader does.
func TestAnAdditiveApplysStartPowerPresentsAStagedRebindOfAKeptMachine(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h, machine, _ := keptMachineHarness(t, `{"name":"base"}`, `{"credentialsRef":"bmc","name":"cluster"}`)
	h.capability.access = map[string][]string{"cluster": {"bmc"}}
	completeApply(t, h)
	h.binder.versions["bmc"] = "ver-2"
	if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true, Authorizations: []string{RebindAuthorization + "bmc"}}); err != nil {
		t.Fatalf("the rebound staged destroy: %+v", diagnostics.Of(err))
	}
	h.capability.definitions[1].Dependencies = []string{"base", "node-a"}
	restoreWith(t, h)
	restoring, _ := currentRecord(t, h)
	machine.read()
	err := h.workspace.ReadLifecycle(ctx, testContextName, func(view View) error {
		decided, err := h.service.decider(nil).decide(ctx, view, reconciliation.Apply, reconciliation.Selection{})
		if err != nil {
			return err
		}
		chain, err := appliedLineage(ctx, h.service.store(view), restoring)
		if err != nil {
			return err
		}
		merged, err := chain.plan()
		if err != nil {
			return err
		}
		decided.fresh, decided.noop, decided.predecessor, decided.lineagePlan = true, false, restoring.ID, merged
		prove, err := h.service.prepareStart(ctx, view, decided, decided.basis)
		if err != nil || prove == nil {
			t.Fatalf("the additive apply's start power: %v (%+v)", prove == nil, diagnostics.Of(err))
		}
		return prove(view)
	})
	if err != nil {
		t.Fatalf("the additive apply's start power: %+v", diagnostics.Of(err))
	}
	if got := machine.read(); !slices.Equal(got, []string{"second"}) {
		t.Fatalf("the additive apply read the kept Machine's power presenting %v, want the rebound version", got)
	}
}

// presentedCount records, per step, how many Secrets each removal was lent.
func presentedCount(h *harness) func() map[string][]int {
	var mutex sync.Mutex
	seen := map[string][]int{}
	h.capability.presenting = func(execution Execution) bool {
		mutex.Lock()
		seen[execution.Block.ID] = append(seen[execution.Block.ID], len(execution.Material))
		mutex.Unlock()
		return true
	}
	return func() map[string][]int {
		mutex.Lock()
		defer mutex.Unlock()
		return seen
	}
}

// B1870: a step whose additive apply froze no binding is presented nothing,
// never the root's material, while the root's step keeps the root's binding.
func TestAStepWhoseAdditiveApplyFrozeNoBindingIsPresentedNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newHarness(t, "producer-a")
	f := newLineageFixture(t, h, consumerDefinition())
	f.finish(t, "consumer", `{"head":true}`)
	f.head.Bindings = []string{}
	f.complete(t)
	presents, err := appliedPresenting(ctx, f.store, f.head)
	if err != nil || presents.of("consumer") != "" || presents.of("producer-a") != f.root.Bindings[0] {
		t.Fatalf("the lineage presents %+v (%+v)", presents, diagnostics.Of(err))
	}
	seen := presentedCount(h)
	if err := destroyWith(h); err != nil {
		t.Fatalf("the whole destroy: %+v", diagnostics.Of(err))
	}
	if got := seen(); !slices.Equal(got["consumer"], []int{0}) || len(got["producer-a"]) != 1 || got["producer-a"][0] == 0 {
		t.Fatalf("the whole destroy lent %v, want the added step nothing and the root's step its binding", got)
	}
}

// B1870: a whole destroy over an apply after a non-restoring automatic
// preparation presents the installation the binding that apply froze, which
// alone pins its Secret, not the preparation's.
func TestAWholeDestroyAfterAPreparationPresentsTheApplyAfterItsOwnBinding(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h, _ := physicalHarness(t)
	h.capability.definitions[4].Request = json.RawMessage(`{"name":"os-install-node","secretRef":"pull"}`)
	h.capability.secrets = append(h.capability.secrets, "pull")
	h.binder.material["pull"] = password("pull")
	h.binder.versions = map[string]string{"pull": "pull-1"}
	h.binder.versionMaterial = map[string]secrets.Material{"pull-1": password("pull")}
	if _, err := apply(h); err != nil {
		t.Fatalf("the apply: %+v", diagnostics.Of(err))
	}
	installed, installedPlan := currentRecord(t, h)
	prepared, err := lineageReader(h).ReadOperation(ctx, installed.Predecessor)
	if err != nil || installed.Predecessor == "" || ownBinding(installed) == "" || ownBinding(installed) == ownBinding(prepared) {
		t.Fatalf("the apply after the preparation is %+v over %v (%v)", installed, planIDs(installedPlan), err)
	}
	if _, pinned := h.binder.pins[ownBinding(prepared)]["pull"]; pinned {
		t.Fatalf("the preparation's binding pins %v", h.binder.pins[ownBinding(prepared)])
	}
	seen := presentedSecret(h, "pull")
	if err := destroyWith(h); err != nil {
		t.Fatalf("the whole destroy: %+v", diagnostics.Of(err))
	}
	if got := seen(); !slices.Equal(got["os-install-node"], []string{"pull"}) {
		t.Fatalf("the whole destroy presented %v, want the installation its own apply's Secret; pins %v installed %v", got, h.binder.pins, installed.Bindings)
	}
}

// B1870: a Secret an added step reads at removal, rotated after the additive
// apply, is rebound by a staged destroy that takes the step back, which
// replaces the additive apply's binding alone and presents the new version;
// the whole destroy after it takes the rest back over the root's binding.
func TestAStagedDestroyRebindsTheRotatedSecretOfAnAddedStep(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "producer-a")
	consumer := consumerDefinition()
	consumer.Stage = reconciliation.StageClusters
	consumer.Request = json.RawMessage(`{"credentialsRef":"bmc","name":"consumer"}`)
	h.capability.access = map[string][]string{"consumer": {"bmc"}}
	h.binder.material["bmc"] = password("first")
	h.binder.versions = map[string]string{"bmc": "ver-1"}
	h.binder.versionMaterial = map[string]secrets.Material{"ver-1": password("first"), "ver-2": password("second")}
	f := newLineageFixture(t, h, consumer)
	h.binder.pinCurrent(f.head.Bindings[0], []string{"bmc"})
	f.finish(t, "consumer", `{"head":true}`)
	f.complete(t)
	h.binder.versions["bmc"] = "ver-2"
	seen := presentedSecret(h, "bmc")
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true, Authorizations: []string{RebindAuthorization + "bmc"}}); err != nil {
		t.Fatalf("the rebound staged destroy: %+v", diagnostics.Of(err))
	}
	removal, _ := currentRecord(t, h)
	if issued := removal.Rebound[f.head.Bindings[0]]; issued == "" || len(removal.Rebound) != 1 || h.binder.pins[issued]["bmc"] != "ver-2" {
		t.Fatalf("the staged destroy recorded %v", removal.Rebound)
	}
	if got := seen(); !slices.Equal(got["consumer"], []string{"second"}) {
		t.Fatalf("the staged destroy presented %v, want the added step the rebound version", got)
	}
	if err := destroyWith(h); err != nil {
		t.Fatalf("the whole destroy: %+v", diagnostics.Of(err))
	}
	if got := seen(); len(got["consumer"]) != 0 || len(got["producer-a"]) != 1 {
		t.Fatalf("the whole destroy presented %v", got)
	}
}
