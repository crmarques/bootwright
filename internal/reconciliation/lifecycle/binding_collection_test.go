package lifecycle

import (
	"bytes"
	"context"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

// loweredThen runs then once, right after the first transaction that lowers the
// context's evidence to pristine returns: where the invocation that lowered it
// has not yet released the Secret bindings it collects.
type loweredThen struct {
	*testWorkspace
	pristine []byte
	then     func()
}

func (w *loweredThen) MutateLifecycle(ctx context.Context, name string, callback func(Transaction) error) error {
	raised := !bytes.Equal(w.evidence, w.pristine)
	err := w.testWorkspace.MutateLifecycle(ctx, name, callback)
	if then := w.then; then != nil && raised && bytes.Equal(w.evidence, w.pristine) {
		w.then = nil
		then()
	}
	return err
}

// requireTheLaterBindingKept runs repair over what run left, with a complete
// fresh apply registering, binding and finishing right after repair's pristine
// publication lands and before it collects. The collection releases only the
// bindings repair read before its first transaction, so the binding that apply
// registered stays held, and a later destroy removes the apply and releases it.
func requireTheLaterBindingKept(ctx context.Context, t *testing.T, run killedRun, repair func(Service) (*OperationResult, error)) {
	t.Helper()
	service, _ := run.repairing()
	w, binder := run.snapshot.workspace, run.snapshot.binder
	registered := ""
	hook := &loweredThen{testWorkspace: w, pristine: killPristine(t)}
	hook.then = func() {
		if err := authorized(ctx, service, reconciliation.Apply); err != nil {
			t.Fatalf("the apply after the pristine publication failed: %v", err)
		}
		record, _ := durableOperation(t, &harness{workspace: w})
		if record.Verb != reconciliation.Apply || record.State != reconciliation.OperationDone || len(record.Bindings) != 1 {
			t.Fatalf("the apply after the pristine publication left %s %s bound to %v", record.Verb, record.State, record.Bindings)
		}
		registered = record.Bindings[0]
	}
	service.workspace = hook
	result, err := repair(service)
	if err != nil || !result.Settled || registered == "" {
		t.Fatalf("the repair = %+v (%v), with an apply registering %q after its pristine publication", result, err, registered)
	}
	applied := evidenceBytes(t, reconciliation.Apply, reconciliation.OperationDone)
	if killRest(w) != "applied" || !bytes.Equal(w.evidence, applied) || slices.Contains(binder.released, registered) {
		t.Fatalf("the repair left %s under evidence %q and released %v, including the apply's %s", killRest(w), w.evidence, binder.released, registered)
	}
	if err := authorized(ctx, service, reconciliation.Destroy); err != nil {
		t.Fatalf("the destroy of the apply failed: %v", err)
	}
	if killRest(w) != "removed" || !bytes.Equal(w.evidence, killPristine(t)) || !slices.Contains(binder.released, registered) || killUnreleased(binder) != 0 {
		t.Fatalf("the destroy left %s under evidence %q and released %v, not the apply's %s", killRest(w), w.evidence, binder.released, registered)
	}
}

// A destroy's release of what an apply killed as it wrote its index left reads
// the context's bindings before its transaction, so an apply that registers,
// binds and completes after that release publishes pristine evidence and before
// it collects keeps its binding.
func TestAnUnclaimedReleaseCollectsOnlyTheBindingsItReadFirst(t *testing.T) {
	ctx := context.Background()
	run := killedAt(ctx, t, nil, reconciliation.Apply, "replace index.json#1")
	w := run.snapshot.workspace
	running := evidenceBytes(t, reconciliation.Apply, reconciliation.OperationRunning)
	if current, directories := operations(t, w); current != "" || len(directories) != 1 || !bytes.Equal(w.evidence, running) ||
		len(w.reservations) == 0 || killUnreleased(run.snapshot.binder) != 1 {
		t.Fatalf("the kill left %q %v under evidence %q holding %v and %d bindings",
			current, directories, w.evidence, w.reservations, killUnreleased(run.snapshot.binder))
	}
	requireTheLaterBindingKept(ctx, t, run, func(service Service) (*OperationResult, error) {
		return service.Destroy(ctx, DestroyRequest{ContextName: testContextName})
	})
}

// A removal's finalization reads the context's bindings before its first
// transaction, so an apply that registers, binds and completes after the
// finalization publishes pristine evidence and before it collects keeps its
// binding. The apply that finalized the removal then settles over it.
func TestAFinalizationCollectsOnlyTheBindingsItReadFirst(t *testing.T) {
	ctx := context.Background()
	point := "publish evidence#2"
	run := killedAt(ctx, t, []reconciliation.Verb{reconciliation.Apply}, reconciliation.Destroy, point)
	if run.writes[len(run.writes)-1] != point {
		t.Fatalf("pristine evidence is not a removal's last write: %v", run.writes)
	}
	if w := run.snapshot.workspace; killRest(w) != "removed" || bytes.Equal(w.evidence, killPristine(t)) {
		t.Fatalf("the kill left %s under evidence %q", killRest(w), w.evidence)
	}
	requireTheLaterBindingKept(ctx, t, run, func(service Service) (*OperationResult, error) {
		return service.Apply(ctx, ApplyRequest{ContextName: testContextName})
	})
}
