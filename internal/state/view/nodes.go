package stateview

import (
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// ComposeFQDN builds the fully-qualified node name <host>.<cluster>.<baseDomain>
// — the OpenShift node convention, generalized to every cluster node. An empty
// host, cluster, or baseDomain yields the bare host: there is nothing to
// qualify it with, so callers fall back to the short name.
func ComposeFQDN(host, clusterName, baseDomain string) string {
	if host == "" || clusterName == "" || baseDomain == "" {
		return host
	}
	return host + "." + clusterName + "." + baseDomain
}

// IsSingleNodeCluster reports whether a ContainerCluster is single-node (SNO):
// exactly one host, carrying the master role.
func IsSingleNodeCluster(ocp v1alpha1.ContainerCluster) bool {
	return len(ocp.Spec.Hosts) == 1 && ocp.Spec.Hosts[0].Role == v1alpha1.NodeRoleMaster
}

// NodeHostname returns the registered hostname of the node a machine backs —
// the value its cluster topology carries and that cephadm, the OS installer,
// and DNS must all agree on. After normalize this is the FQDN from ComposeFQDN
// (or an explicit verbatim hostname). It returns ("", false) for machines no
// cluster node-binds (e.g. a bastion), leaving the bare-name fallback to the
// caller.
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

// Cluster kind stamps for a MachineClusterBinding, mirroring the two topologies
// a Machine can be node-bound by.
const (
	MachineClusterKindContainer = "container"
	MachineClusterKindStorage   = "storage"
)

// MachineClusterBinding identifies the cluster node a Machine backs: the
// enclosing cluster's name, which topology it lives in, and the node's role
// within it (the OCP master/worker/infra role, or the comma-joined Ceph host
// roles).
type MachineClusterBinding struct {
	Cluster string
	Kind    string
	Role    string
}

// MachineCluster returns the cluster a Machine is node-bound to, if any. A
// Machine is node-bound by at most one cluster (and at most one host entry)
// across every ContainerCluster and StorageCluster, so the first match is
// authoritative. It returns (zero, false) for machines no cluster binds — a
// bastion, a provider host, or a not-yet-referenced Machine.
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
