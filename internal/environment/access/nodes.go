package access

import (
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/environment"
)

type nodeMatch int

const (
	nodeMissing nodeMatch = iota
	nodeFound
	nodeShared
)

func canonicalNames(requested []string) []string {
	var names []string
	for _, name := range requested {
		if name = strings.TrimSpace(name); name != "" {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

type roster func(api.Object) (environment.Membership, bool)

func (r roster) sorted(cluster api.Object) []environment.Member {
	var nodes []environment.Member
	if r != nil {
		membership, _ := r(cluster)
		nodes = slices.Clone(membership.Nodes)
	}
	slices.SortStableFunc(nodes, func(a, b environment.Member) int { return strings.Compare(a.Name, b.Name) })
	return nodes
}

func nodeNames(nodes []environment.Member) []string {
	names := make([]string, 0, len(nodes))
	for _, node := range nodes {
		names = append(names, node.Name)
	}
	return names
}

func nodeRemedy(cluster api.Object, contextName string, nodes []environment.Member) string {
	return "choose one declared node name (" + strings.Join(nodeNames(nodes), ", ") + "); bootwright cluster info --context " +
		contextName + " --name " + cluster.Name() + " lists the nodes"
}

func matchNode(nodes []environment.Member, selector string) (environment.Member, nodeMatch) {
	if index := slices.Index(nodeNames(nodes), selector); index >= 0 {
		return nodes[index], nodeFound
	}
	var matched []environment.Member
	for _, node := range nodes {
		if node.FQDN == selector {
			matched = append(matched, node)
		}
	}
	switch len(matched) {
	case 0:
		return environment.Member{}, nodeMissing
	case 1:
		return matched[0], nodeFound
	}
	return environment.Member{}, nodeShared
}

func firstNode(cluster api.Object, contextName string, nodesOf roster) (environment.Member, error) {
	nodes := nodesOf.sorted(cluster)
	if len(nodes) == 0 {
		return environment.Member{}, failure("access.target", "CephCluster "+cluster.Name()+" declares no node", nodeRemedy(cluster, contextName, nodes))
	}
	return nodes[0], nil
}

func selectMachines(clusters []api.Object, contextName string, selectors []string, nodesOf roster) ([]string, error) {
	var machines []string
	add := func(node environment.Member) {
		if ref := node.Machine; !slices.Contains(machines, ref) {
			machines = append(machines, ref)
		}
	}
	if len(selectors) == 0 {
		for _, cluster := range clusters {
			node, err := firstNode(cluster, contextName, nodesOf)
			if err != nil {
				return nil, err
			}
			add(node)
		}
		return machines, nil
	}
	declared := make(map[string]bool, len(selectors))
	var idle []string
	for _, cluster := range clusters {
		nodes, hit := nodesOf.sorted(cluster), false
		for _, selector := range selectors {
			node, match := matchNode(nodes, selector)
			switch match {
			case nodeShared:
				return nil, failure("access.target",
					"the FQDN "+selector+" names several nodes of CephCluster "+cluster.Name()+", so it selects none", nodeRemedy(cluster, contextName, nodes))
			case nodeFound:
				declared[selector], hit = true, true
				add(node)
			}
		}
		if !hit {
			idle = append(idle, cluster.Name())
		}
	}
	return machines, unselectedNodes(clusters, contextName, selectors, declared, idle, nodesOf)
}

func unselectedNodes(clusters []api.Object, contextName string, selectors []string, declared map[string]bool, idle []string, nodesOf roster) error {
	var missing []string
	for _, selector := range selectors {
		if !declared[selector] {
			missing = append(missing, selector)
		}
	}
	if len(missing) > 0 {
		if len(clusters) == 1 {
			return failure("access.target", "CephCluster "+clusters[0].Name()+" declares no node named "+strings.Join(missing, ", ")+" and none with that FQDN",
				nodeRemedy(clusters[0], contextName, nodesOf.sorted(clusters[0])))
		}
		return failure("access.target", "none of CephClusters "+strings.Join(clusterNames(clusters), ", ")+" declares a node named "+
			strings.Join(missing, ", ")+" or one with that FQDN", "choose declared node names; bootwright cluster info --context "+contextName+" lists the nodes")
	}
	if len(idle) > 0 {
		subject := "CephCluster " + idle[0] + " declares"
		if len(idle) > 1 {
			subject = "CephClusters " + strings.Join(idle, ", ") + " declare"
		}
		return failure("access.target", subject+" none of the nodes "+strings.Join(selectors, ", "),
			"name only nodes each listed cluster declares, or run the command per cluster; bootwright cluster info --context "+contextName+" lists the nodes")
	}
	return nil
}

func clusterNames(clusters []api.Object) []string {
	names := make([]string, 0, len(clusters))
	for _, cluster := range clusters {
		names = append(names, cluster.Name())
	}
	return names
}
