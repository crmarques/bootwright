package prerequisites

import (
	"context"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
)

type presentTools struct{ TargetToolCatalog }

func (presentTools) Select(requests []controller.ToolRequest, _ []DependencySource) ([]ToolDefinition, bool, error) {
	return []ToolDefinition{{Kind: requests[0].Kind, Version: requests[0].Version}}, true, nil
}

func (presentTools) Present(context.Context, BundleArea, []ToolDefinition) (bool, error) {
	return true, nil
}

type fixedStorage struct{ view StorageView }

func (s fixedStorage) ReadController(_ context.Context, _ string, read func(StorageView) error) error {
	return read(s.view)
}

func controllerMachine() api.Object {
	return api.NewObject(api.Machine, "controller", api.Value{}, api.MapValue().WithPath(api.BoolValue(true), "os", "provided").WithPath(api.BoolValue(true), "access", "local").WithPath(api.MapValue(), "proxy", "direct"))
}

func agentClusterObjects(spec api.Value, extra ...api.Object) []api.Object {
	spec = spec.WithPath(api.StringValue("openshift"), "distribution", "type").WithPath(api.StringValue("4.21.15"), "distribution", "release", "version").WithPath(api.MapValue(), "install", "agent")
	objects := []api.Object{
		api.NewObject(api.Environment, "example", api.Value{}, api.MapValue().WithPath(api.StringValue("controller"), "controller", "machineRef")),
		controllerMachine(),
		api.NewObject(api.OpenShiftCluster, "cluster", api.Value{}, spec),
	}
	return append(objects, extra...)
}

func nodeNetworkConfiguration() []api.Object {
	return agentClusterObjects(api.MapValue().With("nodes", api.ListValue(api.MapValue().With("machineRef", api.StringValue("node")))),
		api.NewObject(api.Machine, "node", api.Value{}, api.MapValue()))
}

func probedVirtualIPGraph() []api.Object {
	attachment := api.MapValue().With("libvirt", api.MapValue().With("management", api.StringValue("managed")).With("address", api.StringValue("198.51.100.1/24")).With("bridge", api.StringValue("virbr-lab")))
	provider := api.NewObject(api.InfraProvider, "hosted", api.Value{}, api.MapValue().WithPath(api.StringValue("controller"), "libvirt", "machineRef").With("networkAttachments", api.ListValue(attachment)))
	spec := api.MapValue().WithPath(api.MapValue(), "install", "agent").WithPath(api.MapValue(), "install", "platform", "baremetal").With("nodes", api.ListValue(api.MapValue(), api.MapValue(), api.MapValue()))
	for _, slot := range []string{"api", "api-int", "ingress"} {
		spec = spec.WithPath(api.StringValue("198.51.100.10"), "install", "endpoints", slot, "address").WithPath(api.StringValue("installer"), "install", "endpoints", slot, "source", "type")
	}
	return agentClusterObjects(spec, provider)
}

// A context whose graph selects the nmstate or iputils closure reports that
// check in its own context scope, missing while the host's RPM database lacks
// the package, and is not ready until it is installed, so the stage and
// preflight never disagree about what a context needs.
func TestContextPreflightReportsTheNMStateAndIPUtilsClosures(t *testing.T) {
	for _, test := range []struct {
		name    string
		objects []api.Object
		closure string
		pkg     string
	}{
		{"agent node network configuration", nodeNetworkConfiguration(), "nmstate", "nmstate"},
		{"a probed virtual IP", probedVirtualIPGraph(), "iputils", "iputils"},
	} {
		t.Run(test.name, func(t *testing.T) {
			catalog := api.NewCatalog(test.objects)
			selection, err := controller.Select(catalog)
			if err != nil {
				t.Fatal(err)
			}
			requirements := StageRequirements(StageNativeOf(selection))
			if !slices.Contains(requirements, test.pkg) {
				t.Fatalf("the graph selects %v", requirements)
			}
			f := newSetupFixture(t)
			f.prepared()
			digest, err := f.host.identity.PrivateDigest()
			if err != nil {
				t.Fatal(err)
			}
			area := contextToolArea{source: "none"}
			f.service.compiler = contextToolCompiler{compilation.NewState(catalog, catalog, nil)}
			f.service.options.Tools = presentTools{}
			f.service.storage = fixedStorage{StorageView{
				Context:    SetupContext{Name: "example"},
				State:      HostState{Bindings: []ControllerBinding{{Context: "example", Machine: "controller", HostDigest: digest}}},
				OpenBundle: func(context.Context, string) (BundleArea, error) { return area, nil },
			}}
			for _, requirement := range requirements {
				f.packages.present[requirement] = true
			}
			report, err := f.service.Check(context.Background(), CheckRequest{ContextName: "example"})
			if err != nil || report == nil || report.Outcome != "ready" {
				t.Fatalf("a host holding %v: %+v, %v", requirements, report, err)
			}
			if check := checkOf(report.Checks, test.closure); check.Status != "ready" || check.Scope != ContextScope {
				t.Fatalf("the ready host reports %+v", check)
			}
			f.packages.present[test.pkg] = false
			report, err = f.service.Check(context.Background(), CheckRequest{ContextName: "example"})
			if codeOf(err) != "preflight.failed" || report == nil || report.Outcome != "not-ready" {
				t.Fatalf("a host lacking %s: %+v, %v", test.pkg, report, err)
			}
			check := checkOf(report.Checks, test.closure)
			if check.Status != "not-ready" || check.Scope != ContextScope || check.Observed != "missing "+test.pkg {
				t.Fatalf("the missing package reports %+v", check)
			}
			if report.Next == "" || PendingScope(*report) != ContextScope {
				t.Fatalf("next %q, pending %q", report.Next, PendingScope(*report))
			}
		})
	}
}

func checkOf(checks []Check, id string) Check {
	for _, check := range checks {
		if check.ID == id {
			return check
		}
	}
	return Check{}
}
