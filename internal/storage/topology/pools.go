package topology

import (
	"regexp"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

var (
	internalPoolNames      = []string{".mgr", ".nfs", "device_health_metrics"}
	internalPoolSubstrings = []string{".rgw."}
)

const (
	StretchReplicatedPoolSize    = 4
	StretchReplicatedPoolMinSize = 2

	DefaultReplicatedPoolSize    = 3
	DefaultReplicatedPoolMinSize = 2
	SingleHostReplicatedPoolSize = SingleHostMinimumOSDs
	SingleHostReplicatedMinSize  = 1

	CephDashboardDefaultTLSPort = 8443

	CephAdminConfigPath  = "/etc/ceph/ceph.conf"
	CephAdminKeyringPath = "/etc/ceph/ceph.client.admin.keyring"

	CephDashboardDefaultUser = "admin"
)

func IsInternalPool(name string) bool {
	for _, item := range internalPoolNames {
		if name == item {
			return true
		}
	}
	for _, item := range internalPoolSubstrings {
		if strings.Contains(name, item) {
			return true
		}
	}
	return false
}

func InternalPoolPattern() string {
	quoted := make([]string, 0, len(internalPoolNames))
	for _, name := range internalPoolNames {
		quoted = append(quoted, regexp.QuoteMeta(name))
	}
	parts := []string{"^(" + strings.Join(quoted, "|") + ")$"}
	for _, item := range internalPoolSubstrings {
		parts = append(parts, regexp.QuoteMeta(item))
	}
	return strings.Join(parts, "|")
}

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
	return DefaultPoolReplicas(cluster, replicas)
}

func DefaultPoolReplicas(cluster v1alpha1.StorageCluster, replicas v1alpha1.StorageCephPoolReplicas) v1alpha1.StorageCephPoolReplicas {
	if replicas.Size == 0 && cluster.Spec.Ceph != nil && cluster.Spec.Ceph.Topology.Stretch != nil {
		replicas.Size = StretchReplicatedPoolSize
	}
	if replicas.MinSize == 0 && cluster.Spec.Ceph != nil && cluster.Spec.Ceph.Topology.Stretch != nil {
		replicas.MinSize = StretchReplicatedPoolMinSize
	}
	if replicas.Size == 0 && cluster.Spec.Ceph != nil && cluster.Spec.Ceph.Cephadm.Bootstrap.SingleHostDefaults {
		replicas.Size = SingleHostReplicatedPoolSize
	}
	if replicas.MinSize == 0 && cluster.Spec.Ceph != nil && cluster.Spec.Ceph.Cephadm.Bootstrap.SingleHostDefaults {
		replicas.MinSize = SingleHostReplicatedMinSize
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
