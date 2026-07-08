package stateview

import (
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// ClusterNode is one node of a cluster resolved for access: the Machine that
// backs it, its role, its registered hostname, and — for a container node — its
// ordinal among same-role nodes in declaration order. Kind distinguishes the
// container vs storage topology it came from.
type ClusterNode struct {
	MachineName string
	Role        string
	Roles       []string
	Hostname    string
	Ordinal     int
	Kind        string
}

// ClusterNodes returns the nodes of a container OR storage cluster in
// declaration order, or (nil, false) when no cluster of that name exists. A
// container node's Ordinal is its index among same-role nodes (master-0,
// master-1, worker-0, …); a storage node carries its Ceph role list instead,
// since a Ceph host fills several roles and has no single-role ordinal. A
// storage cluster with no ceph topology yields (nil, true): the cluster exists
// but binds no SSH-reachable node.
func ClusterNodes(state v1alpha1.State, clusterName string) ([]ClusterNode, bool) {
	if clusterName == "" {
		return nil, false
	}
	for _, ocp := range state.ContainerClusters {
		if ocp.Metadata.Name != clusterName {
			continue
		}
		roleCounts := map[string]int{}
		nodes := make([]ClusterNode, 0, len(ocp.Spec.Hosts))
		for _, host := range ocp.Spec.Hosts {
			nodes = append(nodes, ClusterNode{
				MachineName: host.MachineRef.Name,
				Role:        host.Role,
				Hostname:    host.Hostname,
				Ordinal:     roleCounts[host.Role],
				Kind:        MachineClusterKindContainer,
			})
			roleCounts[host.Role]++
		}
		return nodes, true
	}
	for _, sc := range state.StorageClusters {
		if sc.Metadata.Name != clusterName {
			continue
		}
		if sc.Spec.Ceph == nil {
			return nil, true
		}
		nodes := make([]ClusterNode, 0, len(sc.Spec.Ceph.Topology.Hosts))
		for i, host := range sc.Spec.Ceph.Topology.Hosts {
			nodes = append(nodes, ClusterNode{
				MachineName: host.MachineRef.Name,
				Role:        strings.Join(host.Roles, ","),
				Roles:       host.Roles,
				Hostname:    host.Hostname,
				Ordinal:     i,
				Kind:        MachineClusterKindStorage,
			})
		}
		return nodes, true
	}
	return nil, false
}

// HasRole reports whether a node fills the given role — matching a container
// node's single role or any of a storage node's Ceph roles.
func (n ClusterNode) HasRole(role string) bool {
	if len(n.Roles) > 0 {
		for _, r := range n.Roles {
			if r == role {
				return true
			}
		}
		return false
	}
	return n.Role == role
}
