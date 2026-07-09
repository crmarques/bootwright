package stateview

import (
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func ComposeFQDN(host, clusterName, baseDomain string) string {
	if host == "" || clusterName == "" || baseDomain == "" {
		return host
	}
	return host + "." + clusterName + "." + baseDomain
}

func IsSingleNodeCluster(ocp v1alpha1.ContainerCluster) bool {
	return len(ocp.Spec.Hosts) == 1 && ocp.Spec.Hosts[0].Role == v1alpha1.NodeRoleMaster
}

func NodeHostname(state v1alpha1.State, machineName string) (string, bool) {
	if machineName == "" {
		return "", false
	}
	for _, ocp := range state.ContainerClusters {
		for _, node := range ocp.Spec.Hosts {
			if node.MachineRef.Name == machineName {
				return node.Hostname, node.Hostname != ""
			}
		}
	}
	for _, sc := range state.StorageClusters {
		if sc.Spec.Ceph == nil {
			continue
		}
		for _, node := range sc.Spec.Ceph.Topology.Hosts {
			if node.MachineRef.Name == machineName {
				return node.Hostname, node.Hostname != ""
			}
		}
	}
	return "", false
}

const (
	MachineClusterKindContainer = "container"
	MachineClusterKindStorage   = "storage"
)

type MachineClusterBinding struct {
	Cluster string
	Kind    string
	Role    string
}

func MachineCluster(state v1alpha1.State, machineName string) (MachineClusterBinding, bool) {
	if machineName == "" {
		return MachineClusterBinding{}, false
	}
	for _, ocp := range state.ContainerClusters {
		for _, node := range ocp.Spec.Hosts {
			if node.MachineRef.Name == machineName {
				return MachineClusterBinding{Cluster: ocp.Metadata.Name, Kind: MachineClusterKindContainer, Role: node.Role}, true
			}
		}
	}
	for _, sc := range state.StorageClusters {
		if sc.Spec.Ceph == nil {
			continue
		}
		for _, node := range sc.Spec.Ceph.Topology.Hosts {
			if node.MachineRef.Name == machineName {
				return MachineClusterBinding{Cluster: sc.Metadata.Name, Kind: MachineClusterKindStorage, Role: strings.Join(node.Roles, ",")}, true
			}
		}
	}
	return MachineClusterBinding{}, false
}
