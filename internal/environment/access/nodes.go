package access

import (
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func selectNode(cluster api.Object, contextName, selector string) (api.Value, error) {
	nodes := cluster.Spec().Get("ceph", "topology", "nodes").Items()
	slices.SortStableFunc(nodes, func(a, b api.Value) int {
		return strings.Compare(a.Get("name").Text(), b.Get("name").Text())
	})
	names := make([]string, 0, len(nodes))
	for _, node := range nodes {
		names = append(names, node.Get("name").Text())
	}
	remedy := "choose one declared node name (" + strings.Join(names, ", ") + "); bootwright cluster info --context " +
		contextName + " --name " + cluster.Name() + " lists the nodes"
	if selector == "" {
		if len(nodes) == 0 {
			return api.Value{}, failure("access.target", "StorageCluster "+cluster.Name()+" declares no node", remedy)
		}
		return nodes[0], nil
	}
	if index := slices.Index(names, selector); index >= 0 {
		return nodes[index], nil
	}
	var matched []api.Value
	for _, node := range nodes {
		if node.Get("fqdn").Text() == selector {
			matched = append(matched, node)
		}
	}
	switch len(matched) {
	case 1:
		return matched[0], nil
	case 0:
		return api.Value{}, failure("access.target",
			"StorageCluster "+cluster.Name()+" declares no node named "+selector+" and none with that FQDN", remedy)
	default:
		return api.Value{}, failure("access.target",
			"the FQDN "+selector+" names several nodes of StorageCluster "+cluster.Name()+", so it selects none", remedy)
	}
}
