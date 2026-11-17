package lifecycle

import (
	"context"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

type countingCompiler struct {
	inner Compiler
	calls *int
}

func (c countingCompiler) Compile(ctx context.Context, sources desiredstate.Sources) (*compilation.State, *compilation.Report, error) {
	*c.calls++
	return c.inner.Compile(ctx, sources)
}

func countedAddition(t *testing.T) (*additiveFixture, *int) {
	t.Helper()
	f := newAdditiveFixture(t)
	f.root(t)
	f.publish("rev-2", addonBinding("cluster", "nmstate"))
	calls := 0
	f.service.compiler = countingCompiler{inner: f.compiler, calls: &calls}
	return f, &calls
}

func TestADecisionCompilesEachRevisionOnceAndLendsProducedMaterialOnce(t *testing.T) {
	for name, verb := range map[string]reconciliation.Verb{"a plan's decision": "", "an apply's decision": reconciliation.Apply} {
		t.Run(name, func(t *testing.T) {
			f, calls := countedAddition(t)
			lent := f.binder.producedReads
			for round := 1; round <= 2; round++ {
				decided, _, err := f.service.decideShared(context.Background(), testContextName, verb, reconciliation.Selection{})
				if err != nil || decided.predecessor == "" {
					t.Fatalf("the decision = %+v (%v), want the additive apply", decided, err)
				}
				if *calls != 2*round || f.binder.producedReads-lent != round {
					t.Fatalf("after %d decisions the compiler ran %d times and custody lent %d times, want each revision compiled and the produced input lent once per decision", round, *calls, f.binder.producedReads-lent)
				}
			}
		})
	}
}

func TestAPlanOverAnAdditionCompilesEachRevisionOnce(t *testing.T) {
	f, calls := countedAddition(t)
	inspected := 0
	f.service.options.PlanInspection = func(context.Context, string, api.Catalog, reconciliation.Plan, []reconciliation.Block) ([]PlanCheck, error) {
		inspected++
		return nil, nil
	}
	lent := f.binder.producedReads
	if _, err := f.service.Plan(context.Background(), PlanRequest{ContextName: testContextName}); err != nil {
		t.Fatal(err)
	}
	if inspected != 1 || *calls != 2 || f.binder.producedReads-lent != 1 {
		t.Fatalf("the plan inspected %d times, compiled %d times and lent %d times, want one inspection over two compilations and one loan", inspected, *calls, f.binder.producedReads-lent)
	}
}

func TestAFailedCompilationIsNeverRememberedAsASuccess(t *testing.T) {
	f, _ := countedAddition(t)
	current := digestOf(f.workspace.inputs)
	f.compiler.errs[current] = diagnostics.NewFailure("input.state", "the input does not compile", "")
	if decided, _, err := f.service.decideShared(context.Background(), testContextName, reconciliation.Apply, reconciliation.Selection{}); err == nil || decided.predecessor != "" {
		t.Fatalf("a decision over an input that fails to compile = %+v (%v)", decided, err)
	}
	delete(f.compiler.errs, current)
	if decided, _, err := f.service.decideShared(context.Background(), testContextName, reconciliation.Apply, reconciliation.Selection{}); err != nil || decided.predecessor == "" {
		t.Fatalf("the next decision = %+v (%v), want the additive apply", decided, err)
	}
}
