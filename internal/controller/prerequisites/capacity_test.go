package prerequisites

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

const (
	boundRefusal     = "this host already retains the 16 bundle areas or resolutions it may hold, so the new execution bundle has no room"
	boundRemediation = "run bootwright setup --purge-old-bundles to retire the superseded execution bundles first"
	keptRefusal      = "only client areas and the current execution bundle hold this host's 16 bundle areas or resolutions, and neither is ever retired, so the new execution bundle has no room"
	keptRemediation  = "no command of this build frees that room, because it never retires a client area; keep using the build whose execution bundle this host holds, or set this build up on another controller host"
)

// supersede gives a host that completed one setup count superseded execution
// bundles, each an area carried by a retained resolution, retained before the
// resolution the store kept when it published that setup's receipt. It returns
// them, sorted.
func supersede(f *fixture, count int) []string {
	var superseded []string
	var retained []Definition
	for index := range count {
		id := strings.Repeat(strconv.FormatInt(int64(index), 16), 64)
		superseded = append(superseded, id)
		retained = append(retained, Definition{CatalogDigest: id})
		f.store.areas = append(f.store.areas, HeldArea{ID: id})
	}
	f.store.state.RetainedDefinitions = append(retained, f.store.state.RetainedDefinitions...)
	slices.SortFunc(f.store.areas, func(a, b HeldArea) int { return strings.Compare(a.ID, b.ID) })
	return superseded
}

// atTheBound fails the test unless the host holds exactly its bound of areas
// and of retained resolutions.
func atTheBound(t *testing.T, f *fixture) {
	t.Helper()
	if len(f.store.areas) != MaxRetainedBundles || len(f.store.state.RetainedDefinitions) != MaxRetainedBundles {
		t.Fatalf("the host holds %d areas and %d resolutions, not its bound", len(f.store.areas), len(f.store.state.RetainedDefinitions))
	}
}

// fullHost completes one setup, then fills the host to its bound with
// superseded execution bundles beside the bundle the receipt names. The next
// setup runs an executable whose automation moved, so it must publish a new
// bundle, carried forward from the one the receipt names.
func fullHost(t *testing.T) (*fixture, string, []string) {
	t.Helper()
	f, r := dynamicFixture(t)
	if _, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	current := CloneDefinition(*f.store.state.Receipt.Definition)
	superseded := supersede(f, MaxRetainedBundles-1)
	atTheBound(t, f)
	moved := errors.Join(ErrBootstrapIncompatible, ErrAutomationSuperseded, failure("controller.setup", "superseded automation", ""))
	f.service.bundle = obsoleteBundle{BundleManager: &f.bundle, digest: current.CatalogDigest, err: moved}
	f.bundle.ready, f.bundle.sealed = false, false
	f.bundle.automation = strings.Repeat("9", 64)
	r.bootstrapError = errors.New("no publisher may be contacted for a carried resolution")
	r.nativeError = errors.New("no repository may be refreshed for a carried resolution")
	f.events, f.plan = nil, Report{}
	return f, current.CatalogDigest, superseded
}

// holdsReadable reports whether an area is still held and not being retired,
// which is what lets the receipt and a carry-forward read it.
func holdsReadable(f *fixture, id string) bool {
	return slices.ContainsFunc(f.store.areas, func(held HeldArea) bool { return held.ID == id && !held.Retiring }) &&
		!slices.Contains(f.store.retired, id)
}

func refusal(t *testing.T, err error, message, remediation string) {
	t.Helper()
	found := diagnostics.Of(err)
	if len(found) != 1 || found[0].Code != "controller.conflict" || found[0].Message != message || found[0].Remediation != remediation {
		t.Fatalf("refusal = %#v (%v)", found, err)
	}
}

// A host at its bound under --purge-old-bundles retires every superseded
// execution bundle before it publishes the new one, keeps the bundle the
// receipt names and the carry-forward reads, and completes. Only once the new
// receipt names the new bundle is the one it replaced superseded in turn.
func TestPurgeAtTheBoundRetiresSupersededBundlesBeforePublishing(t *testing.T) {
	f, current, superseded := fullHost(t)
	report, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true, PurgeOldBundles: true})
	if err != nil || report.Outcome != "changed" {
		t.Fatalf("setup at the bound: %#v %v", report, err)
	}
	if !slices.Contains(f.plan.Actions, "Retire 15 superseded execution bundles to make room for the new one") {
		t.Fatalf("the plan did not present the retirement: %q", f.plan.Actions)
	}
	if len(f.store.retired) != MaxRetainedBundles || !slices.Equal(f.store.retired[:len(superseded)], superseded) {
		t.Fatalf("retired before publishing = %v", f.store.retired)
	}
	if f.store.retired[len(superseded)] != current {
		t.Fatalf("retired after completing = %v", f.store.retired[len(superseded):])
	}
	if !slices.Contains(f.bundle.retainedSeeds, true) {
		t.Fatal("preparation could not read the bundle the resolution was carried from")
	}
	next := f.store.state.Receipt
	if next.Status != "complete" || next.CatalogDigest == current || next.Definition.Bootstrap.AutomationDigest != f.bundle.automation {
		t.Fatalf("the automation-only revision did not complete: %#v", next)
	}
	if !slices.Equal(f.store.areas, []HeldArea{{ID: next.CatalogDigest}}) {
		t.Fatalf("areas after setup = %#v", f.store.areas)
	}
	if !slices.IsSorted(report.RetiredBundles) || len(report.RetiredBundles) != MaxRetainedBundles || !slices.Contains(report.RetiredBundles, current) {
		t.Fatalf("reported retirement = %v", report.RetiredBundles)
	}
}

// The carry-forward may read a retained bundle other than the one the receipt
// names. Retiring before publishing keeps both, and only once the new receipt
// completes are they superseded in turn.
func TestPurgeAtTheBoundKeepsTheCarryForwardSourceBesideTheReceiptBundle(t *testing.T) {
	f, current, superseded := fullHost(t)
	receipt := *f.store.state.Receipt.Definition
	bootstrap := *receipt.Bootstrap
	bootstrap.AutomationDigest = strings.Repeat("8", 64)
	bootstrap, err := CanonicalBootstrap(bootstrap)
	if err != nil {
		t.Fatal(err)
	}
	source, err := NewResolvedDefinition(bootstrap, *receipt.Native)
	if err != nil {
		t.Fatal(err)
	}
	// The source takes the first superseded bundle's slot, and is retained
	// after the receipt's resolution, so it is the one carried forward.
	f.store.state.RetainedDefinitions = append(slices.DeleteFunc(f.store.state.RetainedDefinitions,
		func(definition Definition) bool { return definition.CatalogDigest == superseded[0] }), source)
	f.store.areas = slices.DeleteFunc(f.store.areas, func(held HeldArea) bool { return held.ID == superseded[0] })
	f.store.areas = append(f.store.areas, HeldArea{ID: source.CatalogDigest})
	moved := errors.Join(ErrBootstrapIncompatible, ErrAutomationSuperseded, failure("controller.setup", "superseded automation", ""))
	f.service.bundle = obsoleteBundle{BundleManager: &f.bundle, digest: source.CatalogDigest, err: moved}
	report, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true, PurgeOldBundles: true})
	if err != nil || report.Outcome != "changed" {
		t.Fatalf("setup at the bound: %#v %v", report, err)
	}
	before := superseded[1:]
	if len(f.store.retired) != MaxRetainedBundles || !slices.Equal(f.store.retired[:len(before)], before) {
		t.Fatalf("retired before publishing = %v", f.store.retired)
	}
	after := []string{current, source.CatalogDigest}
	slices.Sort(after)
	if !slices.Equal(f.store.retired[len(before):], after) {
		t.Fatalf("retired after completing = %v", f.store.retired[len(before):])
	}
	if !slices.Contains(f.bundle.retainedSeeds, true) || f.store.state.Receipt.Definition.Bootstrap.AutomationDigest != f.bundle.automation {
		t.Fatalf("the revision was not carried from its source: %#v", f.store.state.Receipt.Definition.Bootstrap)
	}
}

// Retained resolutions are bounded like areas. A resolution a controller stage
// retained holds no area, so a host can reach the resolution bound first, and
// retiring the superseded bundles, which takes their resolutions, makes room.
func TestPurgeAtTheResolutionBoundRetiresSupersededBundlesBeforePublishing(t *testing.T) {
	f, current, superseded := fullHost(t)
	stage := strings.Repeat("f", 64)
	f.store.state.RetainedDefinitions = slices.Insert(slices.DeleteFunc(f.store.state.RetainedDefinitions,
		func(definition Definition) bool { return definition.CatalogDigest == superseded[0] }), 0, Definition{CatalogDigest: stage})
	f.store.areas = slices.DeleteFunc(f.store.areas, func(held HeldArea) bool { return held.ID == superseded[0] })
	report, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true, PurgeOldBundles: true})
	if err != nil || report.Outcome != "changed" {
		t.Fatalf("setup at the resolution bound: %#v %v", report, err)
	}
	if !slices.Contains(f.plan.Actions, "Retire 14 superseded execution bundles to make room for the new one") {
		t.Fatalf("the plan did not present the retirement: %q", f.plan.Actions)
	}
	before := len(superseded) - 1
	if len(f.store.retired) <= before || !slices.Equal(f.store.retired[:before], superseded[1:]) {
		t.Fatalf("retired before publishing = %v", f.store.retired)
	}
	if !slices.Contains(f.store.retired[before:], current) {
		t.Fatalf("retired after completing = %v", f.store.retired[before:])
	}
}

// Without the flag a host at its bound refuses before it presents a plan, and
// the refusal names the command that makes room.
func TestTheBoundRefusalNamesPurgeOldBundles(t *testing.T) {
	f, current, _ := fullHost(t)
	writes := f.store.writes
	_, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	refusal(t, err, boundRefusal, boundRemediation)
	if slices.Contains(f.events, "present") || f.store.writes != writes || len(f.store.retired) != 0 {
		t.Fatalf("a refused setup acted: events=%v writes=%d retired=%v", f.events, f.store.writes-writes, f.store.retired)
	}
	if f.store.state.Receipt.Status != "complete" || f.store.state.Receipt.CatalogDigest != current || !holdsReadable(f, current) {
		t.Fatalf("the refusal moved the receipt: %#v", f.store.state.Receipt)
	}
}

// When client areas and the current bundle hold every slot, nothing may be
// retired, and the refusal says so whether or not retirement was asked for. No
// command of this build frees that room, so its remedy names none and the
// result offers no next command (D90).
func TestTheBoundHeldByClientAreasAndTheCurrentBundleSaysSo(t *testing.T) {
	for _, purge := range []bool{false, true} {
		t.Run("purge="+strconv.FormatBool(purge), func(t *testing.T) {
			f, current, _ := fullHost(t)
			f.store.state.RetainedDefinitions = slices.DeleteFunc(f.store.state.RetainedDefinitions,
				func(definition Definition) bool { return definition.CatalogDigest != current })
			writes := f.store.writes
			report, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true, PurgeOldBundles: purge})
			refusal(t, err, keptRefusal, keptRemediation)
			if strings.Contains(keptRemediation, "purge") || strings.Contains(keptRemediation, "bootwright") {
				t.Fatalf("the remedy of a bound no command frees names a command: %q", keptRemediation)
			}
			if report == nil || report.Next != "" {
				t.Fatalf("the refusal offers a next command: %#v", report)
			}
			if slices.Contains(f.events, "present") || f.store.writes != writes || len(f.store.retired) != 0 || len(f.store.areas) != MaxRetainedBundles {
				t.Fatalf("a refused setup acted: events=%v writes=%d retired=%v", f.events, f.store.writes-writes, f.store.retired)
			}
		})
	}
}

// upgradePodman makes the next native solve upgrade podman from the release the
// fixture installs to version, from a source of its own, against the package
// inventory before.
func upgradePodman(t *testing.T, r *resolvingFixture, base NativeResolvedPlan, version, before string) {
	t.Helper()
	plan := base
	plan.Roots, plan.Packages = slices.Clone(base.Roots), slices.Clone(base.Packages)
	index := slices.IndexFunc(plan.Roots, func(root NativeRoot) bool { return root.Key == "podman" })
	installed := plan.Roots[index].Package
	upgraded := installed
	upgraded.Version = version
	plan.Roots[index].Package = upgraded
	id := "podman-" + version
	for index := range plan.Packages {
		if plan.Packages[index].Name == installed.Name {
			plan.Packages[index].Version = version
			plan.Packages[index].Source.ID = id
			plan.Packages[index].Source.URL = strings.TrimSuffix(plan.Packages[index].Source.URL, ".rpm") + "-" + version + ".rpm"
		}
	}
	plan.Actions = []NativeAction{{Kind: "upgrade", Before: &installed, After: upgraded, SourceID: id, Reason: "root"}}
	plan.BeforeSHA256, plan.AfterSHA256 = before, strings.Repeat("f", 64)
	var err error
	if r.native, err = CanonicalNativePlan(plan); err != nil {
		t.Fatal(err)
	}
}

// oneShortOfTheBound completes one setup and gives the host fourteen superseded
// bundles beside the one it named, one area and one resolution short of its
// bound. The next setup must upgrade podman, which names a new bundle, and its
// native transaction fails definitively. It returns the native plan the
// fixture first solved, the bundles that setup's receipt supersedes, sorted,
// and its runtime installer.
func oneShortOfTheBound(t *testing.T) (*fixture, NativeResolvedPlan, []string, *testRuntimeInstaller) {
	t.Helper()
	f, r := dynamicFixture(t)
	base := r.native
	if _, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	superseded := append(supersede(f, MaxRetainedBundles-2), f.store.state.Receipt.CatalogDigest)
	slices.Sort(superseded)
	installer := &testRuntimeInstaller{owner: f, result: ActionResult{Outcome: "failed", Evidence: object(map[string]any{"installationEntered": false})}, err: failure("controller.unknown", "native inventory changed", "")}
	f.service.runtime = installer
	f.host.runtime = RuntimeInspection{}
	f.bundle.ready, f.bundle.sealed = false, false
	upgradePodman(t, r, base, "1.2.4", strings.Repeat("e", 64))
	return f, base, superseded, installer
}

// failedAtTheBound leaves a host at its bound under a failed receipt. The
// setup oneShortOfTheBound prepares publishes its receipt, whose resolution
// the store retains, prepares the sixteenth bundle, which that receipt names,
// and its native transaction then fails definitively. It returns the native
// plan the fixture first solved and the superseded bundles, sorted.
func failedAtTheBound(t *testing.T) (*fixture, NativeResolvedPlan, []string) {
	t.Helper()
	f, base, superseded, installer := oneShortOfTheBound(t)
	if _, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true}); err == nil {
		t.Fatal("the failed native transaction was not reported")
	}
	receipt := f.store.state.Receipt
	if receipt.Status != "failed" || !slices.ContainsFunc(f.store.areas, func(held HeldArea) bool { return held.ID == receipt.CatalogDigest }) {
		t.Fatalf("the failed setup left receipt %q over %#v", receipt.Status, f.store.areas)
	}
	atTheBound(t, f)
	installer.result, installer.err = ActionResult{Outcome: "changed", Evidence: object(map[string]any{"nativePostcondition": "verified"})}, nil
	f.events, f.plan = nil, Report{}
	return f, base, superseded
}

// retriesAfterAFailure are the solves a retry after a failed native
// transaction reaches once the operator changed the host's packages: a new
// release, which names a new bundle, or the same release against the new
// inventory, which names the same bundle under a new resolution.
var retriesAfterAFailure = map[string]string{"a new bundle": "1.2.5", "the same bundle": "1.2.4"}

// A failed setup is replaced by the next one, so a host it left at its bound is
// not stuck: under --purge-old-bundles the retry retires every superseded
// bundle except the one the failed receipt names, then publishes and completes.
func TestPurgeAtTheBoundAfterAFailedSetupRetiresAndCompletes(t *testing.T) {
	for name, version := range retriesAfterAFailure {
		t.Run(name, func(t *testing.T) {
			f, base, superseded := failedAtTheBound(t)
			failed := f.store.state.Receipt.CatalogDigest
			upgradePodman(t, f.resolution, base, version, strings.Repeat("d", 64))
			f.bundle.ready = version == "1.2.4"
			report, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true, PurgeOldBundles: true})
			if err != nil || report.Outcome != "changed" {
				t.Fatalf("the retry at the bound: %#v %v", report, err)
			}
			if !slices.Contains(f.plan.Actions, "Retire 15 superseded execution bundles to make room for the new one") {
				t.Fatalf("the plan did not present the retirement: %q", f.plan.Actions)
			}
			if len(f.store.retired) < len(superseded) || !slices.Equal(f.store.retired[:len(superseded)], superseded) {
				t.Fatalf("retired before publishing = %v, want %v", f.store.retired, superseded)
			}
			receipt := f.store.state.Receipt
			if receipt.Status != "complete" || receipt.Definition.Native.BeforeSHA256 != strings.Repeat("d", 64) || (receipt.CatalogDigest == failed) != (version == "1.2.4") {
				t.Fatalf("the retry did not complete its own resolution: %#v", receipt)
			}
			if !slices.Equal(f.store.areas, []HeldArea{{ID: receipt.CatalogDigest}}) {
				t.Fatalf("areas after the retry = %#v", f.store.areas)
			}
		})
	}
}

// Without the flag, a retry after a failed setup at the bound refuses before any
// effect, and the refusal names the command that makes room.
func TestTheBoundRefusalAfterAFailedSetupNamesPurgeOldBundles(t *testing.T) {
	for name, version := range retriesAfterAFailure {
		t.Run(name, func(t *testing.T) {
			f, base, _ := failedAtTheBound(t)
			failed := f.store.state.Receipt
			upgradePodman(t, f.resolution, base, version, strings.Repeat("d", 64))
			writes := f.store.writes
			_, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
			refusal(t, err, boundRefusal, boundRemediation)
			if slices.Contains(f.events, "present") || f.store.writes != writes || len(f.store.retired) != 0 {
				t.Fatalf("a refused setup acted: events=%v writes=%d retired=%v", f.events, f.store.writes-writes, f.store.retired)
			}
			if f.store.state.Receipt.ID != failed.ID || f.store.state.Receipt.Status != "failed" {
				t.Fatalf("the refusal moved the failed receipt: %#v", f.store.state.Receipt)
			}
		})
	}
}

// inventory is the package inventory a native solve reads before an attempt,
// distinct for each attempt.
func inventory(attempt int) string { return fmt.Sprintf("%064x", attempt+1) }

// sameBundleFailures fills a fresh host's resolution bound with one bundle. Its
// first setup and every retry fail in their native transaction, and each retry,
// finding the runtime still missing, solves the same podman release again
// against the inventory the failure left, which names the same bundle under a
// new resolution. The next solve reads yet another inventory and its native
// transaction succeeds. It returns the bundle and the resolutions in the order
// the store retained them.
func sameBundleFailures(t *testing.T) (*fixture, string, []string) {
	t.Helper()
	f, r := dynamicFixture(t)
	base := r.native
	installer := &testRuntimeInstaller{owner: f, result: ActionResult{Outcome: "failed", Evidence: object(map[string]any{"installationEntered": false})}, err: failure("controller.unknown", "native inventory changed", "")}
	f.service.runtime = installer
	f.host.runtime = RuntimeInspection{}
	var resolutions []string
	for attempt := range MaxRetainedBundles {
		upgradePodman(t, r, base, "1.2.4", inventory(attempt))
		if _, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true}); err == nil {
			t.Fatalf("attempt %d: the failed native transaction was not reported", attempt)
		}
		receipt := f.store.state.Receipt
		if receipt.Status != "failed" || receipt.Definition == nil || receipt.Definition.Native.BeforeSHA256 != inventory(attempt) {
			t.Fatalf("attempt %d left receipt %#v", attempt, receipt)
		}
		resolutions = append(resolutions, receipt.Definition.ResolutionDigest)
	}
	bundle := f.store.state.Receipt.CatalogDigest
	if !slices.Equal(f.store.areas, []HeldArea{{ID: bundle}}) || len(f.store.state.RetainedDefinitions) != MaxRetainedBundles ||
		slices.ContainsFunc(f.store.state.RetainedDefinitions, func(definition Definition) bool { return definition.CatalogDigest != bundle }) {
		t.Fatalf("the failed setups left areas %#v and %d resolutions", f.store.areas, len(f.store.state.RetainedDefinitions))
	}
	installer.result, installer.err = ActionResult{Outcome: "changed", Evidence: object(map[string]any{"nativePostcondition": "verified"})}, nil
	upgradePodman(t, r, base, "1.2.4", inventory(MaxRetainedBundles))
	f.events, f.plan = nil, Report{}
	return f, bundle, resolutions
}

// Every retained resolution can name the one bundle a failed receipt keeps.
// Under --purge-old-bundles the retry retires each of them but the failed
// receipt's own, which is also the latest, retires no area, then publishes its
// own resolution and completes.
func TestPurgeAtTheBoundRetiresSupersededResolutionsOfTheKeptBundle(t *testing.T) {
	f, bundle, resolutions := sameBundleFailures(t)
	report, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true, PurgeOldBundles: true})
	if err != nil || report.Outcome != "changed" {
		t.Fatalf("the retry at the resolution bound: %#v %v", report, diagnostics.Of(err))
	}
	retirements := slices.DeleteFunc(slices.Clone(f.plan.Actions), func(action string) bool { return !strings.HasPrefix(action, "Retire ") })
	if !slices.Equal(retirements, []string{"Retire 15 superseded resolutions of kept execution bundles to make room for the new one"}) {
		t.Fatalf("the plan presented the retirement as %q", retirements)
	}
	superseded := slices.Sorted(slices.Values(resolutions[:len(resolutions)-1]))
	if !slices.Equal(f.store.retiredResolutions, superseded) || len(f.store.retired) != 0 || len(report.RetiredBundles) != 0 {
		t.Fatalf("retired resolutions %v and bundles %v", f.store.retiredResolutions, f.store.retired)
	}
	receipt := f.store.state.Receipt
	if receipt.Status != "complete" || receipt.CatalogDigest != bundle || receipt.Definition.Native.BeforeSHA256 != inventory(MaxRetainedBundles) {
		t.Fatalf("the retry did not complete its own resolution: %#v", receipt)
	}
	var retained []string
	for _, definition := range f.store.state.RetainedDefinitions {
		retained = append(retained, definition.ResolutionDigest)
	}
	if !slices.Equal(retained, []string{resolutions[len(resolutions)-1], receipt.Definition.ResolutionDigest}) {
		t.Fatalf("retained resolutions after the retry = %v", retained)
	}
	if !slices.Equal(f.store.areas, []HeldArea{{ID: bundle}}) {
		t.Fatalf("areas after the retry = %#v", f.store.areas)
	}
}

// At the bound a resolution of a kept bundle is retired unless it is the
// receipt's own, the one this setup publishes or the latest naming its bundle,
// which keeps that bundle identifiable. The resolutions of a retired area
// leave with it, and those of a bundle this host holds no area for, as a
// controller stage retains, are never named.
func TestRoomRetiresOnlySupersededResolutionsOfKeptBundles(t *testing.T) {
	digest := func(digit string) string { return strings.Repeat(digit, 64) }
	receipt, carried, target, superseded, stage := digest("1"), digest("2"), digest("3"), digest("4"), digest("5")
	resolution := func(bundle, digit string) Definition {
		return Definition{CatalogDigest: bundle, ResolutionDigest: digest(digit)}
	}
	own, fresh := resolution(receipt, "b"), resolution(target, "e")
	areas := []HeldArea{{ID: receipt}, {ID: carried}, {ID: superseded}}
	for index := range MaxRetainedBundles - len(areas) {
		areas = append(areas, HeldArea{ID: fmt.Sprintf("%064x", index+256)})
	}
	view := StorageView{Areas: areas, State: HostState{
		Receipt: SetupReceipt{ID: "setup-" + digest("1")[:32], Status: "complete", CatalogDigest: receipt, Definition: &own},
		RetainedDefinitions: []Definition{
			resolution(receipt, "a"), own, resolution(receipt, "c"),
			resolution(carried, "d"), resolution(carried, "f"),
			fresh, resolution(target, "6"),
			resolution(superseded, "7"), resolution(superseded, "8"),
			resolution(stage, "9"), resolution(stage, "0"),
		},
	}}
	current := inspection{definition: Definition{CatalogDigest: target, ResolutionDigest: fresh.ResolutionDigest, Bootstrap: &BootstrapDefinition{}}, retainedDigest: carried}
	retiring, err := current.room(view, true)
	if err != nil {
		t.Fatalf("room at the bound: %v", err)
	}
	if !slices.Equal(retiring.bundles, []string{superseded}) || !slices.Equal(retiring.resolutions, []string{digest("a"), digest("d")}) {
		t.Fatalf("retiring bundles %v and resolutions %v", retiring.bundles, retiring.resolutions)
	}
	if actions := retiring.actions(); !slices.Equal(actions, []string{
		"Retire 1 superseded execution bundle to make room for the new one",
		"Retire 2 superseded resolutions of kept execution bundles to make room for the new one",
	}) {
		t.Fatalf("plan actions = %q", actions)
	}
}

// Without the flag that host refuses with the command that makes room, rather
// than claiming that nothing may be retired.
func TestTheBoundOfOneBundlesResolutionsNamesPurgeOldBundles(t *testing.T) {
	f, _, resolutions := sameBundleFailures(t)
	writes := f.store.writes
	_, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	refusal(t, err, boundRefusal, boundRemediation)
	if slices.Contains(f.events, "present") || f.store.writes != writes || len(f.store.retiredResolutions) != 0 || len(f.store.state.RetainedDefinitions) != len(resolutions) {
		t.Fatalf("a refused setup acted: events=%v writes=%d retired=%v", f.events, f.store.writes-writes, f.store.retiredResolutions)
	}
}

// strandedClient is the client area that fills a stranded host's sixteenth
// slot. No resolution names it, so no retirement may remove it.
var strandedClient = strings.Repeat("f", 64)

// strandedAtTheBound leaves a host as a build that published a receipt at the
// bound left it: the setup oneShortOfTheBound prepares published its pending
// receipt, whose resolution the store retains, and a client area holds the
// sixteenth area, so the bundle that receipt names was never reserved. With
// intent, that setup's first intent was durable before its reservation
// refused; without, its publication was lost. The native transaction now
// succeeds, and the executable that runs next embeds that receipt's
// automation, so its bundle manager, which refuses any other as the
// production one does, can prepare that receipt's bundle. It returns the
// pending receipt and the superseded bundles, sorted.
func strandedAtTheBound(t *testing.T, intent bool) (*fixture, SetupReceipt, []string) {
	t.Helper()
	f, _, superseded, installer := oneShortOfTheBound(t)
	f.store.failPublication = f.store.writes + 2
	if _, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true}); err == nil {
		t.Fatal("the lost intent publication was not reported")
	}
	f.store.failPublication = 0
	if intent {
		f.store.state.Receipt.Actions[0].Phase = "intent"
	}
	pending := f.store.state.Receipt
	if !pending.Incomplete() || slices.ContainsFunc(f.store.areas, func(held HeldArea) bool { return held.ID == pending.CatalogDigest }) {
		t.Fatalf("the interrupted setup left receipt %q over %#v", pending.Status, f.store.areas)
	}
	f.store.areas = append(f.store.areas, HeldArea{ID: strandedClient})
	atTheBound(t, f)
	installer.result, installer.err = ActionResult{Outcome: "changed", Evidence: object(map[string]any{"nativePostcondition": "verified"})}, nil
	f.service.bundle = executableBundle{BundleManager: &f.bundle, automation: pending.Definition.Bootstrap.AutomationDigest}
	f.events, f.plan = nil, Report{}
	return f, pending, superseded
}

// resumedStranded fails the test unless the stranded receipt completed exactly
// as it was published, over its own bundle and its own resolution alone, and
// the client area survived.
func resumedStranded(t *testing.T, f *fixture, pending SetupReceipt) {
	t.Helper()
	receipt := f.store.state.Receipt
	if receipt.ID != pending.ID || receipt.PlanDigest != pending.PlanDigest || receipt.Status != "complete" {
		t.Fatalf("the stranded receipt was not resumed exactly: %#v", receipt)
	}
	want := []HeldArea{{ID: pending.CatalogDigest}, {ID: strandedClient}}
	slices.SortFunc(want, func(a, b HeldArea) int { return strings.Compare(a.ID, b.ID) })
	if !slices.Equal(f.store.areas, want) {
		t.Fatalf("areas after the resumed setup = %#v", f.store.areas)
	}
	if retained := f.store.state.RetainedDefinitions; len(retained) != 1 || retained[0].ResolutionDigest != pending.Definition.ResolutionDigest {
		t.Fatalf("retained resolutions after the resumed setup = %#v", retained)
	}
}

// A pending receipt whose bundle holds no area at the bound is resumed under
// --purge-old-bundles: every superseded execution bundle is retired first, the
// one the completed receipt before it named included, the client area and the
// pending receipt's resolution are kept, and the receipt then completes.
func TestPurgeAtTheBoundResumesAStrandedPendingSetup(t *testing.T) {
	for name, intent := range map[string]bool{"its intent lost": false, "its reservation refused": true} {
		t.Run(name, func(t *testing.T) {
			f, pending, superseded := strandedAtTheBound(t, intent)
			report, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true, PurgeOldBundles: true})
			if err != nil || report.Outcome != "changed" {
				t.Fatalf("the stranded setup under the flag: %#v %#v", report, diagnostics.Of(err))
			}
			if !slices.Contains(f.plan.Actions, "Retire 15 superseded execution bundles to make room for the new one") {
				t.Fatalf("the plan did not present the retirement: %q", f.plan.Actions)
			}
			if !slices.Equal(f.store.retired, superseded) || !slices.Equal(report.RetiredBundles, superseded) || len(f.store.retiredResolutions) != 0 {
				t.Fatalf("retired bundles %v, reported %v, resolutions %v", f.store.retired, report.RetiredBundles, f.store.retiredResolutions)
			}
			resumedStranded(t, f, pending)
		})
	}
}

// Without the flag that host refuses before it presents a plan, names the
// command that makes room and leaves the receipt pending.
func TestTheBoundRefusalOverAStrandedPendingSetupNamesPurgeOldBundles(t *testing.T) {
	f, pending, _ := strandedAtTheBound(t, true)
	writes := f.store.writes
	_, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	refusal(t, err, boundRefusal, boundRemediation)
	if slices.Contains(f.events, "present") || f.store.writes != writes || len(f.store.retired) != 0 {
		t.Fatalf("a refused setup acted: events=%v writes=%d retired=%v", f.events, f.store.writes-writes, f.store.retired)
	}
	if receipt := f.store.state.Receipt; receipt.ID != pending.ID || !receipt.Incomplete() {
		t.Fatalf("the refusal moved the pending receipt: %#v", receipt)
	}
}

// However the resumed setup is interrupted, the receipt stays pending with its
// resolution retained, and repeating the command completes it. An interrupted
// retirement still holds the bound, so the flag is needed again. Each
// interruption disarms itself and reports whether it fired.
func TestAnInterruptedStrandedSetupAtTheBoundStaysResumable(t *testing.T) {
	for name, interrupt := range map[string]func(*fixture) func(error) bool{
		"retirement refused": func(f *fixture) func(error) bool {
			busy := errors.New("busy")
			f.store.retireErr = busy
			return func(err error) bool { f.store.retireErr = nil; return errors.Is(err, busy) }
		},
		"retirement intent recorded": func(f *fixture) func(error) bool {
			f.store.interrupted = true
			return func(error) bool { return !f.store.interrupted }
		},
		"intent publication": func(f *fixture) func(error) bool {
			f.store.failPublication = f.store.writes + 1
			return func(error) bool {
				fired := f.store.writes >= f.store.failPublication
				f.store.failPublication = 0
				return fired
			}
		},
		"bundle preparation": func(f *fixture) func(error) bool {
			failed := errors.New("acquisition failed")
			f.bundle.err = failed
			return func(err error) bool { f.bundle.err = nil; return errors.Is(err, failed) }
		},
	} {
		t.Run(name, func(t *testing.T) {
			f, pending, _ := strandedAtTheBound(t, false)
			fired := interrupt(f)
			_, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true, PurgeOldBundles: true})
			if !fired(err) || err == nil {
				t.Fatalf("the interruption did not fire or was not reported: %v", err)
			}
			receipt := f.store.state.Receipt
			retained := slices.ContainsFunc(f.store.state.RetainedDefinitions, func(definition Definition) bool {
				return definition.ResolutionDigest == pending.Definition.ResolutionDigest
			})
			if receipt.ID != pending.ID || !receipt.Incomplete() || !retained {
				t.Fatalf("the interruption left receipt %#v", receipt)
			}
			if name == "retirement intent recorded" {
				_, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
				refusal(t, err, boundRefusal, boundRemediation)
			}
			report, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true, PurgeOldBundles: true})
			if err != nil || report.Outcome != "changed" {
				t.Fatalf("repeating the setup: %#v %#v", report, diagnostics.Of(err))
			}
			resumedStranded(t, f, pending)
		})
	}
}

// A pending receipt whose bundle holds an area is resumed exactly and needs no
// room, so none is planned around it even at the bound. One left stranded
// gives up only areas: its own resolution is already retained, so no
// resolution of the bundle it names is retired, and a client area stays.
func TestRoomUnderAPendingReceiptRetiresOnlyTheAreasAStrandedOneLacks(t *testing.T) {
	digest := func(digit string) string { return strings.Repeat(digit, 64) }
	pending, superseded, marked := digest("1"), digest("2"), digest("3")
	own := Definition{CatalogDigest: pending, ResolutionDigest: digest("b"), Bootstrap: &BootstrapDefinition{}}
	areas := []HeldArea{{ID: superseded}, {ID: marked, Retiring: true}}
	for index := range MaxRetainedBundles - len(areas) {
		areas = append(areas, HeldArea{ID: fmt.Sprintf("%064x", index+256)})
	}
	view := StorageView{Areas: areas, State: HostState{
		Receipt:             SetupReceipt{ID: "setup-" + digest("1")[:32], Status: "pending", CatalogDigest: pending, Definition: &own},
		RetainedDefinitions: []Definition{{CatalogDigest: pending, ResolutionDigest: digest("a")}, {CatalogDigest: superseded, ResolutionDigest: digest("c")}, own},
	}}
	current := inspection{definition: own}
	_, err := current.room(view, false)
	refusal(t, err, boundRefusal, boundRemediation)
	planned, err := current.room(view, true)
	if err != nil || !slices.Equal(planned.bundles, []string{superseded, marked}) || len(planned.resolutions) != 0 {
		t.Fatalf("room under a stranded receipt = %#v (%v)", planned, err)
	}
	view.Areas[len(view.Areas)-1] = HeldArea{ID: pending}
	for _, purge := range []bool{false, true} {
		if planned, err := current.room(view, purge); err != nil || len(planned.bundles) != 0 || len(planned.resolutions) != 0 {
			t.Fatalf("room under a pending receipt whose bundle is held, purge=%t: %#v (%v)", purge, planned, err)
		}
	}
}

// However the setup at the bound is interrupted, the bundle the receipt names
// and the one the carry-forward reads stay readable, and repeating the command
// completes it. An interrupted retirement counts as retirable, so the host is
// never left at a bound nothing can lower.
func TestAnInterruptedSetupAtTheBoundKeepsItsBundlesAndCompletes(t *testing.T) {
	for name, interrupt := range map[string]func(*fixture) func(){
		"retirement refused": func(f *fixture) func() {
			f.store.retireErr = errors.New("busy")
			return func() { f.store.retireErr = nil }
		},
		"retirement intent recorded": func(f *fixture) func() {
			f.store.interrupted = true
			return func() {}
		},
		"receipt publication": func(f *fixture) func() {
			f.store.failPublication = f.store.writes + 1
			return func() { f.store.failPublication = 0 }
		},
		"bundle preparation": func(f *fixture) func() {
			f.bundle.err = errors.New("acquisition failed")
			return func() { f.bundle.err = nil }
		},
	} {
		t.Run(name, func(t *testing.T) {
			f, current, _ := fullHost(t)
			restore := interrupt(f)
			if _, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true, PurgeOldBundles: true}); err == nil {
				t.Fatal("the interruption was not reported")
			}
			restore()
			if !holdsReadable(f, current) {
				t.Fatalf("the current bundle is no longer readable: %#v", f.store.areas)
			}
			if name == "retirement intent recorded" {
				_, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
				refusal(t, err, boundRefusal, boundRemediation)
			}
			report, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true, PurgeOldBundles: true})
			if err != nil || report.Outcome != "changed" {
				t.Fatalf("repeating the setup: %#v %v", report, err)
			}
			if f.store.state.Receipt.Status != "complete" || f.store.state.Receipt.CatalogDigest == current || len(f.store.areas) != 1 {
				t.Fatalf("the repeat did not complete: %#v %#v", f.store.state.Receipt, f.store.areas)
			}
		})
	}
}
