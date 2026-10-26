//go:build linux && amd64

package contextfs

import (
	"context"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// A command presents its plan from an inspection: it holds the shared root
// lock and shared leases, so another reader runs beside it while a mutator is
// refused at once, and every write of its transaction refuses. Once it ends,
// the exclusive transaction the confirmed command takes runs.
func TestAnInspectionHoldsTheSharedLockAndWritesNothing(t *testing.T) {
	ctx := context.Background()
	store, sources := fixture(t)
	record := publish(t, store, "alpha", sources)
	for _, scope := range []string{"", "alpha"} {
		err := store.Inspect(ctx, scope, func(tx contexts.Transaction) error {
			if _, err := tx.MutationState(ctx, "alpha"); err != nil {
				t.Fatalf("the inspection could not read the evidence: %v", err)
			}
			if _, err := store.View(ctx); err != nil {
				t.Fatalf("a reader beside the inspection refused: %v", err)
			}
			mutated := store.Transact(ctx, false, nil, func(contexts.Transaction) error {
				t.Fatal("a mutator ran beside the inspection")
				return nil
			})
			if reported := diagnostics.Of(mutated); len(reported) != 1 || reported[0].Code != "lifecycle.lease" {
				t.Fatalf("a mutator beside the inspection reported %#v", reported)
			}
			if err := tx.Delete(ctx, record); err == nil {
				t.Fatal("the inspection deleted its context")
			}
			if scope == "" {
				if err := tx.Commit(ctx, tx.Registry()); err == nil {
					t.Fatal("the inspection committed the registry")
				}
				if _, err := tx.Publish(ctx, "alpha", record.EnvironmentDirectory, sources); err == nil {
					t.Fatal("the inspection published input")
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("inspection %q: %v", scope, err)
		}
	}
	if err := store.Transact(ctx, false, nil, func(tx contexts.Transaction) error {
		_, err := tx.MutationState(ctx, "alpha")
		return err
	}); err != nil {
		t.Fatalf("the transaction after the inspection refused: %v", err)
	}
	if view, err := store.View(ctx); err != nil || len(view.Contexts) != 1 || view.Contexts[0] != record {
		t.Fatalf("the inspections changed the registry: %#v (%v)", view, err)
	}
}
