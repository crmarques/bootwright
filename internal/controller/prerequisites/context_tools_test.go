package prerequisites

import (
	"context"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
)

type contextToolCompiler struct{ state *compilation.State }

func (c contextToolCompiler) Compile(context.Context, desiredstate.Sources) (*compilation.State, *compilation.Report, error) {
	return c.state, nil, nil
}

type contextToolCatalog struct {
	TargetToolCatalog
	offered []DependencySource
}

func (c *contextToolCatalog) Select(requests []controller.ToolRequest, sources []DependencySource) ([]ToolDefinition, bool, error) {
	if requests[0].Kind == "helm" {
		c.offered = slices.Clone(sources)
	}
	return []ToolDefinition{}, false, nil
}

type contextToolArea struct {
	BundleArea
	source string
}

func (a contextToolArea) Entries(context.Context) ([]BundleEntry, error) {
	return []BundleEntry{{Path: "sources/" + a.source}}, nil
}

func TestContextPreflightSelectsLatestOnlyFromItsOwnBinding(t *testing.T) {
	objects := []api.Object{
		api.NewObject(api.Environment, "example", api.Value{}, api.MapValue().WithPath(api.StringValue("controller"), "controller", "machineRef")),
		api.NewObject(api.Machine, "controller", api.Value{}, api.MapValue().WithPath(api.BoolValue(true), "os", "provided").WithPath(api.BoolValue(true), "access", "local").WithPath(api.MapValue(), "proxy", "direct")),
		api.NewObject(api.OpenShiftCluster, "cluster", api.Value{}, api.MapValue().WithPath(api.StringValue("openshift"), "distribution", "type").WithPath(api.StringValue("4.21.15"), "distribution", "release", "version")),
	}
	catalog := api.NewCatalog(objects)
	tools := &contextToolCatalog{}
	service := Service{compiler: contextToolCompiler{compilation.NewState(catalog, catalog, nil)}, options: Options{Tools: tools}}
	older, newer := DependencySource{ID: "old"}, DependencySource{ID: "new"}
	current := inspection{stored: StorageView{Context: SetupContext{Name: "example"}, State: HostState{ToolSources: []DependencySource{older, newer}, Bindings: []ControllerBinding{{Context: "example", Tools: "own"}}}, OpenBundle: func(_ context.Context, id string) (BundleArea, error) {
		if id != "own" {
			t.Fatalf("opened %s", id)
		}
		return contextToolArea{source: older.ID}, nil
	}}}
	if err := service.selectContext(context.Background(), &current); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(tools.offered, []DependencySource{older}) {
		t.Fatalf("preflight offered host-wide latest releases: %+v", tools.offered)
	}
}
