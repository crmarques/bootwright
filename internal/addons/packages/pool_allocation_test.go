package packages

import (
	"fmt"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
)

func selector(labels ...string) api.Value {
	fields := []api.FieldValue{}
	for i := 0; i+1 < len(labels); i += 2 {
		fields = append(fields, api.FieldValue{Name: labels[i], Value: api.StringValue(labels[i+1])})
	}
	return api.MapValue(api.FieldValue{Name: "matchLabels", Value: api.MapValue(fields...)})
}

func allocated(p api.Value, namespaces []string, namespaceSelectors, serviceSelectors []api.Value) api.Value {
	allocation := api.MapValue()
	if namespaces != nil {
		allocation = allocation.With("namespaces", api.StringList(namespaces...))
	}
	if namespaceSelectors != nil {
		allocation = allocation.With("namespaceSelectors", api.ListValue(namespaceSelectors...))
	}
	if serviceSelectors != nil {
		allocation = allocation.With("serviceSelectors", api.ListValue(serviceSelectors...))
	}
	return p.With("serviceAllocation", allocation)
}

func selectors(n int) []api.Value {
	items := []api.Value{}
	for i := 0; i < n; i++ {
		items = append(items, selector("app", fmt.Sprintf("v%d", i)))
	}
	return items
}

func names(n int) []string {
	items := []string{}
	for i := 0; i < n; i++ {
		items = append(items, fmt.Sprintf("ns-%d", i))
	}
	return items
}

func TestMetalLBServiceAllocationRenderGoldens(t *testing.T) {
	base := pool("services", "192.0.2.80-192.0.2.90")
	ingress := selector("ingresscontroller.operator.openshift.io/owning-ingresscontroller", "routed")
	for _, test := range []struct {
		name string
		pool api.Value
	}{
		{"metallb-allocation-namespaces", allocated(base, []string{"apps", "web"}, nil, nil)},
		{"metallb-allocation-namespace-selectors", allocated(base, nil, []api.Value{selector("team", "blue", "env", "prod")}, nil)},
		{"metallb-allocation-service-selectors", allocated(base, nil, nil, []api.Value{ingress})},
		{"metallb-allocation-all", allocated(base, []string{"apps"}, []api.Value{selector("team", "blue")}, []api.Value{ingress, selector("tier", "edge")})},
	} {
		t.Run(test.name, func(t *testing.T) {
			instance := addons.Instance{Cluster: "cluster", Binding: "binding", Config: config(test.pool)}
			if issues := admitPools(instance, facts()); len(issues) != 0 {
				t.Fatalf("golden configuration is not admitted: %+v", issues)
			}
			p, _ := Builtin().Find("metallb")
			renderedGolden(t, test.name, p, instance)
		})
	}
}

func TestMetalLBWithoutAllocationRendersNone(t *testing.T) {
	p, _ := Builtin().Find("metallb")
	rendered, err := p.Render(addons.Instance{Config: config(pool("services", "192.0.2.80-192.0.2.90"))})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(frozenProgram(t, p, rendered)), "serviceAllocation") {
		t.Fatal("a pool without serviceAllocation rendered one")
	}
	normalized := normalizeMetalLB(config(pool("services", "192.0.2.80-192.0.2.90")))
	if normalized.Get("pools").Items()[0].Has("serviceAllocation") {
		t.Fatal("normalization added serviceAllocation")
	}
}

func TestPoolServiceAllocationTwins(t *testing.T) {
	base := pool("services", "192.0.2.80-192.0.2.90")
	good := selector("app", "web")
	for _, test := range []struct {
		name         string
		bad, ok      api.Value
		code, field  string
		messageFound string
	}{
		{"empty", allocated(base, nil, nil, nil), allocated(base, []string{"apps"}, nil, nil), "api.invariant", ".pools[0].serviceAllocation", "serviceAllocation of pool services selects no Service"},
		{"empty lists", allocated(base, []string{}, []api.Value{}, []api.Value{}), allocated(base, nil, nil, []api.Value{good}), "api.invariant", ".pools[0].serviceAllocation", "selects no Service"},
		{"namespaces bound", allocated(base, names(17), nil, nil), allocated(base, names(16), nil, nil), "api.value", ".pools[0].serviceAllocation.namespaces", "more than 16 namespaces"},
		{"namespace selectors bound", allocated(base, nil, selectors(17), nil), allocated(base, nil, selectors(16), nil), "api.value", ".pools[0].serviceAllocation.namespaceSelectors", "more than 16 namespaceSelectors"},
		{"service selectors bound", allocated(base, nil, nil, selectors(17)), allocated(base, nil, nil, selectors(16)), "api.value", ".pools[0].serviceAllocation.serviceSelectors", "more than 16 serviceSelectors"},
		{"no labels", allocated(base, nil, nil, []api.Value{selector()}), allocated(base, nil, nil, []api.Value{good}), "api.value", ".pools[0].serviceAllocation.serviceSelectors[0].matchLabels", "matches on no label"},
		{"service label key", allocated(base, nil, nil, []api.Value{selector("bad key", "x")}), allocated(base, nil, nil, []api.Value{selector("example.com/app", "x")}), "api.value", ".pools[0].serviceAllocation.serviceSelectors[0].matchLabels", "invalid label key or value"},
		{"service label value", allocated(base, nil, nil, []api.Value{selector("app", "-x")}), allocated(base, nil, nil, []api.Value{selector("app", "")}), "api.value", ".pools[0].serviceAllocation.serviceSelectors[0].matchLabels", "invalid label key or value"},
		{"namespace label key", allocated(base, nil, []api.Value{good, selector("a/b/c", "x")}, nil), allocated(base, nil, []api.Value{good, selector("a.io/b", "x")}, nil), "api.value", ".pools[0].serviceAllocation.namespaceSelectors[1].matchLabels", "invalid label key or value"},
		{"namespace label value", allocated(base, nil, []api.Value{selector("team", "a b")}, nil), allocated(base, nil, []api.Value{selector("team", "a-b")}, nil), "api.value", ".pools[0].serviceAllocation.namespaceSelectors[0].matchLabels", "invalid label key or value"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := facts()
			if issues := admitPools(addons.Instance{Config: config(test.ok)}, f); len(issues) != 0 {
				t.Fatalf("passing twin: %+v", issues)
			}
			for _, issue := range admitPools(addons.Instance{Config: config(test.bad)}, f) {
				if issue.Code == test.code && issue.Field == test.field && strings.Contains(issue.Message, test.messageFound) && issue.Remediation != "" {
					return
				}
			}
			t.Fatalf("missing refusal: %+v", admitPools(addons.Instance{Config: config(test.bad)}, f))
		})
	}
}

func TestPoolEmptyAllocationRemedy(t *testing.T) {
	issues := admitPools(addons.Instance{Config: config(allocated(pool("services", "192.0.2.80-192.0.2.90"), nil, nil, nil))}, facts())
	if len(issues) != 1 || issues[0].Remediation != "list namespaces, namespaceSelectors or serviceSelectors, or remove serviceAllocation" {
		t.Fatalf("%+v", issues)
	}
}
