package ceph

import (
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

func cephCephxOperations(cluster v1alpha1.StorageCluster) []map[string]any {
	keyType := v1alpha1.StorageClusterCephxKeyType(cluster)
	if keyType == "" {
		return nil
	}
	return []map[string]any{
		operationInPhase("topology", "set-cephx-allowed-ciphers", "ceph", "mon", "set", "auth_allowed_ciphers", v1alpha1.StorageCephCephxAllowedCiphers(keyType)),
		operationInPhase("topology", "set-cephx-preferred-cipher", "ceph", "mon", "set", "auth_preferred_cipher", keyType),
	}
}

func cephTopologyOperations(cluster v1alpha1.StorageCluster) []map[string]any {
	var ops []map[string]any
	ops = append(ops, cephCephxOperations(cluster)...)
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
		for _, node := range cluster.Spec.Ceph.Topology.Nodes {
			if !topology.NodeHasRole(node, v1alpha1.StorageCephRoleMON) {
				continue
			}
			ops = append(ops, operationInPhase("topology", "set-mon-location-"+node.Name, "ceph", "mon", "set_location", topology.CephDaemonName(node.Name), stretch.FailureDomain+"="+node.Site))
		}
		ops = append(ops, cephCrushHostLocationOperations(cluster, stretch)...)
		stretchRule := operationWithIdempotency("topology", "create-crush-rule-"+stretch.RuleName, "stretch-crush-rule", stretch.RuleName)
		stretchRule["structural"] = map[string]any{
			"failureDomain":            stretch.FailureDomain,
			"replicasPerFailureDomain": 2,
		}
		ops = append(ops, stretchRule)
		if stretch.Tiebreaker.Node != "" {
			ops = append(ops, operationWithIdempotency("topology", "enable-stretch-mode", "stretch-mode", "enabled", "ceph", "mon", "enable_stretch_mode", topology.CephDaemonName(topology.CanonicalHostname(cluster, stretch.Tiebreaker.Node)), stretch.RuleName, stretch.FailureDomain))
		}
	}
	return ops
}

func cephStretchInternalPoolOperations(cluster v1alpha1.StorageCluster) []map[string]any {
	stretch := cluster.Spec.Ceph.Topology.Stretch
	if stretch == nil || stretch.RuleName == "" {
		return nil
	}
	op := operationWithIdempotency("late-topology", "reconcile-stretch-internal-pools", "stretch-internal-pools", stretch.RuleName)
	op["structural"] = map[string]any{
		"ruleName":    stretch.RuleName,
		"size":        topology.StretchReplicatedPoolSize,
		"minSize":     topology.StretchReplicatedPoolMinSize,
		"poolPattern": topology.InternalPoolPattern(),
	}
	return []map[string]any{op}
}

func cephCrushHostLocationOperations(cluster v1alpha1.StorageCluster, stretch *v1alpha1.StorageCephStretch) []map[string]any {
	mode, _, _ := topology.OSDReadinessExpectation(cluster)
	if mode == "skip" {
		return nil
	}
	tiebreaker := topology.CanonicalHostname(cluster, stretch.Tiebreaker.Node)
	var ops []map[string]any
	for _, node := range cluster.Spec.Ceph.Topology.Nodes {
		if node.Site == "" || node.Name == tiebreaker || !topology.NodeHasRole(node, v1alpha1.StorageCephRoleOSD) {
			continue
		}
		ops = append(ops, operationInPhase("topology", "set-crush-location-"+node.Name,
			"ceph", "osd", "crush", "move", topology.CephDaemonName(node.Name),
			"root=default", stretch.FailureDomain+"="+node.Site))
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
