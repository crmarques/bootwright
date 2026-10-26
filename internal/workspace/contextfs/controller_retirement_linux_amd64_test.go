//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// retireToolAreas ends a controller stage of the named context that proved
// current.
func retireToolAreas(store *Store, name, current string) error {
	ctx := context.Background()
	return store.MutateLifecycle(ctx, name, func(tx lifecycle.Transaction) error {
		return tx.RetireToolAreas(ctx, current)
	})
}

// requireAreas requires the record to hold exactly the given areas, each
// with its directory under controller/tools, and no other directory there.
func requireAreas(t *testing.T, store *Store, want ...string) {
	t.Helper()
	held := map[string]string{}
	err := store.MutateLifecycle(context.Background(), "example", func(tx lifecycle.Transaction) error {
		for _, area := range tx.(*leasedLifecycle).lifecycleTransaction.stored.bundles {
			held[area.ID] = area.Mode
		}
		return nil
	})
	if err != nil {
		t.Fatalf("the record could not be read: %#v", diagnostics.Of(err))
	}
	entries, err := os.ReadDir(filepath.Join(store.options.Root, "controller", toolAreasName))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	if len(held) != len(want) || len(entries) != len(want) {
		t.Fatalf("the host holds %v with directories %v, want %d areas", held, entries, len(want))
	}
	for _, id := range want {
		if _, found := held[id]; !found {
			t.Fatalf("the host holds %v, want %s", held, id[:8])
		}
		if _, err := os.Stat(filepath.Join(store.options.Root, "controller", toolAreasName, id)); err != nil {
			t.Fatalf("area %s has no directory under controller/tools: %v", id[:8], err)
		}
	}
}

// bindOther binds a second context to the host and names area on its binding,
// as its own controller stage would.
func bindOther(t *testing.T, store *Store, area string) {
	t.Helper()
	_, sources := fixture(t)
	publish(t, store, "other", sources)
	ctx := context.Background()
	err := store.MutateLifecycle(ctx, "other", func(tx lifecycle.Transaction) error {
		if err := tx.Bind(ctx, "controller", syntheticHost(t)); err != nil {
			return err
		}
		return tx.RetireToolAreas(ctx, area)
	})
	if err != nil {
		t.Fatalf("binding the second context failed: %#v", diagnostics.Of(err))
	}
}

// Tool areas live in the controller's own tools directory, and a stage that
// moves its binding to a new area retires the one no binding names any more,
// directory and record entry both, while the area it names stays.
func TestAStageRetiresTheAreaNoBindingNames(t *testing.T) {
	store, record := lifecycleFixture(t)
	sealedBundleFixture(t, store, record)
	if err := publishClients(store, clientClosure, true); err != nil {
		t.Fatalf("client publication failed: %#v", diagnostics.Of(err))
	}
	if _, err := os.Stat(filepath.Join(store.options.Root, "controller", "bundles")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the controller still holds a bundles directory: %v", err)
	}
	requireAreas(t, store, sealedToolArea, clientClosure)
	if err := retireToolAreas(store, "example", clientClosure); err != nil {
		t.Fatalf("retirement failed: %#v", diagnostics.Of(err))
	}
	requireAreas(t, store, clientClosure)
	if err := retireToolAreas(store, "example", clientClosure); err != nil {
		t.Fatalf("a second retirement failed: %#v", diagnostics.Of(err))
	}
	requireAreas(t, store, clientClosure)
}

// An area another context's binding names stays through every retirement,
// and that context's deletion retires it.
func TestAnAreaAnotherBindingNamesStaysUntilItsContextIsDeleted(t *testing.T) {
	ctx := context.Background()
	store, record := lifecycleFixture(t)
	sealedBundleFixture(t, store, record)
	bindOther(t, store, sealedToolArea)
	if err := publishClients(store, clientClosure, true); err != nil {
		t.Fatalf("client publication failed: %#v", diagnostics.Of(err))
	}
	if err := retireToolAreas(store, "example", clientClosure); err != nil {
		t.Fatalf("retirement failed: %#v", diagnostics.Of(err))
	}
	requireAreas(t, store, sealedToolArea, clientClosure)
	err := store.Transact(ctx, false, nil, func(tx contexts.Transaction) error {
		other, found := contexts.Record{}, false
		for _, candidate := range tx.Registry().Contexts {
			if candidate.Name == "other" {
				other, found = candidate, true
			}
		}
		if !found {
			return errors.New("the second context is not registered")
		}
		if _, err := tx.MutationState(ctx, other.Name); err != nil {
			return err
		}
		return tx.Delete(ctx, other)
	})
	if err != nil {
		t.Fatalf("deleting the second context failed: %#v", diagnostics.Of(err))
	}
	requireAreas(t, store, clientClosure)
}

// A stage that selects no tool names none, so the area its binding named is
// retired; an area still being published is an unfinished stage's and stays.
func TestAStageWithoutToolsReleasesItsAreaAndKeepsUnfinishedOnes(t *testing.T) {
	store, record := lifecycleFixture(t)
	sealedBundleFixture(t, store, record)
	if err := publishClients(store, clientClosure, false); err != nil {
		t.Fatalf("client publication failed: %#v", diagnostics.Of(err))
	}
	if err := retireToolAreas(store, "example", ""); err != nil {
		t.Fatalf("retirement failed: %#v", diagnostics.Of(err))
	}
	requireAreas(t, store, clientClosure)
	if reservation := clientReservation(t, store, clientClosure); reservation.Mode != "attributed" {
		t.Fatalf("the unfinished area is %+v", reservation)
	}
}

// A stage that proves an area an earlier attempt published whole but never
// sealed seals it before naming it.
func TestRetirementSealsTheUnsealedAreaItNames(t *testing.T) {
	store, record := lifecycleFixture(t)
	sealedBundleFixture(t, store, record)
	if err := publishClients(store, clientClosure, false); err != nil {
		t.Fatalf("client publication failed: %#v", diagnostics.Of(err))
	}
	if err := retireToolAreas(store, "example", clientClosure); err != nil {
		t.Fatalf("retirement failed: %#v", diagnostics.Of(err))
	}
	if reservation := clientReservation(t, store, clientClosure); reservation.Mode != "sealed" {
		t.Fatalf("the named area is %+v", reservation)
	}
	requireAreas(t, store, clientClosure)
}

// An area this operation holds open is never retired under it.
func TestAnAreaTheOperationHoldsOpenStays(t *testing.T) {
	ctx := context.Background()
	store, record := lifecycleFixture(t)
	sealedBundleFixture(t, store, record)
	if err := publishClients(store, clientClosure, true); err != nil {
		t.Fatalf("client publication failed: %#v", diagnostics.Of(err))
	}
	err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
		area, err := tx.Controller().OpenBundle(ctx, sealedToolArea)
		if err != nil || area == nil {
			return fmt.Errorf("the sealed area did not open: %w", err)
		}
		if err := tx.RetireToolAreas(ctx, clientClosure); err != nil {
			return err
		}
		_, err = area.Entries(ctx)
		return err
	})
	if err != nil {
		t.Fatalf("the open area was disturbed: %#v", diagnostics.Of(err))
	}
	requireAreas(t, store, sealedToolArea, clientClosure)
}

// A retirement interrupted after its areas turned retiring leaves a store
// every read accepts, and the next retirement finishes it.
func TestAnInterruptedRetirementIsFinishedByTheNext(t *testing.T) {
	store, record := lifecycleFixture(t)
	sealedBundleFixture(t, store, record)
	if err := publishClients(store, clientClosure, true); err != nil {
		t.Fatalf("client publication failed: %#v", diagnostics.Of(err))
	}
	store.fail = func(point string) error {
		if point == string(checkpointBeforeToolAreaRemoval) {
			return errors.New("synthetic interruption")
		}
		return nil
	}
	if err := retireToolAreas(store, "example", clientClosure); err == nil {
		t.Fatal("the interrupted retirement succeeded")
	}
	store.fail = nil
	if err := store.ReadController(context.Background(), "", func(view prerequisites.StorageView) error {
		if len(view.Areas) != 1 || view.Areas[0].ID != clientClosure {
			return fmt.Errorf("the view holds %v, want only the named area", view.Areas)
		}
		area, err := view.OpenBundle(context.Background(), sealedToolArea)
		if err != nil || area != nil {
			return fmt.Errorf("a retiring area opened: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatalf("a read refused the interrupted retirement: %v", err)
	}
	if err := retireToolAreas(store, "example", clientClosure); err != nil {
		t.Fatalf("the retry failed: %#v", diagnostics.Of(err))
	}
	requireAreas(t, store, clientClosure)
}
