package stateview

import "github.com/crmarques/bootwright/api/v1alpha1"

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
