package topology

import "github.com/crmarques/bootwright/api/v1alpha1"

const (
	StretchReplicatedPoolSize    = 4
	StretchReplicatedPoolMinSize = 2

	DefaultReplicatedPoolSize    = 3
	DefaultReplicatedPoolMinSize = 2

	CephManagementDefaultPort = 8443

	CephAdminConfigPath  = "/etc/ceph/ceph.conf"
	CephAdminKeyringPath = "/etc/ceph/ceph.client.admin.keyring"

	CephDashboardDefaultUser = "admin"
)

func EffectivePoolReplicas(state v1alpha1.State, cluster v1alpha1.StorageCluster, pool v1alpha1.StoragePool) v1alpha1.StorageCephPoolReplicas {
	replicas := pool.Spec.Ceph.Replicated
	if pool.Spec.PlacementPolicyRef.Name != "" {
		for _, policy := range state.StoragePlacementPolicies {
			if policy.Metadata.Name != pool.Spec.PlacementPolicyRef.Name {
				continue
			}
			if replicas.Size == 0 {
				replicas.Size = policy.Spec.Ceph.Replicated.Size
			}
			if replicas.MinSize == 0 {
				replicas.MinSize = policy.Spec.Ceph.Replicated.MinSize
			}
			break
		}
	}
	if replicas.Size == 0 && cluster.Spec.Ceph.Topology.Stretch != nil {
		replicas.Size = StretchReplicatedPoolSize
	}
	if replicas.MinSize == 0 && cluster.Spec.Ceph.Topology.Stretch != nil {
		replicas.MinSize = StretchReplicatedPoolMinSize
	}
	if replicas.Size == 0 {
		replicas.Size = DefaultReplicatedPoolSize
	}
	if replicas.MinSize == 0 {
		replicas.MinSize = DefaultReplicatedPoolMinSize
	}
	return replicas
}

func StoragePoolApplication(pool v1alpha1.StoragePool) string {
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

func StoragePoolFailureDomain(state v1alpha1.State, cluster v1alpha1.StorageCluster, pool v1alpha1.StoragePool) string {
	if pool.Spec.PlacementPolicyRef.Name != "" {
		for _, policy := range state.StoragePlacementPolicies {
			if policy.Metadata.Name == pool.Spec.PlacementPolicyRef.Name && policy.Spec.Ceph.FailureDomain != "" {
				return policy.Spec.Ceph.FailureDomain
			}
		}
	}
	return FailureDomain(cluster)
}

func StoragePoolCRUSHRule(state v1alpha1.State, cluster v1alpha1.StorageCluster, pool v1alpha1.StoragePool) string {
	if pool.Spec.PlacementPolicyRef.Name != "" {
		for _, policy := range state.StoragePlacementPolicies {
			if policy.Metadata.Name == pool.Spec.PlacementPolicyRef.Name {
				return policy.Spec.Ceph.RuleName
			}
		}
	}
	if stretch := cluster.Spec.Ceph.Topology.Stretch; stretch != nil {
		return stretch.RuleName
	}
	return ""
}
