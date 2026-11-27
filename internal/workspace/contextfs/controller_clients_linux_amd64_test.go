//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/workspace/contextfs/kernel"
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
	t.Parallel()
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
	published := filepath.Join(store.options.Root, controllerContainer, controllerSubtree, toolAreasName, clientClosure, "tools", "helm", "helm")
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	store, record := lifecycleFixture(t)
	sealedBundleFixture(t, store, record)

	injection := &interrupt{point: "after-client-area-directory"}
	injection.install(store)
	_ = publishClients(store, clientClosure, true)
	injection.release(t, store)

	foreign := filepath.Join(store.options.Root, controllerContainer, controllerSubtree, toolAreasName, clientClosure, "foreign")
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
	t.Parallel()
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
	t.Parallel()
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

func TestClientAreaReplayAfterReadOnlyInspection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, record := lifecycleFixture(t)
	sealedBundleFixture(t, store, record)
	if err := publishClients(store, clientClosure, false); err != nil {
		t.Fatalf("partial publication failed: %#v", diagnostics.Of(err))
	}
	err := store.MutateLifecycle(ctx, record.Name, func(tx lifecycle.Transaction) error {
		view := tx.Controller()
		reader, err := view.OpenBundle(ctx, clientClosure)
		if err != nil {
			return err
		}
		before, err := reader.Location(ctx)
		if err != nil {
			return err
		}
		if before.Sealed || before.Writable {
			t.Fatalf("inspection held the wrong capability: %+v", before)
		}
		// Another holder may inspect this same area. Publication must
		// refuse while that holder is active and keep our shared lease.
		other, err := os.Open(before.Path)
		if err != nil {
			return err
		}
		defer other.Close()
		if err := syscall.Flock(int(other.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
			return err
		}
		_, err = tx.ClientArea(ctx, clientClosure)
		var busy *kernel.BusyError
		if !errors.As(err, &busy) {
			t.Fatalf("publication while another reader holds the bundle = %v", err)
		}
		if err := syscall.Flock(int(other.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); !errors.Is(err, syscall.EWOULDBLOCK) {
			t.Fatalf("inspection did not retain its shared lease: %v", err)
		}
		if err := other.Close(); err != nil {
			return err
		}
		writer, err := tx.ClientArea(ctx, clientClosure)
		if err != nil {
			return err
		}
		after, err := writer.Location(ctx)
		if err != nil {
			return err
		}
		if !after.Writable || after.Sealed || after.Device != before.Device || after.Inode != before.Inode {
			t.Fatalf("replay requires the same writable pending bundle: inspection %+v, replay %+v", before, after)
		}
		if err := reader.Write(ctx, "tools/helm/extra", []byte("x"), true); err == nil {
			t.Fatal("inspection gained write authority")
		}
		if err := writer.Write(ctx, "tools/helm/extra", []byte("x"), true); err != nil {
			return err
		}
		if err := tx.SealClientArea(ctx, clientClosure); err != nil {
			return err
		}
		after, err = writer.Location(ctx)
		if err != nil {
			return err
		}
		if after.Writable || !after.Sealed {
			t.Fatalf("replayed bundle was not sealed: %+v", after)
		}
		inspected, err := reader.Location(ctx)
		if err != nil {
			return err
		}
		if inspected.Writable || !inspected.Sealed {
			t.Fatalf("inspection did not reflect the sealed bundle: %+v", inspected)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("client replay failed: %#v", diagnostics.Of(err))
	}
}

func TestClientAreaCannotPromoteARetiringArea(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, sources := fixture(t)
	owner := publish(t, store, "example", sources)
	other := publish(t, store, "other", sources)
	id := sealedBundleFixture(t, store, owner)
	err := store.MutateLifecycle(ctx, other.Name, func(tx lifecycle.Transaction) error {
		reader, err := tx.Controller().OpenBundle(ctx, id)
		if err != nil {
			return err
		}
		err = store.MutateLifecycle(ctx, owner.Name, func(owner lifecycle.Transaction) error {
			return owner.RetireToolAreas(ctx, "")
		})
		var busy *kernel.BusyError
		if !errors.As(err, &busy) {
			t.Fatalf("retirement while an operator holds the area = %v", err)
		}
		if mode := clientReservation(t, store, id).Mode; mode != retiringArea {
			t.Fatalf("held area reached mode %q, want retiring", mode)
		}
		writer, err := tx.ClientArea(ctx, id)
		if writer != nil || !errors.As(err, &busy) {
			t.Fatalf("publication of a retiring area = %v", err)
		}
		location, err := reader.Location(ctx)
		if err != nil {
			return err
		}
		if location.Writable || !location.Sealed {
			t.Fatalf("retirement changed the read capability: %+v", location)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("cache and retirement test failed: %#v", diagnostics.Of(err))
	}
}
