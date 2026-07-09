package ceph

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

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
		if class := policy.Spec.Ceph.CrushDeviceClass; class != "" {
			ruleCmd = append(ruleCmd, class)
		}
		ops = append(ops, operationWithIdempotency("topology", "create-crush-rule-"+policy.Spec.Ceph.RuleName, "crush-rule", policy.Spec.Ceph.RuleName, ruleCmd...))
	}
	return ops
}

func cephPoolOperations(state v1alpha1.State, cluster v1alpha1.StorageCluster) []map[string]any {
	var ops []map[string]any
	for _, pool := range state.StoragePools {
		if pool.Spec.StorageClusterRef.Name != cluster.Metadata.Name {
			continue
		}
		if ec := pool.Spec.Ceph.ErasureCoded; pool.Spec.Ceph.Type == v1alpha1.StoragePoolTypeErasureCode && ec != nil {
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
		ops = append(ops, storagePoolTuningOperations(pool)...)
	}
	return ops
}

func storagePoolStructural(pool v1alpha1.StoragePool) map[string]any {
	poolType := pool.Spec.Ceph.Type
	if poolType == "" {
		poolType = v1alpha1.StoragePoolTypeReplicated
	}
	structural := map[string]any{"type": poolType}
	if ec := pool.Spec.Ceph.ErasureCoded; ec != nil {
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
		ops = append(ops, operationInPhase("storage", "enable-rbd-mirror-"+name, "rbd", "mirror", "pool", "enable", name, m.Mode))
	}
	return ops
}

func storageRuleCreatedByStretch(cluster v1alpha1.StorageCluster, rule string) bool {
	stretch := cluster.Spec.Ceph.Topology.Stretch
	return stretch != nil && stretch.RuleName == rule
}
