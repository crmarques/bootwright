//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

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

// reserveAndWrite is the durably intended setup action: it reserves the bundle
// and publishes one file into it, leaving the receipt incomplete.
func reserveAndWrite(store *Store, value prerequisites.HostState) error {
	return store.MutateController(context.Background(), prerequisites.SetupContext{}, false, func(tx prerequisites.StorageTransaction) error {
		next := tx.Snapshot().State
		next.Receipt.Actions[0].Phase = "intent"
		if _, err := tx.Publish(context.Background(), next); err != nil {
			return err
		}
		area, err := tx.Bundle(context.Background(), value.Receipt.CatalogDigest)
		if err != nil {
			return err
		}
		if err := area.EnsureDirectory(context.Background(), "bin"); err != nil {
			return err
		}
		return area.Write(context.Background(), "bin/marker", []byte("published\n"), true)
	})
}

// sealBundle completes the receipt, which is what forces the durable sync of
// every published bundle file.
func sealBundle(store *Store) error {
	return store.MutateController(context.Background(), prerequisites.SetupContext{}, false, func(tx prerequisites.StorageTransaction) error {
		_, err := tx.Publish(context.Background(), completeControllerState(tx.Snapshot().State))
		return err
	})
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

func controllerFixture(t *testing.T) (*Store, prerequisites.HostState) {
	t.Helper()
	store, sources := fixture(t)
	publish(t, store, "alpha", sources)
	value := syntheticControllerState(t, prerequisites.SetupContext{})
	publishControllerState(t, store, prerequisites.SetupContext{}, value)
	return store, value
}

// A crash at any bundle publication checkpoint must leave the store readable:
// an interrupted controller setup may end in exact retry or in a typed safe
// refusal, but it may never brick context inspection or preflight.
func TestControllerPublicationCheckpointsLeaveStoreReadable(t *testing.T) {
	for _, point := range []string{"before-controller-rename", "after-controller-bundle-directory", "before-controller-bundle-write", "before-controller-bundle-sync"} {
		t.Run(point, func(t *testing.T) {
			store, value := controllerFixture(t)

			injection := &interrupt{point: point}
			injection.install(store)
			if err := reserveAndWrite(store, value); err == nil {
				_ = sealBundle(store)
			}
			injection.release(t, store)

			readable(t, store, "after interruption at "+point)
			// Whatever the retry decides, it must not destroy readability.
			if err := reserveAndWrite(store, value); err == nil {
				_ = sealBundle(store)
			}
			readable(t, store, "after retry following "+point)
		})
	}
}

// The reservation checkpoint is the one case where recovery must actually
// complete: intent was published durably before the directory was created, so
// the exact retry owns that directory and finishes the setup.
func TestControllerBundleReservationInterruptionCompletesOnRetry(t *testing.T) {
	store, value := controllerFixture(t)

	injection := &interrupt{point: "after-controller-bundle-directory"}
	injection.install(store)
	_ = reserveAndWrite(store, value)
	injection.release(t, store)

	readable(t, store, "after interrupted bundle reservation")
	if err := reserveAndWrite(store, value); err != nil {
		t.Fatalf("exact retry cannot recover its own reserved bundle: %v", err)
	}
	if err := sealBundle(store); err != nil {
		t.Fatalf("recovered bundle cannot be sealed: %v", err)
	}
}

// Recovery must not become a way to absorb content: a reserved directory that
// is no longer empty stays unadoptable.
func TestControllerRetryRefusesNonEmptyBundleDirectory(t *testing.T) {
	store, value := controllerFixture(t)

	injection := &interrupt{point: "after-controller-bundle-directory"}
	injection.install(store)
	_ = reserveAndWrite(store, value)
	injection.release(t, store)

	intruder := filepath.Join(store.options.Root, "controller", "bundles", value.Receipt.CatalogDigest, "intruder")
	if err := os.Mkdir(intruder, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := reserveAndWrite(store, value); err == nil {
		t.Fatal("retry adopted a bundle directory holding unattributed content")
	}
	readable(t, store, "after refusing an unattributable bundle directory")
}

// An interrupted controller-directory initialization is a terminal safe
// refusal for setup, but ordinary context commands must still work: a crash in
// controller setup may not make the workspace unlistable.
func TestControllerDirectoryInterruptionLeavesStoreReadable(t *testing.T) {
	store, sources := fixture(t)
	publish(t, store, "alpha", sources)
	value := syntheticControllerState(t, prerequisites.SetupContext{})

	injection := &interrupt{point: "after-controller-directory"}
	injection.install(store)
	if err := store.MutateController(context.Background(), prerequisites.SetupContext{}, true, func(tx prerequisites.StorageTransaction) error {
		_, err := tx.Publish(context.Background(), value)
		return err
	}); err == nil {
		t.Fatal("interrupted directory attribution succeeded")
	}
	injection.release(t, store)

	readable(t, store, "after interrupted controller-directory initialization")
	if err := store.ReadController(context.Background(), "", func(view prerequisites.StorageView) error {
		if view.Initialized {
			t.Fatal("orphaned controller directory was reported as initialized setup")
		}
		return nil
	}); err != nil {
		t.Fatalf("preflight cannot report missing setup: %v", err)
	}
	// The orphan itself stays unadoptable; recovery is explicit operator work.
	if err := store.MutateController(context.Background(), prerequisites.SetupContext{}, true, func(tx prerequisites.StorageTransaction) error {
		_, err := tx.Publish(context.Background(), value)
		return err
	}); err == nil {
		t.Fatal("retry adopted an unattributed controller directory")
	}
}
