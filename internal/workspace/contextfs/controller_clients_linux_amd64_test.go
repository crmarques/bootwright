//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const clientClosure = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"

// publishClients is one controller stage: it opens the shared closure area,
// publishes a member and seals it.
func publishClients(store *Store, id string, seal bool) error {
	ctx := context.Background()
	return store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
		area, err := tx.ClientArea(ctx, id)
		if err != nil {
			return err
		}
		if err := area.EnsureDirectory(ctx, "tools/helm"); err != nil {
			return err
		}
		if err := area.Write(ctx, "tools/helm/helm", []byte("#!/bin/sh\n"), true); err != nil {
			return err
		}
		if !seal {
			return nil
		}
		return tx.SealClientArea(ctx, id)
	})
}

func clientReservation(t *testing.T, store *Store, id string) controllerBundleReservation {
	t.Helper()
	ctx := context.Background()
	var found controllerBundleReservation
	err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
		transaction := tx.(*leasedLifecycle).lifecycleTransaction
		index := slices.IndexFunc(transaction.stored.bundles, func(item controllerBundleReservation) bool { return item.ID == id })
		if index >= 0 {
			found = transaction.stored.bundles[index]
		}
		return nil
	})
	if err != nil {
		t.Fatalf("reservation read failed: %#v", diagnostics.Of(err))
	}
	return found
}

// The shared area is content-addressed and durable: its reservation names the
// closure, its attribution names the directory, and sealing makes it immutable.
func TestClientAreaPublishesUnderItsOwnReservation(t *testing.T) {
	ctx := context.Background()
	store, record := lifecycleFixture(t)
	sealedBundleFixture(t, store, record)
	if err := publishClients(store, clientClosure, true); err != nil {
		t.Fatalf("client publication failed: %#v", diagnostics.Of(err))
	}
	reservation := clientReservation(t, store, clientClosure)
	if reservation.Mode != "sealed" || reservation.DirectoryInode == 0 {
		t.Fatalf("reservation = %+v", reservation)
	}
	published := filepath.Join(store.options.Root, "controller", toolAreasName, clientClosure, "tools", "helm", "helm")
	if info, err := os.Stat(published); err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("published client = %v (%v)", info, err)
	}
	// A sealed closure is shared evidence: it reopens read-only for every
	// context that selects it, and no later operation may rewrite it.
	err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
		area, err := tx.ClientArea(ctx, clientClosure)
		if err != nil {
			return err
		}
		location, err := area.Location(ctx)
		if err != nil {
			return err
		}
		if !location.Sealed || location.Writable {
			t.Fatalf("sealed area location = %+v", location)
		}
		return area.Write(ctx, "tools/helm/extra", []byte("x"), true)
	})
	if err == nil {
		t.Fatal("a sealed client closure accepted a new file")
	}
}

// A tool area is named only by a closure digest, and one area never stands
// for another: a digest the record does not hold opens a new reservation.
func TestClientAreaRefusesANameThatIsNoDigest(t *testing.T) {
	ctx := context.Background()
	store, record := lifecycleFixture(t)
	sealedBundleFixture(t, store, record)
	err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
		_, err := tx.ClientArea(ctx, "not-a-digest")
		return err
	})
	expectState(t, err)
}

// An interrupted attribution leaves this store's own reservation beside an
// empty directory. The exact retry owns it and completes; a directory carrying
// foreign content is never adopted.
func TestClientAreaAttributionInterruptionCompletesOnRetry(t *testing.T) {
	store, record := lifecycleFixture(t)
	sealedBundleFixture(t, store, record)

	injection := &interrupt{point: "after-client-area-directory"}
	injection.install(store)
	if err := publishClients(store, clientClosure, true); err == nil {
		t.Fatal("the injected interruption did not fail the publication")
	}
	injection.release(t, store)

	if reservation := clientReservation(t, store, clientClosure); reservation.Mode != "reserved" || reservation.DirectoryInode != 0 {
		t.Fatalf("interrupted reservation = %+v", reservation)
	}
	readable(t, store, "after an interrupted client area attribution")
	if err := publishClients(store, clientClosure, true); err != nil {
		t.Fatalf("exact retry cannot recover its own reserved closure: %#v", diagnostics.Of(err))
	}
}

func TestClientAreaRefusesAnUnattributableDirectory(t *testing.T) {
	store, record := lifecycleFixture(t)
	sealedBundleFixture(t, store, record)

	injection := &interrupt{point: "after-client-area-directory"}
	injection.install(store)
	_ = publishClients(store, clientClosure, true)
	injection.release(t, store)

	foreign := filepath.Join(store.options.Root, "controller", toolAreasName, clientClosure, "foreign")
	if err := os.WriteFile(foreign, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := publishClients(store, clientClosure, true); err == nil {
		t.Fatal("a reserved directory carrying foreign content was adopted")
	}
}

// Retained identities are the durable intent an acquisition is recorded under,
// and immutable once published.
func TestRetainDependenciesIsAppendOnly(t *testing.T) {
	ctx := context.Background()
	store, record := lifecycleFixture(t)
	sealedBundleFixture(t, store, record)
	source := prerequisites.DependencySource{ID: "tool-helm-v3.17.0", URL: "https://mirror.example.test/helm.tar.gz", SHA256: strings.Repeat("b", 64), Bytes: 1024}
	retain := func(value prerequisites.DependencySource) error {
		return store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
			return tx.RetainDependencies(ctx, []prerequisites.DependencySource{value})
		})
	}
	if err := retain(source); err != nil {
		t.Fatalf("retention failed: %#v", diagnostics.Of(err))
	}
	if err := retain(source); err != nil {
		t.Fatalf("repeating the exact retention failed: %#v", diagnostics.Of(err))
	}
	replaced := source
	replaced.SHA256 = strings.Repeat("c", 64)
	if err := retain(replaced); err == nil {
		t.Fatal("a retained acquisition identity was replaced")
	}
	err := store.ReadController(ctx, "example", func(view prerequisites.StorageView) error {
		if !slices.Contains(view.State.ToolSources, source) {
			t.Fatalf("retained sources = %+v", view.State.ToolSources)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("controller read failed: %#v", diagnostics.Of(err))
	}
}

// Every client-area checkpoint must leave the store readable, because an
// interrupted controller stage may never brick inspection or preflight.
func TestClientAreaCheckpointsLeaveStoreReadable(t *testing.T) {
	for _, point := range []string{"before-client-area-reservation", "after-client-area-directory", "before-client-area-attribution", "before-client-area-sealing"} {
		t.Run(point, func(t *testing.T) {
			store, record := lifecycleFixture(t)
			sealedBundleFixture(t, store, record)

			injection := &interrupt{point: point}
			injection.install(store)
			_ = publishClients(store, clientClosure, true)
			injection.release(t, store)

			readable(t, store, "after interruption at "+point)
			_ = publishClients(store, clientClosure, true)
			readable(t, store, "after retry following "+point)
		})
	}
}

// interrupt fails one publication checkpoint exactly once, leaving the store in
// the state an abrupt process death would leave behind. It records whether the
// checkpoint was reached, because an injection that never fires proves nothing.
type interrupt struct {
	point string
	fired bool
}

func (i *interrupt) install(store *Store) {
	store.fail = func(reached string) error {
		if reached != i.point || i.fired {
			return nil
		}
		i.fired = true
		return errors.New("synthetic process death")
	}
}

func (i *interrupt) release(t *testing.T, store *Store) {
	t.Helper()
	store.fail = nil
	if !i.fired {
		t.Fatalf("checkpoint %s was never reached; the case proves nothing", i.point)
	}
}

func readable(t *testing.T, store *Store, when string) {
	t.Helper()
	if _, err := store.View(context.Background()); err != nil {
		t.Fatalf("context inspection broken %s: %v", when, err)
	}
	if err := store.ReadController(context.Background(), "", func(prerequisites.StorageView) error { return nil }); err != nil {
		t.Fatalf("preflight broken %s: %v", when, err)
	}
}
