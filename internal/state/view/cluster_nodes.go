package stateview

import (
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

type ClusterNode struct {
	MachineName string
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
