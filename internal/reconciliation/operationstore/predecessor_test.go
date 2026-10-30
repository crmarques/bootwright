package operationstore

import (
	"context"
	"encoding/json"
	"path"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// additivePlan is an apply that adds one block to the lineage goldenPlan makes,
// waiting for that lineage's server block as a prerequisite.
func additivePlan(t *testing.T) reconciliation.Plan {
	t.Helper()
	fresh, err := reconciliation.NewPlan(reconciliation.Apply, []reconciliation.BlockDefinition{
		{
			ID: "gateway-lab", Description: "serve a gateway for lab", Stage: reconciliation.StageInfrastructureServices,
			Dependencies: []string{"server-lab"}, Impacts: []string{"create-container-unit gateway"},
			Kind: "ArtifactServer", Object: "gateway", Implementation: "artifact-server-nginx-v1",
			ContentDigest: strings.Repeat("a", 64), Request: json.RawMessage(`{"name":"gateway"}`),
		},
		goldenPlanBlock(t, "server-lab"), goldenPlanBlock(t, "install-node-01"),
	})
	if err != nil {
		t.Fatal(err)
	}
	added, err := reconciliation.Additive(fresh, goldenPlan(t))
	if err != nil {
		t.Fatalf("adding: %v", diagnostics.Of(err))
	}
	return added
}

func goldenPlanBlock(t *testing.T, id string) reconciliation.BlockDefinition {
	t.Helper()
	block, found := goldenPlan(t).Block(id)
	if !found {
		t.Fatalf("the golden plan holds no %s", id)
	}
	return block.BlockDefinition
}

func additiveOperation(t *testing.T, plan reconciliation.Plan, predecessor string) Operation {
	t.Helper()
	operation := testOperation(t, plan)
	operation.ID, operation.Predecessor = "op-"+strings.Repeat("ef", 16), predecessor
	return operation
}

// An additive apply names the apply it adds to and freezes its edges into that
// apply as prerequisites. The pair reads back exactly, and its goldens pin the
// encoding of both new members.
func TestAnAdditiveApplyRecordMatchesItsGolden(t *testing.T) {
	ctx := context.Background()
	store, area := newStore(t)
	root := goldenPlan(t)
	applied := testOperation(t, root)
	if _, err := store.Index(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Register(ctx, applied, root); err != nil {
		t.Fatal(err)
	}
	added := additivePlan(t)
	operation := additiveOperation(t, added, applied.ID)
	if err := store.Register(ctx, operation, added); err != nil {
		t.Fatalf("registering the addition: %v", diagnostics.Of(err))
	}
	matchesGolden(t, "operation-additive", area.files[path.Join(operation.ID, "operation.json")], true)
	matchesGolden(t, "plan-additive", area.files[path.Join(operation.ID, "plan.json")], true)
	fresh := New(area, fixedClock())
	if stored, err := fresh.ReadOperation(ctx, operation.ID); err != nil || !reflect.DeepEqual(stored, operation) {
		t.Fatalf("the addition read back as %+v (%v)", stored, err)
	}
	plan, err := fresh.ReadPlan(ctx, operation.ID)
	if err != nil || !reflect.DeepEqual(plan, added) {
		t.Fatalf("the addition's plan read back as %+v (%v)", plan, err)
	}
	block, _ := plan.Block("gateway-lab")
	if !slices.Equal(block.Prerequisites, []string{"server-lab"}) || len(block.Dependencies) != 0 {
		t.Fatalf("gateway-lab carries prerequisites %v and dependencies %v", block.Prerequisites, block.Dependencies)
	}
	whole, err := reconciliation.Lineage(root, plan)
	if err != nil {
		t.Fatalf("joining: %v", diagnostics.Of(err))
	}
	if got, _ := whole.Block("gateway-lab"); !slices.Equal(got.Dependencies, []string{"server-lab"}) {
		t.Fatalf("the lineage folds gateway-lab to %v", got.Dependencies)
	}
}

// predecessor is an optional member: a record that names none encodes exactly
// as a record written before the member existed, so no version moves.
func TestAnOperationWithoutAPredecessorEncodesNoMember(t *testing.T) {
	plan := testPlan(t, "alpha")
	encoded, err := encode(testOperation(t, plan), MaxOperationBytes)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "predecessor") {
		t.Fatalf("operation.json = %s", encoded)
	}
	withPredecessor, err := encode(additiveOperation(t, plan, "op-"+strings.Repeat("11", 16)), MaxOperationBytes)
	if err != nil || !strings.Contains(string(withPredecessor), `"predecessor":"op-`+strings.Repeat("11", 16)+`","bindings":`) {
		t.Fatalf("operation.json = %s (%v)", withPredecessor, err)
	}
	var read Operation
	if err := decode(withPredecessor, MaxOperationBytes, &read); err != nil || validateOperation(read) != nil || read.Predecessor == "" {
		t.Fatalf("the record decoded as %+v (%v)", read, err)
	}
}

func TestAPredecessorIsAnotherApplyOperationsIdentity(t *testing.T) {
	ctx := context.Background()
	plan := testPlan(t, "alpha")
	inverse, err := plan.Inverse()
	if err != nil {
		t.Fatal(err)
	}
	other := "op-" + strings.Repeat("11", 16)
	for name, arrange := range map[string]func() (Operation, reconciliation.Plan){
		"itself": func() (Operation, reconciliation.Plan) {
			operation := testOperation(t, plan)
			operation.Predecessor = operation.ID
			return operation, plan
		},
		"a removal": func() (Operation, reconciliation.Plan) {
			operation := testOperation(t, inverse)
			operation.Source, operation.Predecessor = other, "op-"+strings.Repeat("22", 16)
			return operation, inverse
		},
		"a malformed identity": func() (Operation, reconciliation.Plan) {
			operation := testOperation(t, plan)
			operation.Predecessor = "operation-1"
			return operation, plan
		},
		"a run identity": func() (Operation, reconciliation.Plan) {
			operation := testOperation(t, plan)
			operation.Predecessor = "run-" + strings.Repeat("11", 16)
			return operation, plan
		},
	} {
		t.Run(name, func(t *testing.T) {
			store, _ := newStore(t)
			if _, err := store.Index(ctx); err != nil {
				t.Fatal(err)
			}
			operation, frozen := arrange()
			if err := validateOperation(operation); !slices.Equal(diagnostics.Of(err), diagnostics.Of(recordError("lifecycle operation predecessor is invalid"))) {
				t.Fatalf("an operation with %s validated: %v", name, diagnostics.Of(err))
			}
			err := store.Register(ctx, operation, frozen)
			if !slices.Equal(diagnostics.Of(err), diagnostics.Of(recordError("lifecycle operation predecessor is invalid"))) {
				t.Fatalf("an operation with %s registered: %v", name, diagnostics.Of(err))
			}
		})
	}
}

func TestPrerequisitesRefuseBesideARecordThatNamesNoPredecessor(t *testing.T) {
	ctx := context.Background()
	added := additivePlan(t)
	want := recordError("the frozen plan holds prerequisites beside an operation that names no predecessor")
	store, _ := newStore(t)
	if _, err := store.Index(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Register(ctx, additiveOperation(t, added, ""), added); !slices.Equal(diagnostics.Of(err), diagnostics.Of(want)) {
		t.Fatalf("registering prerequisites without a predecessor: %v", diagnostics.Of(err))
	}

	registered, area := newStore(t)
	if _, err := registered.Index(ctx); err != nil {
		t.Fatal(err)
	}
	operation := additiveOperation(t, added, "op-"+strings.Repeat("ab", 16))
	if err := registered.Register(ctx, operation, added); err != nil {
		t.Fatal(err)
	}
	record := string(area.files[path.Join(operation.ID, "operation.json")])
	area.files[path.Join(operation.ID, "operation.json")] = []byte(strings.Replace(record, `"predecessor":"op-`+strings.Repeat("ab", 16)+`",`, "", 1))
	if _, err := New(area, fixedClock()).ReadPlan(ctx, operation.ID); !slices.Equal(diagnostics.Of(err), diagnostics.Of(want)) {
		t.Fatalf("reading prerequisites beside a record without a predecessor: %v", diagnostics.Of(err))
	}
}

// An additive apply's attempts keep free what the one removal of its whole
// lineage needs, so an attempt of a plan that cannot be inverted alone is
// admitted, and the reserve is that of the lineage's inverse.
func TestAnAdditiveApplyKeepsTheReserveOfItsWholeLineagesRemoval(t *testing.T) {
	ctx := context.Background()
	store, _ := newStore(t)
	root := goldenPlan(t)
	applied := testOperation(t, root)
	if _, err := store.Index(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Register(ctx, applied, root); err != nil {
		t.Fatal(err)
	}
	added := additivePlan(t)
	operation := additiveOperation(t, added, applied.ID)
	if err := store.Register(ctx, operation, added); err != nil {
		t.Fatal(err)
	}
	release, err := store.AdmitAttempt(ctx, operation, added, "gateway-lab", false)
	if err != nil {
		t.Fatalf("an attempt of the additive apply refused: %+v", diagnostics.Of(err))
	}
	release()
	whole, err := reconciliation.Lineage(root, added)
	if err != nil {
		t.Fatal(err)
	}
	removal, err := whole.Inverse()
	if err != nil {
		t.Fatal(err)
	}
	entries, kept, err := store.removalReserve(ctx, operation, added)
	if err != nil || entries != AdmissionEntries(removal) {
		t.Fatalf("the additive apply keeps %d entries (%v), want its lineage removal's %d", entries, err, AdmissionEntries(removal))
	}
	rootOnly, err := root.Inverse()
	if err != nil {
		t.Fatal(err)
	}
	if _, alone, err := store.removalReserve(ctx, applied, root); err != nil || alone >= kept || len(removal.Blocks) <= len(rootOnly.Blocks) {
		t.Fatalf("the lineage keeps %d bytes beside the root's %d (%v)", kept, alone, err)
	}
}

func TestTheReserveOfAnAdditiveApplyRefusesALineageItCannotRead(t *testing.T) {
	ctx := context.Background()
	store, _ := newStore(t)
	added := additivePlan(t)
	operation := additiveOperation(t, added, "op-"+strings.Repeat("77", 16))
	if _, _, err := store.removalReserve(ctx, operation, added); err == nil {
		t.Fatal("a reserve was computed over a predecessor that has no record")
	}
}
