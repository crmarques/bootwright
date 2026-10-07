package prerequisites

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

const (
	unresumableRefusal     = "an earlier build left a setup pending at this host's bound that this executable cannot resume"
	unresumableRemediation = "run bootwright setup --purge-old-bundles to cancel it, retire the superseded execution bundles and set this host up afresh"
	abandonmentAction      = "Cancel the setup an earlier build left pending at this host's bound, which this executable cannot resume and which never published its bundle"
	frozenRefusal          = "frozen setup dependencies differ from current intent"
	frozenRemediation      = "restore the original input before retrying"
)

// frozenFor reports the refusal of a pending receipt frozen for dependencies
// other than the ones the host now selects, when setup does not abandon it:
// it names the input to restore, and no purge.
func frozenFor(found []diagnostics.Diagnostic) bool {
	return len(found) == 1 && found[0].Code == "controller.unknown" && found[0].Message == frozenRefusal && found[0].Remediation == frozenRemediation
}

// executableBundle judges a definition as the production bundle manager does:
// one whose automation is not the one this executable embeds is refused with
// ErrBootstrapIncompatible and ErrAutomationSuperseded, before any area is
// read, by Validate, Inspect and Prepare alike.
type executableBundle struct {
	BundleManager
	automation string
}

func (b executableBundle) superseded(definition Definition) error {
	if definition.Bootstrap == nil || definition.Bootstrap.AutomationDigest == b.automation {
		return nil
	}
	return errors.Join(ErrBootstrapIncompatible, ErrAutomationSuperseded, failure("controller.setup", "retained bootstrap automation is superseded by the current executable", ""))
}

func (b executableBundle) Validate(definition Definition) error {
	if err := b.superseded(definition); err != nil {
		return err
	}
	return b.BundleManager.Validate(definition)
}

func (b executableBundle) Inspect(ctx context.Context, area BundleArea, definition Definition, probe bool) (BundleInspection, error) {
	if err := b.superseded(definition); err != nil {
		return BundleInspection{}, err
	}
	return b.BundleManager.Inspect(ctx, area, definition, probe)
}

func (b executableBundle) Prepare(ctx context.Context, area, retained BundleArea, definition Definition, egress SetupEgress, progress func(ProgressEvent)) (BundleInspection, error) {
	if err := b.superseded(definition); err != nil {
		return BundleInspection{}, err
	}
	return b.BundleManager.Prepare(ctx, area, retained, definition, egress, progress)
}

// movedExecutable makes the executable that runs next embed automation the
// stranded receipt does not name, as every build after the one that stranded
// it does: its bundle manager refuses that receipt's bundle, and a fresh
// resolution names its own automation, as a publisher resolution under it
// does. It returns that automation.
func movedExecutable(t *testing.T, f *fixture) string {
	t.Helper()
	moved := strings.Repeat("9", 64)
	f.service.bundle = executableBundle{BundleManager: &f.bundle, automation: moved}
	f.bundle.automation = moved
	f.resolution.bootstrap.AutomationDigest = moved
	var err error
	if f.resolution.bootstrap, err = CanonicalBootstrap(f.resolution.bootstrap); err != nil {
		t.Fatal(err)
	}
	return moved
}

// canceledAs fails the test unless the store holds the stranded receipt as
// setup abandons it: canceled, its intended execution bundle observed as never
// started, its native transaction still planned, and its resolution kept.
func canceledAs(t *testing.T, f *fixture, pending SetupReceipt) {
	t.Helper()
	canceledWith(t, f, pending, 0, `{"bundleArea":"absent"}`)
}

// canceledWith fails the test unless the store holds the pending receipt as
// setup cancels it: canceled under its own ID and plan, the action at index
// observed canceled with evidence and its preparation kept, every other
// action exactly as it was, and its resolution kept.
func canceledWith(t *testing.T, f *fixture, pending SetupReceipt, index int, evidence string) {
	t.Helper()
	receipt := f.store.state.Receipt
	if receipt.ID != pending.ID || receipt.PlanDigest != pending.PlanDigest || receipt.Status != "canceled" || len(receipt.Actions) != len(pending.Actions) {
		t.Fatalf("the pending receipt was not canceled as setup cancels it: %#v", receipt)
	}
	for position, action := range receipt.Actions {
		if position != index {
			if !reflect.DeepEqual(action, pending.Actions[position]) {
				t.Fatalf("the cancellation moved action %s: %#v", action.ID, action)
			}
			continue
		}
		if action.Phase != "observed" || action.Outcome != "canceled" || string(action.Evidence) != evidence || !bytes.Equal(action.Preparation, pending.Actions[position].Preparation) {
			t.Fatalf("action %s was not observed canceled with %s: %#v", action.ID, evidence, action)
		}
	}
	if !slices.ContainsFunc(f.store.state.RetainedDefinitions, func(definition Definition) bool {
		return definition.ResolutionDigest == pending.Definition.ResolutionDigest
	}) {
		t.Fatal("the canceled receipt lost the resolution it carries")
	}
}

// setUpAfresh fails the test unless a fresh setup under the moved automation
// took the stranded receipt's place: a new receipt completed over a bundle of
// its own, beside the client area, with no superseded area left and only its
// own resolution retained, the canceled receipt's retired once it completed.
func setUpAfresh(t *testing.T, f *fixture, pending SetupReceipt, moved string) {
	t.Helper()
	receipt := f.store.state.Receipt
	if receipt.ID == pending.ID || receipt.Status != "complete" || receipt.CatalogDigest == pending.CatalogDigest ||
		receipt.Definition == nil || receipt.Definition.Bootstrap.AutomationDigest != moved {
		t.Fatalf("no fresh setup took the stranded receipt's place: %#v", receipt)
	}
	want := []HeldArea{{ID: receipt.CatalogDigest}, {ID: strandedClient}}
	slices.SortFunc(want, func(a, b HeldArea) int { return strings.Compare(a.ID, b.ID) })
	if !slices.Equal(f.store.areas, want) {
		t.Fatalf("areas after the fresh setup = %#v", f.store.areas)
	}
	if retained := f.store.state.RetainedDefinitions; len(retained) != 1 || retained[0].ResolutionDigest != receipt.Definition.ResolutionDigest {
		t.Fatalf("retained resolutions after the fresh setup = %#v", retained)
	}
}

// A setup an earlier build left pending at the bound names a bundle this
// executable cannot prepare, and never took effect: at most its first action's
// intent was recorded before the bound refused that bundle's area. Under
// --purge-old-bundles setup cancels it, retires every superseded execution
// bundle but never the client area, and sets the host up afresh from a fresh
// resolution, all under the one plan it presents. Once that setup completes,
// the resolution the canceled receipt carried, whose bundle never held an
// area, is retired too, and the result names only the areas removed, so a
// later purge of the ready host finds nothing left to retire.
func TestPurgeAtTheBoundAbandonsAStrandedSetupThisExecutableCannotResume(t *testing.T) {
	for name, intent := range map[string]bool{"its intent lost": false, "its reservation refused": true} {
		t.Run(name, func(t *testing.T) {
			f, pending, superseded := strandedAtTheBound(t, intent)
			moved := movedExecutable(t, f)
			resolutions := f.resolution.bootstrapCalls
			report, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true, PurgeOldBundles: true})
			if err != nil || report.Outcome != "changed" {
				t.Fatalf("the abandonment under the flag: %#v %#v", report, diagnostics.Of(err))
			}
			if !slices.Contains(f.plan.Actions, abandonmentAction) || !slices.Contains(f.plan.Actions, "Retire 15 superseded execution bundles to make room for the new one") {
				t.Fatalf("the plan did not present the abandonment: %q", f.plan.Actions)
			}
			if index := slices.IndexFunc(f.plan.Checks, func(check Check) bool { return check.ID == "setup-state" }); index < 0 || f.plan.Checks[index].Observed != "pending" {
				t.Fatalf("the plan did not report the receipt it cancels as still pending: %#v", f.plan.Checks)
			}
			if !slices.Equal(f.store.retired, superseded) || !slices.Equal(report.RetiredBundles, superseded) {
				t.Fatalf("retired %v, reported %v, want %v and never the client area", f.store.retired, report.RetiredBundles, superseded)
			}
			if !slices.Equal(f.store.retiredResolutions, []string{pending.Definition.ResolutionDigest}) {
				t.Fatalf("retired resolutions %v, want only the canceled receipt's", f.store.retiredResolutions)
			}
			if f.resolution.bootstrapCalls != resolutions+1 {
				t.Fatalf("the fresh setup consulted the publisher %d times", f.resolution.bootstrapCalls-resolutions)
			}
			setUpAfresh(t, f, pending, moved)
			report, err = f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true, PurgeOldBundles: true})
			if err != nil || report.Outcome != "unchanged" || len(report.RetiredBundles) != 0 || len(f.store.retired) != len(superseded) || len(f.store.retiredResolutions) != 1 {
				t.Fatalf("a later purge of the ready host: %#v %v, retired %v and resolutions %v", report, err, f.store.retired, f.store.retiredResolutions)
			}
		})
	}
}

// Without the flag that setup refuses before any effect, and so does preflight,
// each naming the command that abandons the receipt.
func TestTheRefusalOfAStrandedSetupThisExecutableCannotResumeNamesPurgeOldBundles(t *testing.T) {
	f, pending, _ := strandedAtTheBound(t, true)
	movedExecutable(t, f)
	writes, resolutions := f.store.writes, f.resolution.bootstrapCalls
	_, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	refusal(t, err, unresumableRefusal, unresumableRemediation)
	report, err := f.service.Check(context.Background(), CheckRequest{})
	refusal(t, err, unresumableRefusal, unresumableRemediation)
	if report == nil || report.Outcome != "not-ready" {
		t.Fatalf("preflight over the stranded receipt = %#v", report)
	}
	if slices.Contains(f.events, "present") || f.store.writes != writes || len(f.store.retired) != 0 || f.resolution.bootstrapCalls != resolutions {
		t.Fatalf("a refusal acted: events=%v writes=%d retired=%v resolutions=%d", f.events, f.store.writes-writes, f.store.retired, f.resolution.bootstrapCalls-resolutions)
	}
	if receipt := f.store.state.Receipt; receipt.ID != pending.ID || receipt.Status != "pending" || receipt.Actions[0].Phase != "intent" {
		t.Fatalf("the refusal moved the stranded receipt: %#v", receipt)
	}
}

// The route a stranded receipt recorded binds only what its setup would
// acquire, and one that never took effect acquired nothing over it. So a setup
// over another route abandons it as over its own: without the flag setup and
// preflight refuse naming the command that does, and with it the fresh setup
// records the route it ran over. A stranded receipt this executable can
// prepare is resumed exactly instead, so over another route it still refuses
// as another pending attempt, before any effect.
func TestAStrandedSetupRecordedOverAnotherRouteIsAbandonedAlike(t *testing.T) {
	proxied := []string{"HTTPS_PROXY", "http://proxy.example:3128"}
	t.Run("one this executable cannot resume", func(t *testing.T) {
		f, pending, superseded := strandedAtTheBound(t, true)
		moved := movedExecutable(t, f)
		route := testRoute(t, proxied...)
		f.service.options.AmbientRoute = route
		if pending.Egress.HTTPSProxy == route.HTTPSProxy() {
			t.Fatal("the stranded receipt recorded the route the setup runs over")
		}
		_, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
		refusal(t, err, unresumableRefusal, unresumableRemediation)
		_, err = f.service.Check(context.Background(), CheckRequest{})
		refusal(t, err, unresumableRefusal, unresumableRemediation)
		report, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true, PurgeOldBundles: true})
		if err != nil || report.Outcome != "changed" || !slices.Equal(report.RetiredBundles, superseded) {
			t.Fatalf("the abandonment over another route: %#v %#v", report, diagnostics.Of(err))
		}
		setUpAfresh(t, f, pending, moved)
		if egress := f.store.state.Receipt.Egress; egress.HTTPSProxy != route.HTTPSProxy() {
			t.Fatalf("the fresh setup recorded route %#v", egress)
		}
	})
	t.Run("one this executable can prepare", func(t *testing.T) {
		f, pending, _ := strandedAtTheBound(t, true)
		f.service.options.AmbientRoute = testRoute(t, proxied...)
		writes := f.store.writes
		_, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true, PurgeOldBundles: true})
		if found := diagnostics.Of(err); len(found) != 1 || found[0].Code != "controller.unknown" || found[0].Message != "another exact setup attempt remains unresolved" {
			t.Fatalf("a resumable stranded receipt over another route: %#v (%v)", found, err)
		}
		if receipt := f.store.state.Receipt; f.store.writes != writes || len(f.store.retired) != 0 || receipt.ID != pending.ID || receipt.Status != "pending" {
			t.Fatalf("a refused setup acted: writes=%d retired=%v receipt=%#v", f.store.writes-writes, f.store.retired, receipt)
		}
	})
}

// upgradedPlatform is the release a host moves to after a setup froze its
// dependencies for the release before it.
var upgradedPlatform = Platform{OS: "rhel", Release: "9.9", Architecture: "amd64"}

// movePlatform moves the host, its publishers and its solver to
// upgradedPlatform, so a fresh resolution is for the release it now runs.
func movePlatform(t *testing.T, f *fixture) {
	t.Helper()
	f.host.platform = upgradedPlatform
	f.resolution.bootstrap.Platform, f.resolution.native.Platform = upgradedPlatform, upgradedPlatform
	var err error
	if f.resolution.bootstrap, err = CanonicalBootstrap(f.resolution.bootstrap); err != nil {
		t.Fatal(err)
	}
	if f.resolution.native, err = CanonicalNativePlan(f.resolution.native); err != nil {
		t.Fatal(err)
	}
}

// A stranded receipt frozen for a platform the host has since left cannot be
// resumed on it by any executable. One that never took effect is abandoned
// alike, and the fresh setup resolves for the platform the host runs now; one
// in any other shape still refuses as frozen for other dependencies.
func TestAStrandedSetupFrozenForAnotherPlatformIsAbandonedAlike(t *testing.T) {
	t.Run("one that never took effect", func(t *testing.T) {
		f, pending, superseded := strandedAtTheBound(t, true)
		movePlatform(t, f)
		_, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
		refusal(t, err, unresumableRefusal, unresumableRemediation)
		_, err = f.service.Check(context.Background(), CheckRequest{})
		refusal(t, err, unresumableRefusal, unresumableRemediation)
		report, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true, PurgeOldBundles: true})
		if err != nil || report.Outcome != "changed" || !slices.Equal(report.RetiredBundles, superseded) {
			t.Fatalf("the abandonment on the moved platform: %#v %#v", report, diagnostics.Of(err))
		}
		setUpAfresh(t, f, pending, pending.Definition.Bootstrap.AutomationDigest)
		if platform := f.store.state.Receipt.Definition.Platform; platform != upgradedPlatform {
			t.Fatalf("the fresh setup resolved for %#v", platform)
		}
	})
	t.Run("one with another unresolved action", func(t *testing.T) {
		f, pending, _ := strandedAtTheBound(t, true)
		movePlatform(t, f)
		f.store.state.Receipt.Actions[1].Phase = "intent"
		writes := f.store.writes
		_, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true, PurgeOldBundles: true})
		if found := diagnostics.Of(err); !frozenFor(found) {
			t.Fatalf("a stranded receipt that may have taken effect: %#v (%v)", found, err)
		}
		if receipt := f.store.state.Receipt; f.store.writes != writes || len(f.store.retired) != 0 || receipt.ID != pending.ID || receipt.Status != "pending" {
			t.Fatalf("a refused setup acted: writes=%d retired=%v receipt=%#v", f.store.writes-writes, f.store.retired, receipt)
		}
	})
}

// Setup cancels only a pending receipt of its own that never took effect.
// Below the bound, one whose native transaction holds its intent with no
// recorded preparation may have taken effect, and a receipt naming a context
// is never setup's. Frozen for a platform the host has since left, each still
// refuses before any effect as frozen for other dependencies, with or without
// the flag, and so does preflight, rather than naming
// setup --purge-old-bundles, which would refuse it the same way.
func TestOnlySetupsOwnUnstartedSetupIsCanceledOnAMovedPlatform(t *testing.T) {
	for name, pending := range map[string]func(*testing.T) *fixture{
		"one below the bound whose native transaction may have started": func(t *testing.T) *fixture {
			f, _ := belowTheBound(t, false)
			f.store.state.Receipt.Actions[1].Phase = "intent"
			if receipt := f.store.state.Receipt; neverStarted(receipt) {
				t.Fatalf("the pending receipt never started: %#v", receipt)
			}
			return f
		},
		"one naming a context": func(t *testing.T) *fixture {
			f, _, _ := strandedAtTheBound(t, true)
			f.store.state.Receipt.Context = SetupContext{Name: "example", Revision: "rev-" + strings.Repeat("2", 32), Machine: "controller"}
			return f
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := pending(t)
			if receipt := f.store.state.Receipt; !receipt.Incomplete() {
				t.Fatalf("the receipt is not pending: %#v", receipt)
			}
			movePlatform(t, f)
			f.events = nil
			receipt, writes, areas := copyState(f.store.state).Receipt, f.store.writes, slices.Clone(f.store.areas)
			for _, purge := range []bool{false, true} {
				_, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true, PurgeOldBundles: purge})
				if found := diagnostics.Of(err); !frozenFor(found) {
					t.Fatalf("purge=%t: %#v (%v)", purge, found, err)
				}
			}
			if _, err := f.service.Check(context.Background(), CheckRequest{}); !frozenFor(diagnostics.Of(err)) {
				t.Fatalf("preflight: %#v (%v)", diagnostics.Of(err), err)
			}
			if slices.Contains(f.events, "present") || f.store.writes != writes || len(f.store.retired) != 0 || !slices.Equal(f.store.areas, areas) || !reflect.DeepEqual(f.store.state.Receipt, receipt) {
				t.Fatalf("a refusal acted: events=%v writes=%d retired=%v areas=%#v receipt=%#v", f.events, f.store.writes-writes, f.store.retired, f.store.areas, f.store.state.Receipt)
			}
		})
	}
}

// A stranded receipt with any other unresolved action may have taken effect,
// so it is never abandoned: with or without the flag setup refuses before any
// effect, as it refuses another executable's pending attempt.
func TestAStrandedSetupWithAnotherUnresolvedActionStillRefuses(t *testing.T) {
	for name, unresolve := range map[string]func(*SetupAction, *SetupAction){
		"an intended native transaction": func(_, native *SetupAction) { native.Phase = "intent" },
		"an unknown native outcome": func(_, native *SetupAction) {
			native.Phase, native.Outcome, native.Evidence = "observed", "unknown", object(map[string]any{"ready": false})
		},
		"an unknown bundle outcome": func(bundle, _ *SetupAction) {
			bundle.Phase, bundle.Outcome, bundle.Evidence = "observed", "unknown", object(map[string]any{"ready": false})
		},
	} {
		t.Run(name, func(t *testing.T) {
			f, _, _ := strandedAtTheBound(t, true)
			movedExecutable(t, f)
			unresolve(&f.store.state.Receipt.Actions[0], &f.store.state.Receipt.Actions[1])
			pending, writes, areas := copyState(f.store.state).Receipt, f.store.writes, slices.Clone(f.store.areas)
			for _, purge := range []bool{true, false} {
				_, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true, PurgeOldBundles: purge})
				found := diagnostics.Of(err)
				if len(found) != 1 || found[0].Code != "controller.unknown" || found[0].Message != "another exact setup attempt remains unresolved" {
					t.Fatalf("purge=%t: refusal = %#v (%v)", purge, found, err)
				}
			}
			if slices.Contains(f.events, "present") || f.store.writes != writes || len(f.store.retired) != 0 || !slices.Equal(f.store.areas, areas) {
				t.Fatalf("a refused setup acted: events=%v writes=%d retired=%v areas=%#v", f.events, f.store.writes-writes, f.store.retired, f.store.areas)
			}
			if !reflect.DeepEqual(f.store.state.Receipt, pending) {
				t.Fatalf("the refusal moved the stranded receipt: %#v", f.store.state.Receipt)
			}
		})
	}
}

// The abandonment is decided again under the mutation that records it: a
// stranded receipt that is no longer the one the plan was approved over, or
// whose other action now holds an intent, refuses before anything is canceled
// or retired. So does one whose bundle now holds an area, because its
// cancellation would no longer observe what the plan presented, that no area
// holds that bundle. Each change disarms itself once made.
func TestAnAbandonmentIsDecidedAgainUnderItsMutation(t *testing.T) {
	client := HeldArea{ID: strandedClient}
	for name, change := range map[string]func(*fixture, SetupReceipt){
		"another action now intended": func(f *fixture, _ SetupReceipt) { f.store.state.Receipt.Actions[1].Phase = "intent" },
		"another receipt":             func(f *fixture, _ SetupReceipt) { f.store.state.Receipt.ID = "setup-" + strings.Repeat("e", 32) },
		"another plan":                func(f *fixture, _ SetupReceipt) { f.store.state.Receipt.PlanDigest = strings.Repeat("e", 64) },
		"its bundle now holding an area": func(f *fixture, pending SetupReceipt) {
			f.store.areas[slices.Index(f.store.areas, client)] = HeldArea{ID: pending.CatalogDigest}
		},
	} {
		t.Run(name, func(t *testing.T) {
			f, pending, _ := strandedAtTheBound(t, true)
			movedExecutable(t, f)
			fired, changed, areas := false, SetupReceipt{}, []HeldArea(nil)
			f.beforeMutation = func() {
				f.beforeMutation, fired = nil, true
				change(f, pending)
				changed, areas = copyState(f.store.state).Receipt, slices.Clone(f.store.areas)
			}
			writes := f.store.writes
			_, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true, PurgeOldBundles: true})
			if found := diagnostics.Of(err); !fired || len(found) != 1 || found[0].Code != "controller.conflict" || found[0].Message != "controller state changed after plan confirmation" {
				t.Fatalf("a changed receipt was abandoned (changed: %t): %#v (%v)", fired, found, err)
			}
			receipt := f.store.state.Receipt
			if f.store.writes != writes || len(f.store.retired) != 0 || !slices.Equal(f.store.areas, areas) ||
				!reflect.DeepEqual(receipt, changed) || receipt.Status != "pending" || receipt.Actions[0].Phase != "intent" {
				t.Fatalf("a refused abandonment acted: writes=%d retired=%v areas=%#v receipt=%#v", f.store.writes-writes, f.store.retired, f.store.areas, receipt)
			}
		})
	}
}

// However the abandonment is interrupted, the next setup --purge-old-bundles
// completes it. A lost cancellation leaves the receipt pending and stranded.
// One interrupted after the cancellation, or during the retirement, leaves it
// canceled at the bound, where setup without the flag refuses as at any bound.
// Each interruption disarms itself and reports whether it fired.
func TestAnInterruptedAbandonmentAtTheBoundCompletesOnTheNextPurge(t *testing.T) {
	for name, interrupt := range map[string]func(*fixture) func(error) bool{
		"cancellation lost": func(f *fixture) func(error) bool {
			f.store.failPublication = f.store.writes + 1
			return func(error) bool {
				fired := f.store.writes >= f.store.failPublication
				f.store.failPublication = 0
				return fired
			}
		},
		"after the cancellation": func(f *fixture) func(error) bool {
			busy := errors.New("busy")
			f.store.retireErr = busy
			return func(err error) bool { f.store.retireErr = nil; return errors.Is(err, busy) }
		},
		"during the retirement": func(f *fixture) func(error) bool {
			f.store.interrupted = true
			return func(error) bool { return !f.store.interrupted }
		},
	} {
		t.Run(name, func(t *testing.T) {
			f, pending, _ := strandedAtTheBound(t, true)
			moved := movedExecutable(t, f)
			fired := interrupt(f)
			_, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true, PurgeOldBundles: true})
			if !fired(err) || err == nil {
				t.Fatalf("the interruption did not fire or was not reported: %v", err)
			}
			if name == "cancellation lost" {
				if receipt := f.store.state.Receipt; receipt.ID != pending.ID || receipt.Status != "pending" || receipt.Actions[0].Phase != "intent" {
					t.Fatalf("a lost cancellation moved the stranded receipt: %#v", receipt)
				}
			} else {
				canceledAs(t, f, pending)
				_, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
				refusal(t, err, boundRefusal, boundRemediation)
			}
			report, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true, PurgeOldBundles: true})
			if err != nil || report.Outcome != "changed" {
				t.Fatalf("repeating the setup: %#v %#v", report, diagnostics.Of(err))
			}
			setUpAfresh(t, f, pending, moved)
		})
	}
}

// belowTheBoundRefusal refuses a pending setup below the bound that this
// executable cannot resume, naming no bound the host has not met.
const belowTheBoundRefusal = "a setup left pending on this host never took effect and cannot be resumed by this executable"

// belowTheBound leaves a host as a first setup that lost the publication after
// its first intent left it, below the bound: its pending receipt holds the
// intent of its execution bundle and plans its native transaction. With held
// the record holds the area that bundle names, unsealed, as a preparation
// that began before the loss leaves it. It returns the pending receipt.
func belowTheBound(t *testing.T, held bool) (*fixture, SetupReceipt) {
	t.Helper()
	f, _ := dynamicFixture(t)
	f.store.failPublication = f.store.writes + 2
	if _, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true}); err == nil {
		t.Fatal("the lost intent publication was not reported")
	}
	f.store.failPublication = 0
	f.store.state.Receipt.Actions[0].Phase = "intent"
	if held {
		f.store.areas = append(f.store.areas, HeldArea{ID: f.store.state.Receipt.CatalogDigest})
	}
	pending := copyState(f.store.state).Receipt
	if !pending.Incomplete() || !neverStarted(pending) || stranded(f.store.view()) {
		t.Fatalf("the interrupted setup left receipt %#v over %#v", pending, f.store.areas)
	}
	f.events, f.plan = nil, Report{}
	return f, pending
}

// inventoryInspector is the native inspector with the inventory port. It
// reads digest as the host's installed package inventory, or fails with err,
// and counts its reads.
type inventoryInspector struct {
	*resolvingFixture
	digest string
	err    error
	reads  int
}

func (i *inventoryInspector) Inventory(context.Context, Platform) (string, error) {
	i.reads++
	return i.digest, i.err
}

// nativeBefore and nativeAfter are the package inventories, before and after
// it, of the podman upgrade nativeNeverStarted plans.
var nativeBefore, nativeAfter = strings.Repeat("d", 64), strings.Repeat("f", 64)

// nativeNeverStarted leaves a host as a setup whose native transaction
// refused before authorizing one, as every Python-side download did before
// B297: a completed setup, then one that published its bundle, recorded the
// preparation of a podman upgrade and failed with no definitive result, so its
// receipt stays pending with that transaction's intent. The host's inventory
// still reads as the preparation's before-state, and the installer's next
// transaction succeeds. It returns the pending receipt, the installer and the
// inventory port.
func nativeNeverStarted(t *testing.T) (*fixture, SetupReceipt, *testRuntimeInstaller, *inventoryInspector) {
	t.Helper()
	f, r := dynamicFixture(t)
	base := r.native
	if _, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	installer := &testRuntimeInstaller{owner: f, result: ActionResult{Outcome: "unknown"}, err: failure("controller.unknown", "the Ansible operation has no complete result", "")}
	f.service.runtime = installer
	f.host.runtime = RuntimeInspection{}
	f.bundle.ready, f.bundle.sealed = false, false
	upgradePodman(t, r, base, "1.2.4", nativeBefore)
	if _, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true}); err == nil {
		t.Fatal("the refused native transaction was not reported")
	}
	pending := copyState(f.store.state).Receipt
	if _, prepared := nativePrepared(pending); !pending.Incomplete() || !prepared || !holdsArea(f.store.areas, pending.CatalogDigest) {
		t.Fatalf("the refused native transaction left receipt %#v", pending)
	}
	inventory := &inventoryInspector{resolvingFixture: r, digest: nativeBefore}
	f.service.options.NativeInspector = inventory
	installer.result, installer.err = ActionResult{Outcome: "changed", Evidence: object(map[string]any{"nativePostcondition": "verified"})}, nil
	f.events, f.plan = nil, Report{}
	return f, pending, installer, inventory
}

// untouched fails the test unless a refusal left the store as it found it and
// presented no plan.
func untouched(t *testing.T, f *fixture, pending SetupReceipt, writes int, areas []HeldArea) {
	t.Helper()
	if slices.Contains(f.events, "present") || f.store.writes != writes || len(f.store.retired) != 0 || !slices.Equal(f.store.areas, areas) || !reflect.DeepEqual(f.store.state.Receipt, pending) {
		t.Fatalf("a refusal acted: events=%v writes=%d retired=%v areas=%#v receipt=%#v", f.events, f.store.writes-writes, f.store.retired, f.store.areas, f.store.state.Receipt)
	}
}

// A setup that never started, below the bound, is canceled under
// --purge-old-bundles once the host's release moved since it froze its
// dependencies (D93), whether or not its bundle's area was reserved. The plan
// names the cancellation and no retirement, the fresh setup resolves for the
// release the host now runs and completes, and the purge after it retires
// what the canceled receipt left: its area when it held one, and otherwise its
// resolution.
func TestPurgeBelowTheBoundCancelsANeverStartedSetupAfterAHostReleaseChange(t *testing.T) {
	for name, held := range map[string]bool{"its bundle holding no area": false, "its bundle's area unsealed": true} {
		t.Run(name, func(t *testing.T) {
			f, pending := belowTheBound(t, held)
			movePlatform(t, f)
			report, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true, PurgeOldBundles: true})
			if err != nil || report.Outcome != "changed" {
				t.Fatalf("the cancellation under the flag: %#v %#v", report, diagnostics.Of(err))
			}
			action := "Cancel the setup left pending on this host, which this executable cannot resume and which never published its bundle"
			if held {
				action = "Cancel the setup left pending on this host, which this executable cannot resume and which never completed its bundle"
			}
			if !slices.Contains(f.plan.Actions, action) || slices.ContainsFunc(f.plan.Actions, func(line string) bool { return strings.HasPrefix(line, "Retire ") }) {
				t.Fatalf("the plan did not present the cancellation alone: %q", f.plan.Actions)
			}
			receipt := f.store.state.Receipt
			if receipt.ID == pending.ID || receipt.Status != "complete" || receipt.Definition.Platform != upgradedPlatform {
				t.Fatalf("no fresh setup for the moved release took the pending receipt's place: %#v", receipt)
			}
			if held && (!slices.Equal(report.RetiredBundles, []string{pending.CatalogDigest}) || len(f.store.retiredResolutions) != 0) ||
				!held && (len(report.RetiredBundles) != 0 || !slices.Equal(f.store.retiredResolutions, []string{pending.Definition.ResolutionDigest})) {
				t.Fatalf("the purge after completion retired bundles %v and resolutions %v", report.RetiredBundles, f.store.retiredResolutions)
			}
			if retained := f.store.state.RetainedDefinitions; !slices.Equal(f.store.areas, []HeldArea{{ID: receipt.CatalogDigest}}) || len(retained) != 1 || retained[0].ResolutionDigest != receipt.Definition.ResolutionDigest {
				t.Fatalf("the host keeps areas %#v and resolutions %#v", f.store.areas, retained)
			}
		})
	}
}

// Below the bound, a pending setup of setup's own that this executable cannot
// resume and that never took effect refuses setup without the flag, and
// preflight, before any effect: controller.conflict, the purge as the next
// command, and nothing said of a bound the host has not met, whether the
// host's release moved or its executable did.
func TestBelowTheBoundAnUnresumableSetupNamesPurgeOldBundles(t *testing.T) {
	for name, move := range map[string]func(*testing.T, *fixture){
		"a host release change": movePlatform,
		"a moved executable":    func(t *testing.T, f *fixture) { movedExecutable(t, f) },
	} {
		t.Run(name, func(t *testing.T) {
			f, pending := belowTheBound(t, false)
			move(t, f)
			writes, areas := f.store.writes, slices.Clone(f.store.areas)
			report, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
			refusal(t, err, belowTheBoundRefusal, unresumableRemediation)
			if report == nil || report.Next != purgeInvocation {
				t.Fatalf("setup's refusal offers %#v", report)
			}
			report, err = f.service.Check(context.Background(), CheckRequest{})
			refusal(t, err, belowTheBoundRefusal, unresumableRemediation)
			if name == "a moved executable" && report == nil || report != nil && (report.Outcome != "not-ready" || report.Next != purgeInvocation) {
				t.Fatalf("preflight's refusal offers %#v", report)
			}
			untouched(t, f, pending, writes, areas)
		})
	}
}

// A setup whose native transaction refused before authorizing one, its
// bundle published and its preparation recorded, is canceled under
// --purge-old-bundles by the executable that follows (D93), once the host's
// package inventory still reads as that preparation's before-state: without
// the flag setup and preflight refuse naming the purge, and with it the
// native action is observed canceled over the unchanged inventory, a fresh
// setup completes, the purge after it retires the canceled receipt's area, and
// the next setup finds the host ready.
func TestPurgeCancelsASetupWhoseNativeTransactionNeverStarted(t *testing.T) {
	f, pending, installer, inventory := nativeNeverStarted(t)
	movedExecutable(t, f)
	writes, areas := f.store.writes, slices.Clone(f.store.areas)
	if _, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true}); diagnostics.Of(err) == nil || diagnostics.Of(err)[0].Remediation != unresumableRemediation {
		t.Fatalf("setup without the flag: %#v", diagnostics.Of(err))
	}
	if _, err := f.service.Check(context.Background(), CheckRequest{}); diagnostics.Of(err) == nil || diagnostics.Of(err)[0].Remediation != unresumableRemediation {
		t.Fatalf("preflight: %#v", diagnostics.Of(err))
	}
	untouched(t, f, pending, writes, areas)
	report, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true, PurgeOldBundles: true})
	if err != nil || report.Outcome != "changed" {
		t.Fatalf("the cancellation under the flag: %#v %#v", report, diagnostics.Of(err))
	}
	if !slices.Contains(f.plan.Actions, "Cancel the setup left pending on this host, which this executable cannot resume and whose native package transaction never started, as its unchanged package inventory shows") {
		t.Fatalf("the plan did not present the cancellation: %q", f.plan.Actions)
	}
	receipt := f.store.state.Receipt
	if receipt.ID == pending.ID || receipt.Status != "complete" || installer.calls != 2 || inventory.reads == 0 {
		t.Fatalf("no fresh setup took the pending receipt's place: %#v, %d transactions, %d inventory reads", receipt, installer.calls, inventory.reads)
	}
	if !slices.Contains(report.RetiredBundles, pending.CatalogDigest) || !slices.Equal(f.store.areas, []HeldArea{{ID: receipt.CatalogDigest}}) {
		t.Fatalf("the purge after completion retired %v and the host keeps %#v", report.RetiredBundles, f.store.areas)
	}
	report, err = f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	if err != nil || report.Outcome != "unchanged" || installer.calls != 2 {
		t.Fatalf("the next setup: %#v %#v", report, diagnostics.Of(err))
	}
}

// A setup whose native action holds its intent is canceled only on proof that
// its transaction never started. With an inventory that differs from the
// preparation's before-state, one that reads as its after-state, one that
// cannot be read, or no inventory port at all, setup with or without the
// flag, and preflight, refuse with controller.unknown before any effect, as
// they refuse another executable's pending attempt.
func TestASetupWhoseInventoryMovedIsNeverCanceled(t *testing.T) {
	for name, observe := range map[string]func(*fixture, *inventoryInspector){
		"an inventory that differs": func(_ *fixture, inventory *inventoryInspector) { inventory.digest = strings.Repeat("e", 64) },
		"the after-state":           func(_ *fixture, inventory *inventoryInspector) { inventory.digest = nativeAfter },
		"an unreadable inventory":   func(_ *fixture, inventory *inventoryInspector) { inventory.err = errors.New("native database is busy") },
		"no inventory port":         func(f *fixture, _ *inventoryInspector) { f.service.options.NativeInspector = f.resolution },
	} {
		t.Run(name, func(t *testing.T) {
			f, pending, _, inventory := nativeNeverStarted(t)
			movedExecutable(t, f)
			observe(f, inventory)
			writes, areas := f.store.writes, slices.Clone(f.store.areas)
			unresolved := func(what string, err error) {
				if found := diagnostics.Of(err); len(found) != 1 || found[0].Code != "controller.unknown" || found[0].Message != "another exact setup attempt remains unresolved" {
					t.Fatalf("%s: %#v (%v)", what, found, err)
				}
			}
			for _, purge := range []bool{false, true} {
				_, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true, PurgeOldBundles: purge})
				unresolved("setup", err)
			}
			_, err := f.service.Check(context.Background(), CheckRequest{})
			unresolved("preflight", err)
			untouched(t, f, pending, writes, areas)
		})
	}
}

// A pending setup this executable can resume is resumed, never canceled, even
// under --purge-old-bundles and over an unchanged inventory: its recorded
// native transaction is recovered exactly and the receipt completes, and its
// inventory is never read to judge a cancellation.
func TestAPendingSetupThisExecutableCanResumeIsNotCanceled(t *testing.T) {
	f, pending, installer, inventory := nativeNeverStarted(t)
	f.host.runtime = RuntimeInspection{Present: true, Ready: true}
	report, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true, PurgeOldBundles: true})
	if err != nil || report.Outcome != "changed" {
		t.Fatalf("the resumed setup: %#v %#v", report, diagnostics.Of(err))
	}
	receipt := f.store.state.Receipt
	if receipt.ID != pending.ID || receipt.Status != "complete" || installer.recovers != 1 || inventory.reads != 0 ||
		slices.ContainsFunc(f.plan.Actions, func(line string) bool { return strings.HasPrefix(line, "Cancel ") }) {
		t.Fatalf("the pending receipt was not resumed exactly: %#v, %d recoveries, %d inventory reads, plan %q", receipt, installer.recovers, inventory.reads, f.plan.Actions)
	}
}

// The cancellation below the bound is decided again under the mutation that
// records it: a receipt whose native transaction now holds an intent, whose
// bundle now holds an area its plan said it lacked, whose plan changed, or
// whose package inventory moved since the plan was presented refuses before
// anything is canceled. Each change disarms itself once made.
func TestTheCancellationBelowTheBoundIsDecidedAgainUnderItsMutation(t *testing.T) {
	type pending struct {
		f       *fixture
		receipt SetupReceipt
		native  *inventoryInspector
	}
	neverStartedSetup := func(t *testing.T) pending {
		f, receipt := belowTheBound(t, false)
		movePlatform(t, f)
		return pending{f: f, receipt: receipt}
	}
	refusedTransaction := func(t *testing.T) pending {
		f, receipt, _, inventory := nativeNeverStarted(t)
		movedExecutable(t, f)
		return pending{f: f, receipt: receipt, native: inventory}
	}
	for name, test := range map[string]struct {
		left   func(*testing.T) pending
		change func(pending)
	}{
		"another action now intended": {neverStartedSetup, func(p pending) { p.f.store.state.Receipt.Actions[1].Phase = "intent" }},
		"its bundle now holding an area": {neverStartedSetup, func(p pending) {
			p.f.store.areas = append(p.f.store.areas, HeldArea{ID: p.receipt.CatalogDigest})
		}},
		"another plan":                {refusedTransaction, func(p pending) { p.f.store.state.Receipt.PlanDigest = strings.Repeat("e", 64) }},
		"its package inventory moved": {refusedTransaction, func(p pending) { p.native.digest = nativeAfter }},
	} {
		t.Run(name, func(t *testing.T) {
			p := test.left(t)
			fired, changed, areas := false, SetupReceipt{}, []HeldArea(nil)
			p.f.beforeMutation = func() {
				p.f.beforeMutation, fired = nil, true
				test.change(p)
				changed, areas = copyState(p.f.store.state).Receipt, slices.Clone(p.f.store.areas)
			}
			writes := p.f.store.writes
			_, err := p.f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true, PurgeOldBundles: true})
			if found := diagnostics.Of(err); !fired || len(found) != 1 || found[0].Code != "controller.conflict" || found[0].Message != "controller state changed after plan confirmation" {
				t.Fatalf("a changed receipt was canceled (changed: %t): %#v (%v)", fired, found, err)
			}
			if p.f.store.writes != writes || len(p.f.store.retired) != 0 || !slices.Equal(p.f.store.areas, areas) || !reflect.DeepEqual(p.f.store.state.Receipt, changed) || !changed.Incomplete() {
				t.Fatalf("a refused cancellation acted: writes=%d retired=%v areas=%#v receipt=%#v", p.f.store.writes-writes, p.f.store.retired, p.f.store.areas, p.f.store.state.Receipt)
			}
		})
	}
}

// However the cancellation below the bound is interrupted, the next
// setup --purge-old-bundles completes it. A lost cancellation leaves the
// receipt pending; one interrupted after the cancellation leaves it canceled
// as it observed it, settled, so the next setup sets the host up afresh
// whatever it is run with. Each interruption disarms itself.
func TestAnInterruptedCancellationBelowTheBoundCompletesOnTheNextPurge(t *testing.T) {
	type shape struct {
		left     func(*testing.T) (*fixture, SetupReceipt)
		index    int
		evidence string
	}
	shapes := map[string]shape{
		"a setup that never started": {func(t *testing.T) (*fixture, SetupReceipt) {
			f, pending := belowTheBound(t, false)
			movePlatform(t, f)
			return f, pending
		}, 0, `{"bundleArea":"absent"}`},
		"a setup over an unsealed area": {func(t *testing.T) (*fixture, SetupReceipt) {
			f, pending := belowTheBound(t, true)
			movePlatform(t, f)
			return f, pending
		}, 0, `{"bundleArea":"unsealed"}`},
		"a native transaction that never started": {func(t *testing.T) (*fixture, SetupReceipt) {
			f, pending, _, _ := nativeNeverStarted(t)
			movedExecutable(t, f)
			return f, pending
		}, 1, `{"nativeInventory":"unchanged"}`},
	}
	for shapeName, test := range shapes {
		for _, lost := range []bool{true, false} {
			name := shapeName + ", after the cancellation"
			if lost {
				name = shapeName + ", cancellation lost"
			}
			t.Run(name, func(t *testing.T) {
				f, pending := test.left(t)
				f.store.failPublication = f.store.writes + 2
				if lost {
					f.store.failPublication = f.store.writes + 1
				}
				_, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true, PurgeOldBundles: true})
				if err == nil || f.store.writes < f.store.failPublication {
					t.Fatalf("the interruption did not fire or was not reported: %v", err)
				}
				f.store.failPublication = 0
				if lost {
					if !reflect.DeepEqual(f.store.state.Receipt, pending) {
						t.Fatalf("a lost cancellation moved the pending receipt: %#v", f.store.state.Receipt)
					}
				} else {
					canceledWith(t, f, pending, test.index, test.evidence)
				}
				report, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true, PurgeOldBundles: true})
				if err != nil || report.Outcome != "changed" {
					t.Fatalf("repeating the setup: %#v %#v", report, diagnostics.Of(err))
				}
				if receipt := f.store.state.Receipt; receipt.ID == pending.ID || receipt.Status != "complete" || !slices.Equal(f.store.areas, []HeldArea{{ID: receipt.CatalogDigest}}) {
					t.Fatalf("the repeated setup left receipt %#v over %#v", receipt, f.store.areas)
				}
			})
		}
	}
}
