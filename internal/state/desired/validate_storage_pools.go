package desiredstate

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

func validateStoragePlacementPolicies(items []v1alpha1.StoragePlacementPolicy, clusters map[string]v1alpha1.StorageCluster) []string {
	var errs []string
	seen := map[string]bool{}
	for _, policy := range items {
		if e := validateName(v1alpha1.KindStoragePlacementPolicy, policy.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
		if seen[policy.Metadata.Name] {
			errs = append(errs, fmt.Sprintf("duplicate StoragePlacementPolicy %q", policy.Metadata.Name))
		}
		seen[policy.Metadata.Name] = true
		prefix := fmt.Sprintf("StoragePlacementPolicy/%s spec", policy.Metadata.Name)
		if policy.Spec.StorageClusterRef.Name == "" {
			errs = append(errs, prefix+".storageClusterRef is required")
		} else if cluster, ok := clusters[policy.Spec.StorageClusterRef.Name]; !ok {
			errs = append(errs, fmt.Sprintf("%s.storageClusterRef %q does not match any StorageCluster", prefix, policy.Spec.StorageClusterRef.Name))
		} else if storageClusterExternal(cluster) {
			errs = append(errs, fmt.Sprintf("%s.storageClusterRef %q references an external StorageCluster; Bootwright-managed placement policies are not declared for imported Ceph", prefix, policy.Spec.StorageClusterRef.Name))
		}
		if policy.Spec.Ceph.RuleName == "" {
			errs = append(errs, prefix+".ceph.ruleName is required")
		}
	}
	return errs
}

func validateStoragePools(items []v1alpha1.StoragePool, clusters map[string]v1alpha1.StorageCluster, policies map[string]v1alpha1.StoragePlacementPolicy) []string {
	var errs []string
	seen := map[string]bool{}
	for _, pool := range items {
		if e := validateName(v1alpha1.KindStoragePool, pool.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
		if seen[pool.Metadata.Name] {
			errs = append(errs, fmt.Sprintf("duplicate StoragePool %q", pool.Metadata.Name))
		}
		seen[pool.Metadata.Name] = true
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
			} else if pool.Spec.Ceph.ErasureCoded.DataChunks < 1 || pool.Spec.Ceph.ErasureCoded.CodingChunks < 1 {
				errs = append(errs, prefix+".ceph.erasure.dataChunks and codingChunks must be positive")
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
	}
	return errs
}

func validateStorageFilesystems(items []v1alpha1.StorageFilesystem, clusters map[string]v1alpha1.StorageCluster, pools map[string]v1alpha1.StoragePool) []string {
	var errs []string
	seen := map[string]bool{}
	for _, fs := range items {
		if e := validateName(v1alpha1.KindStorageFilesystem, fs.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
		if seen[fs.Metadata.Name] {
			errs = append(errs, fmt.Sprintf("duplicate StorageFilesystem %q", fs.Metadata.Name))
		}
		seen[fs.Metadata.Name] = true
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
	}
	return errs
}

func validateStorageObjectGateways(state v1alpha1.State, items []v1alpha1.StorageObjectGateway, clusters map[string]v1alpha1.StorageCluster) []string {
	var errs []string
	seen := map[string]bool{}
	for _, gw := range items {
		if e := validateName(v1alpha1.KindStorageObjectGateway, gw.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
		if seen[gw.Metadata.Name] {
			errs = append(errs, fmt.Sprintf("duplicate StorageObjectGateway %q", gw.Metadata.Name))
		}
		seen[gw.Metadata.Name] = true
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

func validateStorageGatewayPublicEndpoint(prefix string, gw v1alpha1.StorageObjectGateway) []string {
	if gw.Spec.Public.DNSName == "" {
		return []string{fmt.Sprintf("%s.dnsName is required for StorageObjectGateway/%s", prefix, gw.Metadata.Name)}
	}
	return nil
}

func validateStorageGatewayIngressEndpoint(prefix string, ingress v1alpha1.StorageObjectGatewayIngress, gw v1alpha1.StorageObjectGateway) []string {
	var errs []string
	if ingress.Address == "" {
		errs = append(errs, fmt.Sprintf("%s.address is required for StorageObjectGateway/%s ingress", prefix, gw.Metadata.Name))
	}
	if ingress.PrefixLength == 0 {
		errs = append(errs, fmt.Sprintf("%s.prefixLength is required for StorageObjectGateway/%s ingress", prefix, gw.Metadata.Name))
	}
	return errs
}
