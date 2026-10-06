package prerequisites

import (
	"context"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

var fedora = Platform{"fedora", "43", "amd64"}

// closureInspector answers as the host's package manager: every root of a
// plan installed but the keys it lacks, and on RHEL what the operator
// installed. It records every plan it was asked about.
type closureInspector struct {
	absent    map[string]bool
	checked   []string
	operator  OperatorPresence
	operators int
}

func (i *closureInspector) Check(_ context.Context, plan NativeResolvedPlan) (NativePresence, error) {
	i.checked = append(i.checked, plan.Digest)
	presence := NativePresence{Ready: true}
	for _, root := range plan.Roots {
		if i.absent[root.Key] {
			presence.Ready = false
			continue
		}
		presence.Installed = append(presence.Installed, NativeRootPresence{Key: root.Key, Package: root.Package})
	}
	return presence, nil
}

func (i *closureInspector) OperatorRoots(context.Context, Platform, []string) (OperatorPresence, error) {
	i.operators++
	return i.operator, nil
}

// stageResolution is a controller stage's retained resolution of these
// requirements, named by digest.
func stageResolution(platform Platform, requirements NativeRequirements, digest string) Definition {
	plan := &NativeResolvedPlan{Platform: platform, Requirements: requirements, Digest: digest}
	keys := []string{"podman", "openssh", "nmstate"}
	if requirements.LibvirtClient {
		keys = append(keys, "libvirt")
	}
	if requirements.Hypervisor {
		keys = append(keys, "hypervisor")
	}
	if requirements.InstallerMedia {
		keys = append(keys, "installer-media")
	}
	for _, key := range keys {
		for _, name := range NativeRootNames()[key] {
			plan.Roots = append(plan.Roots, NativeRoot{Key: key, Requested: "latest", Package: NativeIdentity{Name: name, Version: "1.2.3", Release: "1", Architecture: "x86_64"}})
		}
	}
	return Definition{Platform: platform, Versions: controller.DefaultDependencyVersions(), NativeRequirements: requirements, ResolutionDigest: digest, Native: plan}
}

func hostedProvider() api.Object {
	return api.NewObject(api.InfraProvider, "hosted", api.Value{}, api.MapValue().WithPath(api.StringValue("controller"), "libvirt", "machineRef"))
}

// boundContext is a prepared host whose context named example selects the
// extra objects and is bound to it, with the inspector answering its closures.
func boundContext(t *testing.T, platform Platform, inspector NativeInspector, retained []Definition, extra ...api.Object) *fixture {
	t.Helper()
	f := newFixture(t, platform)
	f.store.scope = SetupContext{Name: "example", Revision: "rev-" + strings.Repeat("2", 32)}
	f.compiler.extra = extra
	if _, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	digest, err := f.host.identity.PrivateDigest()
	if err != nil {
		t.Fatal(err)
	}
	f.store.state.Bindings = []ControllerBinding{{Context: "example", Machine: "controller", HostDigest: digest}}
	f.store.state.RetainedDefinitions = append(f.store.state.RetainedDefinitions, retained...)
	f.service.options.NativeInspector = inspector
	return f
}

func checkOf(report *Report, id string) (Check, bool) {
	index := slices.IndexFunc(report.Checks, func(check Check) bool { return check.ID == id })
	if index < 0 {
		return Check{}, false
	}
	return report.Checks[index], true
}

// Preflight reads only the latest resolution of the context's own selection.
// An older installed resolution of another selection proves nothing about
// this one's hypervisor, so a missing hypervisor root is not ready and that
// older plan is never inspected.
func TestPreflightOverAnotherSelectionsResolutionIsNotReady(t *testing.T) {
	inspector := &closureInspector{absent: map[string]bool{"hypervisor": true}}
	older := stageResolution(fedora, NativeRequirements{ContainerRuntime: true, LibvirtClient: true}, "older")
	own := stageResolution(fedora, NativeRequirements{ContainerRuntime: true, LibvirtClient: true, Hypervisor: true}, "own")
	f := boundContext(t, fedora, inspector, []Definition{older, own}, hostedProvider())
	report, err := f.service.Check(context.Background(), CheckRequest{ContextName: "example"})
	if code(err) != "preflight.failed" || report == nil || report.Outcome != "not-ready" {
		t.Fatalf("preflight = %#v %v", report, err)
	}
	if client, _ := checkOf(report, "libvirt-client"); client.Status != "ready" {
		t.Fatalf("libvirt client = %+v", client)
	}
	if hypervisor, found := checkOf(report, "hypervisor"); !found || hypervisor.Status != "not-ready" || hypervisor.Scope != ContextScope {
		t.Fatalf("hypervisor = %+v", hypervisor)
	}
	if setup := f.store.state.Receipt.Definition.Native.Digest; !slices.Equal(inspector.checked, []string{setup, "own"}) {
		t.Fatalf("preflight inspected %v, want setup's own resolution and its own selection's latest", inspector.checked)
	}
	if report.Next != "bootwright apply --stage controller --context example" {
		t.Fatalf("next = %q", report.Next)
	}
}

// Each closure the context's stage selects is declared, settled exactly as
// the stage reads it, and required for readiness; a closure it does not
// select is not reported at all.
func TestPreflightChecksTheHypervisorAndInstallerMediaClosures(t *testing.T) {
	every := NativeRequirements{ContainerRuntime: true, LibvirtClient: true, Hypervisor: true, InstallerMedia: true}
	selected := append([]api.Object{hostedProvider()}, installerMediaGraph()...)
	for _, test := range []struct {
		name   string
		absent string
	}{{name: "every closure present"}, {name: "the hypervisor missing", absent: "hypervisor"}, {name: "the installer media missing", absent: "installer-media"}} {
		t.Run(test.name, func(t *testing.T) {
			inspector := &closureInspector{absent: map[string]bool{test.absent: true}}
			retained := []Definition{stageResolution(fedora, every, "own")}
			f := boundContext(t, fedora, inspector, retained, selected...)
			report, err := f.service.Check(context.Background(), CheckRequest{ContextName: "example"})
			if report == nil {
				t.Fatalf("preflight reported nothing: %v", err)
			}
			want := StageNative{LibvirtClient: true, Hypervisor: true, InstallerMedia: true, Libvirt: "latest"}
			closures, closuresErr := StageClosures(context.Background(), &closureInspector{absent: inspector.absent}, f.store.state.RetainedDefinitions, fedora, want)
			if closuresErr != nil || len(closures) != 3 {
				t.Fatalf("closures = %+v (%v)", closures, closuresErr)
			}
			for _, closure := range closures {
				check, found := checkOf(report, closure.ID)
				if !found || check.Scope != ContextScope || (check.Status == "ready") != closure.Ready || closure.Ready == (closure.ID == test.absent) {
					t.Fatalf("check %s = %+v, the stage reads %+v", closure.ID, check, closure)
				}
			}
			if test.absent == "" {
				if err != nil || report.Outcome != "ready" {
					t.Fatalf("every closure present: %#v %v", report, err)
				}
				return
			}
			if code(err) != "preflight.failed" || report.Outcome != "not-ready" || report.Next != "bootwright apply --stage controller --context example" {
				t.Fatalf("%s: %#v %v", test.absent, report, err)
			}
		})
	}
	inspector := &closureInspector{}
	f := boundContext(t, fedora, inspector, []Definition{stageResolution(fedora, NativeRequirements{ContainerRuntime: true, LibvirtClient: true}, "client")},
		api.NewObject(api.InfraProvider, "lab", api.Value{}, api.MapValue().WithPath(api.StringValue("hypervisor-host"), "libvirt", "machineRef")),
		api.NewObject(api.Machine, "guest", api.Value{}, api.MapValue().WithPath(api.StringValue("lab"), "substrate", "providerRef")))
	report, err := f.service.Check(context.Background(), CheckRequest{ContextName: "example"})
	if err != nil || report.Outcome != "ready" {
		t.Fatalf("a client-only context: %#v %v", report, err)
	}
	for _, id := range []string{"hypervisor", "installer-media"} {
		if _, found := checkOf(report, id); found {
			t.Fatalf("an unselected %s closure was reported: %+v", id, report.Checks)
		}
	}
}

// On RHEL the operator installs lorax and xorriso (D106), so a missing one is
// settled by that step first and the stage after it, and one another key
// signed by its reinstallation; present and signed, they
// make the context ready with no resolution read. A libvirt selection, which no
// approved source carries for RHEL, reports its own refusal and no command.
func TestPreflightOnRHELNamesTheOperatorStep(t *testing.T) {
	rhel := Platform{"rhel", "9.8", "amd64"}
	inspector := &closureInspector{operator: OperatorPresence{Installed: []NativeRootPresence{{Key: "installer-media", Package: NativeIdentity{Name: "xorriso"}}}, Missing: []string{"lorax"}, Foreign: []string{}}}
	f := boundContext(t, rhel, inspector, nil, installerMediaGraph()...)
	report, err := f.service.Check(context.Background(), CheckRequest{ContextName: "example"})
	reported := diagnostics.Of(err)
	want := "Install lorax and xorriso from this host's enabled Red Hat repositories (dnf install lorax xorriso), then run bootwright apply --stage controller --context example"
	if len(reported) != 1 || reported[0].Code != "preflight.failed" || reported[0].Remediation != want {
		t.Fatalf("refusal = %+v", reported)
	}
	if report.Next != "dnf install lorax xorriso, then bootwright apply --stage controller --context example" {
		t.Fatalf("next = %q", report.Next)
	}
	if media, _ := checkOf(report, "installer-media"); media.Status != "not-ready" || media.Required != "installer-media tooling (lorax, xorriso)" {
		t.Fatalf("installer media = %+v", media)
	}
	// A package another key signed is already installed, so installing its
	// name again would change nothing: the step removes it first.
	inspector.operator = OperatorPresence{Installed: []NativeRootPresence{{Key: "installer-media", Package: NativeIdentity{Name: "lorax"}}}, Missing: []string{}, Foreign: []string{"xorriso"}}
	report, err = f.service.Check(context.Background(), CheckRequest{ContextName: "example"})
	reported = diagnostics.Of(err)
	want = "Reinstall xorriso from this host's enabled Red Hat repositories (dnf remove xorriso && dnf install lorax xorriso), then run bootwright apply --stage controller --context example"
	if len(reported) != 1 || reported[0].Code != "preflight.failed" || reported[0].Remediation != want {
		t.Fatalf("foreign refusal = %+v", reported)
	}
	if report.Next != "dnf remove xorriso && dnf install lorax xorriso, then bootwright apply --stage controller --context example" {
		t.Fatalf("foreign next = %q", report.Next)
	}
	inspector.operator = OperatorPresence{Ready: true, Installed: []NativeRootPresence{{Key: "installer-media"}, {Key: "installer-media"}}, Missing: []string{}, Foreign: []string{}}
	inspector.checked = nil
	setup := f.store.state.Receipt.Definition.Native.Digest
	if report, err := f.service.Check(context.Background(), CheckRequest{ContextName: "example"}); err != nil || report.Outcome != "ready" || !slices.Equal(inspector.checked, []string{setup}) {
		t.Fatalf("signed operator tooling: %#v %v, inspected %v", report, err, inspector.checked)
	}

	// Tooling that could not be inspected is not found missing: the failure
	// names its own remedy, and the report's next command is that remedy's.
	f = boundContext(t, rhel, nil, nil, installerMediaGraph()...)
	f.service.options.NativeInspector = failingInspector{err: &ScopedFailure{Message: "the installed package database could not be read", Correction: "Restore the provided OS package-manager foundation and approved repository access"}, setup: f.resolution, plan: f.store.state.Receipt.Definition.Native.Digest}
	report, err = f.service.Check(context.Background(), CheckRequest{ContextName: "example"})
	reported = diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Remediation != "Restore the provided OS package-manager foundation and approved repository access, then run bootwright apply --stage controller --context example." ||
		report == nil || report.Next != "bootwright apply --stage controller --context example" {
		t.Fatalf("an uninspected tooling: %+v %#v", reported, report)
	}

	libvirt := &closureInspector{}
	f = boundContext(t, rhel, libvirt, nil, hostedProvider())
	report, err = f.service.Check(context.Background(), CheckRequest{ContextName: "example"})
	reported = diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "controller.unsupported" || !strings.HasPrefix(reported[0].Remediation, "Use a Fedora controller") {
		t.Fatalf("libvirt refusal = %+v", reported)
	}
	if report == nil || report.Outcome != "not-ready" || report.Next != "" || !slices.Equal(libvirt.checked, []string{f.store.state.Receipt.Definition.Native.Digest}) {
		t.Fatalf("libvirt report = %#v, inspected %v", report, libvirt.checked)
	}
	for _, id := range []string{"libvirt-client", "hypervisor"} {
		if check, _ := checkOf(report, id); check.Status != "not-ready" || check.Observed != "unsupported on rhel 9.8" {
			t.Fatalf("%s = %+v", id, check)
		}
	}
}
