package render

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

func cephOperations(state v1alpha1.State, cluster v1alpha1.StorageCluster) map[string]any {
	var ops []map[string]any
	if stretch := cluster.Spec.Ceph.Topology.Stretch; stretch != nil && stretch.Enabled {
		for _, node := range cluster.Spec.Ceph.Topology.Nodes {
			if !topology.NodeHasRole(node, v1alpha1.StorageCephRoleMON) {
				continue
			}
			ops = append(ops, operationInPhase("topology", "set-mon-location-"+node.Name, "ceph", "mon", "set_location", node.Name, stretch.FailureDomain+"="+node.Site))
		}
		ops = append(ops, operationWithIdempotency("topology", "create-crush-rule-"+stretch.RuleName, "crush-rule", stretch.RuleName, "ceph", "osd", "crush", "rule", "create-replicated", stretch.RuleName, "default", stretch.FailureDomain))
		ops = append(ops, operationWithIdempotency("topology", "enable-stretch-mode", "stretch-mode", "enabled", "ceph", "mon", "enable_stretch_mode", stretch.Tiebreaker.Node, stretch.RuleName, stretch.FailureDomain))
	}
	for _, policy := range state.StoragePlacementPolicies {
		if policy.Spec.StorageClusterRef.Name != cluster.Metadata.Name {
			continue
		}
		if policy.Spec.Ceph.RuleName == "" || storageRuleCreatedByStretch(cluster, policy.Spec.Ceph.RuleName) {
			continue
		}
		failureDomain := policy.Spec.Ceph.FailureDomain
		if failureDomain == "" {
			failureDomain = topology.FailureDomain(cluster)
		}
		ops = append(ops, operationWithIdempotency("topology", "create-crush-rule-"+policy.Spec.Ceph.RuleName, "crush-rule", policy.Spec.Ceph.RuleName, "ceph", "osd", "crush", "rule", "create-replicated", policy.Spec.Ceph.RuleName, "default", failureDomain))
	}
	for _, pool := range state.StoragePools {
		if pool.Spec.StorageClusterRef.Name != cluster.Metadata.Name {
			continue
		}
		ops = append(ops, operationWithIdempotency("storage", "create-pool-"+pool.Metadata.Name, "ceph-pool", pool.Metadata.Name, "ceph", "osd", "pool", "create", pool.Metadata.Name))
		if rule := storagePoolCRUSHRule(state, cluster, pool); rule != "" {
			ops = append(ops, operationInPhase("storage", "set-pool-crush-rule-"+pool.Metadata.Name, "ceph", "osd", "pool", "set", pool.Metadata.Name, "crush_rule", rule))
		}
		replicas := effectivePoolReplicas(cluster, pool)
		ops = append(ops, operationInPhase("storage", "set-pool-size-"+pool.Metadata.Name, "ceph", "osd", "pool", "set", pool.Metadata.Name, "size", fmt.Sprint(replicas.Size)))
		ops = append(ops, operationInPhase("storage", "set-pool-min-size-"+pool.Metadata.Name, "ceph", "osd", "pool", "set", pool.Metadata.Name, "min_size", fmt.Sprint(replicas.MinSize)))
		if app := storagePoolApplication(pool); app != "" {
			ops = append(ops, operationInPhase("storage", "enable-pool-application-"+pool.Metadata.Name, "ceph", "osd", "pool", "application", "enable", pool.Metadata.Name, app))
		}
	}
	for _, fs := range state.StorageFilesystems {
		if fs.Spec.StorageClusterRef.Name != cluster.Metadata.Name {
			continue
		}
		defaultData := topology.FilesystemDefaultDataPool(fs)
		ops = append(ops, operationWithIdempotency("storage", "create-cephfs-"+fs.Metadata.Name, "cephfs", fs.Metadata.Name, "ceph", "fs", "new", fs.Metadata.Name, fs.Spec.CephFS.MetadataPoolRef.Name, defaultData))
		if fs.Spec.CephFS.MDS.ActiveCount > 0 {
			ops = append(ops, operationInPhase("storage", "set-cephfs-max-mds-"+fs.Metadata.Name, "ceph", "fs", "set", fs.Metadata.Name, "max_mds", fmt.Sprint(fs.Spec.CephFS.MDS.ActiveCount)))
		}
	}
	for _, gw := range state.StorageObjectGateways {
		if gw.Spec.StorageClusterRef.Name != cluster.Metadata.Name {
			continue
		}
		uid := "bootwright-" + gw.Metadata.Name + "-admin"
		ops = append(ops, operationWithIdempotency("object-gateway", "create-rgw-admin-user-"+gw.Metadata.Name, "rgw-user", uid, "radosgw-admin", "user", "create", "--uid", uid, "--display-name", "Bootwright "+gw.Metadata.Name+" admin", "--format", "json"))
	}
	ops = append(ops, dataFoundationCredentialOperations(state, cluster)...)
	return map[string]any{
		"apiVersion": "bootwright.io/v1alpha1",
		"kind":       "StorageOperations",
		"metadata": map[string]any{
			"name": cluster.Metadata.Name,
		},
		"operations": ops,
	}
}

func operationInPhase(phase, name string, command ...string) map[string]any {
	return map[string]any{
		"phase":   phase,
		"name":    name,
		"command": command,
	}
}

func operationWithIdempotency(phase, name, kind, resourceName string, command ...string) map[string]any {
	op := operationInPhase(phase, name, command...)
	op["idempotency"] = map[string]any{
		"kind": kind,
		"name": resourceName,
	}
	return op
}

func effectivePoolReplicas(cluster v1alpha1.StorageCluster, pool v1alpha1.StoragePool) v1alpha1.StorageCephPoolReplicas {
	replicas := pool.Spec.Ceph.Replicated
	if replicas.Size == 0 && cluster.Spec.Ceph.Topology.Stretch != nil {
		replicas.Size = cluster.Spec.Ceph.Topology.Stretch.ReplicatedPoolDefaults.Size
	}
	if replicas.MinSize == 0 && cluster.Spec.Ceph.Topology.Stretch != nil {
		replicas.MinSize = cluster.Spec.Ceph.Topology.Stretch.ReplicatedPoolDefaults.MinSize
	}
	if replicas.Size == 0 {
		replicas.Size = 3
	}
	if replicas.MinSize == 0 {
		replicas.MinSize = 2
	}
	return replicas
}

func storagePoolApplication(pool v1alpha1.StoragePool) string {
	if pool.Spec.Ceph.Application != "" {
		return pool.Spec.Ceph.Application
	}
	switch pool.Spec.Ceph.Role {
	case v1alpha1.StoragePoolRoleRBD:
		return "rbd"
	case v1alpha1.StoragePoolRoleCephFSMetadata, v1alpha1.StoragePoolRoleCephFSData:
		return "cephfs"
	case v1alpha1.StoragePoolRoleRGW:
		return "rgw"
	default:
		return ""
	}
}

func storagePoolCRUSHRule(state v1alpha1.State, cluster v1alpha1.StorageCluster, pool v1alpha1.StoragePool) string {
	if pool.Spec.PlacementPolicyRef.Name != "" {
		for _, policy := range state.StoragePlacementPolicies {
			if policy.Metadata.Name == pool.Spec.PlacementPolicyRef.Name {
				return policy.Spec.Ceph.RuleName
			}
		}
	}
	if stretch := cluster.Spec.Ceph.Topology.Stretch; stretch != nil && stretch.Enabled {
		return stretch.RuleName
	}
	return ""
}

func storageRuleCreatedByStretch(cluster v1alpha1.StorageCluster, rule string) bool {
	stretch := cluster.Spec.Ceph.Topology.Stretch
	return stretch != nil && stretch.Enabled && stretch.RuleName == rule
}
