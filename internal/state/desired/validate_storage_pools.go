package desiredstate

import (
	"fmt"
	"regexp"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

func validateStoragePlacementPolicies(items []v1alpha1.StoragePlacementPolicy, clusters map[string]v1alpha1.StorageCluster) []string {
	var errs []string
	for _, policy := range items {
		if e := validateName(v1alpha1.KindStoragePlacementPolicy, policy.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
		prefix := fmt.Sprintf("StoragePlacementPolicy/%s spec", policy.Metadata.Name)
		if policy.Spec.StorageClusterRef.Name == "" {
			errs = append(errs, prefix+".storageClusterRef is required")
		} else if cluster, ok := clusters[policy.Spec.StorageClusterRef.Name]; !ok {
			errs = append(errs, fmt.Sprintf("%s.storageClusterRef %q does not match any StorageCluster", prefix, policy.Spec.StorageClusterRef.Name))
		} else if storageClusterExternal(cluster) {
			errs = append(errs, fmt.Sprintf("%s.storageClusterRef %q references an external StorageCluster; Bootwright-managed placement policies are not declared for imported Ceph", prefix, policy.Spec.StorageClusterRef.Name))
		} else if storageClusterStretchEnabled(cluster) {
			replicas := policy.Spec.Ceph.Replicated
			if replicas.Size != 0 && replicas.Size != topology.StretchReplicatedPoolSize {
				errs = append(errs, fmt.Sprintf("%s.ceph.replicated.size must be %d for stretch-mode StorageCluster/%s", prefix, topology.StretchReplicatedPoolSize, cluster.Metadata.Name))
			}
			if replicas.MinSize != 0 && replicas.MinSize != topology.StretchReplicatedPoolMinSize {
				errs = append(errs, fmt.Sprintf("%s.ceph.replicated.minSize must be %d for stretch-mode StorageCluster/%s", prefix, topology.StretchReplicatedPoolMinSize, cluster.Metadata.Name))
			}
		}
		if policy.Spec.Ceph.RuleName == "" {
			errs = append(errs, prefix+".ceph.ruleName is required")
		}
		if cluster, ok := clusters[policy.Spec.StorageClusterRef.Name]; ok && !storageClusterExternal(cluster) && !storageClusterStretchEnabled(cluster) {
			replicas := topology.DefaultPoolReplicas(cluster, policy.Spec.Ceph.Replicated)
			errs = append(errs, validateStorageReplicaValues(prefix+".ceph.replicated", policy.Spec.Ceph.Replicated, replicas)...)
			errs = append(errs, validateStorageReplicatedHostCapacity(prefix+".ceph.replicated", replicas, policy.Spec.Ceph.FailureDomain, cluster)...)
		}
	}
	return errs
}

func validateStoragePools(items []v1alpha1.StoragePool, clusters map[string]v1alpha1.StorageCluster, policies map[string]v1alpha1.StoragePlacementPolicy) []string {
	var errs []string
	for _, pool := range items {
		if e := validateName(v1alpha1.KindStoragePool, pool.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
		prefix := fmt.Sprintf("StoragePool/%s spec", pool.Metadata.Name)
		cluster, ok := clusters[pool.Spec.StorageClusterRef.Name]
		if pool.Spec.StorageClusterRef.Name == "" {
			errs = append(errs, prefix+".storageClusterRef is required")
		} else if !ok {
			errs = append(errs, fmt.Sprintf("%s.storageClusterRef %q does not match any StorageCluster", prefix, pool.Spec.StorageClusterRef.Name))
		} else if storageClusterExternal(cluster) {
			errs = append(errs, fmt.Sprintf("%s.storageClusterRef %q references an external StorageCluster; Bootwright-managed pools are not declared for imported Ceph", prefix, pool.Spec.StorageClusterRef.Name))
		}
		if pool.Spec.PlacementPolicyRef.Name != "" {
			policy, policyOK := policies[pool.Spec.PlacementPolicyRef.Name]
			if !policyOK {
				errs = append(errs, fmt.Sprintf("%s.placementPolicyRef %q does not match any StoragePlacementPolicy", prefix, pool.Spec.PlacementPolicyRef.Name))
			} else if policy.Spec.StorageClusterRef.Name != pool.Spec.StorageClusterRef.Name {
				errs = append(errs, fmt.Sprintf("%s.placementPolicyRef %q belongs to StorageCluster/%s, want StorageCluster/%s", prefix, pool.Spec.PlacementPolicyRef.Name, policy.Spec.StorageClusterRef.Name, pool.Spec.StorageClusterRef.Name))
			}
			if pool.Spec.Ceph.Replicated.Size != 0 || pool.Spec.Ceph.Replicated.MinSize != 0 {
				errs = append(errs, prefix+".ceph.replicated must not be set when placementPolicyRef is set; the StoragePlacementPolicy owns replication")
			}
		}
		poolType := pool.Spec.Ceph.Type
		if poolType == "" {
			poolType = v1alpha1.StoragePoolTypeReplicated
		}
		switch poolType {
		case v1alpha1.StoragePoolTypeReplicated:
			if pool.Spec.Ceph.ErasureCoded != nil {
				errs = append(errs, prefix+".ceph.type=replicated must not set erasure")
			}
		case v1alpha1.StoragePoolTypeErasureCode:
			if pool.Spec.Ceph.ErasureCoded == nil {
				errs = append(errs, prefix+".ceph.erasure is required when ceph.type=erasure")
			} else {
				if pool.Spec.Ceph.ErasureCoded.DataChunks < 1 || pool.Spec.Ceph.ErasureCoded.CodingChunks < 1 {
					errs = append(errs, prefix+".ceph.erasure.dataChunks and codingChunks must be positive")
				}
				errs = append(errs, validateStorageECProfile(prefix+".ceph.erasure", pool.Spec.Ceph.ErasureCoded)...)
			}
			if pool.Spec.Ceph.Replicated.Size != 0 || pool.Spec.Ceph.Replicated.MinSize != 0 {
				errs = append(errs, prefix+".ceph.type=erasure must not set replicated")
			}
			if ok && storageClusterStretchEnabled(cluster) {
				errs = append(errs, fmt.Sprintf("%s.ceph.type %q is not supported for stretch-mode StorageCluster/%s", prefix, poolType, cluster.Metadata.Name))
			}
		default:
			errs = append(errs, fmt.Sprintf("%s.ceph.type %q must be one of {%s, %s}", prefix, pool.Spec.Ceph.Type, v1alpha1.StoragePoolTypeReplicated, v1alpha1.StoragePoolTypeErasureCode))
		}
		switch pool.Spec.Ceph.Role {
		case "", v1alpha1.StoragePoolRoleRBD, v1alpha1.StoragePoolRoleCephFSMetadata, v1alpha1.StoragePoolRoleCephFSData, v1alpha1.StoragePoolRoleRGW:
		default:
			errs = append(errs, fmt.Sprintf("%s.ceph.role %q must be one of {%s, %s, %s, %s}", prefix, pool.Spec.Ceph.Role,
				v1alpha1.StoragePoolRoleRBD, v1alpha1.StoragePoolRoleCephFSMetadata, v1alpha1.StoragePoolRoleCephFSData, v1alpha1.StoragePoolRoleRGW))
		}
		if ok && storageClusterStretchEnabled(cluster) {
			replicas := pool.Spec.Ceph.Replicated
			if replicas.Size != 0 && replicas.Size != topology.StretchReplicatedPoolSize {
				errs = append(errs, fmt.Sprintf("%s.ceph.replicated.size must be %d for stretch-mode StorageCluster/%s", prefix, topology.StretchReplicatedPoolSize, cluster.Metadata.Name))
			}
			if replicas.MinSize != 0 && replicas.MinSize != topology.StretchReplicatedPoolMinSize {
				errs = append(errs, fmt.Sprintf("%s.ceph.replicated.minSize must be %d for stretch-mode StorageCluster/%s", prefix, topology.StretchReplicatedPoolMinSize, cluster.Metadata.Name))
			}
		}
		if ok && !storageClusterExternal(cluster) && cluster.Spec.Ceph != nil && !storageClusterStretchEnabled(cluster) {
			failureDomain := "host"
			if cluster.Spec.Ceph.Cephadm.Bootstrap.SingleHostDefaults {
				failureDomain = "osd"
			}
			replicas := pool.Spec.Ceph.Replicated
			if policy, policyOK := policies[pool.Spec.PlacementPolicyRef.Name]; pool.Spec.PlacementPolicyRef.Name != "" && policyOK {
				failureDomain = policy.Spec.Ceph.FailureDomain
				replicas = policy.Spec.Ceph.Replicated
			}
			if poolType == v1alpha1.StoragePoolTypeReplicated && pool.Spec.PlacementPolicyRef.Name == "" {
				replicas = topology.DefaultPoolReplicas(cluster, replicas)
				errs = append(errs, validateStorageReplicaValues(prefix+".ceph.replicated", pool.Spec.Ceph.Replicated, replicas)...)
				errs = append(errs, validateStorageReplicatedHostCapacity(prefix+".ceph.replicated", replicas, failureDomain, cluster)...)
			}
			if poolType == v1alpha1.StoragePoolTypeErasureCode && pool.Spec.Ceph.ErasureCoded != nil && (failureDomain == "" || failureDomain == "host") {
				chunks := pool.Spec.Ceph.ErasureCoded.DataChunks + pool.Spec.Ceph.ErasureCoded.CodingChunks
				if hosts := storageCephOSDHostCount(cluster); chunks > hosts {
					errs = append(errs, fmt.Sprintf("%s.ceph.erasure requires %d chunks across host failure domains, but StorageCluster/%s has only %d OSD host(s)", prefix, chunks, cluster.Metadata.Name, hosts))
				}
			}
		}
		errs = append(errs, validateStoragePoolTuning(prefix+".ceph", pool.Spec.Ceph)...)
	}
	return errs
}

func validateStorageReplicaValues(prefix string, authored, effective v1alpha1.StorageCephPoolReplicas) []string {
	var errs []string
	if authored.Size < 0 {
		errs = append(errs, prefix+".size must be non-negative")
	}
	if authored.MinSize < 0 {
		errs = append(errs, prefix+".minSize must be non-negative")
	}
	if effective.Size > 0 && effective.MinSize > effective.Size {
		errs = append(errs, fmt.Sprintf("%s.minSize %d must not exceed effective size %d", prefix, effective.MinSize, effective.Size))
	}
	return errs
}

func validateStorageReplicatedHostCapacity(prefix string, replicas v1alpha1.StorageCephPoolReplicas, failureDomain string, cluster v1alpha1.StorageCluster) []string {
	if (failureDomain != "" && failureDomain != "host") || replicas.Size <= 0 || cluster.Spec.Ceph == nil {
		return nil
	}
	hosts := storageCephOSDHostCount(cluster)
	if replicas.Size <= hosts {
		return nil
	}
	return []string{fmt.Sprintf("%s.size %d cannot be placed across host failure domains; StorageCluster/%s has only %d OSD host(s)", prefix, replicas.Size, cluster.Metadata.Name, hosts)}
}

func storageCephOSDHostCount(cluster v1alpha1.StorageCluster) int {
	count := 0
	for _, host := range cluster.Spec.Ceph.Topology.Hosts {
		if topology.NodeHasRole(host, v1alpha1.StorageCephRoleOSD) {
			count++
		}
	}
	return count
}

func validateStorageECProfile(prefix string, ec *v1alpha1.StoragePoolErasureCode) []string {
	var errs []string
	switch ec.Plugin {
	case "", "jerasure", "isa", "clay", "lrc", "shec":
	default:
		errs = append(errs, fmt.Sprintf("%s.plugin %q must be one of {jerasure, isa, clay, lrc, shec}", prefix, ec.Plugin))
	}
	owned := map[string]bool{
		"k": true, "m": true, "plugin": true, "technique": true,
		"crush-device-class": true, "crush-root": true, "stripe_unit": true,
		"crush-failure-domain": true,
	}
	for key, value := range ec.Parameters {
		owner := fmt.Sprintf("%s.parameters[%s]", prefix, key)
		if key == "" {
			errs = append(errs, prefix+".parameters has an empty key")
			continue
		}
		if owned[key] {
			errs = append(errs, fmt.Sprintf("%s is owned by a first-class erasure field; declare it there, not in parameters", owner))
		}
		if value == "" {
			errs = append(errs, owner+" must not be empty")
		}
	}
	return errs
}

func validateStoragePoolTuning(prefix string, spec v1alpha1.StoragePoolCephSpec) []string {
	var errs []string
	if a := spec.Autoscale; a != nil {
		switch a.Mode {
		case "", v1alpha1.StoragePoolAutoscaleModeOn, v1alpha1.StoragePoolAutoscaleModeOff, v1alpha1.StoragePoolAutoscaleModeWarn:
		default:
			errs = append(errs, fmt.Sprintf("%s.autoscale.mode %q must be one of {%s, %s, %s}", prefix, a.Mode,
				v1alpha1.StoragePoolAutoscaleModeOn, v1alpha1.StoragePoolAutoscaleModeOff, v1alpha1.StoragePoolAutoscaleModeWarn))
		}
		if a.TargetSizeRatio < 0 {
			errs = append(errs, prefix+".autoscale.targetSizeRatio must be non-negative")
		}
		if a.TargetSizeRatio > 0 && a.TargetSizeBytes != "" {
			errs = append(errs, prefix+".autoscale must set at most one of targetSizeRatio or targetSizeBytes")
		}
		if a.PGNumMin < 0 || a.PGNumMax < 0 {
			errs = append(errs, prefix+".autoscale pgNumMin/pgNumMax must be non-negative")
		}
		if a.PGNumMin > 0 && a.PGNumMax > 0 && a.PGNumMin > a.PGNumMax {
			errs = append(errs, prefix+".autoscale.pgNumMin must not exceed pgNumMax")
		}
	}
	if q := spec.Quota; q != nil {
		if q.MaxBytes != nil && *q.MaxBytes < 0 {
			errs = append(errs, prefix+".quota.maxBytes must be non-negative (0 means no limit)")
		}
		if q.MaxObjects != nil && *q.MaxObjects < 0 {
			errs = append(errs, prefix+".quota.maxObjects must be non-negative (0 means no limit)")
		}
	}
	if c := spec.Compression; c != nil {
		switch c.Mode {
		case "", v1alpha1.StoragePoolCompressionModeNone, v1alpha1.StoragePoolCompressionModePassive, v1alpha1.StoragePoolCompressionModeAggressive, v1alpha1.StoragePoolCompressionModeForce:
		default:
			errs = append(errs, fmt.Sprintf("%s.compression.mode %q must be one of {none, passive, aggressive, force}", prefix, c.Mode))
		}
		switch c.Algorithm {
		case "", "lz4", "snappy", "zlib", "zstd":
		default:
			errs = append(errs, fmt.Sprintf("%s.compression.algorithm %q must be one of {lz4, snappy, zlib, zstd}", prefix, c.Algorithm))
		}
		if c.RequiredRatio < 0 || c.RequiredRatio > 1 {
			errs = append(errs, fmt.Sprintf("%s.compression.requiredRatio %v must be in (0, 1]", prefix, c.RequiredRatio))
		}
		if c.Mode == "" && (c.Algorithm != "" || c.RequiredRatio != 0 || c.MinBlobSize != "" || c.MaxBlobSize != "") {
			errs = append(errs, prefix+".compression sets tuning without compression.mode")
		}
	}
	if m := spec.Mirroring; m != nil {
		switch m.Mode {
		case "", v1alpha1.StoragePoolMirroringModeImage, v1alpha1.StoragePoolMirroringModePool:
		default:
			errs = append(errs, fmt.Sprintf("%s.mirroring.mode %q must be one of {image, pool}", prefix, m.Mode))
		}
	}
	return errs
}

func validateStorageFilesystems(items []v1alpha1.StorageFilesystem, clusters map[string]v1alpha1.StorageCluster, pools map[string]v1alpha1.StoragePool) []string {
	var errs []string
	for _, fs := range items {
		if e := validateName(v1alpha1.KindStorageFilesystem, fs.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
		prefix := fmt.Sprintf("StorageFilesystem/%s spec", fs.Metadata.Name)
		cluster, ok := clusters[fs.Spec.StorageClusterRef.Name]
		if fs.Spec.StorageClusterRef.Name == "" {
			errs = append(errs, prefix+".storageClusterRef is required")
		} else if !ok {
			errs = append(errs, fmt.Sprintf("%s.storageClusterRef %q does not match any StorageCluster", prefix, fs.Spec.StorageClusterRef.Name))
		} else if storageClusterExternal(cluster) {
			errs = append(errs, fmt.Sprintf("%s.storageClusterRef %q references an external StorageCluster; Bootwright-managed filesystems are not declared for imported Ceph", prefix, fs.Spec.StorageClusterRef.Name))
		}
		metadataPool, metadataOK := pools[fs.Spec.CephFS.MetadataPoolRef.Name]
		if fs.Spec.CephFS.MetadataPoolRef.Name == "" {
			errs = append(errs, prefix+".cephfs.metadataPoolRef is required")
		} else if !metadataOK {
			errs = append(errs, fmt.Sprintf("%s.cephfs.metadataPoolRef %q does not match any StoragePool", prefix, fs.Spec.CephFS.MetadataPoolRef.Name))
		} else if metadataPool.Spec.StorageClusterRef.Name != fs.Spec.StorageClusterRef.Name {
			errs = append(errs, fmt.Sprintf("%s.cephfs.metadataPoolRef %q belongs to StorageCluster/%s, want StorageCluster/%s", prefix, metadataPool.Metadata.Name, metadataPool.Spec.StorageClusterRef.Name, fs.Spec.StorageClusterRef.Name))
		}
		defaults := 0
		for i, ref := range fs.Spec.CephFS.DataPoolRefs {
			owner := fmt.Sprintf("%s.cephfs.dataPoolRefs[%d]", prefix, i)
			pool, poolOK := pools[ref.Name]
			if ref.Name == "" {
				errs = append(errs, owner+".name is required")
			} else if !poolOK {
				errs = append(errs, fmt.Sprintf("%s.name %q does not match any StoragePool", owner, ref.Name))
			} else if pool.Spec.StorageClusterRef.Name != fs.Spec.StorageClusterRef.Name {
				errs = append(errs, fmt.Sprintf("%s.name %q belongs to StorageCluster/%s, want StorageCluster/%s", owner, ref.Name, pool.Spec.StorageClusterRef.Name, fs.Spec.StorageClusterRef.Name))
			}
			if ref.Name != "" && ref.Name == fs.Spec.CephFS.MetadataPoolRef.Name {
				errs = append(errs, fmt.Sprintf("%s.name %q must be distinct from metadataPoolRef", owner, ref.Name))
			}
			if ref.Default {
				defaults++
			}
		}
		if len(fs.Spec.CephFS.DataPoolRefs) == 0 {
			errs = append(errs, prefix+".cephfs.dataPoolRefs is required")
		}
		if defaults != 1 {
			errs = append(errs, fmt.Sprintf("%s.cephfs.dataPoolRefs must mark exactly one default data pool", prefix))
		}
		errs = append(errs, validateStoragePlacementHosts(prefix+".cephfs.mds.placement", fs.Spec.CephFS.MDS.Placement, cluster, ok, v1alpha1.StorageCephRoleMDS)...)
		if ok && storageClusterStretchEnabled(cluster) {
			errs = append(errs, validatePlacementCoversDataSites(prefix+".cephfs.mds.placement", topology.ResolvePlacement(cluster, fs.Spec.CephFS.MDS.Placement, v1alpha1.StorageCephRoleMDS), cluster, v1alpha1.StorageCephRoleMDS)...)
		}
		if fs.Spec.CephFS.MDS.StandbyCountWanted < 0 {
			errs = append(errs, prefix+".cephfs.mds.standbyCountWanted must be non-negative")
		}
		if ok {
			errs = append(errs, validateStorageMDSStandbyFeasible(prefix+".cephfs.mds", fs.Spec.CephFS.MDS, topology.ResolvePlacement(cluster, fs.Spec.CephFS.MDS.Placement, v1alpha1.StorageCephRoleMDS))...)
		}
		if ss := fs.Spec.CephFS.MDS.ServiceSpec; ss != nil {
			for i, cidr := range ss.Networks {
				errs = append(errs, validateCIDR(fmt.Sprintf("%s.cephfs.mds.serviceSpec.networks[%d]", prefix, i), cidr)...)
			}
		}
		errs = append(errs, validateStorageSubvolumeGroups(prefix+".cephfs.subvolumeGroups", fs, pools)...)
	}
	return errs
}

func validateStorageMDSStandbyFeasible(prefix string, mds v1alpha1.StorageCephFSMetadataServices, hosts []string) []string {
	available := len(hosts)
	if available == 0 {
		return nil
	}
	active := mds.ActiveCount
	if active == 0 {
		active = 1
	}
	needed := active + mds.StandbyCountWanted
	if mds.StandbyReplay {
		needed += active
	}
	if needed <= available {
		return nil
	}
	return []string{fmt.Sprintf("%s placement resolves to %d MDS daemon host(s), fewer than the %d the active+standby intent needs (activeCount %d, standbyCountWanted %d, standbyReplay %t); it would converge to a permanent MDS_INSUFFICIENT_STANDBY, so add hosts to the placement or reduce the standby intent",
		prefix, available, needed, active, mds.StandbyCountWanted, mds.StandbyReplay)}
}

var cephOctalModePattern = regexp.MustCompile(`^0?[0-7]{3,4}$`)

func validateStorageSubvolumeGroups(prefix string, fs v1alpha1.StorageFilesystem, pools map[string]v1alpha1.StoragePool) []string {
	var errs []string
	seen := map[string]bool{}
	for i, group := range fs.Spec.CephFS.SubvolumeGroups {
		owner := fmt.Sprintf("%s[%d]", prefix, i)
		if group.Name == "" {
			errs = append(errs, owner+".name is required")
		} else if seen[group.Name] {
			errs = append(errs, fmt.Sprintf("%s.name %q is duplicated", owner, group.Name))
		}
		seen[group.Name] = true
		if group.Mode != "" && !cephOctalModePattern.MatchString(group.Mode) {
			errs = append(errs, fmt.Sprintf("%s.mode %q must be an octal mode such as 0755", owner, group.Mode))
		}
		if group.SizeBytes < 0 {
			errs = append(errs, owner+".sizeBytes must be non-negative")
		}
		if group.UID != nil && *group.UID < 0 {
			errs = append(errs, owner+".uid must be non-negative")
		}
		if group.GID != nil && *group.GID < 0 {
			errs = append(errs, owner+".gid must be non-negative")
		}
		if ref := group.PoolLayoutRef.Name; ref != "" {
			pool, poolOK := pools[ref]
			if !poolOK {
				errs = append(errs, fmt.Sprintf("%s.poolLayoutRef %q does not match any StoragePool", owner, ref))
			} else if pool.Spec.StorageClusterRef.Name != fs.Spec.StorageClusterRef.Name {
				errs = append(errs, fmt.Sprintf("%s.poolLayoutRef %q belongs to StorageCluster/%s, want StorageCluster/%s", owner, ref, pool.Spec.StorageClusterRef.Name, fs.Spec.StorageClusterRef.Name))
			}
		}
	}
	return errs
}

func validateStorageObjectGateways(items []v1alpha1.StorageObjectGateway, clusters map[string]v1alpha1.StorageCluster) []string {
	var errs []string
	for _, gw := range items {
		if e := validateName(v1alpha1.KindStorageObjectGateway, gw.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
		prefix := fmt.Sprintf("StorageObjectGateway/%s spec", gw.Metadata.Name)
		cluster, ok := clusters[gw.Spec.StorageClusterRef.Name]
		if gw.Spec.StorageClusterRef.Name == "" {
			errs = append(errs, prefix+".storageClusterRef is required")
		} else if !ok {
			errs = append(errs, fmt.Sprintf("%s.storageClusterRef %q does not match any StorageCluster", prefix, gw.Spec.StorageClusterRef.Name))
		} else if storageClusterExternal(cluster) {
			errs = append(errs, fmt.Sprintf("%s.storageClusterRef %q references an external StorageCluster; Bootwright-managed object gateways are not declared for imported Ceph", prefix, gw.Spec.StorageClusterRef.Name))
		}
		if gw.Spec.Ceph.ServiceID == "" {
			errs = append(errs, prefix+".ceph.serviceID is required")
		}
		if gw.Spec.Ceph.FrontendPort < 0 || gw.Spec.Ceph.FrontendPort > 65535 {
			errs = append(errs, fmt.Sprintf("%s.ceph.frontendPort %d out of range", prefix, gw.Spec.Ceph.FrontendPort))
		}
		errs = append(errs, validateStorageGatewayRealm(prefix+".ceph", gw)...)
		errs = append(errs, validateStorageGatewayConfig(prefix+".ceph.config", gw, cluster, ok)...)
		errs = append(errs, validateStorageGatewayPublicEndpoint(prefix+".public", gw)...)
		errs = append(errs, validateStoragePlacementHosts(prefix+".ceph.placement", gw.Spec.Ceph.Placement, cluster, ok, v1alpha1.StorageCephRoleRGW)...)
		if ok && storageClusterStretchEnabled(cluster) {
			errs = append(errs, validatePlacementCoversDataSites(prefix+".ceph.placement", topology.ResolvePlacement(cluster, gw.Spec.Ceph.Placement, v1alpha1.StorageCephRoleRGW), cluster, v1alpha1.StorageCephRoleRGW)...)
		}
		ingressNames := map[string]bool{}
		var ingressHosts []string
		for i, ingress := range gw.Spec.Ceph.Ingresses {
			owner := fmt.Sprintf("%s.ceph.ingresses[%d]", prefix, i)
			if ingress.Name == "" {
				errs = append(errs, owner+".name is required")
			} else if ingressNames[ingress.Name] {
				errs = append(errs, fmt.Sprintf("%s.name %q is duplicated", owner, ingress.Name))
			}
			ingressNames[ingress.Name] = true
			errs = append(errs, validateStorageGatewayIngressEndpoint(owner, ingress, gw)...)
			errs = append(errs, validateStoragePlacementHosts(owner+".placement", ingress.Placement, cluster, ok, v1alpha1.StorageCephRoleIngress)...)
			ingressHosts = append(ingressHosts, topology.ResolvePlacement(cluster, ingress.Placement, v1alpha1.StorageCephRoleIngress)...)
		}
		if ok && storageClusterStretchEnabled(cluster) && len(gw.Spec.Ceph.Ingresses) > 0 {
			errs = append(errs, validatePlacementCoversDataSites(prefix+".ceph.ingresses", ingressHosts, cluster, v1alpha1.StorageCephRoleIngress)...)
		}
	}
	return errs
}

func validateStorageGatewayRealm(prefix string, gw v1alpha1.StorageObjectGateway) []string {
	realm, zg, zone := gw.Spec.Ceph.Realm, gw.Spec.Ceph.ZoneGroup, gw.Spec.Ceph.Zone
	if realm == "" && zg == "" && zone == "" {
		return nil
	}
	if realm == "" || zg == "" || zone == "" {
		return []string{prefix + " realm, zoneGroup, and zone must be set together (all-or-nothing)"}
	}
	return nil
}

func validateStorageGatewayConfig(prefix string, gw v1alpha1.StorageObjectGateway, cluster v1alpha1.StorageCluster, clusterOK bool) []string {
	if len(gw.Spec.Ceph.Config) == 0 {
		return nil
	}
	var errs []string
	var clusterSection map[string]string
	if clusterOK && cluster.Spec.Ceph != nil {
		clusterSection = cluster.Spec.Ceph.Config["client.rgw."+gw.Spec.Ceph.ServiceID]
	}
	for _, key := range sortedKeys(gw.Spec.Ceph.Config) {
		owner := fmt.Sprintf("%s.%s", prefix, key)
		if key == "rgw_frontend_port" {
			errs = append(errs, owner+" is owned by spec.ceph.frontendPort; declare it there")
		}
		if gw.Spec.Ceph.Config[key] == "" {
			errs = append(errs, owner+" must not be empty")
		}
		if _, dup := clusterSection[key]; dup {
			errs = append(errs, fmt.Sprintf("%s is also set in StorageCluster spec.ceph.config[client.rgw.%s]; declare it in one place", owner, gw.Spec.Ceph.ServiceID))
		}
	}
	return errs
}

func validateStorageGatewayPublicEndpoint(prefix string, gw v1alpha1.StorageObjectGateway) []string {
	if gw.Spec.Public.DNSName == "" {
		return []string{fmt.Sprintf("%s.dnsName is required for StorageObjectGateway/%s", prefix, gw.Metadata.Name)}
	}
	return nil
}

func validateStorageGatewayIngressEndpoint(prefix string, ingress v1alpha1.StorageObjectGatewayIngress, gw v1alpha1.StorageObjectGateway) []string {
	_ = gw
	return validateStorageIngressVIP(prefix, ingress.Address, ingress.PrefixLength)
}
