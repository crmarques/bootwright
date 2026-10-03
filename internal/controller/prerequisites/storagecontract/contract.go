// Package storagecontract is the shared contract suite for
// prerequisites.Storage. Every implementation's tests run it, the in-memory
// doubles included, so a double cannot accept what the Workspace adapter
// refuses. No production package imports it.
package storagecontract

import (
	"bytes"
	"context"
	"fmt"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

// Subject is one fresh store that holds the ready context Scope names and no
// controller state; every mutation runs in Scope. Unsettle makes the store's
// next receipt publication land and then end with an unknown outcome, as a
// sync that fails after its rename does.
type Subject struct {
	Storage  prerequisites.Storage
	Scope    prerequisites.SetupContext
	Unsettle func()
}

// Within provides one fresh Subject and fails the test when it cannot.
type Within func(t *testing.T) Subject

// Verify holds one implementation to every clause, each against its own store.
func Verify(t *testing.T, within Within) {
	t.Helper()
	for _, clause := range []struct {
		name  string
		check func(*testing.T, Subject)
	}{
		{"each entry point refuses a missing callback", eachEntryPointRefusesAMissingCallback},
		{"a read names the baseline or a context the store holds", aReadNamesTheBaselineOrAContextTheStoreHolds},
		{"a publication is what the transaction and every later read hold", aPublicationIsWhatEveryLaterReadHolds},
		{"what a caller holds is a copy", whatACallerHoldsIsACopy},
		{"a bundle opens only for the approved catalog under a durable intent", aBundleOpensOnlyUnderADurableIntent},
		{"a bundle identity is a digest", aBundleIdentityIsADigest},
		{"a receipt belongs to its scope and host and a pending one is never replaced", aReceiptBelongsToItsScopeAndHost},
		{"a receipt's progress only moves forward", aReceiptsProgressOnlyMovesForward},
		{"an unknown publication ends the transaction's publications", anUnknownPublicationEndsThePublications},
		{"a retirement removes only superseded bundles", aRetirementRemovesOnlySupersededBundles},
		{"a retirement after a failed setup keeps the bundle its receipt names", aRetirementAfterAFailedSetupKeepsItsBundle},
		{"a retirement of a bundle the store never held keeps the resolution naming it", aRetirementOfABundleNeverHeldKeepsItsResolution},
		{"a resolution retirement drops the last one naming a bundle the store never held", aResolutionRetirementDropsOneNamingABundleNeverHeld},
		{"a setup run opens only under a durable intent and is numbered upward", aSetupRunOpensOnlyUnderADurableIntent},
		{"a resolution retirement keeps the receipt's resolution and every bundle named", aResolutionRetirementKeepsEveryBundleNamed},
	} {
		t.Run(clause.name, func(t *testing.T) {
			subject := within(t)
			if subject.Storage == nil || subject.Scope.Name == "" || subject.Unsettle == nil {
				t.Fatal("the runner provided no complete store")
			}
			clause.check(t, subject)
		})
	}
}

func eachEntryPointRefusesAMissingCallback(t *testing.T, s Subject) {
	ctx := context.Background()
	if err := s.Storage.ReadController(ctx, "", nil); err == nil {
		t.Fatal("a controller read without a callback succeeded")
	}
	if err := s.Storage.MutateController(ctx, s.Scope, true, nil); err == nil {
		t.Fatal("a controller mutation without a callback succeeded")
	}
}

func aReadNamesTheBaselineOrAContextTheStoreHolds(t *testing.T, s Subject) {
	read(t, s, "", func(view prerequisites.StorageView) {
		if view.Context != (prerequisites.SetupContext{}) {
			t.Fatalf("a baseline read names context %+v", view.Context)
		}
	})
	read(t, s, s.Scope.Name, func(view prerequisites.StorageView) {
		if view.Context.Name != s.Scope.Name || view.Context.Revision != s.Scope.Revision {
			t.Fatalf("a read of %s names context %+v", s.Scope.Name, view.Context)
		}
	})
	for _, name := range []string{"absent", "Not A Name"} {
		called := false
		if err := s.Storage.ReadController(context.Background(), name, func(prerequisites.StorageView) error {
			called = true
			return nil
		}); err == nil || called {
			t.Fatalf("a read of context %q = %v, called back %t", name, err, called)
		}
	}
}

func aPublicationIsWhatEveryLaterReadHolds(t *testing.T, s Subject) {
	value := planned(t, s.Scope)
	mutate(t, s, func(tx prerequisites.StorageTransaction) {
		publishes(t, tx, value)
		holds(t, tx.Snapshot().State, value, "the transaction's snapshot")
	})
	read(t, s, "", func(view prerequisites.StorageView) { holds(t, view.State, value, "a later read") })
	intended := intent(t, s.Scope)
	mutate(t, s, func(tx prerequisites.StorageTransaction) {
		holds(t, tx.Snapshot().State, value, "a later transaction's snapshot")
		publishes(t, tx, intended)
		holds(t, tx.Snapshot().State, intended, "the snapshot after the intent")
	})
	read(t, s, s.Scope.Name, func(view prerequisites.StorageView) { holds(t, view.State, intended, "a later named read") })
}

func whatACallerHoldsIsACopy(t *testing.T, s Subject) {
	published, original := planned(t, s.Scope), planned(t, s.Scope)
	mutate(t, s, func(tx prerequisites.StorageTransaction) {
		publishes(t, tx, published)
		changes(&published)
		holds(t, tx.Snapshot().State, original, "the snapshot after the published value changed")
		snapshot := tx.Snapshot()
		changes(&snapshot.State)
		holds(t, tx.Snapshot().State, original, "the snapshot after an earlier snapshot changed")
	})
	read(t, s, "", func(view prerequisites.StorageView) { changes(&view.State) })
	read(t, s, "", func(view prerequisites.StorageView) {
		holds(t, view.State, original, "a read after an earlier read changed")
	})
}

func aBundleOpensOnlyUnderADurableIntent(t *testing.T, s Subject) {
	ctx := context.Background()
	value, intended := planned(t, s.Scope), intent(t, s.Scope)
	catalog := value.Receipt.CatalogDigest
	mutate(t, s, func(tx prerequisites.StorageTransaction) {
		opensNothing(t, tx, catalog, "a bundle before any receipt")
		publishes(t, tx, value)
		opensNothing(t, tx, catalog, "a bundle of a planned receipt")
		publishes(t, tx, intended)
		opensNothing(t, tx, strings.Repeat("b", 64), "a bundle of another catalog")
		opensNothing(t, tx, "not-a-digest", "a bundle named by no digest")
		area, err := tx.Bundle(ctx, catalog)
		if err != nil || area == nil {
			t.Fatalf("the approved catalog's bundle under a durable intent = %v (%v)", area, err)
		}
	})
}

func aBundleIdentityIsADigest(t *testing.T, s Subject) {
	ctx := context.Background()
	unheld := strings.Repeat("c", 64)
	read(t, s, "", func(view prerequisites.StorageView) {
		if area, err := view.OpenBundle(ctx, "not-a-digest"); err == nil {
			t.Fatalf("a read opened bundle %v by no digest", area)
		}
		if area, err := view.OpenBundle(ctx, unheld); err != nil || area != nil {
			t.Fatalf("a read of a bundle no store holds = %v (%v), want none", area, err)
		}
	})
	mutate(t, s, func(tx prerequisites.StorageTransaction) {
		publishes(t, tx, planned(t, s.Scope))
		if area, err := tx.Snapshot().OpenBundle(ctx, "not-a-digest"); err == nil {
			t.Fatalf("a snapshot opened bundle %v by no digest", area)
		}
		if area, err := tx.Snapshot().OpenBundle(ctx, unheld); err != nil || area != nil {
			t.Fatalf("a snapshot of a bundle no store holds = %v (%v), want none", area, err)
		}
	})
}

func aReceiptBelongsToItsScopeAndHost(t *testing.T, s Subject) {
	value := planned(t, s.Scope)
	foreign := next(t, s.Scope)
	foreign.Host = otherHost(t)
	foreign.Receipt.PlanDigest = digest(t, foreign)
	mutate(t, s, func(tx prerequisites.StorageTransaction) {
		refusesPublication(t, tx, planned(t, prerequisites.SetupContext{}), "a receipt of another scope")
		publishes(t, tx, value)
		refusesPublication(t, tx, next(t, s.Scope), "a new receipt over a pending one")
		publishes(t, tx, complete(value))
		refusesPublication(t, tx, foreign, "a new receipt of another host")
		holds(t, tx.Snapshot().State, complete(value), "the snapshot after refused publications")
	})
	read(t, s, "", func(view prerequisites.StorageView) {
		holds(t, view.State, complete(value), "a read after refused publications")
	})
}

func aReceiptsProgressOnlyMovesForward(t *testing.T, s Subject) {
	value, intended := planned(t, s.Scope), intent(t, s.Scope)
	unresolved := intent(t, s.Scope)
	unresolved.Receipt.Actions[0].Phase, unresolved.Receipt.Actions[0].Outcome = "observed", "unknown"
	unresolved.Receipt.Actions[0].Evidence = []byte(`{"ready":false}`)
	replanned := intent(t, s.Scope)
	replanned.Receipt.Actions[0].Request = []byte(`{"dependency":"another-python"}`)
	replanned.Receipt.PlanDigest = digest(t, replanned)
	completed, reopened, changed := complete(intended), complete(intended), complete(intended)
	reopened.Receipt.Status = "pending"
	changed.Receipt.Actions[0].Outcome = "changed"
	mutate(t, s, func(tx prerequisites.StorageTransaction) {
		publishes(t, tx, value)
		publishes(t, tx, intended)
		refusesPublication(t, tx, value, "an intent returned to planned")
		refusesPublication(t, tx, replanned, "the same receipt under another plan")
		publishes(t, tx, unresolved)
		refusesPublication(t, tx, intended, "an unresolved effect presumed not to have happened")
		publishes(t, tx, completed)
		refusesPublication(t, tx, reopened, "a completed receipt made pending again")
		refusesPublication(t, tx, changed, "a verified postcondition replaced")
		holds(t, tx.Snapshot().State, completed, "the snapshot after refused publications")
	})
	read(t, s, "", func(view prerequisites.StorageView) {
		holds(t, view.State, completed, "a read after refused publications")
	})
}

func anUnknownPublicationEndsThePublications(t *testing.T, s Subject) {
	ctx := context.Background()
	value, intended := planned(t, s.Scope), intent(t, s.Scope)
	mutate(t, s, func(tx prerequisites.StorageTransaction) { publishes(t, tx, value) })
	mutate(t, s, func(tx prerequisites.StorageTransaction) {
		s.Unsettle()
		if outcome, err := tx.Publish(ctx, intended); err == nil || outcome != prerequisites.Unknown {
			t.Fatalf("an unsettled publication = %s (%v), want unknown", outcome, err)
		}
		if outcome, err := tx.Publish(ctx, intended); err == nil || outcome == prerequisites.Committed {
			t.Fatalf("a publication after an unknown one = %s (%v)", outcome, err)
		}
		if area, err := tx.Bundle(ctx, value.Receipt.CatalogDigest); err == nil {
			t.Fatalf("a bundle opened after an unknown publication: %v", area)
		}
		refuses(t, tx.RetireBundles(ctx, nil), "a retirement after an unknown publication")
		refuses(t, tx.RetireResolutions(ctx, nil), "a resolution retirement after an unknown publication")
	})
	read(t, s, "", func(view prerequisites.StorageView) {
		if view.State.Receipt.Actions[0].Phase != "planned" {
			holds(t, view.State, intended, "a read after an unknown publication")
		} else {
			holds(t, view.State, value, "a read after an unknown publication")
		}
	})
}

func aRetirementRemovesOnlySupersededBundles(t *testing.T, s Subject) {
	ctx := context.Background()
	first, second := resolved(t, s.Scope, "1"), resolved(t, s.Scope, "2")
	superseded, current, absent := first.Receipt.CatalogDigest, second.Receipt.CatalogDigest, strings.Repeat("c", 64)
	mutate(t, s, func(tx prerequisites.StorageTransaction) {
		publishes(t, tx, first)
		refuses(t, tx.RetireBundles(ctx, []string{absent}), "a retirement while the receipt is pending")
		opens(t, tx, superseded)
		publishes(t, tx, complete(first))
	})
	mutate(t, s, func(tx prerequisites.StorageTransaction) {
		publishes(t, tx, second)
		opens(t, tx, current)
		publishes(t, tx, complete(second))
		refuses(t, tx.RetireBundles(ctx, []string{current}), "a retirement of the bundle the receipt names")
		refuses(t, tx.RetireBundles(ctx, []string{"not-a-digest"}), "a retirement named by no digest")
		succeeds(t, tx.RetireBundles(ctx, []string{absent}), "a retirement of an area the store does not hold")
		holdsAreas(t, tx.Snapshot(), superseded, current)
		succeeds(t, tx.RetireBundles(ctx, []string{superseded}), "a retirement of a superseded bundle")
		holdsAreas(t, tx.Snapshot(), current)
	})
	read(t, s, "", func(view prerequisites.StorageView) {
		holdsAreas(t, view, current)
		if area, err := view.OpenBundle(ctx, superseded); err != nil || area != nil {
			t.Fatalf("a retired bundle = %v (%v), want none", area, err)
		}
		if area, err := view.OpenBundle(ctx, current); err != nil || area == nil {
			t.Fatalf("the bundle the receipt names = %v (%v)", area, err)
		}
	})
}

// A setup that failed is replaced by the next one, so its receipt admits a
// retirement as a completed one does, and still keeps the bundle it names.
func aRetirementAfterAFailedSetupKeepsItsBundle(t *testing.T, s Subject) {
	ctx := context.Background()
	first, second := resolved(t, s.Scope, "1"), resolved(t, s.Scope, "2")
	superseded, current := first.Receipt.CatalogDigest, second.Receipt.CatalogDigest
	mutate(t, s, func(tx prerequisites.StorageTransaction) {
		publishes(t, tx, first)
		opens(t, tx, superseded)
		publishes(t, tx, complete(first))
	})
	mutate(t, s, func(tx prerequisites.StorageTransaction) {
		publishes(t, tx, second)
		opens(t, tx, current)
		publishes(t, tx, failed(second))
		refuses(t, tx.RetireBundles(ctx, []string{current}), "a retirement of the bundle a failed receipt names")
		holdsAreas(t, tx.Snapshot(), superseded, current)
		succeeds(t, tx.RetireBundles(ctx, []string{superseded}), "a retirement of a superseded bundle after a failed setup")
		holdsAreas(t, tx.Snapshot(), current)
	})
	read(t, s, "", func(view prerequisites.StorageView) {
		holdsAreas(t, view, current)
		if view.State.Receipt.Status != "failed" {
			t.Fatalf("the retirement moved the failed receipt to %q", view.State.Receipt.Status)
		}
		if area, err := view.OpenBundle(ctx, current); err != nil || area == nil {
			t.Fatalf("the bundle the failed receipt names = %v (%v)", area, err)
		}
	})
}

// A setup canceled before its bundle held an area leaves the resolution its
// receipt carries naming a bundle the store never held, and that resolution
// stays retained once a later receipt replaces the canceled one. Naming that
// bundle in a retirement removes nothing, the resolution naming it included.
func aRetirementOfABundleNeverHeldKeepsItsResolution(t *testing.T, s Subject) {
	ctx := context.Background()
	first, second := resolved(t, s.Scope, "1"), resolved(t, s.Scope, "2")
	never, current := first.Receipt.CatalogDigest, second.Receipt.CatalogDigest
	both := []string{first.Receipt.Definition.ResolutionDigest, second.Receipt.Definition.ResolutionDigest}
	mutate(t, s, func(tx prerequisites.StorageTransaction) {
		publishes(t, tx, first)
		publishes(t, tx, canceled(first))
	})
	mutate(t, s, func(tx prerequisites.StorageTransaction) {
		publishes(t, tx, second)
		opens(t, tx, current)
		publishes(t, tx, complete(second))
		succeeds(t, tx.RetireBundles(ctx, []string{never}), "a retirement of a bundle the store never held")
		retains(t, tx.Snapshot(), both...)
		holdsAreas(t, tx.Snapshot(), current)
	})
	read(t, s, "", func(view prerequisites.StorageView) {
		retains(t, view, both...)
		holdsAreas(t, view, current)
	})
}

// While a canceled receipt is the receipt, the resolution it carries stays.
// Once a later receipt replaces it, that resolution names a bundle the store
// never held, so it identifies no area and a resolution retirement drops it
// alone, although no other resolution names its bundle.
func aResolutionRetirementDropsOneNamingABundleNeverHeld(t *testing.T, s Subject) {
	ctx := context.Background()
	first, second := resolved(t, s.Scope, "1"), resolved(t, s.Scope, "2")
	abandoned, current := first.Receipt.Definition.ResolutionDigest, second.Receipt.Definition.ResolutionDigest
	mutate(t, s, func(tx prerequisites.StorageTransaction) {
		publishes(t, tx, first)
		publishes(t, tx, canceled(first))
		refuses(t, tx.RetireResolutions(ctx, []string{abandoned}), "a retirement of the resolution the canceled receipt carries")
	})
	mutate(t, s, func(tx prerequisites.StorageTransaction) {
		publishes(t, tx, second)
		opens(t, tx, second.Receipt.CatalogDigest)
		publishes(t, tx, complete(second))
		succeeds(t, tx.RetireResolutions(ctx, []string{abandoned}), "a retirement of the last resolution naming a bundle the store never held")
		retains(t, tx.Snapshot(), current)
		holdsAreas(t, tx.Snapshot(), second.Receipt.CatalogDigest)
	})
	read(t, s, "", func(view prerequisites.StorageView) {
		retains(t, view, current)
		holdsAreas(t, view, second.Receipt.CatalogDigest)
	})
}

// aSetupRunOpensOnlyUnderADurableIntent holds a setup run to the receipt that
// intends the Ansible it keeps: none opens before an action's intent is
// durable or after the receipt completes, and runs keep being numbered upward
// once the oldest are no longer kept.
func aSetupRunOpensOnlyUnderADurableIntent(t *testing.T, s Subject) {
	ctx := context.Background()
	value, intended := planned(t, s.Scope), intent(t, s.Scope)
	mutate(t, s, func(tx prerequisites.StorageTransaction) {
		opensNoRun(t, tx, "a run before any receipt")
		publishes(t, tx, value)
		opensNoRun(t, tx, "a run of a planned receipt")
		publishes(t, tx, intended)
		for number := 1; number <= 10; number++ {
			run, err := tx.OpenRun(ctx)
			if err != nil || run == nil {
				t.Fatalf("setup run %d under a durable intent = %v (%v)", number, run, err)
			}
			if want := fmt.Sprintf("setup-%06d", number); path.Base(run.Location()) != want {
				t.Fatalf("setup run %d is at %q, want a directory named %s", number, run.Location(), want)
			}
			if written, err := run.Write([]byte("TASK [synthetic]\n")); err != nil || written != len("TASK [synthetic]\n") {
				t.Fatalf("setup run %d accepted %d bytes (%v)", number, written, err)
			}
			succeeds(t, run.Close(), "closing a setup run")
		}
		publishes(t, tx, complete(intended))
		opensNoRun(t, tx, "a run of a completed receipt")
	})
}

func opensNoRun(t *testing.T, tx prerequisites.StorageTransaction, what string) {
	t.Helper()
	if run, err := tx.OpenRun(context.Background()); err == nil {
		t.Fatalf("%s opened %v", what, run.Location())
	}
}

// A setup can name the bundle an earlier one names under a new resolution. A
// resolution of that kept bundle is retired alone, and the area stays; the
// store refuses the resolution the receipt carries and the last one naming a
// bundle it holds, so that bundle stays identifiable as an execution bundle.
func aResolutionRetirementKeepsEveryBundleNamed(t *testing.T, s Subject) {
	ctx := context.Background()
	first := resolved(t, s.Scope, "1")
	second := reresolved(t, first, "2")
	other := resolved(t, s.Scope, "3")
	bundle, earlier, current := first.Receipt.CatalogDigest, first.Receipt.Definition.ResolutionDigest, second.Receipt.Definition.ResolutionDigest
	mutate(t, s, func(tx prerequisites.StorageTransaction) {
		publishes(t, tx, first)
		refuses(t, tx.RetireResolutions(ctx, []string{earlier}), "a resolution retirement while the receipt is pending")
		opens(t, tx, bundle)
		publishes(t, tx, failed(first))
		refuses(t, tx.RetireResolutions(ctx, []string{earlier}), "a retirement of the resolution the receipt carries")
	})
	mutate(t, s, func(tx prerequisites.StorageTransaction) {
		publishes(t, tx, second)
		opens(t, tx, bundle)
		publishes(t, tx, complete(second))
		retains(t, tx.Snapshot(), earlier, current)
		refuses(t, tx.RetireResolutions(ctx, []string{current}), "a retirement of the resolution the receipt carries")
		refuses(t, tx.RetireResolutions(ctx, []string{"not-a-digest"}), "a resolution retirement named by no digest")
		succeeds(t, tx.RetireResolutions(ctx, []string{strings.Repeat("c", 64)}), "a retirement of a resolution the store does not hold")
		retains(t, tx.Snapshot(), earlier, current)
		succeeds(t, tx.RetireResolutions(ctx, []string{earlier}), "a retirement of a superseded resolution of the kept bundle")
		retains(t, tx.Snapshot(), current)
		holdsAreas(t, tx.Snapshot(), bundle)
	})
	mutate(t, s, func(tx prerequisites.StorageTransaction) {
		publishes(t, tx, other)
		opens(t, tx, other.Receipt.CatalogDigest)
		publishes(t, tx, complete(other))
		refuses(t, tx.RetireResolutions(ctx, []string{current}), "a retirement of the last resolution naming a held bundle")
		retains(t, tx.Snapshot(), current, other.Receipt.Definition.ResolutionDigest)
	})
	read(t, s, "", func(view prerequisites.StorageView) {
		retains(t, view, current, other.Receipt.Definition.ResolutionDigest)
		holdsAreas(t, view, bundle, other.Receipt.CatalogDigest)
		if area, err := view.OpenBundle(ctx, bundle); err != nil || area == nil {
			t.Fatalf("the bundle whose superseded resolution was retired = %v (%v)", area, err)
		}
	})
}

// retains requires view to retain exactly the resolutions digests names, in
// any order.
func retains(t *testing.T, view prerequisites.StorageView, digests ...string) {
	t.Helper()
	retained := []string{}
	for _, definition := range view.State.RetainedDefinitions {
		retained = append(retained, definition.ResolutionDigest)
	}
	slices.Sort(retained)
	if want := slices.Sorted(slices.Values(digests)); !slices.Equal(retained, want) {
		t.Fatalf("retained resolutions = %v, want %v", retained, want)
	}
}

// planned is one pending setup receipt for scope whose single action is
// planned, as setup publishes before it holds any intent.
func planned(t *testing.T, scope prerequisites.SetupContext) prerequisites.HostState {
	t.Helper()
	host, err := controller.NewInstalledHostIdentity(controller.LinuxInstalledIdentityV1,
		"0123456789abcdef0123456789abcdef", "12345678-1234-5678-9abc-def012345678", "fedcba98-7654-3210-fedc-ba9876543210")
	if err != nil {
		t.Fatal(err)
	}
	value := prerequisites.HostState{Host: host, Bindings: []prerequisites.ControllerBinding{}, RetainedSources: []prerequisites.DependencySource{}, Receipt: prerequisites.SetupReceipt{
		ID: "setup-" + strings.Repeat("1", 32), CatalogDigest: strings.Repeat("a", 64), Context: scope,
		Egress: prerequisites.SetupEgress{NoProxy: []string{}}, Sources: []prerequisites.DependencySource{},
		Actions: []prerequisites.SetupAction{{ID: "baseline-bundle", Request: []byte(`{"dependency":"synthetic-python"}`), Phase: "planned", Evidence: []byte(`{}`)}}, Status: "pending",
	}}
	if value.Receipt.PlanDigest, err = prerequisites.SetupPlanDigest(value.Host, value.Receipt); err != nil {
		t.Fatal(err)
	}
	return value
}

// intent is that receipt once its action's intent is durable, which is what
// authorizes the action's effects.
func intent(t *testing.T, scope prerequisites.SetupContext) prerequisites.HostState {
	value := planned(t, scope)
	value.Receipt.Actions[0].Phase = "intent"
	return value
}

// next is a later setup for scope: a new receipt naming another catalog,
// planned, as setup publishes once the earlier receipt is complete.
func next(t *testing.T, scope prerequisites.SetupContext) prerequisites.HostState {
	value := planned(t, scope)
	value.Receipt.ID, value.Receipt.CatalogDigest = "setup-"+strings.Repeat("2", 32), strings.Repeat("d", 64)
	value.Receipt.PlanDigest = digest(t, value)
	return value
}

// complete is value once every action proved its postcondition.
func complete(value prerequisites.HostState) prerequisites.HostState {
	value.Receipt.Status = "complete"
	value.Receipt.Actions = slices.Clone(value.Receipt.Actions)
	for index := range value.Receipt.Actions {
		value.Receipt.Actions[index].Phase, value.Receipt.Actions[index].Outcome = "observed", "unchanged"
		value.Receipt.Actions[index].Evidence = []byte(`{"ready":true}`)
	}
	return value
}

// failed is value once its action failed definitively, which ends its setup.
func failed(value prerequisites.HostState) prerequisites.HostState {
	value.Receipt.Status = "failed"
	value.Receipt.Actions = slices.Clone(value.Receipt.Actions)
	for index := range value.Receipt.Actions {
		value.Receipt.Actions[index].Phase, value.Receipt.Actions[index].Outcome = "observed", "failed"
		value.Receipt.Actions[index].Evidence = []byte(`{"ready":false}`)
	}
	return value
}

// canceled is value once setup abandoned it before its intended action took
// effect: that action observed as never started, because no area holds the
// bundle the receipt names, which ends the receipt canceled.
func canceled(value prerequisites.HostState) prerequisites.HostState {
	value.Receipt.Status = "canceled"
	value.Receipt.Actions = slices.Clone(value.Receipt.Actions)
	value.Receipt.Actions[0].Phase, value.Receipt.Actions[0].Outcome = "observed", "canceled"
	value.Receipt.Actions[0].Evidence = []byte(`{"bundleArea":"absent"}`)
	return value
}

func digest(t *testing.T, value prerequisites.HostState) string {
	t.Helper()
	planned, err := prerequisites.SetupPlanDigest(value.Host, value.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	return planned
}

func otherHost(t *testing.T) controller.InstalledHostIdentity {
	t.Helper()
	host, err := controller.NewInstalledHostIdentity(controller.LinuxInstalledIdentityV1,
		"fedcba9876543210fedcba9876543210", "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", "bbbbbbbb-cccc-4ddd-8eee-ffffffffffff")
	if err != nil {
		t.Fatal(err)
	}
	return host
}

func changes(state *prerequisites.HostState) {
	state.Receipt.Actions[0].Request[2] ^= 0xff
	state.Receipt.Actions[0].Evidence[0] ^= 0xff
	state.Receipt.Actions[0].Phase = "observed"
	state.Receipt.Status = "complete"
	state.Receipt.ID = "changed"
}

func read(t *testing.T, s Subject, name string, use func(prerequisites.StorageView)) {
	t.Helper()
	called := false
	if err := s.Storage.ReadController(context.Background(), name, func(view prerequisites.StorageView) error {
		called = true
		use(view)
		return nil
	}); err != nil || !called {
		t.Fatalf("a controller read of %q failed (%v) or never called back", name, err)
	}
}

func mutate(t *testing.T, s Subject, use func(prerequisites.StorageTransaction)) {
	t.Helper()
	called := false
	if err := s.Storage.MutateController(context.Background(), s.Scope, true, func(tx prerequisites.StorageTransaction) error {
		called = true
		use(tx)
		return nil
	}); err != nil || !called {
		t.Fatalf("a controller mutation failed (%v) or never called back", err)
	}
}

func publishes(t *testing.T, tx prerequisites.StorageTransaction, value prerequisites.HostState) {
	t.Helper()
	if outcome, err := tx.Publish(context.Background(), value); err != nil || outcome != prerequisites.Committed {
		t.Fatalf("a publication = %s (%v), want committed", outcome, err)
	}
}

func refusesPublication(t *testing.T, tx prerequisites.StorageTransaction, value prerequisites.HostState, what string) {
	t.Helper()
	if outcome, err := tx.Publish(context.Background(), value); err == nil || outcome != prerequisites.NotCommitted {
		t.Fatalf("%s = %s (%v), want a refusal that commits nothing", what, outcome, err)
	}
}

func opens(t *testing.T, tx prerequisites.StorageTransaction, id string) {
	t.Helper()
	if area, err := tx.Bundle(context.Background(), id); err != nil || area == nil {
		t.Fatalf("the bundle of the approved catalog under a durable intent = %v (%v)", area, err)
	}
}

// holdsAreas requires view to report exactly the areas ids names, none retiring.
func holdsAreas(t *testing.T, view prerequisites.StorageView, ids ...string) {
	t.Helper()
	reported := []string{}
	for _, area := range view.Areas {
		if area.Retiring {
			t.Fatalf("area %s is still retiring", area.ID)
		}
		reported = append(reported, area.ID)
	}
	slices.Sort(reported)
	want := slices.Sorted(slices.Values(ids))
	if !slices.Equal(reported, want) {
		t.Fatalf("held areas = %v, want %v", reported, want)
	}
}

func succeeds(t *testing.T, err error, what string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s failed: %v", what, err)
	}
}

func refuses(t *testing.T, err error, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s succeeded", what)
	}
}

func opensNothing(t *testing.T, tx prerequisites.StorageTransaction, id, what string) {
	t.Helper()
	if area, err := tx.Bundle(context.Background(), id); err == nil {
		t.Fatalf("%s opened %v", what, area)
	}
}

func holds(t *testing.T, got, want prerequisites.HostState, what string) {
	t.Helper()
	if !got.Host.Equal(want.Host) || got.Receipt.ID != want.Receipt.ID || got.Receipt.PlanDigest != want.Receipt.PlanDigest ||
		got.Receipt.Status != want.Receipt.Status || got.Receipt.Context != want.Receipt.Context || got.Receipt.CatalogDigest != want.Receipt.CatalogDigest ||
		len(got.Receipt.Actions) != len(want.Receipt.Actions) {
		t.Fatalf("%s holds receipt %+v, want %+v", what, got.Receipt, want.Receipt)
	}
	for index, action := range want.Receipt.Actions {
		held := got.Receipt.Actions[index]
		if held.ID != action.ID || held.Phase != action.Phase || held.Outcome != action.Outcome || !bytes.Equal(held.Request, action.Request) || !bytes.Equal(held.Evidence, action.Evidence) {
			t.Fatalf("%s holds action %+v, want %+v", what, held, action)
		}
	}
}
