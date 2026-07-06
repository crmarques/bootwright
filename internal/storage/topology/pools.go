package topology

import "github.com/crmarques/bootwright/api/v1alpha1"

const (
	// StretchReplicatedPoolSize and StretchReplicatedPoolMinSize are the
	// Ceph-required replication for two-site stretch (two replicas per data
	// site). Non-4/2 stretch is unsupported today, so these are domain
	// constants rather than authored API. Validation rejects authored values
	// that depart from them; rendering applies them as the policy-less default.
	StretchReplicatedPoolSize    = 4
	StretchReplicatedPoolMinSize = 2

	// DefaultReplicatedPoolSize and DefaultReplicatedPoolMinSize are the
	// defaults Bootwright applies to a replicated pool with no authored or
	// placement-policy-derived replication on a non-stretch cluster.
	DefaultReplicatedPoolSize    = 3
	DefaultReplicatedPoolMinSize = 2

	// CephManagementDefaultPort is the mgmt-gateway frontend (https) port and
	// the Ceph dashboard's own default. It is the single owner of that default
	// for both the renderer (spec.ceph.management.port) and the access-summary
	// dashboard URL, so the deployed gateway and the reported URL cannot drift.
	CephManagementDefaultPort = 8443

	// CephAdminConfigPath and CephAdminKeyringPath are the seed-node paths where
	// cephadm places the cluster config and the client.admin keyring. Bootwright
	// keeps no copy on the controller, so access (ssh + cephadm shell) references
	// these on-node paths; they live here with the other Ceph default-layout facts
	// so the access summary and any future storage consumer read one owner.
	CephAdminConfigPath  = "/etc/ceph/ceph.conf"
	CephAdminKeyringPath = "/etc/ceph/ceph.client.admin.keyring"

	// CephDashboardDefaultUser is cephadm's default initial dashboard user.
	// Bootwright does not override it at bootstrap, so it is always "admin".
	CephDashboardDefaultUser = "admin"
)

// EffectivePoolReplicas resolves a replicated pool's size/minSize: authored
// values win, then placement-policy defaults, then the stretch 4/2 constants
// for stretch clusters, then the cephadm 3/2 default.
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

// StoragePoolApplication resolves the Ceph pool application: the authored
// value, else the application implied by the pool role.
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

// StoragePoolFailureDomain resolves the CRUSH failure domain for a pool's
// erasure-code profile: the referenced placement policy's failureDomain when
// set, else the cluster-wide failure domain (stretch failureDomain or "host").
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

// StoragePoolCRUSHRule resolves the CRUSH rule a pool binds to: the referenced
// placement policy's rule, else the stretch rule, else none.
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
