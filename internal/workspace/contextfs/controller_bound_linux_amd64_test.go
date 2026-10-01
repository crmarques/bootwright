//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	p "github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// automationRevision is the resolution an executable whose embedded automation
// moved carries forward: every release, source and signer of the synthetic
// resolution, projected under the given revision's automation, so only its
// bundle identity differs.
func automationRevision(t *testing.T, revision int) p.Definition {
	t.Helper()
	base := syntheticResolution(t)
	bootstrap := *base.Bootstrap
	bootstrap.AutomationDigest = strings.Repeat(strconv.FormatInt(int64(revision%16), 16), 63) + strconv.FormatInt(int64(revision/16), 16)
	bootstrap, err := p.CanonicalBootstrap(bootstrap)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := p.NewResolvedDefinition(bootstrap, *base.Native)
	if err != nil {
		t.Fatal(err)
	}
	return definition
}

// retirable is what a setup at the bound gives up: every area a retained
// resolution names and every area already being retired, except the bundle
// the receipt names, the one the carry-forward reads and the new one.
func retirable(view p.StorageView, carried, target string) []string {
	var ids []string
	for _, held := range view.Areas {
		if held.ID == view.State.Receipt.CatalogDigest || held.ID == carried || held.ID == target {
			continue
		}
		if held.Retiring || slices.ContainsFunc(view.State.RetainedDefinitions, func(definition p.Definition) bool { return definition.CatalogDigest == held.ID }) {
			ids = append(ids, held.ID)
		}
	}
	return ids
}

// supersededResolutions is what a setup at the bound gives up of the bundles it
// keeps: every resolution naming the bundle the receipt names, the one the
// carry-forward reads or the new one, except the receipt's own, the new one
// and the latest naming each of them.
func supersededResolutions(view p.StorageView, carried string, definition p.Definition) []string {
	kept := []string{view.State.Receipt.CatalogDigest, carried, definition.CatalogDigest}
	needed := []string{definition.ResolutionDigest}
	if view.State.Receipt.Definition != nil {
		needed = append(needed, view.State.Receipt.Definition.ResolutionDigest)
	}
	var digests []string
	for index, retained := range view.State.RetainedDefinitions {
		if !slices.Contains(kept, retained.CatalogDigest) || slices.Contains(needed, retained.ResolutionDigest) {
			continue
		}
		if slices.ContainsFunc(view.State.RetainedDefinitions[index+1:], func(later p.Definition) bool { return later.CatalogDigest == retained.CatalogDigest }) {
			digests = append(digests, retained.ResolutionDigest)
		}
	}
	return digests
}

// strandedReceipt reports a pending receipt whose bundle holds no area while
// the host holds every area it may, which setup resumes only once room is made.
func strandedReceipt(view p.StorageView) bool {
	receipt := view.State.Receipt
	return receipt.Incomplete() && len(view.Areas) >= maxControllerBundles &&
		!slices.ContainsFunc(view.Areas, func(held p.HeldArea) bool { return held.ID == receipt.CatalogDigest })
}

// publishRevision drives one setup of a resolution through the store in the
// order setup takes: with purge, room first, then the new receipt under its
// durable intent, the bundle it names, and completion. Each step skips what an
// earlier attempt already made durable, so repeating it is the retry. A
// receipt is this resolution's own only when it carries it, since a new
// resolution can name the bundle an earlier receipt already names. Its own
// pending receipt is resumed, after room is made only when it is stranded.
func publishRevision(t *testing.T, store *Store, definition p.Definition, carried string, purge bool) error {
	t.Helper()
	ctx := context.Background()
	return store.MutateController(ctx, p.SetupContext{}, true, func(tx p.StorageTransaction) error {
		view := tx.Snapshot()
		own := view.State.Receipt.Definition != nil && view.State.Receipt.Definition.ResolutionDigest == definition.ResolutionDigest
		if ids := retirable(view, carried, definition.CatalogDigest); purge && len(ids) != 0 && (!own || strandedReceipt(view)) {
			if err := tx.RetireBundles(ctx, ids); err != nil {
				return err
			}
			// Setup decides from the snapshot what it may publish next, so
			// the snapshot must already present the areas as gone.
			if held := len(tx.Snapshot().Areas); held != len(view.Areas)-len(ids) {
				return errors.New("the snapshot still presents " + strconv.Itoa(held) + " areas after a retirement")
			}
		}
		if !own {
			if digests := supersededResolutions(tx.Snapshot(), carried, definition); purge && len(digests) != 0 {
				if err := tx.RetireResolutions(ctx, digests); err != nil {
					return err
				}
			}
			if _, err := tx.Publish(ctx, revisionReceipt(t, definition)); err != nil {
				return err
			}
		}
		if err := fillBundle(ctx, tx, definition.CatalogDigest); err != nil {
			return err
		}
		_, err := tx.Publish(ctx, completeControllerState(tx.Snapshot().State))
		return err
	})
}

// revisionReceipt is the new receipt a setup of definition publishes, its
// first action's intent already durable and the given further actions planned.
func revisionReceipt(t *testing.T, definition p.Definition, further ...p.SetupAction) p.HostState {
	t.Helper()
	value := syntheticControllerState(t, p.SetupContext{})
	value.Receipt.ID = "setup-" + definition.ResolutionDigest[:32]
	value.Receipt.CatalogDigest = definition.CatalogDigest
	value.Receipt.Definition = &definition
	value.Receipt.Sources = slices.Clone(definition.Sources)
	value.Receipt.Actions = append(value.Receipt.Actions, further...)
	value.Receipt.Actions[0].Phase = "intent"
	var err error
	if value.Receipt.PlanDigest, err = p.SetupPlanDigest(value.Host, value.Receipt); err != nil {
		t.Fatal(err)
	}
	return value
}

// fillBundle opens the bundle the receipt names and publishes its bytes,
// unless an earlier attempt already did.
func fillBundle(ctx context.Context, tx p.StorageTransaction, id string) error {
	area, err := tx.Bundle(ctx, id)
	if err != nil {
		return err
	}
	if content, err := area.Read(ctx, "sources/python.tar.gz", 64); err == nil && string(content) == "payload" {
		return nil
	}
	if err := area.EnsureDirectory(ctx, "sources"); err != nil {
		return err
	}
	return area.Write(ctx, "sources/python.tar.gz", []byte("payload"), false)
}

// failRevision drives one setup of definition to a definitive failure in the
// order setup takes: its new receipt, the bundle that receipt names prepared
// under its intent, then a native transaction that failed before any effect,
// which ends the receipt failed.
func failRevision(t *testing.T, store *Store, definition p.Definition) {
	t.Helper()
	ctx := context.Background()
	runtime := p.SetupAction{ID: "container-runtime", Request: []byte(`{"readyBefore":false}`), Phase: "planned", Evidence: []byte(`{}`)}
	err := store.MutateController(ctx, p.SetupContext{}, true, func(tx p.StorageTransaction) error {
		if _, err := tx.Publish(ctx, revisionReceipt(t, definition, runtime)); err != nil {
			return err
		}
		if err := fillBundle(ctx, tx, definition.CatalogDigest); err != nil {
			return err
		}
		value := tx.Snapshot().State
		value.Receipt.Actions[0].Phase, value.Receipt.Actions[0].Outcome, value.Receipt.Actions[0].Evidence = "observed", "changed", []byte(`{"ready":true}`)
		value.Receipt.Actions[1].Phase = "intent"
		if _, err := tx.Publish(ctx, value); err != nil {
			return err
		}
		value = tx.Snapshot().State
		value.Receipt.Actions[1].Phase, value.Receipt.Actions[1].Outcome, value.Receipt.Actions[1].Evidence = "observed", "failed", []byte(`{"installationEntered":false}`)
		value.Receipt.Status = "failed"
		_, err := tx.Publish(ctx, value)
		return err
	})
	if err != nil {
		t.Fatalf("the failing revision: %#v", diagnostics.Of(err))
	}
}

// boundFixture fills the store to its bound the way sixteen automation-only
// revisions would, each publishing and sealing its own bundle beside the
// resolution it carries. The receipt names the last of them.
func boundFixture(t *testing.T) (*Store, []p.Definition) {
	t.Helper()
	store, _ := fixture(t)
	revisions := make([]p.Definition, 0, maxControllerBundles)
	for revision := range maxControllerBundles {
		definition := automationRevision(t, revision)
		if err := publishRevision(t, store, definition, "", false); err != nil {
			t.Fatalf("revision %d: %#v", revision, diagnostics.Of(err))
		}
		revisions = append(revisions, definition)
	}
	return store, revisions
}

// readableBundle proves a bundle is still what a receipt or a carry-forward can
// read: the store opens it and its published bytes are intact.
func readableBundle(t *testing.T, store *Store, id, when string) {
	t.Helper()
	err := store.ReadController(context.Background(), "", func(view p.StorageView) error {
		area, err := view.OpenBundle(context.Background(), id)
		if err != nil || area == nil {
			return errors.Join(errors.New("the bundle cannot be opened"), err)
		}
		content, err := area.Read(context.Background(), "sources/python.tar.gz", 64)
		if err != nil || string(content) != "payload" {
			return errors.Join(errors.New("the bundle's published bytes are gone"), err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("bundle %s is not readable %s: %v %#v", id[:8], when, err, diagnostics.Of(err))
	}
}

func controllerReceipt(t *testing.T, store *Store) p.SetupReceipt {
	t.Helper()
	var receipt p.SetupReceipt
	if err := store.ReadController(context.Background(), "", func(view p.StorageView) error {
		receipt = view.State.Receipt
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return receipt
}

// Setup decides whether it must make room against its own copy of the bound,
// so it must be the bound this store enforces.
func TestSetupPlansRoomAgainstTheBoundTheStoreEnforces(t *testing.T) {
	if maxControllerBundles != p.MaxRetainedBundles {
		t.Fatalf("the store holds %d areas, setup plans for %d", maxControllerBundles, p.MaxRetainedBundles)
	}
}

// A host holding sixteen areas refuses an automation-only revision until the
// superseded areas are retired, and changes nothing while it refuses. Retiring
// all but the bundle the receipt names, which the carry-forward also reads,
// makes room, and the revision then publishes and completes.
func TestAnAutomationOnlyRevisionAtTheBoundCompletesAfterRetiringSupersededAreas(t *testing.T) {
	store, revisions := boundFixture(t)
	current, next := revisions[len(revisions)-1], automationRevision(t, len(revisions))
	if err := publishRevision(t, store, next, current.CatalogDigest, false); err == nil {
		t.Fatal("a seventeenth bundle was admitted")
	}
	if receipt := controllerReceipt(t, store); receipt.Status != "complete" || receipt.CatalogDigest != current.CatalogDigest {
		t.Fatalf("the refused revision moved the receipt: %#v", receipt)
	}
	if err := publishRevision(t, store, next, current.CatalogDigest, true); err != nil {
		t.Fatalf("the revision did not complete under retirement: %#v", diagnostics.Of(err))
	}
	if receipt := controllerReceipt(t, store); receipt.Status != "complete" || receipt.CatalogDigest != next.CatalogDigest {
		t.Fatalf("the revision did not become the receipt: %#v", receipt)
	}
	readableBundle(t, store, current.CatalogDigest, "after the revision")
	readableBundle(t, store, next.CatalogDigest, "after the revision")
	err := store.ReadController(context.Background(), "", func(view p.StorageView) error {
		want := []p.HeldArea{{ID: current.CatalogDigest}, {ID: next.CatalogDigest}}
		slices.SortFunc(want, func(a, b p.HeldArea) int { return strings.Compare(a.ID, b.ID) })
		if !slices.Equal(view.Areas, want) || len(view.State.RetainedDefinitions) != 2 {
			t.Fatalf("areas=%#v resolutions=%d", view.Areas, len(view.State.RetainedDefinitions))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, superseded := range revisions[:len(revisions)-1] {
		if _, err := os.Stat(bundlePath(store, superseded.CatalogDigest)); !os.IsNotExist(err) {
			t.Fatalf("superseded area %s survived: %v", superseded.CatalogDigest[:8], err)
		}
	}
}

// A setup that failed after its receipt reserved the sixteenth area leaves a
// receipt the next setup replaces, so the host is not stuck at its bound. The
// store refuses the retry until the superseded areas are retired, admits their
// retirement beside the failed receipt while still refusing the bundle that
// receipt names, and the retry then publishes and completes, whether it names
// a new bundle or the same one under a new resolution.
func TestARetryAfterAFailedSetupAtTheBoundCompletesAfterRetiringSupersededAreas(t *testing.T) {
	for name, retry := range map[string]func(*testing.T, p.Definition) p.Definition{
		"a new bundle": func(t *testing.T, _ p.Definition) p.Definition { return automationRevision(t, maxControllerBundles) },
		"the same bundle": func(t *testing.T, failing p.Definition) p.Definition {
			native := *failing.Native
			native.BeforeSHA256, native.AfterSHA256 = strings.Repeat("d", 64), strings.Repeat("d", 64)
			native, err := p.CanonicalNativePlan(native)
			if err != nil {
				t.Fatal(err)
			}
			definition, err := p.NewResolvedDefinition(*failing.Bootstrap, native)
			if err != nil || definition.CatalogDigest != failing.CatalogDigest || definition.ResolutionDigest == failing.ResolutionDigest {
				t.Fatalf("the retry's resolution does not name the same bundle anew: %v", err)
			}
			return definition
		},
	} {
		t.Run(name, func(t *testing.T) {
			store, _ := fixture(t)
			var superseded []string
			for revision := range maxControllerBundles - 1 {
				definition := automationRevision(t, revision)
				if err := publishRevision(t, store, definition, "", false); err != nil {
					t.Fatalf("revision %d: %#v", revision, diagnostics.Of(err))
				}
				superseded = append(superseded, definition.CatalogDigest)
			}
			failing := automationRevision(t, maxControllerBundles-1)
			failRevision(t, store, failing)
			if err := store.ReadController(context.Background(), "", func(view p.StorageView) error {
				if len(view.Areas) != maxControllerBundles || len(view.State.RetainedDefinitions) != maxControllerBundles || view.State.Receipt.Status != "failed" {
					t.Fatalf("the failed setup left areas=%d resolutions=%d receipt=%q", len(view.Areas), len(view.State.RetainedDefinitions), view.State.Receipt.Status)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			next := retry(t, failing)
			if err := publishRevision(t, store, next, "", false); err == nil {
				t.Fatal("the retry was admitted beyond the bound")
			}
			if err := retire(store, failing.CatalogDigest); err == nil {
				t.Fatal("the bundle the failed receipt names was retired")
			}
			if receipt := controllerReceipt(t, store); receipt.Status != "failed" || receipt.CatalogDigest != failing.CatalogDigest {
				t.Fatalf("a refusal moved the failed receipt: %#v", receipt)
			}
			if err := publishRevision(t, store, next, "", true); err != nil {
				t.Fatalf("the retry did not complete under retirement: %#v", diagnostics.Of(err))
			}
			if receipt := controllerReceipt(t, store); receipt.Status != "complete" || receipt.Definition.ResolutionDigest != next.ResolutionDigest {
				t.Fatalf("the retry did not become the receipt: %#v", receipt)
			}
			readableBundle(t, store, failing.CatalogDigest, "after the retry")
			readableBundle(t, store, next.CatalogDigest, "after the retry")
			for _, id := range superseded {
				if _, err := os.Stat(bundlePath(store, id)); !os.IsNotExist(err) {
					t.Fatalf("superseded area %s survived: %v", id[:8], err)
				}
			}
		})
	}
}

// sameBundleRevision is what a retry after a failed setup solves against the
// package inventory attempt names: the synthetic resolution's bundle under a
// resolution of its own.
func sameBundleRevision(t *testing.T, attempt int) p.Definition {
	t.Helper()
	base := syntheticResolution(t)
	native := *base.Native
	native.BeforeSHA256 = fmt.Sprintf("%064x", attempt+1)
	native.AfterSHA256 = native.BeforeSHA256
	native, err := p.CanonicalNativePlan(native)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := p.NewResolvedDefinition(*base.Bootstrap, native)
	if err != nil || definition.CatalogDigest != base.CatalogDigest || definition.ResolutionDigest == base.ResolutionDigest {
		t.Fatalf("attempt %d does not name the same bundle under a new resolution: %v", attempt, err)
	}
	return definition
}

// Each retry after a failed setup can name the bundle the failed receipt
// keeps under a new resolution, until every resolution the host may retain
// names that one bundle. The store refuses the next retry's receipt, admits
// the retirement of every resolution of that bundle but the receipt's own,
// retires no area, and the retry then publishes and completes over it.
func TestARetryAtTheResolutionBoundOfOneBundleCompletesAfterRetiringItsSupersededResolutions(t *testing.T) {
	store, _ := fixture(t)
	var resolutions []string
	for attempt := range maxControllerBundles {
		definition := sameBundleRevision(t, attempt)
		failRevision(t, store, definition)
		resolutions = append(resolutions, definition.ResolutionDigest)
	}
	bundle, next := sameBundleRevision(t, 0).CatalogDigest, sameBundleRevision(t, maxControllerBundles)
	if err := publishRevision(t, store, next, "", false); err == nil {
		t.Fatal("a resolution beyond the bound was admitted")
	}
	if receipt := controllerReceipt(t, store); receipt.Status != "failed" || receipt.Definition.ResolutionDigest != resolutions[len(resolutions)-1] {
		t.Fatalf("the refused retry moved the failed receipt: %#v", receipt)
	}
	if err := publishRevision(t, store, next, "", true); err != nil {
		t.Fatalf("the retry did not complete after retiring the superseded resolutions: %#v", diagnostics.Of(err))
	}
	if receipt := controllerReceipt(t, store); receipt.Status != "complete" || receipt.Definition.ResolutionDigest != next.ResolutionDigest {
		t.Fatalf("the retry did not become the receipt: %#v", receipt)
	}
	readableBundle(t, store, bundle, "after the retry")
	err := store.ReadController(context.Background(), "", func(view p.StorageView) error {
		var retained []string
		for _, definition := range view.State.RetainedDefinitions {
			retained = append(retained, definition.ResolutionDigest)
		}
		if !slices.Equal(view.Areas, []p.HeldArea{{ID: bundle}}) || !slices.Equal(retained, []string{resolutions[len(resolutions)-1], next.ResolutionDigest}) {
			t.Fatalf("areas=%#v resolutions=%v", view.Areas, retained)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// A receipt naming a bundle the store could never reserve would stay pending
// with nothing able to complete or replace it. The store refuses it while the
// record still holds the completed receipt, however the bound was reached.
func TestAReceiptWhoseBundleCannotBeReservedIsRefused(t *testing.T) {
	store, _ := fixture(t)
	for digit := range maxControllerBundles {
		publishBundle(t, store, strings.Repeat(strconv.FormatInt(int64(digit), 16), 64))
	}
	before := controllerReceipt(t, store)
	value := syntheticControllerState(t, p.SetupContext{})
	value.Receipt.ID = "setup-" + strings.Repeat("01", 16)
	value.Receipt.CatalogDigest = strings.Repeat("0123456789abcdef", 4)
	value.Receipt.Actions[0].Phase = "intent"
	var err error
	if value.Receipt.PlanDigest, err = p.SetupPlanDigest(value.Host, value.Receipt); err != nil {
		t.Fatal(err)
	}
	err = store.MutateController(context.Background(), p.SetupContext{}, false, func(tx p.StorageTransaction) error {
		outcome, err := tx.Publish(context.Background(), value)
		if outcome != p.NotCommitted {
			t.Fatalf("outcome = %s", outcome)
		}
		return err
	})
	if err == nil {
		t.Fatal("a receipt whose bundle cannot be reserved was published")
	}
	if after := controllerReceipt(t, store); after.ID != before.ID || after.Status != "complete" {
		t.Fatalf("the refusal moved the receipt: %#v", after)
	}
}

// nthInterrupt fails the nth time a checkpoint is reached, which is how one
// publication among several at the same checkpoint is interrupted.
type nthInterrupt struct {
	point     string
	nth, seen int
	fired     bool
}

func (i *nthInterrupt) install(store *Store) {
	store.fail = func(reached string) error {
		if reached != i.point || i.fired {
			return nil
		}
		if i.seen++; i.seen < i.nth {
			return nil
		}
		i.fired = true
		return errors.New("synthetic process death")
	}
}

// Interrupting a setup at the bound at each durable step, from recording the
// retirement through sealing the new bundle, leaves the bundle the receipt
// names and the carry-forward reads readable, and repeating it goes on from
// what the interruption left. Each repeat is interrupted one step further than
// the last, so one host at the bound is carried through every step.
func TestAnInterruptedRevisionAtTheBoundKeepsItsBundlesReadable(t *testing.T) {
	store, revisions := boundFixture(t)
	current, next := revisions[len(revisions)-1].CatalogDigest, automationRevision(t, len(revisions))
	retiring := func(count int) func(p.StorageView) bool {
		return func(view p.StorageView) bool {
			return len(view.Areas) == maxControllerBundles && len(slices.DeleteFunc(slices.Clone(view.Areas), func(held p.HeldArea) bool { return !held.Retiring })) == count
		}
	}
	receipt := func(id, status string) func(p.StorageView) bool {
		return func(view p.StorageView) bool {
			return view.State.Receipt.CatalogDigest == id && view.State.Receipt.Status == status
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
		{"after-controller-rename", 2, func(view p.StorageView) bool { return slices.Equal(view.Areas, []p.HeldArea{{ID: current}}) }},
		{"after-controller-rename", 1, receipt(next.CatalogDigest, "pending")},
		{"after-controller-bundle-directory", 1, receipt(next.CatalogDigest, "pending")},
		{"before-controller-bundle-sync", 1, receipt(next.CatalogDigest, "pending")},
		{"after-controller-rename", 1, receipt(next.CatalogDigest, "complete")},
	} {
		when := "after an interruption at " + step.point + " #" + strconv.Itoa(step.nth)
		injection := &nthInterrupt{point: step.point, nth: step.nth}
		injection.install(store)
		err := publishRevision(t, store, next, current, true)
		store.fail = nil
		if !injection.fired || err == nil {
			t.Fatalf("%s: fired=%t err=%v", when, injection.fired, err)
		}
		readableBundle(t, store, current, when)
		if err := store.ReadController(context.Background(), "", func(view p.StorageView) error {
			if !step.left(view) {
				t.Fatalf("%s the host holds %#v under %#v", when, view.Areas, view.State.Receipt)
			}
			return nil
		}); err != nil {
			t.Fatalf("%s: %#v", when, diagnostics.Of(err))
		}
	}
	if err := publishRevision(t, store, next, current, true); err != nil {
		t.Fatalf("repeating the revision: %#v", diagnostics.Of(err))
	}
	readableBundle(t, store, current, "once completed")
	readableBundle(t, store, next.CatalogDigest, "once completed")
}
