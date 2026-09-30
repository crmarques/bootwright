//go:build linux && amd64

package contextfs

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// publishBundle drives one setup to completion against its own bundle area,
// exactly as a real setup does: reserve, fill, then complete the receipt. The
// receipt carries no resolution, so no retained resolution names the bundle.
func publishBundle(t *testing.T, store *Store, id string) {
	t.Helper()
	scope := prerequisites.SetupContext{}
	value := syntheticControllerState(t, scope)
	value.Receipt.CatalogDigest = id
	// A second setup is a new receipt, which is what admits a new catalog.
	value.Receipt.ID = "setup-" + strings.Repeat(id[:1], 32)
	value.Receipt.Actions[0].Phase = "intent"
	digest, err := prerequisites.SetupPlanDigest(value.Host, value.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	value.Receipt.PlanDigest = digest
	publishControllerState(t, store, scope, value)
	err = store.MutateController(context.Background(), scope, false, func(tx prerequisites.StorageTransaction) error {
		area, err := tx.Bundle(context.Background(), id)
		if err != nil {
			return err
		}
		if err := area.EnsureDirectory(context.Background(), "sources"); err != nil {
			return err
		}
		return area.Write(context.Background(), "sources/python.tar.gz", []byte("payload"), false)
	})
	if err != nil {
		t.Fatalf("bundle publication for %s failed: %#v", id[:8], diagnostics.Of(err))
	}
	publishControllerState(t, store, scope, completeControllerState(value))
}

// retiredBundle and currentBundle are the execution bundles two successive
// setups publish, each named by the resolution its receipt carries.
func retiredBundle(t *testing.T) string { return automationRevision(t, 0).CatalogDigest }
func currentBundle(t *testing.T) string { return automationRevision(t, 1).CatalogDigest }

// supersede drives those two setups, which leaves the bundle the completed
// receipt names and one superseded bundle a retained resolution still carries.
func supersede(t *testing.T, store *Store) {
	t.Helper()
	for revision := range 2 {
		if err := publishRevision(t, store, automationRevision(t, revision), "", false); err != nil {
			t.Fatalf("setup %d: %#v", revision, diagnostics.Of(err))
		}
	}
}

func retirementFixture(t *testing.T) *Store {
	t.Helper()
	store, _ := fixture(t)
	supersede(t, store)
	return store
}

func bundlePath(store *Store, id string) string {
	return filepath.Join(store.options.Root, "controller", "bundles", id)
}

func retire(store *Store, ids ...string) error {
	return store.MutateController(context.Background(), prerequisites.SetupContext{}, false,
		func(tx prerequisites.StorageTransaction) error {
			return tx.RetireBundles(context.Background(), ids)
		})
}

// Retirement removes the superseded area and the resolution it carries, and
// leaves the bundle the receipt names exactly where it is.
func TestRetirementRemovesASupersededBundleAndKeepsTheCurrentOne(t *testing.T) {
	store, superseded, current := retirementFixture(t), retiredBundle(t), currentBundle(t)
	if err := retire(store, superseded); err != nil {
		t.Fatalf("retirement failed: %#v", diagnostics.Of(err))
	}
	if _, err := os.Stat(bundlePath(store, superseded)); !os.IsNotExist(err) {
		t.Fatalf("the superseded bundle survived: %v", err)
	}
	if _, err := os.Stat(bundlePath(store, current)); err != nil {
		t.Fatalf("the bundle this receipt names was removed: %v", err)
	}
	err := store.ReadController(context.Background(), "", func(view prerequisites.StorageView) error {
		for _, definition := range view.State.RetainedDefinitions {
			if definition.CatalogDigest == superseded {
				t.Fatal("the resolution a retired bundle carries was kept")
			}
		}
		if area, err := view.OpenBundle(context.Background(), superseded); err == nil && area != nil {
			t.Fatal("a retired bundle is still readable")
		}
		if area, err := view.OpenBundle(context.Background(), current); err != nil || area == nil {
			t.Fatalf("the current bundle became unreadable: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read after retirement: %#v", diagnostics.Of(err))
	}
}

// The bundle a completed receipt names is what every lifecycle operation runs,
// so retiring it refuses and changes nothing.
func TestRetirementRefusesTheBundleTheReceiptNames(t *testing.T) {
	store, superseded, current := retirementFixture(t), retiredBundle(t), currentBundle(t)
	if err := retire(store, current); err == nil {
		t.Fatal("the current execution bundle was retired")
	}
	for _, id := range []string{current, superseded} {
		if _, err := os.Stat(bundlePath(store, id)); err != nil {
			t.Fatalf("a refused retirement removed %s: %v", id[:8], err)
		}
	}
}

// An area this host does not hold is already gone, so naming it completes
// rather than failing: repeating the command is always safe.
func TestRetiringAnAreaThisHostDoesNotHoldRemovesNothing(t *testing.T) {
	store, superseded, current := retirementFixture(t), retiredBundle(t), currentBundle(t)
	if err := retire(store, strings.Repeat("c", 64)); err != nil {
		t.Fatalf("an absent area refused: %#v", diagnostics.Of(err))
	}
	for _, id := range []string{current, superseded} {
		if _, err := os.Stat(bundlePath(store, id)); err != nil {
			t.Fatalf("naming an absent area removed %s: %v", id[:8], err)
		}
	}
}

// An interruption between recording the intent and removing the bytes leaves an
// area that is never read, and repeating the command completes it.
func TestAnInterruptedRetirementIsCompletedByRepeatingIt(t *testing.T) {
	store, superseded := retirementFixture(t), retiredBundle(t)
	store.fail = func(name string) error {
		if name == "after-controller-bundle-retiring" {
			return diagnostics.NewFailure("context.state", "interrupted", "")
		}
		return nil
	}
	if err := retire(store, superseded); err == nil {
		t.Fatal("the interruption was not reported")
	}
	store.fail = nil
	// The intent is durable, so the area is already unreadable even though its
	// bytes are still there.
	err := store.ReadController(context.Background(), "", func(view prerequisites.StorageView) error {
		if area, openErr := view.OpenBundle(context.Background(), superseded); openErr == nil && area != nil {
			t.Fatal("an area marked retiring is still readable")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read after interruption: %#v", diagnostics.Of(err))
	}
	if err := retire(store, superseded); err != nil {
		t.Fatalf("repeating the retirement failed: %#v", diagnostics.Of(err))
	}
	if _, err := os.Stat(bundlePath(store, superseded)); !os.IsNotExist(err) {
		t.Fatalf("the repeated retirement left the area: %v", err)
	}
}

// reservedFixture leaves the superseded bundle as an interrupted publication
// leaves it: its setup reserved the area, was interrupted before it attributed
// the directory it created, and was then canceled, and a later setup completed
// the current bundle. Without the directory, the crash lost it; with content,
// something other than this store wrote into it.
func reservedFixture(t *testing.T, directory, content bool) *Store {
	t.Helper()
	ctx := context.Background()
	store, _ := fixture(t)
	superseded := automationRevision(t, 0)
	store.fail = func(name string) error {
		if name == "after-controller-bundle-directory" {
			return diagnostics.NewFailure("context.state", "interrupted", "")
		}
		return nil
	}
	err := store.MutateController(ctx, prerequisites.SetupContext{}, true, func(tx prerequisites.StorageTransaction) error {
		if _, err := tx.Publish(ctx, revisionReceipt(t, superseded)); err != nil {
			return err
		}
		_, err := tx.Bundle(ctx, superseded.CatalogDigest)
		return err
	})
	store.fail = nil
	if err == nil {
		t.Fatal("the bundle publication was not interrupted")
	}
	switch path := bundlePath(store, superseded.CatalogDigest); {
	case !directory:
		err = os.Remove(path)
	case content:
		err = os.WriteFile(filepath.Join(path, "foreign"), []byte("foreign"), 0o600)
	default:
		err = nil
	}
	if err != nil {
		t.Fatal(err)
	}
	err = store.MutateController(ctx, prerequisites.SetupContext{}, false, func(tx prerequisites.StorageTransaction) error {
		value := tx.Snapshot().State
		value.Receipt.Actions[0].Phase, value.Receipt.Actions[0].Outcome, value.Receipt.Actions[0].Evidence = "observed", "canceled", []byte(`{"canceled":true}`)
		value.Receipt.Status = "canceled"
		_, err := tx.Publish(ctx, value)
		return err
	})
	if err != nil {
		t.Fatalf("canceling the interrupted setup: %#v", diagnostics.Of(err))
	}
	if err := publishRevision(t, store, automationRevision(t, 1), "", false); err != nil {
		t.Fatalf("the current setup: %#v", diagnostics.Of(err))
	}
	if reservation := bundleReservation(t, store, superseded.CatalogDigest); reservation.Mode != "reserved" || reservation.DirectoryInode != 0 {
		t.Fatalf("the superseded area is not left reserved: %+v", reservation)
	}
	return store
}

func bundleReservation(t *testing.T, store *Store, id string) controllerBundleReservation {
	t.Helper()
	var found controllerBundleReservation
	err := store.MutateController(context.Background(), prerequisites.SetupContext{}, false, func(tx prerequisites.StorageTransaction) error {
		bundles := tx.(*controllerTransaction).stored.bundles
		if index := slices.IndexFunc(bundles, func(item controllerBundleReservation) bool { return item.ID == id }); index >= 0 {
			found = bundles[index]
		}
		return nil
	})
	if err != nil {
		t.Fatalf("reservation read failed: %#v", diagnostics.Of(err))
	}
	return found
}

// A reserved area records no directory identity, while a retiring one keeps
// the identity its removal is verified against. Retiring a reserved area
// therefore attributes the empty directory its interrupted publication left,
// or drops the reservation when there is none, so the record stays readable
// even when an interruption follows the intent, and the repeat leaves only
// the bundle the receipt names and its resolution.
func TestRetiringAReservedAreaKeepsTheRecordReadable(t *testing.T) {
	for name, directory := range map[string]bool{"without a directory": false, "with its empty directory": true} {
		t.Run(name, func(t *testing.T) {
			store, superseded, current := reservedFixture(t, directory, false), retiredBundle(t), currentBundle(t)
			store.fail = func(name string) error {
				if name == "after-controller-bundle-retiring" {
					return diagnostics.NewFailure("context.state", "interrupted", "")
				}
				return nil
			}
			if err := retire(store, superseded); err == nil {
				t.Fatal("the interruption was not reported")
			}
			store.fail = nil
			read := func(when string) prerequisites.StorageView {
				var held prerequisites.StorageView
				err := store.ReadController(context.Background(), "", func(view prerequisites.StorageView) error {
					if area, err := view.OpenBundle(context.Background(), superseded); err == nil && area != nil {
						t.Fatalf("the retired area is readable %s", when)
					}
					held = view
					return nil
				})
				if err != nil {
					t.Fatalf("the record is unreadable %s: %#v", when, diagnostics.Of(err))
				}
				return held
			}
			read("after the interrupted retirement")
			if err := retire(store, superseded); err != nil {
				t.Fatalf("repeating the retirement failed: %#v", diagnostics.Of(err))
			}
			view := read("after the repeated retirement")
			if !slices.Equal(view.Areas, []prerequisites.HeldArea{{ID: current}}) || len(view.State.RetainedDefinitions) != 1 || view.State.RetainedDefinitions[0].CatalogDigest != current {
				t.Fatalf("areas=%#v resolutions=%d", view.Areas, len(view.State.RetainedDefinitions))
			}
			if _, err := os.Stat(bundlePath(store, superseded)); !os.IsNotExist(err) {
				t.Fatalf("the reserved area's directory survived: %v", err)
			}
			readableBundle(t, store, current, "after the retirement")
		})
	}
}

// A directory beside a reservation holds nothing this store published until
// the store attributes it, so one holding anything is not this store's own
// interrupted attempt: retiring it refuses and changes nothing.
func TestRetirementRefusesAReservedDirectoryHoldingContent(t *testing.T) {
	store, superseded := reservedFixture(t, true, true), retiredBundle(t)
	expectState(t, retire(store, superseded))
	if _, err := os.Stat(filepath.Join(bundlePath(store, superseded), "foreign")); err != nil {
		t.Fatalf("the refused retirement removed the content: %v", err)
	}
	if reservation := bundleReservation(t, store, superseded); reservation.Mode != "reserved" {
		t.Fatalf("the refused retirement changed the reservation to %+v", reservation)
	}
}

// Retirement removes only a directory whose identity the record proves, so one
// that appears after a reserved area's reservation was dropped survives.
func TestRetirementNeverRemovesADirectoryItDidNotAttribute(t *testing.T) {
	store, superseded := reservedFixture(t, false, false), retiredBundle(t)
	foreign := filepath.Join(bundlePath(store, superseded), "foreign")
	store.fail = func(name string) error {
		if name != "after-controller-bundle-retiring" {
			return nil
		}
		if err := os.Mkdir(filepath.Dir(foreign), 0o700); err != nil {
			return err
		}
		return os.WriteFile(foreign, []byte("foreign"), 0o600)
	}
	expectState(t, retire(store, superseded))
	store.fail = nil
	if _, err := os.Stat(foreign); err != nil {
		t.Fatalf("retirement removed a directory it did not attribute: %v", err)
	}
}

// A client area is shared host state no context uninstalls, so the store
// refuses to retire one whoever names it, and the refusal removes nothing,
// not even a superseded bundle named beside it. The same holds for an
// execution bundle no retained resolution names.
func TestRetirementRefusesAClientArea(t *testing.T) {
	store, _ := lifecycleFixture(t)
	unnamed, superseded := strings.Repeat("e", 64), retiredBundle(t)
	publishBundle(t, store, unnamed)
	supersede(t, store)
	if err := publishClients(store, clientClosure, true); err != nil {
		t.Fatalf("client publication failed: %#v", diagnostics.Of(err))
	}
	for _, ids := range [][]string{{clientClosure}, {superseded, clientClosure}, {unnamed}} {
		expectState(t, retire(store, ids...))
		for _, id := range []string{clientClosure, superseded, unnamed} {
			if _, err := os.Stat(bundlePath(store, id)); err != nil {
				t.Fatalf("retiring %d areas removed %s: %v", len(ids), id[:8], err)
			}
		}
	}
	if reservation := clientReservation(t, store, clientClosure); reservation.Mode != "sealed" {
		t.Fatalf("the client area reservation became %+v", reservation)
	}
}
