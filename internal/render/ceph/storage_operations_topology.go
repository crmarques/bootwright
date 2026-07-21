package ceph

import (
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

func cephTopologyOperations(cluster v1alpha1.StorageCluster) []map[string]any {
	var ops []map[string]any
	if publics := cluster.Spec.Ceph.Networks.PublicCIDRs; len(publics) > 0 {
		ops = append(ops, operationInPhase("topology", "set-public-network", "ceph", "config", "set", "global", "public_network", strings.Join(publics, ",")))
	}
	if clusters := cluster.Spec.Ceph.Networks.ClusterCIDRs; len(clusters) > 0 {
		ops = append(ops, operationInPhase("topology", "set-cluster-network", "ceph", "config", "set", "global", "cluster_network", strings.Join(clusters, ",")))
	}
	for _, section := range sortedKeys(cluster.Spec.Ceph.Config) {
		options := cluster.Spec.Ceph.Config[section]
		for _, key := range sortedKeys(options) {
			ops = append(ops, operationInPhase("topology", "set-config-"+section+"-"+key, "ceph", "config", "set", section, key, options[key]))
		}
	}
	if stretch := cluster.Spec.Ceph.Topology.Stretch; stretch != nil {
		ops = append(ops, operationInPhase("topology", "set-election-strategy", "ceph", "mon", "set", "election_strategy", "connectivity"))
		for _, node := range cluster.Spec.Ceph.Topology.Hosts {
			if !topology.NodeHasRole(node, v1alpha1.StorageCephRoleMON) {
				continue
			}
			ops = append(ops, operationInPhase("topology", "set-mon-location-"+node.Hostname, "ceph", "mon", "set_location", node.Hostname, stretch.FailureDomain+"="+node.Site))
		}
		stretchRule := operationWithIdempotency("topology", "create-crush-rule-"+stretch.RuleName, "stretch-crush-rule", stretch.RuleName)
		stretchRule["structural"] = map[string]any{
			"failureDomain":            stretch.FailureDomain,
			"replicasPerFailureDomain": 2,
		}
		ops = append(ops, stretchRule)
		if stretch.Tiebreaker.Host != "" {
			ops = append(ops, operationWithIdempotency("topology", "enable-stretch-mode", "stretch-mode", "enabled", "ceph", "mon", "enable_stretch_mode", topology.CanonicalHostname(cluster, stretch.Tiebreaker.Host), stretch.RuleName, stretch.FailureDomain))
		}
	}
	return ops
}

func cephMgrAndLoggingOperations(cluster v1alpha1.StorageCluster) []map[string]any {
	var ops []map[string]any
	for _, module := range cluster.Spec.Ceph.MgrModules {
		ops = append(ops, operationWithIdempotency("topology", "enable-mgr-module-"+module, "mgr-module", module, "ceph", "mgr", "module", "enable", module))
	}
	return ops
}
