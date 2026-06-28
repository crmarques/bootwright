package ceph

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

// cephCrushRuleOperations renders the topology-phase create-replicated CRUSH
// rules declared by placement policies, skipping policies with no rule name and
// those whose rule is compiled by stretch mode (which owns that rule's create).
func cephCrushRuleOperations(state v1alpha1.State, cluster v1alpha1.StorageCluster) []map[string]any {
	var ops []map[string]any
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
		ruleCmd := []string{"ceph", "osd", "crush", "rule", "create-replicated", policy.Spec.Ceph.RuleName, "default", failureDomain}
		// The optional trailing class pins the rule to one device class; it is
		// fixed at create time, so a class change means a new ruleName.
		if class := policy.Spec.Ceph.CrushDeviceClass; class != "" {
			ruleCmd = append(ruleCmd, class)
		}
		ops = append(ops, operationWithIdempotency("topology", "create-crush-rule-"+policy.Spec.Ceph.RuleName, "crush-rule", policy.Spec.Ceph.RuleName, ruleCmd...))
	}
	return ops
}

// cephPoolOperations renders the storage-phase pool lifecycle for every pool
// owned by the cluster: the create (replicated or erasure-coded, with the EC
// profile created first), the crush-rule/size reconcile, the application enable,
// and the steady-state tuning intents.
func cephPoolOperations(state v1alpha1.State, cluster v1alpha1.StorageCluster) []map[string]any {
	var ops []map[string]any
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
				"crush-failure-domain=" + topology.StoragePoolFailureDomain(state, cluster, pool),
			}
			profileCmd = append(profileCmd, storageECProfileArgs(ec)...)
			ops = append(ops, erasureProfileOperation(pool.Metadata.Name, profile, profileCmd, topology.StoragePoolFailureDomain(state, cluster, pool), ec))
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
			if rule := topology.StoragePoolCRUSHRule(state, cluster, pool); rule != "" {
				ops = append(ops, operationInPhase("storage", "set-pool-crush-rule-"+pool.Metadata.Name, "ceph", "osd", "pool", "set", pool.Metadata.Name, "crush_rule", rule))
			}
			replicas := topology.EffectivePoolReplicas(state, cluster, pool)
			ops = append(ops, operationInPhase("storage", "set-pool-size-"+pool.Metadata.Name, "ceph", "osd", "pool", "set", pool.Metadata.Name, "size", fmt.Sprint(replicas.Size)))
			ops = append(ops, operationInPhase("storage", "set-pool-min-size-"+pool.Metadata.Name, "ceph", "osd", "pool", "set", pool.Metadata.Name, "min_size", fmt.Sprint(replicas.MinSize)))
		}
		if app := topology.StoragePoolApplication(pool); app != "" {
			ops = append(ops, operationInPhase("storage", "enable-pool-application-"+pool.Metadata.Name, "ceph", "osd", "pool", "application", "enable", pool.Metadata.Name, app))
		}
		// Steady-state per-pool intents apply to both the replicated and EC arms;
		// each reconciles in place (last-write-wins) and none is structural.
		ops = append(ops, storagePoolTuningOperations(pool)...)
	}
	return ops
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
	if ec := pool.Spec.Ceph.ErasureCoded; ec != nil {
		// The EC profile is immutable on Ceph, so every authored profile field —
		// including every opaque parameters key — is part of the structural
		// identity: a change must trigger the --override rebuild, never a no-op.
		structural["dataChunks"] = ec.DataChunks
		structural["codingChunks"] = ec.CodingChunks
		if ec.Plugin != "" {
			structural["plugin"] = ec.Plugin
		}
		if ec.Technique != "" {
			structural["technique"] = ec.Technique
		}
		if ec.CrushDeviceClass != "" {
			structural["crushDeviceClass"] = ec.CrushDeviceClass
		}
		if ec.CrushRoot != "" {
			structural["crushRoot"] = ec.CrushRoot
		}
		if ec.StripeUnit != "" {
			structural["stripeUnit"] = ec.StripeUnit
		}
		if len(ec.Parameters) > 0 {
			params := map[string]any{}
			for k, v := range ec.Parameters {
				params[k] = v
			}
			structural["parameters"] = params
		}
	}
	return structural
}

// storageECProfileArgs renders the optional erasure-code-profile knobs beyond
// k/m/crush-failure-domain (which the caller already emits) using their native
// `erasure-code-profile set` spellings. parameters render last, sorted, verbatim.
func storageECProfileArgs(ec *v1alpha1.StoragePoolErasureCode) []string {
	var args []string
	if ec.Plugin != "" {
		args = append(args, "plugin="+ec.Plugin)
	}
	if ec.Technique != "" {
		args = append(args, "technique="+ec.Technique)
	}
	if ec.CrushDeviceClass != "" {
		args = append(args, "crush-device-class="+ec.CrushDeviceClass)
	}
	if ec.CrushRoot != "" {
		args = append(args, "crush-root="+ec.CrushRoot)
	}
	if ec.StripeUnit != "" {
		args = append(args, "stripe_unit="+ec.StripeUnit)
	}
	for _, key := range sortedKeys(ec.Parameters) {
		args = append(args, key+"="+ec.Parameters[key])
	}
	return args
}

// storagePoolTuningOperations renders the per-pool steady-state intents
// (autoscaler, quota, compression) as idempotent ceph operations. Quota uses
// the distinct set-quota verb; an authored 0 is the native "no limit". Every op
// reconciles in place — none is part of the pool's structural identity.
func storagePoolTuningOperations(pool v1alpha1.StoragePool) []map[string]any {
	var ops []map[string]any
	name := pool.Metadata.Name
	setOp := func(suffix, key, value string) {
		ops = append(ops, operationInPhase("storage", "set-pool-"+suffix+"-"+name, "ceph", "osd", "pool", "set", name, key, value))
	}
	if a := pool.Spec.Ceph.Autoscale; a != nil {
		if a.Mode != "" {
			setOp("autoscale-mode", "pg_autoscale_mode", a.Mode)
		}
		if a.TargetSizeRatio > 0 {
			setOp("target-size-ratio", "target_size_ratio", fmt.Sprint(a.TargetSizeRatio))
		}
		if a.TargetSizeBytes != "" {
			setOp("target-size-bytes", "target_size_bytes", a.TargetSizeBytes)
		}
		if a.PGNumMin > 0 {
			setOp("pg-num-min", "pg_num_min", fmt.Sprint(a.PGNumMin))
		}
		if a.PGNumMax > 0 {
			setOp("pg-num-max", "pg_num_max", fmt.Sprint(a.PGNumMax))
		}
		if a.Bulk != nil {
			setOp("bulk", "bulk", fmt.Sprint(*a.Bulk))
		}
	}
	if q := pool.Spec.Ceph.Quota; q != nil {
		if q.MaxBytes != nil {
			ops = append(ops, operationInPhase("storage", "set-pool-quota-max-bytes-"+name, "ceph", "osd", "pool", "set-quota", name, "max_bytes", fmt.Sprint(*q.MaxBytes)))
		}
		if q.MaxObjects != nil {
			ops = append(ops, operationInPhase("storage", "set-pool-quota-max-objects-"+name, "ceph", "osd", "pool", "set-quota", name, "max_objects", fmt.Sprint(*q.MaxObjects)))
		}
	}
	if c := pool.Spec.Ceph.Compression; c != nil {
		if c.Mode != "" {
			setOp("compression-mode", "compression_mode", c.Mode)
		}
		if c.Algorithm != "" {
			setOp("compression-algorithm", "compression_algorithm", c.Algorithm)
		}
		if c.RequiredRatio > 0 {
			setOp("compression-required-ratio", "compression_required_ratio", fmt.Sprint(c.RequiredRatio))
		}
		if c.MinBlobSize != "" {
			setOp("compression-min-blob-size", "compression_min_blob_size", c.MinBlobSize)
		}
		if c.MaxBlobSize != "" {
			setOp("compression-max-blob-size", "compression_max_blob_size", c.MaxBlobSize)
		}
	}
	if m := pool.Spec.Ceph.Mirroring; m != nil && m.Mode != "" {
		// rbd mirror pool enable is idempotent on Ceph; additive-only, so a
		// removed mirroring block never disables the live pool.
		ops = append(ops, operationInPhase("storage", "enable-rbd-mirror-"+name, "rbd", "mirror", "pool", "enable", name, m.Mode))
	}
	return ops
}

func storageRuleCreatedByStretch(cluster v1alpha1.StorageCluster, rule string) bool {
	stretch := cluster.Spec.Ceph.Topology.Stretch
	return stretch != nil && stretch.RuleName == rule
}
