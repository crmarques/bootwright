package instance

import (
	"net/netip"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
	"github.com/crmarques/bootwright/internal/addons/packages"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func authoredPackageConfig(name string) api.Value {
	field := func(key string, value api.Value) api.FieldValue { return api.FieldValue{Name: key, Value: value} }
	text := api.StringValue
	subscription := api.MapValue(field("channel", text("stable")), field("startingCSV", text("operator.v1")))
	switch name {
	case "fdf":
		subscription = subscription.With("source", text("redhat-operators"))
		return api.MapValue(field("subscription", subscription), field("cephConsumerRef", text("export")))
	case "acm":
		return api.MapValue(field("subscription", subscription), field("mce", api.MapValue(field("channel", text("stable")), field("startingCSV", text("engine.v1")))))
	case "metallb":
		pool := api.MapValue(field("name", text("services")), field("addresses", api.StringList("192.0.2.80-192.0.2.90")))
		return api.MapValue(field("subscription", subscription), field("pools", api.ListValue(pool)))
	}
	return api.MapValue(field("subscription", subscription))
}

func fixedPackageFacts() addons.ClusterFacts {
	return addons.ClusterFacts{Name: "cluster", Release: "4.20", Family: "ipv4", MachineNetworks: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}, ClusterNetworks: []netip.Prefix{netip.MustParsePrefix("10.128.0.0/14")}, ServiceNetworks: []netip.Prefix{netip.MustParsePrefix("172.30.0.0/16")}, NodeNetworks: []addons.NodeNetwork{{Machine: "node", DefaultRouteInterface: "eth0", Interfaces: []addons.NodeInterface{{Name: "eth0", Addresses: []netip.Prefix{netip.MustParsePrefix("192.0.2.10/24")}}}, NextHops: []netip.Addr{netip.MustParseAddr("192.0.2.1")}}}}
}

func TestBuiltinPackageRequestGoldens(t *testing.T) {
	registry := packages.Builtin()
	facts := fixedPackageFacts()
	for _, p := range registry.Packages() {
		t.Run(p.Name, func(t *testing.T) {
			binding := api.NewObject(api.ClusterAddonBinding, "binding", api.MapValue(), api.MapValue(api.FieldValue{Name: "clusterRef", Value: api.StringValue("cluster")}, api.FieldValue{Name: "packages", Value: api.MapValue(api.FieldValue{Name: p.Name, Value: authoredPackageConfig(p.Name)})}))
			binding, _ = addons.NormalizePackages(binding, registry)
			if issues := addons.ValidatePackages(binding, api.NewCatalog([]api.Object{binding}), registry, facts); len(issues) != 0 {
				t.Fatalf("the golden configuration is not admitted: %+v", issues)
			}
			selected := addons.SelectedInstance{Package: p, Instance: addons.Instance{Cluster: "cluster", Binding: "binding", Config: binding.Spec().Get("packages", p.Name)}}
			request, requires, err := Render(selected, facts, "controller")
			if err != nil {
				t.Fatal(err)
			}
			data, err := request.Canonical()
			if err != nil {
				t.Fatal(err)
			}
			matchesGolden(t, "request-"+p.Name, data, false)
			if _, err := DecodeRequest(data); err != nil {
				t.Fatal(err)
			}
			if p.Consumes == nil {
				return
			}
			for _, use := range p.Consumes(selected.Instance.Config) {
				if !containsRef(requires, reconciliation.ObjectRef{Kind: string(api.CephConsumer), Object: use.Name}) {
					t.Fatalf("requires %v omit the consumed CephConsumer/%s", requires, use.Name)
				}
			}
		})
	}
}

func containsRef(refs []reconciliation.ObjectRef, want reconciliation.ObjectRef) bool {
	for _, ref := range refs {
		if ref == want {
			return true
		}
	}
	return false
}

func TestSyntheticPackageWithoutOLMRendersBeforeThenAfter(t *testing.T) {
	phase := func(name string) co.Phase {
		return co.Phase{Objects: []co.Object{nativeObject("v1", "ConfigMap", "synthetic", name, api.Value{})}}
	}
	p := addons.Package{Name: "synthetic", Summary: "synthetic package without an operator", Budget: time.Minute, Render: func(addons.Instance) (addons.Rendered, error) {
		return addons.Rendered{Before: []co.Phase{phase("first")}, After: []co.Phase{phase("second")}, Checks: []co.Check{{ResourceExists: &co.Ref{APIVersion: "v1", Kind: "ConfigMap", Namespace: "synthetic", Name: "second", Namespaced: true}}}}, nil
	}}
	request, _, err := Render(addons.SelectedInstance{Package: p, Instance: addons.Instance{Cluster: "cluster", Binding: "binding", Config: api.MapValue()}}, addons.ClusterFacts{Release: "4.20"}, "controller")
	if err != nil {
		t.Fatal(err)
	}
	data, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	program, err := request.Objects()
	if err != nil {
		t.Fatal(err)
	}
	if len(program.Steps) != 2 || !strings.Contains(string(program.Steps[0].Create.Body), `"first"`) || !strings.Contains(string(program.Steps[1].Create.Body), `"second"`) {
		t.Fatalf("Before must precede After: %+v", program.Steps)
	}
	if _, err := DecodeRequest(data); err != nil {
		t.Fatal(err)
	}
}

func TestLongestAdmittedPackageNameYieldsAValidBlockIdentity(t *testing.T) {
	for _, name := range []string{strings.Repeat("a", addons.MaxPackageNameBytes), "a" + strings.Repeat("-", addons.MaxPackageNameBytes-2) + "a", "ab-cd-efg"} {
		p := addons.Package{Name: name, Summary: "longest name", Budget: time.Minute, Render: func(addons.Instance) (addons.Rendered, error) { return addons.Rendered{}, nil }}
		if _, err := addons.NewRegistry([]addons.Package{p}); err != nil {
			t.Fatalf("%q: %v", name, err)
		}
		for length := 1; length <= 63; length++ {
			cluster := strings.Repeat("c", length)
			if id := BlockID(name, cluster); !reconciliation.ValidSegment(id) {
				t.Fatalf("BlockID(%q, %d bytes) = %q is not a valid segment", name, length, id)
			}
		}
	}
}
