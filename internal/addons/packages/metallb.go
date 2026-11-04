package packages

import (
	"slices"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
)

func metallb() addons.Package {
	return addons.Package{Name: "metallb", Summary: "MetalLB load balancer with optional layer 2 address pools", Provides: []string{"metallb"}, Budget: 30 * time.Minute, Normalize: normalizeMetalLB, OLM: &addons.OLM{Namespace: "metallb-system", OperatorGroup: addons.AllNamespaces, GroupName: "metallb-operator", Subscription: "metallb-operator-sub", Package: "metallb-operator", DefaultSource: "redhat-operators", Approval: addons.Exact}, Admit: admitPools, Render: renderMetalLB}
}
func normalizeMetalLB(v api.Value) api.Value {
	pools := v.Get("pools").Items()
	for i := range pools {
		pools[i] = pools[i].Default("autoAssign", api.BoolValue(true))
	}
	return v.With("pools", addons.SortedNamed(api.ListValue(pools...), "name"))
}
func renderMetalLB(i addons.Instance) (addons.Rendered, error) {
	config, err := addons.Decode[api.MetalLBConfig](i.Config)
	if err != nil {
		return addons.Rendered{}, err
	}
	result := addons.Rendered{After: []co.Phase{{Gate: []co.Check{crd("metallbs.metallb.io")}, Objects: []co.Object{object("metallb.io/v1beta1", "MetalLB", "metallb-system", "metallb", api.Value{})}}}, Checks: []co.Check{condition("metallb.io/v1beta1", "MetalLB", "metallb-system", "metallb", "Available")}}
	if len(config.Pools) > 0 {
		phase := co.Phase{Gate: []co.Check{condition("metallb.io/v1beta1", "MetalLB", "metallb-system", "metallb", "Available"), crd("ipaddresspools.metallb.io"), crd("l2advertisements.metallb.io")}}
		for _, pool := range config.Pools {
			spec := api.MapValue(api.FieldValue{Name: "addresses", Value: api.StringList(pool.Addresses...)}, api.FieldValue{Name: "autoAssign", Value: api.BoolValue(pool.AutoAssign)})
			if allocation := renderServiceAllocation(pool.ServiceAllocation); allocation.Type() == api.Mapping {
				spec = spec.With("serviceAllocation", allocation)
			}
			advertisement := api.MapValue(api.FieldValue{Name: "ipAddressPools", Value: api.StringList(pool.Name)})
			if pool.L2 != nil && pool.L2.Interface != "" {
				advertisement = advertisement.With("interfaces", api.StringList(pool.L2.Interface))
			}
			phase.Objects = append(phase.Objects, object("metallb.io/v1beta1", "IPAddressPool", "metallb-system", pool.Name, spec), object("metallb.io/v1beta1", "L2Advertisement", "metallb-system", pool.Name, advertisement))
		}
		result.After = append(result.After, phase)
	}
	return result, nil
}

func renderServiceAllocation(a *api.MetalLBServiceAllocation) api.Value {
	if a == nil {
		return api.Value{}
	}
	allocation := api.MapValue()
	if len(a.Namespaces) > 0 {
		allocation = allocation.With("namespaces", api.StringList(a.Namespaces...))
	}
	allocation = withSelectors(allocation, "namespaceSelectors", a.NamespaceSelectors)
	return withSelectors(allocation, "serviceSelectors", a.ServiceSelectors)
}
func withSelectors(allocation api.Value, name string, selectors []api.MetalLBSelector) api.Value {
	if len(selectors) == 0 {
		return allocation
	}
	items := make([]api.Value, 0, len(selectors))
	for _, selector := range selectors {
		keys := make([]string, 0, len(selector.MatchLabels))
		for key := range selector.MatchLabels {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		labels := make([]api.FieldValue, 0, len(keys))
		for _, key := range keys {
			labels = append(labels, api.FieldValue{Name: key, Value: api.StringValue(selector.MatchLabels[key])})
		}
		items = append(items, api.MapValue(api.FieldValue{Name: "matchLabels", Value: api.MapValue(labels...)}))
	}
	return allocation.With(name, api.ListValue(items...))
}
