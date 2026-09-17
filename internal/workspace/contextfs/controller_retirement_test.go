//go:build linux && amd64

package contextfs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

const (
	retiredBundleID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	currentBundleID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

// publishBundle drives one setup to completion against its own bundle area,
// exactly as a real setup does: reserve, fill, then complete the receipt.
func publishBundle(t *testing.T, store *Store, id string, retained []prerequisites.Definition) {
	t.Helper()
	scope := prerequisites.SetupContext{}
	value := syntheticControllerState(t, scope)
	value.Receipt.CatalogDigest = id
	// A second setup is a new receipt, which is what admits a new catalog.
	value.Receipt.ID = "setup-" + strings.Repeat(id[:1], 32)
	value.Receipt.Actions[0].Phase = "intent"
	value.RetainedDefinitions = retained
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

// retirementFixture prepares a host holding the bundle its completed receipt
// names and one superseded bundle a retained resolution still carries.
func retirementFixture(t *testing.T) *Store {
	t.Helper()
	store, _ := fixture(t)
	superseded := []prerequisites.Definition{{CatalogDigest: retiredBundleID}}
	publishBundle(t, store, retiredBundleID, superseded)
	publishBundle(t, store, currentBundleID, append(superseded, prerequisites.Definition{CatalogDigest: currentBundleID}))
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
	store := retirementFixture(t)
	if err := retire(store, retiredBundleID); err != nil {
		t.Fatalf("retirement failed: %#v", diagnostics.Of(err))
	}
	if _, err := os.Stat(bundlePath(store, retiredBundleID)); !os.IsNotExist(err) {
		t.Fatalf("the superseded bundle survived: %v", err)
	}
	if _, err := os.Stat(bundlePath(store, currentBundleID)); err != nil {
		t.Fatalf("the bundle this receipt names was removed: %v", err)
	}
	err := store.ReadController(context.Background(), "", func(view prerequisites.StorageView) error {
		for _, definition := range view.State.RetainedDefinitions {
			if definition.CatalogDigest == retiredBundleID {
				t.Fatal("the resolution a retired bundle carries was kept")
			}
		}
		if area, err := view.OpenBundle(context.Background(), retiredBundleID); err == nil && area != nil {
			t.Fatal("a retired bundle is still readable")
		}
		if area, err := view.OpenBundle(context.Background(), currentBundleID); err != nil || area == nil {
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
	store := retirementFixture(t)
	if err := retire(store, currentBundleID); err == nil {
		t.Fatal("the current execution bundle was retired")
	}
	for _, id := range []string{currentBundleID, retiredBundleID} {
		if _, err := os.Stat(bundlePath(store, id)); err != nil {
			t.Fatalf("a refused retirement removed %s: %v", id[:8], err)
		}
	}
}

// An area this host does not hold is already gone, so naming it completes
// rather than failing: repeating the command is always safe.
func TestRetiringAnAreaThisHostDoesNotHoldRemovesNothing(t *testing.T) {
	store := retirementFixture(t)
	if err := retire(store, strings.Repeat("c", 64)); err != nil {
		t.Fatalf("an absent area refused: %#v", diagnostics.Of(err))
	}
	for _, id := range []string{currentBundleID, retiredBundleID} {
		if _, err := os.Stat(bundlePath(store, id)); err != nil {
			t.Fatalf("naming an absent area removed %s: %v", id[:8], err)
		}
	}
}

// An interruption between recording the intent and removing the bytes leaves an
// area that is never read, and repeating the command completes it.
func TestAnInterruptedRetirementIsCompletedByRepeatingIt(t *testing.T) {
	store := retirementFixture(t)
	store.fail = func(name string) error {
		if name == "after-controller-bundle-retiring" {
			return diagnostics.NewFailure("context.state", "interrupted", "")
		}
		return nil
	}
	if err := retire(store, retiredBundleID); err == nil {
		t.Fatal("the interruption was not reported")
	}
	store.fail = nil
	// The intent is durable, so the area is already unreadable even though its
	// bytes are still there.
	err := store.ReadController(context.Background(), "", func(view prerequisites.StorageView) error {
		if area, openErr := view.OpenBundle(context.Background(), retiredBundleID); openErr == nil && area != nil {
			t.Fatal("an area marked retiring is still readable")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read after interruption: %#v", diagnostics.Of(err))
	}
	if err := retire(store, retiredBundleID); err != nil {
		t.Fatalf("repeating the retirement failed: %#v", diagnostics.Of(err))
	}
	if _, err := os.Stat(bundlePath(store, retiredBundleID)); !os.IsNotExist(err) {
		t.Fatalf("the repeated retirement left the area: %v", err)
	}
}
