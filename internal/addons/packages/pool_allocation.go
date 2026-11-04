package packages

import (
	"fmt"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

const maxAllocationEntries = 16

func admitServiceAllocation(pool api.MetalLBPool, base string, add poolIssue) {
	a := pool.ServiceAllocation
	if a == nil {
		return
	}
	field := base + ".serviceAllocation"
	if len(a.Namespaces) == 0 && len(a.NamespaceSelectors) == 0 && len(a.ServiceSelectors) == 0 {
		add("api.invariant", field, "serviceAllocation of pool "+pool.Name+" selects no Service", "list namespaces, namespaceSelectors or serviceSelectors, or remove serviceAllocation")
		return
	}
	if len(a.Namespaces) > maxAllocationEntries {
		add("api.value", field+".namespaces", "serviceAllocation of pool "+pool.Name+" lists more than 16 namespaces", "list at most 16 namespaces")
	}
	admitSelectors(pool.Name, field+".namespaceSelectors", "namespaceSelectors", a.NamespaceSelectors, add)
	admitSelectors(pool.Name, field+".serviceSelectors", "serviceSelectors", a.ServiceSelectors, add)
}

func admitSelectors(pool, field, list string, selectors []api.MetalLBSelector, add poolIssue) {
	if len(selectors) > maxAllocationEntries {
		add("api.value", field, fmt.Sprintf("serviceAllocation of pool %s lists more than 16 %s", pool, list), "list at most 16 "+list)
	}
	for i, selector := range selectors {
		path := fmt.Sprintf("%s[%d].matchLabels", field, i)
		if len(selector.MatchLabels) == 0 {
			add("api.value", path, fmt.Sprintf("%s[%d] of pool %s matches on no label", list, i, pool), "supply at least one label")
			continue
		}
		keys := make([]string, 0, len(selector.MatchLabels))
		for key := range selector.MatchLabels {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			if !api.ValidLexical("kubernetes-label-key", key) || !api.ValidLexical("kubernetes-label-value", selector.MatchLabels[key]) {
				add("api.value", path, fmt.Sprintf("%s[%d] of pool %s has an invalid label key or value", list, i, pool), "use valid Kubernetes label keys and values")
				break
			}
		}
	}
}
