package prerequisites

import (
	"context"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
)

func TestControllerPreflightCannotPassClosedOrUnknownListener(t *testing.T) {
	for _, status := range []string{"ready", "not-ready", "unverified"} {
		f := newSetupFixture(t)
		f.prepared()
		catalog := api.NewCatalog(agentClusterObjects(api.MapValue()))
		f.service.compiler = contextToolCompiler{compilation.NewState(catalog, catalog, nil)}
		f.service.options.Tools = presentTools{}
		digest, _ := f.host.identity.PrivateDigest()
		f.service.storage = fixedStorage{StorageView{Context: SetupContext{Name: "example"}, State: HostState{Bindings: []ControllerBinding{{Context: "example", Machine: "controller", HostDigest: digest}}}, OpenBundle: func(context.Context, string) (BundleArea, error) { return contextToolArea{source: "none"}, nil }}}
		f.service.options.ListenerChecks = func(context.Context, api.Catalog, string) ([]Check, error) {
			return []Check{{Required: "synthetic listener", Observed: "synthetic zone; manually verify runtime firewall", Status: status}}, nil
		}
		result, err := f.service.Check(context.Background(), CheckRequest{ContextName: "example"})
		if result == nil || (err == nil) != (status == "ready") || (result.Outcome == "ready") != (status == "ready") {
			t.Fatal(status, result, err)
		}
		if len(f.effects()) != 0 {
			t.Fatal("listener preflight mutated prerequisites", f.effects())
		}
		if row := checkOf(result.Checks, "listener-firewall"); row.Status != status {
			t.Fatal(row)
		}
	}
}
