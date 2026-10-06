package prerequisites

import (
	"context"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// failingSelection recovers no retained client closure: its Select fails as
// the bundle catalog does over sources it cannot read.
type failingSelection struct {
	*testTools
	err error
}

func (f failingSelection) Select([]controller.ToolRequest, []DependencySource) ([]ToolDefinition, bool, error) {
	return nil, false, f.err
}

// Target tools are selected only by a context, and only its controller stage
// installs them, so a selection failure preflight meets names that stage.
func TestPreflightNamesTheStageForAToolSelectionFailure(t *testing.T) {
	f, _, catalog := toolsFixture(t)
	if _, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	f.service.options.Tools = failingSelection{testTools: catalog, err: &ScopedFailure{
		Message: "retained tool release has conflicting publisher identities", Correction: "Restore approved dependency sources or the exact retained bundle",
	}}
	_, err := f.service.Check(context.Background(), CheckRequest{ContextName: "example"})
	reported := diagnostics.Of(err)
	want := "Restore approved dependency sources or the exact retained bundle, then run bootwright apply --stage controller --context example."
	if len(reported) != 1 || reported[0].Code != "controller.setup" || reported[0].Remediation != want {
		t.Fatalf("refusal = %+v", reported)
	}
}

// failingInspector cannot read the native inventory a presence check of a
// context's closures needs. The plan setup froze, when it is named, still
// reads through setup's own inspector.
type failingInspector struct {
	err   error
	setup NativeInspector
	plan  string
}

func (f failingInspector) Check(ctx context.Context, plan NativeResolvedPlan) (NativePresence, error) {
	if f.setup != nil && plan.Digest == f.plan {
		return f.setup.Check(ctx, plan)
	}
	return NativePresence{}, f.err
}

func (f failingInspector) OperatorRoots(context.Context, Platform, []string) (OperatorPresence, error) {
	return OperatorPresence{}, f.err
}

// The libvirt client is selected only by a context, and only its controller
// stage installs it, so a presence failure preflight meets names that stage.
// The host is Fedora, whose stage can realize that client.
func TestPreflightNamesTheStageForANativePresenceFailure(t *testing.T) {
	f := newFixture(t, Platform{"fedora", "43", "amd64"})
	f.store.scope = SetupContext{Name: "example", Revision: "rev-" + "22222222222222222222222222222222"}
	f.compiler.extra = []api.Object{
		api.NewObject(api.InfraProvider, "lab", api.Value{}, api.MapValue().WithPath(api.StringValue("hypervisor-host"), "libvirt", "machineRef")),
		api.NewObject(api.Machine, "guest", api.Value{}, api.MapValue().WithPath(api.StringValue("lab"), "substrate", "providerRef")),
	}
	if _, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	f.store.state.RetainedDefinitions = append(f.store.state.RetainedDefinitions, Definition{Platform: f.host.platform, Versions: controller.DefaultDependencyVersions(), NativeRequirements: NativeRequirements{ContainerRuntime: true, LibvirtClient: true}, Native: &NativeResolvedPlan{}})
	f.service.options.NativeInspector = failingInspector{err: &ScopedFailure{Message: "the native inventory could not be read", Correction: "Restore the provided OS package-manager foundation and approved repository access"}, setup: f.resolution, plan: f.store.state.Receipt.Definition.Native.Digest}
	report, err := f.service.Check(context.Background(), CheckRequest{ContextName: "example"})
	reported := diagnostics.Of(err)
	want := "Restore the provided OS package-manager foundation and approved repository access, then run bootwright apply --stage controller --context example."
	if len(reported) != 1 || reported[0].Code != "controller.setup" || reported[0].Remediation != want {
		t.Fatalf("refusal = %+v", reported)
	}
	// The CLI prints this not-ready report beside the refusal, so its next
	// command must be the one the refusal's remedy names.
	if report == nil || report.Outcome != "not-ready" || report.Next != "bootwright apply --stage controller --context example" {
		t.Fatalf("report = %#v", report)
	}
}

// A consumer that cannot find a client its context's stage should have
// installed names that context's stage; without a context the stage is named
// alone.
func TestALocatedToolNamesItsContextsStage(t *testing.T) {
	tool := controller.InstalledTool{Kind: "helm", Executable: "helm", Version: "v3.17.0"}
	for _, test := range []struct {
		name, unavailable, missing string
		view                       StorageView
	}{
		{name: "a context", view: StorageView{Context: SetupContext{Name: "example"}},
			unavailable: "run bootwright setup, then bootwright apply --stage controller --context example", missing: "run bootwright apply --stage controller --context example"},
		{name: "no context", unavailable: "run bootwright setup, then apply --stage controller", missing: "run bootwright apply --stage controller"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := LocateInstalledTool(context.Background(), test.view, nil, tool)
			if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Remediation != test.unavailable {
				t.Fatalf("unavailable areas: %+v", reported)
			}
			test.view.OpenBundle = func(context.Context, string) (BundleArea, error) { return nil, nil }
			_, err = LocateInstalledTool(context.Background(), test.view, nil, tool)
			if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Remediation != test.missing {
				t.Fatalf("missing client: %+v", reported)
			}
		})
	}
}

// installerMediaGraph places an Anaconda installation's artifact server on the
// controller, so the context's controller stage installs the image-building
// tooling, which preflight reports as its installer-media check.
func installerMediaGraph() []api.Object {
	return []api.Object{
		api.NewObject(api.MachineInstallProfile, "rhel", api.Value{}, api.MapValue().
			WithPath(api.StringValue("lab-artifacts"), "installer", "anaconda", "redfishVirtualMedia", "artifactServerEndpoint", "serverRef")),
		api.NewObject(api.Machine, "node", api.Value{}, api.MapValue().
			WithPath(api.BoolValue(false), "os", "provided").WithPath(api.StringValue("rhel"), "os", "installProfileRef")),
		api.NewObject(api.ArtifactServer, "lab-artifacts", api.Value{}, api.MapValue().With("machineRef", api.StringValue("controller"))),
	}
}

// unboundCheck is preflight of a context on a prepared Fedora host, whose
// stage installs every closure itself, while its first apply has not yet
// published its binding.
func unboundCheck(t *testing.T, extra ...api.Object) (*Report, error) {
	t.Helper()
	f := newFixture(t, Platform{"fedora", "43", "amd64"})
	f.store.scope = SetupContext{Name: "example", Revision: "rev-" + "22222222222222222222222222222222"}
	f.compiler.extra = extra
	if _, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	report, err := f.service.Check(context.Background(), CheckRequest{ContextName: "example"})
	if report == nil || report.Outcome != "not-ready" || PendingScope(*report) != ContextScope {
		t.Fatalf("an unbound context was not pending its own binding: %#v %v", report, err)
	}
	return report, err
}

// A context without a controller stage has nothing for that stage to run; its
// first apply publishes the binding, so that apply is what preflight names.
func TestAnUnboundContextWithoutAStageIsSentToItsFirstApply(t *testing.T) {
	report, err := unboundCheck(t)
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "preflight.failed" || reported[0].Remediation != "run bootwright apply --context example" {
		t.Fatalf("refusal = %+v", reported)
	}
	if report.Next != "bootwright apply --context example" {
		t.Fatalf("next = %q", report.Next)
	}
}

// A context with a controller stage is sent to it, even when its only pending
// check is the binding, because that stage's apply publishes the binding too.
func TestAnUnboundContextWithAStageIsSentToItsStage(t *testing.T) {
	report, err := unboundCheck(t, installerMediaGraph()...)
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "preflight.failed" || reported[0].Remediation != "run bootwright apply --stage controller --context example" {
		t.Fatalf("refusal = %+v", reported)
	}
	if report.Next != "bootwright apply --stage controller --context example" {
		t.Fatalf("next = %q", report.Next)
	}
}
