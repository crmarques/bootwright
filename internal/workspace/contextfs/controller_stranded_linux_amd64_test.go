//go:build linux && amd64

package contextfs

import (
	"context"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	p "github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// strandedFixture leaves a store as a build that published a receipt at the
// bound left it. Fifteen setups each sealed their own bundle beside the
// resolution they carry, a controller stage sealed a client area in the
// sixteenth slot, and the next setup's receipt, whose first intent is durable
// before the further actions given, names a bundle the store could never
// reserve. The store now refuses that receipt, so it is written as that build
// wrote it. It returns the stranded resolution and the superseded bundles,
// oldest first.
func strandedFixture(t *testing.T, further ...p.SetupAction) (*Store, p.Definition, []string) {
	t.Helper()
	ctx := context.Background()
	store, _ := lifecycleFixture(t)
	var superseded []string
	for revision := range maxControllerBundles - 1 {
		definition := automationRevision(t, revision)
		if err := publishRevision(t, store, definition, "", false); err != nil {
			t.Fatalf("revision %d: %#v", revision, diagnostics.Of(err))
		}
		superseded = append(superseded, definition.CatalogDigest)
	}
	if err := publishClients(store, clientClosure, true); err != nil {
		t.Fatalf("client publication failed: %#v", diagnostics.Of(err))
	}
	stranded := automationRevision(t, maxControllerBundles-1)
	err := store.MutateController(ctx, p.SetupContext{}, false, func(tx p.StorageTransaction) error {
		controller := tx.(*controllerTransaction)
		value, err := retainControllerSources(controller.stored.value, revisionReceipt(t, stranded, further...))
		if err != nil {
			return err
		}
		_, err = controller.publishValue(ctx, value, controller.stored.bundles)
		return err
	})
	if err != nil {
		t.Fatalf("stranding the receipt: %#v", diagnostics.Of(err))
	}
	if err := store.ReadController(ctx, "", func(view p.StorageView) error {
		if !strandedReceipt(view) || len(view.State.RetainedDefinitions) != maxControllerBundles {
			t.Fatalf("the store holds areas %#v and %d resolutions under %#v", view.Areas, len(view.State.RetainedDefinitions), view.State.Receipt)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return store, stranded, superseded
}

// pendingAs fails the test unless the store still holds the stranded receipt
// pending, with its resolution, and the client area sealed.
func pendingAs(t *testing.T, store *Store, stranded p.Definition, when string) p.StorageView {
	t.Helper()
	var held p.StorageView
	if err := store.ReadController(context.Background(), "", func(view p.StorageView) error {
		held = view
		return nil
	}); err != nil {
		t.Fatalf("the record is unreadable %s: %#v", when, diagnostics.Of(err))
	}
	receipt := held.State.Receipt
	if !receipt.Incomplete() || receipt.Definition == nil || receipt.Definition.ResolutionDigest != stranded.ResolutionDigest ||
		!slices.ContainsFunc(held.State.RetainedDefinitions, func(definition p.Definition) bool { return definition.ResolutionDigest == stranded.ResolutionDigest }) {
		t.Fatalf("%s the store holds receipt %#v", when, receipt)
	}
	if reservation := bundleReservation(t, store, clientClosure); reservation.Mode != "sealed" {
		t.Fatalf("%s the client area is %+v", when, reservation)
	}
	return held
}

// completedStranded fails the test unless the stranded receipt completed over
// its own bundle, beside the client area, with its own resolution alone, and
// no superseded bundle survived.
func completedStranded(t *testing.T, store *Store, stranded p.Definition, superseded []string) {
	t.Helper()
	if receipt := controllerReceipt(t, store); receipt.Status != "complete" || receipt.ID != "setup-"+stranded.ResolutionDigest[:32] {
		t.Fatalf("the stranded receipt did not complete: %#v", receipt)
	}
	readableBundle(t, store, stranded.CatalogDigest, "once completed")
	err := store.ReadController(context.Background(), "", func(view p.StorageView) error {
		want := []p.HeldArea{{ID: clientClosure}, {ID: stranded.CatalogDigest}}
		slices.SortFunc(want, func(a, b p.HeldArea) int { return strings.Compare(a.ID, b.ID) })
		if !slices.Equal(view.Areas, want) || len(view.State.RetainedDefinitions) != 1 || view.State.RetainedDefinitions[0].ResolutionDigest != stranded.ResolutionDigest {
			t.Fatalf("areas=%#v resolutions=%d", view.Areas, len(view.State.RetainedDefinitions))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range superseded {
		if _, err := os.Stat(bundlePath(store, id)); !os.IsNotExist(err) {
			t.Fatalf("superseded area %s survived: %v", id[:8], err)
		}
	}
}

// A pending receipt whose bundle the store cannot reserve at its bound admits a
// retirement of the superseded execution bundles, while the store still
// refuses the bundle that receipt names, the client area and a retirement of
// resolutions alone. Without it the receipt cannot resume; after it the
// receipt resumes exactly and completes.
func TestAStrandedPendingReceiptAtTheBoundResumesAfterRetiringSupersededAreas(t *testing.T) {
	store, stranded, superseded := strandedFixture(t)
	if err := publishRevision(t, store, stranded, "", false); err == nil {
		t.Fatal("the stranded receipt's bundle was reserved beyond the bound")
	}
	if err := retire(store, stranded.CatalogDigest); err == nil {
		t.Fatal("the bundle the pending receipt names was retired")
	}
	expectState(t, retire(store, clientClosure))
	expectState(t, store.MutateController(context.Background(), p.SetupContext{}, false, func(tx p.StorageTransaction) error {
		return tx.RetireResolutions(context.Background(), []string{automationRevision(t, 0).ResolutionDigest})
	}))
	if view := pendingAs(t, store, stranded, "after the refusals"); len(view.Areas) != maxControllerBundles {
		t.Fatalf("a refusal moved the areas: %#v", view.Areas)
	}
	if err := publishRevision(t, store, stranded, "", true); err != nil {
		t.Fatalf("the stranded receipt did not resume under retirement: %#v", diagnostics.Of(err))
	}
	completedStranded(t, store, stranded, superseded)
}

// A pending receipt whose bundle holds an area resumes without any room, so the
// store refuses a retirement beside it even at the bound.
func TestAPendingReceiptWhoseBundleIsReservedAdmitsNoRetirement(t *testing.T) {
	ctx := context.Background()
	store, _ := fixture(t)
	for revision := range maxControllerBundles - 1 {
		if err := publishRevision(t, store, automationRevision(t, revision), "", false); err != nil {
			t.Fatalf("revision %d: %#v", revision, diagnostics.Of(err))
		}
	}
	pending := automationRevision(t, maxControllerBundles-1)
	err := store.MutateController(ctx, p.SetupContext{}, false, func(tx p.StorageTransaction) error {
		if _, err := tx.Publish(ctx, revisionReceipt(t, pending)); err != nil {
			return err
		}
		return fillBundle(ctx, tx, pending.CatalogDigest)
	})
	if err != nil {
		t.Fatalf("the pending setup: %#v", diagnostics.Of(err))
	}
	expectState(t, retire(store, automationRevision(t, 0).CatalogDigest))
	readableBundle(t, store, automationRevision(t, 0).CatalogDigest, "after the refused retirement")
}

// Interrupting the resumed setup at each durable step, from recording the
// retirement through completing the receipt, leaves that receipt pending and
// its resolution retained, and repeating it goes on from what the interruption
// left. Each repeat is interrupted one step further than the last.
func TestAnInterruptedStrandedReceiptAtTheBoundStaysResumable(t *testing.T) {
	store, stranded, superseded := strandedFixture(t)
	retiring := func(count int) func(p.StorageView) bool {
		return func(view p.StorageView) bool {
			return len(view.Areas) == maxControllerBundles && len(slices.DeleteFunc(slices.Clone(view.Areas), func(held p.HeldArea) bool { return !held.Retiring })) == count
		}
	}
	holding := func(ids ...string) func(p.StorageView) bool {
		return func(view p.StorageView) bool {
			held := []string{}
			for _, area := range view.Areas {
				held = append(held, area.ID)
			}
			return slices.Equal(held, slices.Sorted(slices.Values(ids)))
		}
	}
	for _, step := range []struct {
		point string
		nth   int
		left  func(p.StorageView) bool
	}{
		{"before-controller-rename", 1, retiring(0)},
		{"after-controller-bundle-retiring", 1, retiring(maxControllerBundles - 1)},
		{"before-controller-bundle-unlink", 3, retiring(maxControllerBundles - 1)},
		{"after-controller-rename", 2, holding(clientClosure)},
		{"after-controller-rename", 1, holding(clientClosure, stranded.CatalogDigest)},
		{"after-controller-bundle-directory", 1, holding(clientClosure, stranded.CatalogDigest)},
		{"before-controller-bundle-sync", 1, holding(clientClosure, stranded.CatalogDigest)},
	} {
		when := "after an interruption at " + step.point + " #" + strconv.Itoa(step.nth)
		injection := &nthInterrupt{point: step.point, nth: step.nth}
		injection.install(store)
		err := publishRevision(t, store, stranded, "", true)
		store.fail = nil
		if !injection.fired || err == nil {
			t.Fatalf("%s: fired=%t err=%v", when, injection.fired, err)
		}
		if view := pendingAs(t, store, stranded, when); !step.left(view) {
			t.Fatalf("%s the store holds %#v", when, view.Areas)
		}
	}
	injection := &nthInterrupt{point: "after-controller-rename", nth: 1}
	injection.install(store)
	err := publishRevision(t, store, stranded, "", true)
	store.fail = nil
	if !injection.fired || err == nil {
		t.Fatalf("after an interruption once completion is published: fired=%t err=%v", injection.fired, err)
	}
	if receipt := controllerReceipt(t, store); receipt.Status != "complete" {
		t.Fatalf("the completion publication did not land: %#v", receipt)
	}
	if err := publishRevision(t, store, stranded, "", true); err != nil {
		t.Fatalf("repeating the resumed setup: %#v", diagnostics.Of(err))
	}
	completedStranded(t, store, stranded, superseded)
}
