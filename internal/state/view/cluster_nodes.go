package stateview

import (
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

type ClusterNode struct {
	MachineName string
	NodeName    string
	Role        string
	Roles       []string
	Hostname    string
	Ordinal     int
	Kind        string
}

func ClusterNodes(state v1alpha1.State, clusterName string) ([]ClusterNode, bool) {
	if clusterName == "" {
		return nil, false
	}
	for _, ocp := range state.ContainerClusters {
		if ocp.Metadata.Name != clusterName {
			continue
		}
		roleCounts := map[string]int{}
		nodes := make([]ClusterNode, 0, len(ocp.Spec.Nodes))
		for _, host := range ocp.Spec.Nodes {
			nodes = append(nodes, ClusterNode{
				MachineName: host.MachineRef.Name,
				NodeName:    NodeShortName(host.Name),
				Role:        host.Role,
				Hostname:    host.Name,
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
		nodes := make([]ClusterNode, 0, len(sc.Spec.Ceph.Topology.Nodes))
		for i, host := range sc.Spec.Ceph.Topology.Nodes {
			nodes = append(nodes, ClusterNode{
				MachineName: host.MachineRef.Name,
				NodeName:    NodeShortName(host.Name),
				Role:        strings.Join(host.Roles, ","),
				Roles:       host.Roles,
				Hostname:    host.Name,
				Ordinal:     i,
				Kind:        MachineClusterKindStorage,
			})
		}
		return nodes, true
	}
	return nil, false
}

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
