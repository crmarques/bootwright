//go:build linux && amd64

package contextfs

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/clients"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// solvingAgain is the host's package manager as a controller stage meets it
// when the libvirt client keeps being removed: every solve is against an
// inventory that moved, and installs the client at the release the publisher
// then offers, so each one freezes a new resolution. The client reads as
// installed only once the stage's transaction has run.
type solvingAgain struct {
	solves    int
	installed bool
}

func (n *solvingAgain) Resolve(_ context.Context, platform prerequisites.Platform, requirements prerequisites.NativeRequirements, versions controller.DependencyVersions, _ prerequisites.SetupEgress) (prerequisites.NativeResolvedPlan, error) {
	n.solves++
	plan := prerequisites.NativeResolvedPlan{
		Format: "bootwright.native-plan-v1", Platform: platform, Solver: "dnf5", SolverVersion: "5.2.0", Requests: versions, Requirements: requirements,
		Repositories: []prerequisites.NativeRepository{{ID: "base", BaseURL: "https://packages.example.test/fedora", MetadataSHA256: strings.Repeat("d", 64)}},
		Roots:        []prerequisites.NativeRoot{}, Packages: []prerequisites.NativePackage{}, Actions: []prerequisites.NativeAction{},
		BeforeSHA256: fmt.Sprintf("%064x", n.solves), AfterSHA256: strings.Repeat("e", 64),
	}
	client := strconv.Itoa(n.solves) + ".fc43"
	for _, root := range []struct{ key, name, id, release string }{
		{"podman", "podman", "podman", "1.fc43"}, {"openssh", "openssh-clients", "openssh-clients", "1.fc43"}, {"nmstate", "nmstate", "nmstate", "1.fc43"},
		{"libvirt", "libvirt-client", "libvirt-client-" + client, client},
	} {
		identity := prerequisites.NativeIdentity{Name: root.name, Version: "1.2.3", Release: root.release, Architecture: "x86_64"}
		source := prerequisites.DependencySource{ID: root.id, URL: "https://packages.example.test/fedora/" + root.id + ".rpm", SHA256: strings.Repeat("a", 64), Bytes: 64}
		plan.Roots = append(plan.Roots, prerequisites.NativeRoot{Key: root.key, Requested: "latest", Package: identity})
		plan.Packages = append(plan.Packages, prerequisites.NativePackage{
			Name: identity.Name, Version: identity.Version, Release: identity.Release, Architecture: identity.Architecture,
			Signer: strings.Repeat("f", 40), Source: source,
		})
		if root.key == "libvirt" {
			plan.Actions = append(plan.Actions, prerequisites.NativeAction{Kind: "install", After: identity, SourceID: source.ID, Reason: "root"})
		}
	}
	return prerequisites.CanonicalNativePlan(plan)
}

func (n *solvingAgain) Check(context.Context, prerequisites.NativeResolvedPlan) (prerequisites.NativePresence, error) {
	if !n.installed {
		return prerequisites.NativePresence{}, nil
	}
	return prerequisites.NativePresence{Ready: true, Installed: []prerequisites.NativeRootPresence{{Key: "libvirt"}}}, nil
}

func (n *solvingAgain) Clients(context.Context, prerequisites.ClientInstallation) (prerequisites.ActionResult, error) {
	n.installed = true
	return prerequisites.ActionResult{Outcome: "changed"}, nil
}

func controllerView(t *testing.T, store *Store) prerequisites.StorageView {
	t.Helper()
	var held prerequisites.StorageView
	if err := store.ReadController(context.Background(), "", func(view prerequisites.StorageView) error {
		held = view
		return nil
	}); err != nil {
		t.Fatalf("controller read failed: %#v", diagnostics.Of(err))
	}
	return held
}

func retainedResolutions(t *testing.T, store *Store) prerequisites.HostState {
	t.Helper()
	return controllerView(t, store).State
}

// solveLibvirtClient runs the real controller stage once over the real store,
// asking for the latest libvirt client on a host that has lost it, so the
// stage solves again. Unless retiring, it retains as a build that retired no
// resolution did: naming none as superseded.
func solveLibvirtClient(t *testing.T, store *Store, record contexts.Record, native *solvingAgain, retiring bool) error {
	t.Helper()
	ctx := context.Background()
	canonical, err := clients.Request{
		Egress: prerequisites.SetupEgress{NoProxy: []string{}}, Libvirt: "latest", LibvirtClient: true,
		Machine: "controller", Tools: []clients.ToolRequest{}, Version: clients.Version,
	}.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	block := reconciliation.Block{
		BlockDefinition: reconciliation.BlockDefinition{
			ID: clients.BlockID, Stage: reconciliation.StageController, Kind: clients.Kind,
			Implementation: clients.Implementation, Request: canonical,
		},
		RequestDigest: strings.Repeat("f", 64),
	}
	native.installed = false
	return store.MutateLifecycle(ctx, record.Name, func(tx lifecycle.Transaction) error {
		retain := tx.RetainDependencies
		if !retiring {
			retain = func(ctx context.Context, definition *prerequisites.Definition, sources []prerequisites.DependencySource, _ []string) error {
				return tx.RetainDependencies(ctx, definition, sources, nil)
			}
		}
		result, err := clients.New(retainedCatalog{}, native, native, native).Apply(ctx, lifecycle.Execution{Block: block, Stage: &lifecycle.ControllerStage{
			Setup: tx.Controller(), ClientArea: tx.ClientArea, SealClientArea: tx.SealClientArea,
			RetainDependencies: retain,
			Prepare:            func(context.Context, prerequisites.NativePreparation) error { return nil },
			ReleaseFoundation:  func() error { return nil },
		}})
		if err == nil && result.Outcome != reconciliation.OutcomeChanged {
			t.Fatalf("solve %d: the controller stage reported %q", native.solves, result.Outcome)
		}
		return err
	})
}

// requireSetupAndLatest requires the host to retain exactly the setup's own
// resolution and the one the stage's latest solve froze.
func requireSetupAndLatest(t *testing.T, store *Store, setup prerequisites.Definition, native *solvingAgain) {
	t.Helper()
	held := retainedResolutions(t, store)
	if len(held.RetainedDefinitions) != 2 || !prerequisites.SameDefinition(held.RetainedDefinitions[0], setup) {
		t.Fatalf("solve %d: retained %d resolutions, want the setup's and the latest", native.solves, len(held.RetainedDefinitions))
	}
	latest := held.RetainedDefinitions[1]
	if latest.Native == nil || !slices.ContainsFunc(latest.Native.Roots, func(root prerequisites.NativeRoot) bool {
		return root.Package.Name == "libvirt-client" && root.Package.Release == strconv.Itoa(native.solves)+".fc43"
	}) {
		t.Fatalf("solve %d: the retained stage resolution is not this solve's", native.solves)
	}
}

// Every solve a controller stage makes when the libvirt client is missing
// freezes a new resolution, and retaining it retires the one it supersedes,
// so solving again past the host's bound of retained resolutions keeps just
// the setup's own and the latest.
func TestAControllerStageSolvingAgainPastTheBoundKeepsWithinIt(t *testing.T) {
	store, record := lifecycleFixture(t)
	resolvedSetupFixture(t, store, record)
	native := &solvingAgain{}
	setup := retainedResolutions(t, store).Receipt.Definition
	for solve := 1; solve <= maxControllerBundles+1; solve++ {
		if err := solveLibvirtClient(t, store, record, native, true); err != nil {
			t.Fatalf("solve %d: the controller stage failed: %#v", solve, diagnostics.Of(err))
		}
		if native.solves != solve {
			t.Fatalf("solve %d: the controller stage solved %d times", solve, native.solves)
		}
		requireSetupAndLatest(t, store, *setup, native)
	}
}

// A host that a build retiring no resolution filled to the bound with the
// stage's own resolutions heals on its next solve: that publication retires
// the resolutions the new one supersedes before the bound is judged, so the
// bound never refuses it.
func TestAHostAtTheBoundHealsOnItsNextControllerStageSolve(t *testing.T) {
	store, record := lifecycleFixture(t)
	resolvedSetupFixture(t, store, record)
	native := &solvingAgain{}
	setup := retainedResolutions(t, store).Receipt.Definition
	for native.solves < maxControllerBundles-1 {
		if err := solveLibvirtClient(t, store, record, native, false); err != nil {
			t.Fatalf("filling the bound: solve %d failed: %#v", native.solves, diagnostics.Of(err))
		}
	}
	if held := retainedResolutions(t, store); len(held.RetainedDefinitions) != maxControllerBundles {
		t.Fatalf("the fixture retained %d resolutions, want the bound of %d", len(held.RetainedDefinitions), maxControllerBundles)
	}
	if err := solveLibvirtClient(t, store, record, native, true); err != nil {
		t.Fatalf("the solve at the bound failed: %#v", diagnostics.Of(err))
	}
	requireSetupAndLatest(t, store, *setup, native)
}

// Which resolutions are superseded is the stage's judgement, but the store
// still refuses to retire the one the receipt carries, or the last naming an
// area it holds, and changes nothing when it refuses.
func TestAStageRetiresNoResolutionAHeldBundleNeeds(t *testing.T) {
	ctx := context.Background()
	store, record := lifecycleFixture(t)
	resolvedSetupFixture(t, store, record)
	native := &solvingAgain{}
	solve := func() prerequisites.Definition {
		plan, err := native.Resolve(ctx, prerequisites.Platform{OS: "fedora", Release: "43", Architecture: "amd64"},
			prerequisites.NativeRequirements{ContainerRuntime: true, LibvirtClient: true}, controller.DefaultDependencyVersions(), prerequisites.SetupEgress{})
		if err != nil {
			t.Fatal(err)
		}
		definition, err := prerequisites.NewResolvedDefinition(*retainedResolutions(t, store).Receipt.Definition.Bootstrap, plan)
		if err != nil {
			t.Fatal(err)
		}
		return definition
	}
	named, next := solve(), solve()
	err := store.MutateLifecycle(ctx, record.Name, func(tx lifecycle.Transaction) error {
		if err := tx.RetainDependencies(ctx, &named, named.Sources, nil); err != nil {
			return err
		}
		_, err := tx.ClientArea(ctx, named.CatalogDigest)
		return err
	})
	if err != nil {
		t.Fatalf("the fixture failed: %#v", diagnostics.Of(err))
	}
	before := retainedResolutions(t, store)
	for what, refused := range map[string]struct{ digest, reason string }{
		"the receipt's own resolution":           {before.Receipt.Definition.ResolutionDigest, "the resolution this receipt carries may not be retired"},
		"the last resolution naming a held area": {named.ResolutionDigest, "no other names its bundle"},
	} {
		err := store.MutateLifecycle(ctx, record.Name, func(tx lifecycle.Transaction) error {
			return tx.RetainDependencies(ctx, &next, next.Sources, []string{refused.digest})
		})
		if reported := diagnostics.Of(err); len(reported) == 0 || !strings.Contains(reported[0].Message, refused.reason) {
			t.Fatalf("retiring %s was not refused because %q: %v", what, refused.reason, err)
		}
		if after := retainedResolutions(t, store); !slices.EqualFunc(after.RetainedDefinitions, before.RetainedDefinitions, prerequisites.SameDefinition) {
			t.Fatalf("refusing to retire %s changed the retained resolutions", what)
		}
	}
}

// readyHost answers setup as the host a completed setup left ready: the
// identity the store recorded, the resolution that setup's receipt carries,
// its sealed bundle ready and its native roots installed. It answers nothing
// else a setup could ask.
type readyHost struct {
	prerequisites.Compiler
	prerequisites.HostInspector
	prerequisites.BundleManager
	identity   controller.InstalledHostIdentity
	definition prerequisites.Definition
}

func (h readyHost) Platform(context.Context) (prerequisites.Platform, error) {
	return h.definition.Platform, nil
}

func (h readyHost) Identity(context.Context) (controller.InstalledHostIdentity, error) {
	return h.identity, nil
}

func (h readyHost) Select(prerequisites.Platform, prerequisites.NativeRequirements) (prerequisites.Definition, error) {
	return prerequisites.CloneDefinition(h.definition), nil
}

func (readyHost) ValidateEgress(prerequisites.SetupEgress) error { return nil }

func (readyHost) Inspect(context.Context, prerequisites.BundleArea, prerequisites.Definition, bool) (prerequisites.BundleInspection, error) {
	return prerequisites.BundleInspection{Ready: true, Sealed: true, Recoverable: true}, nil
}

func (readyHost) Check(context.Context, prerequisites.NativeResolvedPlan) (prerequisites.NativePresence, error) {
	return prerequisites.NativePresence{Ready: true}, nil
}

// A controller stage's resolution names no bundle area, as the one a canceled
// setup carried does once a later receipt replaced it, and the store, which
// keeps no kind per resolution, drops either alike. Only the canceled setup's
// is setup's own to retire, so setup --purge-old-bundles over the real store
// leaves the stage's resolution, retiring and reporting nothing, however often
// it runs.
func TestSetupsPurgeLeavesAControllerStageResolution(t *testing.T) {
	store, record := lifecycleFixture(t)
	resolvedSetupFixture(t, store, record)
	if err := solveLibvirtClient(t, store, record, &solvingAgain{}, true); err != nil {
		t.Fatalf("the controller stage failed: %#v", diagnostics.Of(err))
	}
	before := controllerView(t, store)
	held := before.State.RetainedDefinitions
	if len(held) != 2 || !held[1].NativeRequirements.LibvirtClient ||
		slices.ContainsFunc(before.Areas, func(area prerequisites.HeldArea) bool { return area.ID == held[1].CatalogDigest }) {
		t.Fatalf("the stage retained no resolution without an area beside the setup's: %d resolutions over %#v", len(held), before.Areas)
	}
	host := readyHost{identity: before.State.Host, definition: *before.State.Receipt.Definition}
	service := prerequisites.New(store, host, host, host, host, nil, prerequisites.Options{NativeInspector: host})
	for purge := 1; purge <= 2; purge++ {
		report, err := service.Setup(context.Background(), prerequisites.SetupRequest{PurgeOldBundles: true})
		if err != nil || report.Outcome != "unchanged" || len(report.RetiredBundles) != 0 {
			t.Fatalf("purge %d: %#v %#v", purge, report, diagnostics.Of(err))
		}
		after := controllerView(t, store)
		if !slices.EqualFunc(after.State.RetainedDefinitions, held, prerequisites.SameDefinition) || !slices.Equal(after.Areas, before.Areas) {
			t.Fatalf("purge %d retired what the stage retained: %d resolutions over %#v", purge, len(after.State.RetainedDefinitions), after.Areas)
		}
	}
}
