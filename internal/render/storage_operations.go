package render

import (
	"fmt"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

func cephOperations(state v1alpha1.State, cluster v1alpha1.StorageCluster) map[string]any {
	var ops []map[string]any
	// public_network has no cephadm bootstrap flag (unlike --cluster-network);
	// it is seeded at bootstrap via --config and kept converged here. Both
	// network ops reconcile in place: `ceph config set` is last-write-wins.
	if publics := cluster.Spec.Ceph.Networks.PublicCIDRs; len(publics) > 0 {
		ops = append(ops, operationInPhase("topology", "set-public-network", "ceph", "config", "set", "global", "public_network", strings.Join(publics, ",")))
	}
	if clusters := cluster.Spec.Ceph.Networks.ClusterCIDRs; len(clusters) > 0 {
		ops = append(ops, operationInPhase("topology", "set-cluster-network", "ceph", "config", "set", "global", "cluster_network", strings.Join(clusters, ",")))
	}
	// Declared ceph config options reconcile in place (`ceph config set` is
	// last-write-wins); convergence is additive-only by design.
	for _, section := range sortedKeys(cluster.Spec.Ceph.Config) {
		options := cluster.Spec.Ceph.Config[section]
		for _, key := range sortedKeys(options) {
			ops = append(ops, operationInPhase("topology", "set-config-"+section+"-"+key, "ceph", "config", "set", section, key, options[key]))
		}
	}
	if stretch := cluster.Spec.Ceph.Topology.Stretch; stretch != nil {
		// Stretch mode requires the connectivity election strategy before
		// enable_stretch_mode; `ceph mon set` reconciles in place.
		ops = append(ops, operationInPhase("topology", "set-election-strategy", "ceph", "mon", "set", "election_strategy", "connectivity"))
		for _, node := range cluster.Spec.Ceph.Topology.Hosts {
			if !topology.NodeHasRole(node, v1alpha1.StorageCephRoleMON) {
				continue
			}
			ops = append(ops, operationInPhase("topology", "set-mon-location-"+node.Hostname, "ceph", "mon", "set_location", node.Hostname, stretch.FailureDomain+"="+node.Site))
		}
		// The stretch rule must place two replicas per data site
		// (choose firstn 0 type <failureDomain> + chooseleaf firstn 2 type
		// host); `crush rule create-replicated` cannot express that two-step
		// rule, so the role compiles it into the CRUSH map itself, keyed on
		// this structured operation (no argv).
		stretchRule := operationWithIdempotency("topology", "create-crush-rule-"+stretch.RuleName, "stretch-crush-rule", stretch.RuleName)
		stretchRule["structural"] = map[string]any{
			"failureDomain":            stretch.FailureDomain,
			"replicasPerFailureDomain": 2,
		}
		ops = append(ops, stretchRule)
		ops = append(ops, operationWithIdempotency("topology", "enable-stretch-mode", "stretch-mode", "enabled", "ceph", "mon", "enable_stretch_mode", stretch.Tiebreaker.Host, stretch.RuleName, stretch.FailureDomain))
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
		if ec := pool.Spec.Ceph.ErasureCoded; pool.Spec.Ceph.Type == v1alpha1.StoragePoolTypeErasureCode && ec != nil {
			// An erasure-coded pool needs its profile before create, and the
			// create itself must say `erasure <profile>` — without it Ceph
			// silently creates a replicated pool. size/min_size are derived
			// from k+m on EC pools and the CRUSH rule comes from the profile,
			// so none of the replicated set-* operations apply.
			profile := pool.Metadata.Name + "-profile"
			profileCmd := []string{
				"ceph", "osd", "erasure-code-profile", "set", profile,
				fmt.Sprintf("k=%d", ec.DataChunks),
				fmt.Sprintf("m=%d", ec.CodingChunks),
				"crush-failure-domain=" + storagePoolFailureDomain(state, cluster, pool),
			}
			ops = append(ops, operationWithIdempotency("storage", "create-ec-profile-"+pool.Metadata.Name, "ec-profile", profile, profileCmd...))
			createPool := operationWithIdempotency("storage", "create-pool-"+pool.Metadata.Name, "ceph-pool", pool.Metadata.Name, "ceph", "osd", "pool", "create", pool.Metadata.Name, "erasure", profile)
			createPool["structural"] = storagePoolStructural(pool)
			ops = append(ops, createPool)
			// RBD images and CephFS data on EC pools require partial-write
			// support; without it the pool converges but every client write
			// fails.
			switch pool.Spec.Ceph.Role {
			case v1alpha1.StoragePoolRoleRBD, v1alpha1.StoragePoolRoleCephFSData:
				ops = append(ops, operationInPhase("storage", "set-pool-ec-overwrites-"+pool.Metadata.Name, "ceph", "osd", "pool", "set", pool.Metadata.Name, "allow_ec_overwrites", "true"))
			}
		} else {
			createPool := operationWithIdempotency("storage", "create-pool-"+pool.Metadata.Name, "ceph-pool", pool.Metadata.Name, "ceph", "osd", "pool", "create", pool.Metadata.Name)
			createPool["structural"] = storagePoolStructural(pool)
			ops = append(ops, createPool)
			if rule := storagePoolCRUSHRule(state, cluster, pool); rule != "" {
				ops = append(ops, operationInPhase("storage", "set-pool-crush-rule-"+pool.Metadata.Name, "ceph", "osd", "pool", "set", pool.Metadata.Name, "crush_rule", rule))
			}
			replicas := effectivePoolReplicas(state, cluster, pool)
			ops = append(ops, operationInPhase("storage", "set-pool-size-"+pool.Metadata.Name, "ceph", "osd", "pool", "set", pool.Metadata.Name, "size", fmt.Sprint(replicas.Size)))
			ops = append(ops, operationInPhase("storage", "set-pool-min-size-"+pool.Metadata.Name, "ceph", "osd", "pool", "set", pool.Metadata.Name, "min_size", fmt.Sprint(replicas.MinSize)))
		}
		if app := storagePoolApplication(pool); app != "" {
			ops = append(ops, operationInPhase("storage", "enable-pool-application-"+pool.Metadata.Name, "ceph", "osd", "pool", "application", "enable", pool.Metadata.Name, app))
		}
	}
	for _, fs := range state.StorageFilesystems {
		if fs.Spec.StorageClusterRef.Name != cluster.Metadata.Name {
			continue
		}
		defaultData := topology.FilesystemDefaultDataPool(fs)
		createFS := operationWithIdempotency("storage", "create-cephfs-"+fs.Metadata.Name, "cephfs", fs.Metadata.Name, "ceph", "fs", "new", fs.Metadata.Name, fs.Spec.CephFS.MetadataPoolRef.Name, defaultData)
		createFS["structural"] = map[string]any{"metadataPool": fs.Spec.CephFS.MetadataPoolRef.Name, "defaultDataPool": defaultData}
		ops = append(ops, createFS)
		// `ceph fs new` wires only the default data pool; attach the remaining
		// declared data pools so a multi-data-pool CephFS matches desired state.
		// add_data_pool is idempotent on Ceph, so no separate skip probe is needed.
		for _, ref := range fs.Spec.CephFS.DataPoolRefs {
			if ref.Name == "" || ref.Name == defaultData {
				continue
			}
			ops = append(ops, operationInPhase("storage", "add-cephfs-data-pool-"+fs.Metadata.Name+"-"+ref.Name, "ceph", "fs", "add_data_pool", fs.Metadata.Name, ref.Name))
		}
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

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func operationInPhase(phase, name string, command ...string) map[string]any {
	if command == nil {
		// A structured operation (role-implemented, no argv) must render
		// `command: []`, not null: the role filters on `command | length`.
		command = []string{}
	}
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

// storagePoolStructural is the pool's immutable identity: the only desired-state
// change that warrants a destroy+recreate under --override (a live Ceph pool cannot
// change type or erasure profile in place). Replica size/min-size, crush rule, and
// application reconcile in place via the set-pool-* operations and so are NOT
// included here — under --override they reconcile without data loss. The role
// compares this against the live pool and only rebuilds (data-destroying) on a
// genuine structural mismatch.
func storagePoolStructural(pool v1alpha1.StoragePool) map[string]any {
	poolType := pool.Spec.Ceph.Type
	if poolType == "" {
		poolType = v1alpha1.StoragePoolTypeReplicated
	}
	structural := map[string]any{"type": poolType}
	if pool.Spec.Ceph.ErasureCoded != nil {
		structural["dataChunks"] = pool.Spec.Ceph.ErasureCoded.DataChunks
		structural["codingChunks"] = pool.Spec.Ceph.ErasureCoded.CodingChunks
	}
	return structural
}

func effectivePoolReplicas(state v1alpha1.State, cluster v1alpha1.StorageCluster, pool v1alpha1.StoragePool) v1alpha1.StorageCephPoolReplicas {
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

// storagePoolFailureDomain resolves the CRUSH failure domain for a pool's
// erasure-code profile: the referenced placement policy's failureDomain when
// set, else the cluster-wide failure domain (stretch failureDomain or "host").
func storagePoolFailureDomain(state v1alpha1.State, cluster v1alpha1.StorageCluster, pool v1alpha1.StoragePool) string {
	if pool.Spec.PlacementPolicyRef.Name != "" {
		for _, policy := range state.StoragePlacementPolicies {
			if policy.Metadata.Name == pool.Spec.PlacementPolicyRef.Name && policy.Spec.Ceph.FailureDomain != "" {
				return policy.Spec.Ceph.FailureDomain
			}
		}
	}
	return topology.FailureDomain(cluster)
}

func storagePoolCRUSHRule(state v1alpha1.State, cluster v1alpha1.StorageCluster, pool v1alpha1.StoragePool) string {
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

func storageRuleCreatedByStretch(cluster v1alpha1.StorageCluster, rule string) bool {
	stretch := cluster.Spec.Ceph.Topology.Stretch
	return stretch != nil && stretch.RuleName == rule
}
