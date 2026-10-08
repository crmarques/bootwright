//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// armUnknownControllerPublication fails the store once right after the
// controller record's rename, which leaves that publication's outcome
// unknown, and reports whether the failure fired.
func armUnknownControllerPublication(store *Store) *bool {
	fired := false
	store.fail = func(point string) error {
		if point == string(checkpointAfterControllerRename) && !fired {
			fired = true
			return errors.New("synthetic uncertain sync")
		}
		return nil
	}
	return &fired
}

func requireUnknownControllerRemedy(t *testing.T, name string, err error, fired bool, remedy string) {
	t.Helper()
	if !fired {
		t.Fatalf("%s: the uncertain publication never fired", name)
	}
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "controller.unknown" || reported[0].Remediation != remedy {
		t.Fatalf("%s: refusal = %+v, want controller.unknown naming %q", name, reported, remedy)
	}
}

// An apply meets the controller record's uncertain publication while it
// reserves, binds, reserves a client area or retains a resolution, and names
// that apply of its own context, never setup's exact retry. Repeating the
// named apply then succeeds over the state the uncertain publication left.
func TestAnUnknownControllerPublicationNamesTheApplyThatMetIt(t *testing.T) {
	ctx := context.Background()
	host := syntheticControllerState(t, prerequisites.SetupContext{}).Host
	for _, test := range []struct {
		name string
		call func(lifecycle.Transaction) error
	}{
		{"reservation", func(tx lifecycle.Transaction) error { return tx.Reserve(ctx, checkpointClaim) }},
		{"binding", func(tx lifecycle.Transaction) error { return tx.Bind(ctx, "controller", host) }},
		{"client area", func(tx lifecycle.Transaction) error {
			_, err := tx.ClientArea(ctx, clientClosure)
			return err
		}},
		{"retained dependencies", func(tx lifecycle.Transaction) error {
			return tx.RetainDependencies(ctx, nil, []prerequisites.DependencySource{checkpointDependency}, nil)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := checkpointSealedFixture(t)
			fired := armUnknownControllerPublication(store)
			err := checkpointMutateLifecycle(ctx, store, test.call)
			store.fail = nil
			requireUnknownControllerRemedy(t, test.name, err, *fired, "once storage is healthy, repeat bootwright apply --context example")
			if err := checkpointMutateLifecycle(ctx, store, test.call); err != nil {
				t.Fatalf("the named retry failed: %+v", diagnostics.Of(err))
			}
		})
	}
}

// A destroy meets it while it releases its reservations and names that
// destroy; repeating it releases them.
func TestAnUnknownControllerPublicationNamesTheDestroyThatMetIt(t *testing.T) {
	ctx := context.Background()
	store := checkpointSealedFixture(t)
	if err := checkpointMutateLifecycle(ctx, store, func(tx lifecycle.Transaction) error { return tx.Reserve(ctx, checkpointClaim) }); err != nil {
		t.Fatalf("the reservation failed: %+v", diagnostics.Of(err))
	}
	release := func(tx lifecycle.Transaction) error { return tx.ReleaseReservations(ctx) }
	fired := armUnknownControllerPublication(store)
	err := checkpointMutateLifecycle(ctx, store, release)
	store.fail = nil
	requireUnknownControllerRemedy(t, "release", err, *fired, "once storage is healthy, repeat bootwright destroy --context example")
	if err := checkpointMutateLifecycle(ctx, store, release); err != nil {
		t.Fatalf("the named retry failed: %+v", diagnostics.Of(err))
	}
	if err := store.ReadController(ctx, "", func(view prerequisites.StorageView) error {
		for _, reservation := range view.State.Reservations {
			if reservation.Context == checkpointContext {
				t.Fatalf("the destroyed context still reserves %v", reservation.Keys)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// A context deletion meets it while it drops the context's claims, after the
// registry recorded the deletion, and names the purge that finishes it.
func TestAnUnknownControllerPublicationNamesTheDeletionThatMetIt(t *testing.T) {
	ctx := context.Background()
	store, record := lifecycleFixture(t)
	sealedBundleFixture(t, store, record)
	if err := store.MutateLifecycle(ctx, record.Name, func(tx lifecycle.Transaction) error {
		if err := tx.Reserve(ctx, checkpointClaim); err != nil {
			return err
		}
		return tx.PublishEvidence(ctx, []byte(`{"version":1,"operation":"unknown","ownership":"retained"}`))
	}); err != nil {
		t.Fatalf("the reservation failed: %+v", diagnostics.Of(err))
	}
	fired := armUnknownControllerPublication(store)
	err := store.Transact(ctx, false, nil, func(tx contexts.Transaction) error {
		if _, err := tx.MutationState(ctx, record.Name); err != nil {
			return err
		}
		return tx.Delete(ctx, record)
	})
	store.fail = nil
	requireUnknownControllerRemedy(t, "deletion", err, *fired, "once storage is healthy, finish the deletion with bootwright context delete --name example --purge")
	if err := store.Transact(ctx, false, nil, func(tx contexts.Transaction) error {
		for _, current := range tx.Registry().Contexts {
			if current.Name == record.Name {
				if current.Mode != contexts.Deleting {
					t.Fatalf("the interrupted deletion left mode %q", current.Mode)
				}
				return tx.Delete(ctx, current)
			}
		}
		t.Fatal("the interrupted deletion lost its record")
		return nil
	}); err != nil {
		t.Fatalf("the named retry failed: %+v", diagnostics.Of(err))
	}
	if err := store.Transact(ctx, false, nil, func(tx contexts.Transaction) error {
		for _, current := range tx.Registry().Contexts {
			if current.Name == record.Name {
				t.Fatalf("the deleted context is still registered as %q", current.Mode)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// Setup's own uncertain publication keeps its exact retry.
func TestASetupsUnknownControllerPublicationKeepsItsExactRetry(t *testing.T) {
	ctx := context.Background()
	store, record := lifecycleFixture(t)
	scope := prerequisites.SetupContext{Name: record.Name, Revision: record.Revision, Machine: "controller"}
	fired := armUnknownControllerPublication(store)
	var published error
	err := store.MutateController(ctx, scope, true, func(tx prerequisites.StorageTransaction) error {
		_, published = tx.Publish(ctx, syntheticControllerState(t, scope))
		return nil
	})
	store.fail = nil
	if err != nil {
		t.Fatal(err)
	}
	requireUnknownControllerRemedy(t, "setup", published, *fired, setupRetry)
}
