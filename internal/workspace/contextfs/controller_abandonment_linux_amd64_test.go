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

// nativeAction is the action a stranded receipt plans after its execution
// bundle, in the phase given: planned in every receipt a build before X30
// stranded, because that build met the bound before it reached it.
func nativeAction(phase, outcome, evidence string) p.SetupAction {
	return p.SetupAction{ID: "container-runtime", Request: []byte(`{"readyBefore":false}`), Phase: phase, Outcome: outcome, Evidence: []byte(evidence)}
}

// abandoned is a stranded receipt as setup --purge-old-bundles records it:
// canceled, its intended first action observed as never started because no
// area holds the bundle it names, and every later action still planned.
func abandoned(receipt p.SetupReceipt) p.SetupReceipt {
	receipt.Actions = slices.Clone(receipt.Actions)
	receipt.Actions[0].Phase, receipt.Actions[0].Outcome = "observed", "canceled"
	receipt.Actions[0].Evidence = []byte(`{"bundleArea":"absent"}`)
	receipt.Status = "canceled"
	return receipt
}

// setUpAfresh drives setup over a receipt stranded at the bound that this
// executable cannot resume, in the order setup takes under one mutation: with
// purge it records that receipt canceled, then makes room and publishes the
// fresh receipt of definition, the bundle it names and completion, as
// publishRevision does, and once that completed it retires what the
// completion superseded, as purgeCompleted does. A receipt an interruption
// already canceled is set up afresh the same way. Without purge setup cancels
// nothing and makes no room.
func setUpAfresh(t *testing.T, store *Store, definition p.Definition, purge bool) error {
	t.Helper()
	ctx := context.Background()
	err := store.MutateController(ctx, p.SetupContext{}, true, func(tx p.StorageTransaction) error {
		if view := tx.Snapshot(); purge && strandedReceipt(view) {
			value := view.State
			value.Receipt = abandoned(value.Receipt)
			if _, err := tx.Publish(ctx, value); err != nil {
				return err
			}
		}
		return revise(t, tx, definition, "", purge)
	})
	if err != nil || !purge {
		return err
	}
	return purgeCompleted(store)
}

// purgeCompleted retires, in a mutation of its own, what
// setup --purge-old-bundles retires once its setup completed: every area a
// retained resolution names other than the receipt's bundle, every area left
// retiring, and every resolution whose bundle holds no area, as the canceled
// receipt's does once a fresh receipt replaced it.
func purgeCompleted(store *Store) error {
	ctx := context.Background()
	return store.MutateController(ctx, p.SetupContext{}, false, func(tx p.StorageTransaction) error {
		view := tx.Snapshot()
		var bundles, resolutions []string
		for _, definition := range view.State.RetainedDefinitions {
			switch id := definition.CatalogDigest; {
			case id == view.State.Receipt.CatalogDigest:
			case slices.ContainsFunc(view.Areas, func(held p.HeldArea) bool { return held.ID == id }):
				bundles = append(bundles, id)
			default:
				resolutions = append(resolutions, definition.ResolutionDigest)
			}
		}
		for _, held := range view.Areas {
			if held.Retiring {
				bundles = append(bundles, held.ID)
			}
		}
		if len(bundles) != 0 {
			if err := tx.RetireBundles(ctx, bundles); err != nil {
				return err
			}
		}
		if len(resolutions) == 0 {
			return nil
		}
		return tx.RetireResolutions(ctx, resolutions)
	})
}

// canceledAs fails the test unless the store holds the stranded receipt as
// setup abandons it, with the resolution it carries, and the client area
// sealed.
func canceledAs(t *testing.T, store *Store, stranded p.Definition, when string) p.StorageView {
	t.Helper()
	var held p.StorageView
	if err := store.ReadController(context.Background(), "", func(view p.StorageView) error {
		held = view
		return nil
	}); err != nil {
		t.Fatalf("the record is unreadable %s: %#v", when, diagnostics.Of(err))
	}
	receipt := held.State.Receipt
	if receipt.ID != "setup-"+stranded.ResolutionDigest[:32] || receipt.Status != "canceled" || len(receipt.Actions) != 2 || receipt.Actions[0].Phase != "observed" ||
		receipt.Actions[0].Outcome != "canceled" || string(receipt.Actions[0].Evidence) != `{"bundleArea":"absent"}` || receipt.Actions[1].Phase != "planned" ||
		!slices.ContainsFunc(held.State.RetainedDefinitions, func(definition p.Definition) bool { return definition.ResolutionDigest == stranded.ResolutionDigest }) {
		t.Fatalf("%s the store holds receipt %#v", when, receipt)
	}
	if reservation := bundleReservation(t, store, clientClosure); reservation.Mode != "sealed" {
		t.Fatalf("%s the client area is %+v", when, reservation)
	}
	return held
}

// completedAfresh fails the test unless the fresh setup completed in place of
// the stranded receipt: its own receipt over its own bundle beside the client
// area, no superseded area left, and its own resolution alone, the one the
// canceled receipt carried, which names no area, retired once it completed.
func completedAfresh(t *testing.T, store *Store, stranded, fresh p.Definition, superseded []string) {
	t.Helper()
	if receipt := controllerReceipt(t, store); receipt.Status != "complete" || receipt.Definition == nil || receipt.Definition.ResolutionDigest != fresh.ResolutionDigest {
		t.Fatalf("the fresh setup did not complete: %#v", receipt)
	}
	readableBundle(t, store, fresh.CatalogDigest, "once set up afresh")
	err := store.ReadController(context.Background(), "", func(view p.StorageView) error {
		want := []p.HeldArea{{ID: clientClosure}, {ID: fresh.CatalogDigest}}
		slices.SortFunc(want, func(a, b p.HeldArea) int { return strings.Compare(a.ID, b.ID) })
		var retained []string
		for _, definition := range view.State.RetainedDefinitions {
			retained = append(retained, definition.ResolutionDigest)
		}
		if !slices.Equal(view.Areas, want) || !slices.Equal(retained, []string{fresh.ResolutionDigest}) {
			t.Fatalf("areas=%#v resolutions=%v, the canceled receipt's %s", view.Areas, retained, stranded.ResolutionDigest)
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

// A receipt a build before X30 stranded at the bound holds its first intent and
// every later action planned. Without the flag setup cancels nothing and makes
// no room, so the store refuses any fresh receipt over the pending one. With it
// the store records that receipt canceled, which no earlier rule refused, then
// retires the superseded areas beside it, and the fresh setup completes.
func TestAStrandedReceiptThisExecutableCannotResumeIsCanceledAndSetUpAfresh(t *testing.T) {
	store, stranded, superseded := strandedFixture(t, nativeAction("planned", "", `{}`))
	fresh := automationRevision(t, maxControllerBundles)
	if err := setUpAfresh(t, store, fresh, false); err == nil {
		t.Fatal("a fresh receipt replaced the stranded one")
	}
	if view := pendingAs(t, store, stranded, "after the refusal"); len(view.Areas) != maxControllerBundles {
		t.Fatalf("the refusal moved the areas: %#v", view.Areas)
	}
	if err := setUpAfresh(t, store, fresh, true); err != nil {
		t.Fatalf("the abandoned receipt was not set up afresh: %#v", diagnostics.Of(err))
	}
	completedAfresh(t, store, stranded, fresh, superseded)
}

// A stranded receipt with another unresolved action may have taken effect, so
// setup never abandons it, and the store refuses the cancellation setup records
// while that action stays unresolved: the receipt stays pending, the host at
// its bound, and no fresh receipt replaces it.
func TestAStrandedReceiptWithAnotherUnresolvedActionIsNeverCanceled(t *testing.T) {
	for name, other := range map[string]p.SetupAction{
		"an intended native transaction": nativeAction("intent", "", `{}`),
		"an unknown native outcome":      nativeAction("observed", "unknown", `{"ready":false}`),
	} {
		t.Run(name, func(t *testing.T) {
			store, stranded, _ := strandedFixture(t, other)
			expectState(t, setUpAfresh(t, store, automationRevision(t, maxControllerBundles), true))
			view := pendingAs(t, store, stranded, "after the refused cancellation")
			if len(view.Areas) != maxControllerBundles || slices.ContainsFunc(view.Areas, func(held p.HeldArea) bool { return held.Retiring }) {
				t.Fatalf("the refused cancellation moved the areas: %#v", view.Areas)
			}
		})
	}
}

// Interrupting the abandonment at each durable step, from recording the
// cancellation through retiring the superseded areas, leaves a state the next
// setup --purge-old-bundles completes. Once the receipt is canceled the host
// stays at its bound until the retirement completes, so setup without the flag
// is refused there. Each repeat is interrupted one step further than the last.
func TestAnInterruptedAbandonmentAtTheBoundCompletesOnTheNextPurge(t *testing.T) {
	store, stranded, superseded := strandedFixture(t, nativeAction("planned", "", `{}`))
	fresh := automationRevision(t, maxControllerBundles)
	retiring := func(count int) func(p.StorageView) bool {
		return func(view p.StorageView) bool {
			return len(view.Areas) == maxControllerBundles && len(slices.DeleteFunc(slices.Clone(view.Areas), func(held p.HeldArea) bool { return !held.Retiring })) == count
		}
	}
	for _, step := range []struct {
		point    string
		nth      int
		canceled bool
		left     func(p.StorageView) bool
	}{
		{"before-controller-rename", 1, false, retiring(0)},
		{"after-controller-rename", 1, true, retiring(0)},
		{"after-controller-bundle-retiring", 1, true, retiring(maxControllerBundles - 1)},
		{"before-controller-bundle-unlink", 3, true, retiring(maxControllerBundles - 1)},
		{"after-controller-rename", 2, true, func(view p.StorageView) bool { return slices.Equal(view.Areas, []p.HeldArea{{ID: clientClosure}}) }},
	} {
		when := "after an interruption at " + step.point + " #" + strconv.Itoa(step.nth)
		injection := &nthInterrupt{point: step.point, nth: step.nth}
		injection.install(store)
		err := setUpAfresh(t, store, fresh, true)
		store.fail = nil
		if !injection.fired || err == nil {
			t.Fatalf("%s: fired=%t err=%v", when, injection.fired, err)
		}
		var view p.StorageView
		if step.canceled {
			view = canceledAs(t, store, stranded, when)
		} else {
			view = pendingAs(t, store, stranded, when)
		}
		if !step.left(view) {
			t.Fatalf("%s the store holds %#v", when, view.Areas)
		}
		if step.canceled && len(view.Areas) == maxControllerBundles {
			expectSetupBound(t, setUpAfresh(t, store, fresh, false))
			canceledAs(t, store, stranded, when+" and a setup without the flag")
		}
	}
	if err := setUpAfresh(t, store, fresh, true); err != nil {
		t.Fatalf("repeating the abandonment: %#v", diagnostics.Of(err))
	}
	completedAfresh(t, store, stranded, fresh, superseded)
}
