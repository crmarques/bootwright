package lifecycle

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

type preparationTestResolver struct {
	CapabilityResolver
	capability *preparationTestCapability
}

func (r preparationTestResolver) Resolve(kind, implementation string) (Capability, bool) {
	if _, ok := r.CapabilityResolver.Resolve(kind, implementation); !ok {
		return nil, false
	}
	return r.capability, true
}

type preparationTestCapability struct{ *testCapability }

func (c preparationTestCapability) Plan(ctx context.Context, input PlanInput) (CapabilityPlan, error) {
	plan, err := c.testCapability.Plan(ctx, input)
	if input.Preparation != nil {
		plan.Definitions = slices.DeleteFunc(slices.Clone(plan.Definitions), func(d reconciliation.BlockDefinition) bool { return d.ID != "inventory" })
		plan.Secrets = nil
		for _, definition := range plan.Definitions {
			refs, namedErr := c.SecretReferences(reconciliation.Block{BlockDefinition: definition})
			if namedErr != nil {
				return CapabilityPlan{}, namedErr
			}
			plan.Secrets = append(plan.Secrets, refs...)
		}
	}
	return plan, err
}

func prepareHarness(t *testing.T, blocks ...string) *harness {
	h := newHarness(t, blocks...)
	h.service.capabilities = preparationTestResolver{h.service.capabilities, &preparationTestCapability{h.capability}}
	h.service.options.Preparations = []PreparationDefinition{{
		Kind: "example-inventory", Command: "machine discover", Bindings: h.service.capabilities.Bindings(),
		Bearing:  func(api.Object) (Bearing, bool) { return Bearing{}, false },
		Validate: func(context.Context, PlanInput, reconciliation.PreparationScope) error { return nil },
		Advance:  func(context.Context, PreparationAdvance) error { return nil },
	}}
	return h
}

func requestPreparation() PrepareRequest {
	return PrepareRequest{ContextName: testContextName, Kind: "example-inventory", Targets: []string{"example-node"}, SkipConfirmation: true}
}

func TestPreparationCompletesBeforeOrdinaryApplyCanAppendInstallation(t *testing.T) {
	ctx := context.Background()
	h := prepareHarness(t, "inventory")
	first, err := h.service.Prepare(ctx, requestPreparation())
	if err != nil {
		t.Fatalf("%v: %+v", err, diagnostics.Of(err))
	}
	if first == nil {
		t.Fatal("preparation returned no receipt")
	}
	h.capability.definitions = append(h.capability.definitions, definition("installation"))
	result, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
	if err != nil {
		t.Fatalf("%v: %+v", err, diagnostics.Of(err))
	}
	if result == nil || !slices.Equal(h.capability.applies, []string{"inventory", "installation"}) {
		t.Fatalf("effects %#v", h.capability.applies)
	}
	if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatalf("%v: %+v", err, diagnostics.Of(err))
	}
	if !slices.Contains(h.capability.destroys, "inventory") || !slices.Contains(h.capability.destroys, "installation") {
		t.Fatalf("lineage lost ownership: %#v", h.capability.destroys)
	}
}

func TestIncompletePreparationCannotBecomeOrdinaryApply(t *testing.T) {
	ctx := context.Background()
	h := prepareHarness(t, "inventory")
	h.capability.applyErr = errors.New("fixture interruption")
	if _, err := h.service.Prepare(ctx, requestPreparation()); err == nil {
		t.Fatal("interruption succeeded")
	}
	calls := len(h.capability.applies)
	h.capability.applyErr = nil
	if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
		t.Fatal("ordinary apply resumed preparation")
	}
	if len(h.capability.applies) != calls {
		t.Fatal("wrong command reached an adapter")
	}
	changed := requestPreparation()
	changed.Targets = []string{"another-node"}
	if _, err := h.service.Prepare(ctx, changed); err == nil {
		t.Fatal("continuation widened frozen preparation")
	}
	if _, err := h.service.Prepare(ctx, requestPreparation()); err != nil {
		t.Fatalf("%v: %+v", err, diagnostics.Of(err))
	}
}

func TestPreparationRetainsUnselectedExistingOwnership(t *testing.T) {
	ctx := context.Background()
	h := prepareHarness(t, "existing")
	if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatalf("%v: %+v", err, diagnostics.Of(err))
	}
	h.capability.definitions = append(h.capability.definitions, definition("inventory"))
	prepared, err := h.service.Prepare(ctx, requestPreparation())
	if err != nil {
		t.Fatalf("%v: %+v", err, diagnostics.Of(err))
	}
	if prepared == nil {
		t.Fatal("preparation returned no receipt")
	}
	if !slices.Equal(h.capability.applies, []string{"existing", "inventory"}) {
		t.Fatalf("old work repeated: %#v", h.capability.applies)
	}
	if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatalf("%v: %+v", err, diagnostics.Of(err))
	}
	if !slices.Contains(h.capability.destroys, "existing") {
		t.Fatal("preparation dropped earlier ownership")
	}
}

func TestPreparationRefusesDestructiveWorkBeforeEffects(t *testing.T) {
	h := prepareHarness(t, "inventory")
	h.capability.definitions[0].Consumes = []string{"data-loss"}
	if _, err := h.service.Prepare(context.Background(), requestPreparation()); err == nil {
		t.Fatal("preparation authorized disk erasure")
	}
	if len(h.capability.applies) != 0 {
		t.Fatal("refused preparation reached an adapter")
	}
}

func TestPreparationAdditionAcquiresAndRetainsReservations(t *testing.T) {
	ctx := context.Background()
	h := prepareHarness(t, "existing")
	h.capability.reservations = reservationOf("alpha")
	if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	old := slices.Clone(h.workspace.reservations)
	h.capability.definitions = append(h.capability.definitions, definition("inventory"))
	h.capability.reservations = reservationOf("beta")
	if _, err := h.service.Prepare(ctx, requestPreparation()); err != nil {
		t.Fatal(err)
	}
	for _, claim := range append(old, reservationOf("beta")...) {
		if !slices.ContainsFunc(h.workspace.reservations, func(held prerequisites.HostReservation) bool { return reflect.DeepEqual(held, claim) }) {
			t.Fatalf("lost claim %+v from %+v", claim, h.workspace.reservations)
		}
	}
}

func TestPreparationAdditionRefusesIncompleteOwner(t *testing.T) {
	ctx := context.Background()
	h := prepareHarness(t, "inventory")
	if _, err := h.service.Prepare(ctx, requestPreparation()); err != nil {
		t.Fatal(err)
	}
	h.capability.definitions = append(h.capability.definitions, definition("installation"))
	h.service.options.Preparations[0].Advance = nil
	if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
		t.Fatal("incomplete owner admitted")
	}
	if len(h.capability.applies) != 1 {
		t.Fatal("incomplete owner reached adapter")
	}
}

type conflictingPreparationWorkspace struct{ Workspace }
type conflictingPreparationTransaction struct{ Transaction }

func (w conflictingPreparationWorkspace) MutateLifecycle(ctx context.Context, name string, callback func(Transaction) error) error {
	return w.Workspace.MutateLifecycle(ctx, name, func(tx Transaction) error { return callback(conflictingPreparationTransaction{tx}) })
}

func (tx conflictingPreparationTransaction) Reserve(ctx context.Context, claims []prerequisites.HostReservation) error {
	for _, claim := range claims {
		if claim.Service == "beta" {
			return errors.New("fixture foreign context owns beta")
		}
	}
	return tx.Transaction.Reserve(ctx, claims)
}

func TestPreparationAdditionRefusesForeignReservationBeforeEffects(t *testing.T) {
	ctx := context.Background()
	h := prepareHarness(t, "existing")
	h.capability.reservations = reservationOf("alpha")
	if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	old := slices.Clone(h.workspace.reservations)
	h.service.workspace = conflictingPreparationWorkspace{h.service.workspace}
	h.capability.definitions = append(h.capability.definitions, definition("inventory"))
	h.capability.reservations = reservationOf("beta")
	if _, err := h.service.Prepare(ctx, requestPreparation()); err == nil {
		t.Fatal("competing reservation admitted preparation")
	}
	if !slices.Equal(h.capability.applies, []string{"existing"}) || !reflect.DeepEqual(h.workspace.reservations, old) {
		t.Fatal("refused reservation changed ownership or reached an adapter")
	}
}
