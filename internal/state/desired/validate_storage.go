package desiredstate

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	addoninputs "github.com/crmarques/bootwright/internal/addons/inputs"
)

func validateStorage(state v1alpha1.State) []string {
	var errs []string
	clusters := indexStorageClusters(state.StorageClusters)
	policies := indexStoragePlacementPolicies(state.StoragePlacementPolicies)
	pools := indexStoragePools(state.StoragePools)
	filesystems := indexStorageFilesystems(state.StorageFilesystems)
	gateways := indexStorageObjectGateways(state.StorageObjectGateways)
	exports := indexStorageExports(state.StorageExports)
	infras := indexClusterInfras(state.ClusterInfras)
	hosts := indexHosts(state.Hosts)

	errs = append(errs, validateStorageClusters(state.StorageClusters, infras)...)
	errs = append(errs, validateStoragePlacementPolicies(state.StoragePlacementPolicies, clusters)...)
	errs = append(errs, validateStoragePools(state.StoragePools, clusters, policies)...)
	errs = append(errs, validateStorageFilesystems(state.StorageFilesystems, clusters, pools)...)
	errs = append(errs, validateStorageObjectGateways(state.StorageObjectGateways, clusters, infras)...)
	errs = append(errs, validateStorageExports(state, clusters, pools, filesystems, gateways, hosts)...)
	errs = append(errs, validateStorageExportAttachmentEffects(state, exports)...)
	return errs
}

func validateStorageClusters(items []v1alpha1.StorageCluster, infras map[string]v1alpha1.ClusterInfra) []string {
	var errs []string
	seen := map[string]bool{}
	for _, cluster := range items {
		if e := validateName(v1alpha1.KindStorageCluster, cluster.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
		if seen[cluster.Metadata.Name] {
			errs = append(errs, fmt.Sprintf("duplicate StorageCluster %q", cluster.Metadata.Name))
		}
		seen[cluster.Metadata.Name] = true
		prefix := fmt.Sprintf("StorageCluster/%s spec", cluster.Metadata.Name)
		switch cluster.Spec.Type {
		case v1alpha1.StorageClusterTypeCeph:
		case "":
			errs = append(errs, prefix+".type is required")
			continue
		default:
			errs = append(errs, fmt.Sprintf("%s.type %q must be %q", prefix, cluster.Spec.Type, v1alpha1.StorageClusterTypeCeph))
			continue
		}
		switch storageClusterManagement(cluster) {
		case v1alpha1.StorageClusterManagementManaged, v1alpha1.StorageClusterManagementFullManaged:
			if cluster.Spec.Ceph == nil {
				errs = append(errs, prefix+".ceph is required when spec.type=ceph")
				continue
			}
		case v1alpha1.StorageClusterManagementExternal:
			if cluster.Spec.ClusterInfraRef.Name != "" {
				errs = append(errs, prefix+".clusterInfraRef.name must be empty when spec.management=external")
			}
			if cluster.Spec.Ceph != nil {
				errs = append(errs, prefix+".ceph must be empty when spec.management=external")
			}
			continue
		default:
			errs = append(errs, fmt.Sprintf("%s.management %q must be one of {%s, %s, %s}", prefix, cluster.Spec.Management, v1alpha1.StorageClusterManagementManaged, v1alpha1.StorageClusterManagementExternal, v1alpha1.StorageClusterManagementFullManaged))
			continue
		}
		infra, ok := infras[cluster.Spec.ClusterInfraRef.Name]
		if cluster.Spec.ClusterInfraRef.Name == "" {
			errs = append(errs, prefix+".clusterInfraRef.name is required")
		} else if !ok {
			errs = append(errs, fmt.Sprintf("%s.clusterInfraRef.name %q does not match any ClusterInfra", prefix, cluster.Spec.ClusterInfraRef.Name))
		}
		if cluster.Spec.Ceph != nil {
			errs = append(errs, validateStorageClusterCeph(cluster, infra, ok)...)
		}
	}
	return errs
}

func validateStorageClusterCeph(cluster v1alpha1.StorageCluster, infra v1alpha1.ClusterInfra, infraOK bool) []string {
	var errs []string
	ceph := cluster.Spec.Ceph
	prefix := fmt.Sprintf("StorageCluster/%s spec.ceph", cluster.Metadata.Name)
	errs = append(errs, validateStorageCephadm(prefix+".cephadm", cluster, infra, infraOK)...)
	for i, cidr := range ceph.Networks.PublicCIDRs {
		errs = append(errs, validateCIDR(fmt.Sprintf("%s.networks.publicCIDRs[%d]", prefix, i), cidr)...)
	}
	for i, cidr := range ceph.Networks.ClusterCIDRs {
		errs = append(errs, validateCIDR(fmt.Sprintf("%s.networks.clusterCIDRs[%d]", prefix, i), cidr)...)
	}
	errs = append(errs, validateStorageCephNodes(prefix+".topology.nodes", ceph.Topology.Nodes, infra, infraOK)...)
	if ceph.Topology.Stretch != nil && ceph.Topology.Stretch.Enabled {
		errs = append(errs, validateStorageCephStretch(cluster)...)
	}
	return errs
}

func validateStorageCephadm(prefix string, cluster v1alpha1.StorageCluster, infra v1alpha1.ClusterInfra, infraOK bool) []string {
	var errs []string
	adm := cluster.Spec.Ceph.Cephadm
	if adm.Bootstrap.SeedNode == "" {
		errs = append(errs, prefix+".bootstrap.seedNode is required")
	} else if !storageCephNodeExists(cluster, adm.Bootstrap.SeedNode) {
		errs = append(errs, fmt.Sprintf("%s.bootstrap.seedNode %q is not listed in spec.ceph.topology.nodes", prefix, adm.Bootstrap.SeedNode))
	}
	mon := adm.Bootstrap.MonIP
	if mon.MachineRef.ClusterInfra == "" {
		errs = append(errs, prefix+".bootstrap.monIP.machineRef.clusterInfra is required")
	} else if mon.MachineRef.ClusterInfra != cluster.Spec.ClusterInfraRef.Name {
		errs = append(errs, fmt.Sprintf("%s.bootstrap.monIP.machineRef.clusterInfra %q must match spec.clusterInfraRef.name %q", prefix, mon.MachineRef.ClusterInfra, cluster.Spec.ClusterInfraRef.Name))
	}
	if mon.MachineRef.Name == "" {
		errs = append(errs, prefix+".bootstrap.monIP.machineRef.name is required")
	} else if infraOK && !clusterInfraMachineExists(infra, mon.MachineRef.Name) {
		errs = append(errs, fmt.Sprintf("%s.bootstrap.monIP.machineRef.name %q is not defined on ClusterInfra/%s", prefix, mon.MachineRef.Name, infra.Metadata.Name))
	}
	if mon.Interface == "" {
		errs = append(errs, prefix+".bootstrap.monIP.interface is required")
	}
	switch mon.Family {
	case "", "ipv4", "ipv6":
	default:
		errs = append(errs, fmt.Sprintf("%s.bootstrap.monIP.family %q must be ipv4 or ipv6", prefix, mon.Family))
	}
	errs = append(errs, validateStorageSSH(prefix+".nodeSSH", adm.NodeSSH, true)...)
	errs = append(errs, validateStorageSSH(prefix+".clusterSSH", adm.ClusterSSH, false)...)
	errs = append(errs, validateStorageCephadmRegistry(prefix+".registry", adm.Registry)...)
	return errs
}

func validateStorageCephadmRegistry(prefix string, registry v1alpha1.StorageCephadmRegistry) []string {
	var errs []string
	if registry.URL == "" {
		errs = append(errs, prefix+".url is required")
	} else if strings.ContainsAny(registry.URL, " \t\r\n") {
		errs = append(errs, prefix+".url must not contain whitespace")
	} else if strings.Contains(registry.URL, "@") {
		errs = append(errs, prefix+".url must not embed credentials; use credentialsRef")
	}
	if registry.CredentialsRef.Name == "" {
		errs = append(errs, prefix+".credentialsRef.name is required")
	}
	return errs
}

func validateStorageSSH(prefix string, ssh v1alpha1.StorageSSHSpec, required bool) []string {
	if ssh.KeyPairRef.Name == "" && ssh.PrivateKeyRef.Name == "" {
		if required {
			return []string{prefix + " must set keyPairRef.name or privateKeyRef.name"}
		}
		return nil
	}
	if ssh.KeyPairRef.Name != "" && ssh.PrivateKeyRef.Name != "" {
		return []string{prefix + " must set only one of {keyPairRef, privateKeyRef}"}
	}
	return nil
}

func validateStorageCephNodes(prefix string, nodes []v1alpha1.StorageCephNode, infra v1alpha1.ClusterInfra, infraOK bool) []string {
	var errs []string
	if len(nodes) == 0 {
		return []string{prefix + " is required"}
	}
	seen := map[string]bool{}
	for i, node := range nodes {
		owner := fmt.Sprintf("%s[%d]", prefix, i)
		if node.Name == "" {
			errs = append(errs, owner+".name is required")
		} else {
			if seen[node.Name] {
				errs = append(errs, fmt.Sprintf("%s.name %q is duplicated", owner, node.Name))
			}
			seen[node.Name] = true
			if infraOK && !clusterInfraMachineExists(infra, node.Name) {
				errs = append(errs, fmt.Sprintf("%s.name %q is not defined on ClusterInfra/%s spec.components.machines", owner, node.Name, infra.Metadata.Name))
			}
		}
		if node.Site == "" {
			errs = append(errs, owner+".site is required")
		}
		if len(node.Roles) == 0 {
			errs = append(errs, owner+".roles is required")
		}
		roleSeen := map[string]bool{}
		for j, role := range node.Roles {
			roleOwner := fmt.Sprintf("%s.roles[%d]", owner, j)
			if !validStorageCephRole(role) {
				errs = append(errs, fmt.Sprintf("%s %q must be one of {%s}", roleOwner, role, strings.Join(storageCephRoles(), ", ")))
				continue
			}
			if roleSeen[role] {
				errs = append(errs, fmt.Sprintf("%s %q is duplicated", roleOwner, role))
			}
			roleSeen[role] = true
		}
	}
	return errs
}

func validateStorageCephStretch(cluster v1alpha1.StorageCluster) []string {
	stretch := cluster.Spec.Ceph.Topology.Stretch
	nodes := cluster.Spec.Ceph.Topology.Nodes
	prefix := fmt.Sprintf("StorageCluster/%s spec.ceph.topology.stretch", cluster.Metadata.Name)
	var errs []string
	if stretch.FailureDomain == "" {
		errs = append(errs, prefix+".failureDomain is required")
	}
	if len(stretch.DataSites) != 2 {
		errs = append(errs, fmt.Sprintf("%s.dataSites must contain exactly two sites", prefix))
	}
	dataSites := map[string]bool{}
	for i, site := range stretch.DataSites {
		owner := fmt.Sprintf("%s.dataSites[%d]", prefix, i)
		if site == "" {
			errs = append(errs, owner+" must not be empty")
			continue
		}
		if dataSites[site] {
			errs = append(errs, fmt.Sprintf("%s %q is duplicated", owner, site))
		}
		dataSites[site] = true
	}
	if stretch.Tiebreaker.Site == "" {
		errs = append(errs, prefix+".tiebreaker.site is required")
	} else if dataSites[stretch.Tiebreaker.Site] {
		errs = append(errs, fmt.Sprintf("%s.tiebreaker.site %q must be distinct from dataSites", prefix, stretch.Tiebreaker.Site))
	}
	if stretch.Tiebreaker.Node == "" {
		errs = append(errs, prefix+".tiebreaker.node is required")
	}
	if stretch.RuleName == "" {
		errs = append(errs, prefix+".ruleName is required")
	}
	if stretch.ReplicatedPoolDefaults.Size != 4 || stretch.ReplicatedPoolDefaults.MinSize != 2 {
		errs = append(errs, fmt.Sprintf("%s.replicatedPoolDefaults must set size: 4 and minSize: 2 for stretch mode", prefix))
	}
	monBySite := map[string]int{}
	for _, node := range nodes {
		if storageCephNodeHasRole(node, v1alpha1.StorageCephRoleMON) {
			monBySite[node.Site]++
		}
		if node.Name == stretch.Tiebreaker.Node {
			if node.Site != stretch.Tiebreaker.Site {
				errs = append(errs, fmt.Sprintf("%s.tiebreaker.node %q is in site %q, want %q", prefix, node.Name, node.Site, stretch.Tiebreaker.Site))
			}
			if !storageCephNodeRolesOnly(node, v1alpha1.StorageCephRoleMON) {
				errs = append(errs, fmt.Sprintf("%s.tiebreaker.node %q must be mon-only", prefix, node.Name))
			}
			if len(node.Devices) > 0 {
				errs = append(errs, fmt.Sprintf("%s.tiebreaker.node %q must not declare OSD devices", prefix, node.Name))
			}
		}
	}
	for _, site := range stretch.DataSites {
		if monBySite[site] != 2 {
			errs = append(errs, fmt.Sprintf("%s requires exactly two mon nodes in data site %q (got %d)", prefix, site, monBySite[site]))
		}
	}
	if monBySite[stretch.Tiebreaker.Site] != 1 {
		errs = append(errs, fmt.Sprintf("%s requires exactly one tiebreaker mon in site %q (got %d)", prefix, stretch.Tiebreaker.Site, monBySite[stretch.Tiebreaker.Site]))
	}
	return errs
}

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
			errs = append(errs, prefix+".storageClusterRef.name is required")
		} else if cluster, ok := clusters[policy.Spec.StorageClusterRef.Name]; !ok {
			errs = append(errs, fmt.Sprintf("%s.storageClusterRef.name %q does not match any StorageCluster", prefix, policy.Spec.StorageClusterRef.Name))
		} else if storageClusterExternal(cluster) {
			errs = append(errs, fmt.Sprintf("%s.storageClusterRef.name %q references an external StorageCluster; Bootwright-managed placement policies are not declared for imported Ceph", prefix, policy.Spec.StorageClusterRef.Name))
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
			errs = append(errs, prefix+".storageClusterRef.name is required")
		} else if !ok {
			errs = append(errs, fmt.Sprintf("%s.storageClusterRef.name %q does not match any StorageCluster", prefix, pool.Spec.StorageClusterRef.Name))
		} else if storageClusterExternal(cluster) {
			errs = append(errs, fmt.Sprintf("%s.storageClusterRef.name %q references an external StorageCluster; Bootwright-managed pools are not declared for imported Ceph", prefix, pool.Spec.StorageClusterRef.Name))
		}
		if pool.Spec.PlacementPolicyRef.Name != "" {
			policy, policyOK := policies[pool.Spec.PlacementPolicyRef.Name]
			if !policyOK {
				errs = append(errs, fmt.Sprintf("%s.placementPolicyRef.name %q does not match any StoragePlacementPolicy", prefix, pool.Spec.PlacementPolicyRef.Name))
			} else if policy.Spec.StorageClusterRef.Name != pool.Spec.StorageClusterRef.Name {
				errs = append(errs, fmt.Sprintf("%s.placementPolicyRef.name %q belongs to StorageCluster/%s, want StorageCluster/%s", prefix, pool.Spec.PlacementPolicyRef.Name, policy.Spec.StorageClusterRef.Name, pool.Spec.StorageClusterRef.Name))
			}
		}
		poolType := pool.Spec.Ceph.Type
		if poolType == "" {
			poolType = v1alpha1.StoragePoolTypeReplicated
		}
		switch poolType {
		case v1alpha1.StoragePoolTypeReplicated:
			if pool.Spec.Ceph.ErasureCoded != nil {
				errs = append(errs, prefix+".ceph.type=replicated must not set erasureCoded")
			}
		case v1alpha1.StoragePoolTypeErasureCode:
			if ok && storageClusterStretchEnabled(cluster) {
				errs = append(errs, fmt.Sprintf("%s.ceph.type %q is not supported for stretch-mode StorageCluster/%s", prefix, poolType, cluster.Metadata.Name))
			}
		default:
			errs = append(errs, fmt.Sprintf("%s.ceph.type %q must be one of {%s, %s}", prefix, pool.Spec.Ceph.Type, v1alpha1.StoragePoolTypeReplicated, v1alpha1.StoragePoolTypeErasureCode))
		}
		if ok && storageClusterStretchEnabled(cluster) {
			replicas := pool.Spec.Ceph.Replicated
			if replicas.Size != 0 && replicas.Size != 4 {
				errs = append(errs, fmt.Sprintf("%s.ceph.replicated.size must be 4 for stretch-mode StorageCluster/%s", prefix, cluster.Metadata.Name))
			}
			if replicas.MinSize != 0 && replicas.MinSize != 2 {
				errs = append(errs, fmt.Sprintf("%s.ceph.replicated.minSize must be 2 for stretch-mode StorageCluster/%s", prefix, cluster.Metadata.Name))
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
			errs = append(errs, prefix+".storageClusterRef.name is required")
		} else if !ok {
			errs = append(errs, fmt.Sprintf("%s.storageClusterRef.name %q does not match any StorageCluster", prefix, fs.Spec.StorageClusterRef.Name))
		} else if storageClusterExternal(cluster) {
			errs = append(errs, fmt.Sprintf("%s.storageClusterRef.name %q references an external StorageCluster; Bootwright-managed filesystems are not declared for imported Ceph", prefix, fs.Spec.StorageClusterRef.Name))
		}
		metadataPool, metadataOK := pools[fs.Spec.CephFS.MetadataPoolRef.Name]
		if fs.Spec.CephFS.MetadataPoolRef.Name == "" {
			errs = append(errs, prefix+".cephfs.metadataPoolRef.name is required")
		} else if !metadataOK {
			errs = append(errs, fmt.Sprintf("%s.cephfs.metadataPoolRef.name %q does not match any StoragePool", prefix, fs.Spec.CephFS.MetadataPoolRef.Name))
		} else if metadataPool.Spec.StorageClusterRef.Name != fs.Spec.StorageClusterRef.Name {
			errs = append(errs, fmt.Sprintf("%s.cephfs.metadataPoolRef.name %q belongs to StorageCluster/%s, want StorageCluster/%s", prefix, metadataPool.Metadata.Name, metadataPool.Spec.StorageClusterRef.Name, fs.Spec.StorageClusterRef.Name))
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
		if ok && storageClusterStretchEnabled(cluster) {
			errs = append(errs, validatePlacementCoversDataSites(prefix+".cephfs.mds.placement", fs.Spec.CephFS.MDS.Placement.Hosts, cluster, v1alpha1.StorageCephRoleMDS)...)
		}
	}
	return errs
}

func validateStorageObjectGateways(items []v1alpha1.StorageObjectGateway, clusters map[string]v1alpha1.StorageCluster, infras map[string]v1alpha1.ClusterInfra) []string {
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
			errs = append(errs, prefix+".storageClusterRef.name is required")
		} else if !ok {
			errs = append(errs, fmt.Sprintf("%s.storageClusterRef.name %q does not match any StorageCluster", prefix, gw.Spec.StorageClusterRef.Name))
		} else if storageClusterExternal(cluster) {
			errs = append(errs, fmt.Sprintf("%s.storageClusterRef.name %q references an external StorageCluster; Bootwright-managed object gateways are not declared for imported Ceph", prefix, gw.Spec.StorageClusterRef.Name))
		}
		if gw.Spec.Ceph.ServiceID == "" {
			errs = append(errs, prefix+".ceph.serviceID is required")
		}
		if gw.Spec.Ceph.FrontendPort < 0 || gw.Spec.Ceph.FrontendPort > 65535 {
			errs = append(errs, fmt.Sprintf("%s.ceph.frontendPort %d out of range", prefix, gw.Spec.Ceph.FrontendPort))
		}
		infra, infraOK := storageGatewayClusterInfra(cluster, ok, infras)
		errs = append(errs, validateStorageGatewayPublicEndpoint(prefix+".publicEndpointRef", gw, infra, infraOK)...)
		errs = append(errs, validateStoragePlacementHosts(prefix+".ceph.placement", gw.Spec.Ceph.Placement, cluster, ok, v1alpha1.StorageCephRoleRGW)...)
		if ok && storageClusterStretchEnabled(cluster) {
			errs = append(errs, validatePlacementCoversDataSites(prefix+".ceph.placement", gw.Spec.Ceph.Placement.Hosts, cluster, v1alpha1.StorageCephRoleRGW)...)
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
			errs = append(errs, validateStorageGatewayIngressEndpoint(owner+".endpointRef", ingress.EndpointRef, gw, infra, infraOK)...)
			errs = append(errs, validateStoragePlacementHosts(owner+".placement", ingress.Placement, cluster, ok, v1alpha1.StorageCephRoleIngress)...)
			ingressHosts = append(ingressHosts, ingress.Placement.Hosts...)
		}
		if ok && storageClusterStretchEnabled(cluster) && len(gw.Spec.Ceph.Ingresses) > 0 {
			errs = append(errs, validatePlacementCoversDataSites(prefix+".ceph.ingresses", ingressHosts, cluster, v1alpha1.StorageCephRoleIngress)...)
		}
	}
	return errs
}

func storageGatewayClusterInfra(cluster v1alpha1.StorageCluster, clusterOK bool, infras map[string]v1alpha1.ClusterInfra) (v1alpha1.ClusterInfra, bool) {
	if !clusterOK {
		return v1alpha1.ClusterInfra{}, false
	}
	infra, ok := infras[cluster.Spec.ClusterInfraRef.Name]
	return infra, ok
}

func validateStorageGatewayPublicEndpoint(prefix string, gw v1alpha1.StorageObjectGateway, infra v1alpha1.ClusterInfra, infraOK bool) []string {
	ref := gw.Spec.PublicEndpointRef
	if ref.Name == "" {
		return []string{prefix + ".name is required"}
	}
	if !infraOK {
		return nil
	}
	endpoint, ok := infra.Spec.Endpoints[ref.Name]
	if !ok {
		return []string{fmt.Sprintf("%s.name %q does not match any ClusterInfra/%s spec.endpoints entry", prefix, ref.Name, infra.Metadata.Name)}
	}
	source := effectiveEndpointSource(endpoint, v1alpha1.EndpointSourceExternal)
	if source != v1alpha1.EndpointSourceExternal {
		return []string{fmt.Sprintf("%s.name %q references endpoint source.type %q; StorageObjectGateway publicEndpointRef requires %q",
			prefix, ref.Name, source, v1alpha1.EndpointSourceExternal)}
	}
	if endpoint.DNSName == "" {
		return []string{fmt.Sprintf("ClusterInfra/%s spec.endpoints.%s.dnsName is required for StorageObjectGateway/%s publicEndpointRef", infra.Metadata.Name, ref.Name, gw.Metadata.Name)}
	}
	return nil
}

func validateStorageGatewayIngressEndpoint(prefix string, ref v1alpha1.EndpointRef, gw v1alpha1.StorageObjectGateway, infra v1alpha1.ClusterInfra, infraOK bool) []string {
	if ref.Name == "" {
		return []string{prefix + ".name is required"}
	}
	if !infraOK {
		return nil
	}
	endpoint, ok := infra.Spec.Endpoints[ref.Name]
	if !ok {
		return []string{fmt.Sprintf("%s.name %q does not match any ClusterInfra/%s spec.endpoints entry", prefix, ref.Name, infra.Metadata.Name)}
	}
	source := effectiveEndpointSource(endpoint, v1alpha1.EndpointSourceCephadm)
	if source != v1alpha1.EndpointSourceCephadm {
		return []string{fmt.Sprintf("%s.name %q references endpoint source.type %q; StorageObjectGateway ingress endpointRef requires %q",
			prefix, ref.Name, source, v1alpha1.EndpointSourceCephadm)}
	}
	var errs []string
	if endpoint.Address == "" {
		errs = append(errs, fmt.Sprintf("ClusterInfra/%s spec.endpoints.%s.address is required for StorageObjectGateway/%s ingress", infra.Metadata.Name, ref.Name, gw.Metadata.Name))
	}
	if endpoint.PrefixLength == 0 {
		errs = append(errs, fmt.Sprintf("ClusterInfra/%s spec.endpoints.%s.prefixLength is required for StorageObjectGateway/%s ingress", infra.Metadata.Name, ref.Name, gw.Metadata.Name))
	}
	return errs
}

func validateStorageExports(state v1alpha1.State, clusters map[string]v1alpha1.StorageCluster, pools map[string]v1alpha1.StoragePool, filesystems map[string]v1alpha1.StorageFilesystem, gateways map[string]v1alpha1.StorageObjectGateway, hosts map[string]v1alpha1.Host) []string {
	var errs []string
	seen := map[string]bool{}
	for _, export := range state.StorageExports {
		if e := validateName(v1alpha1.KindStorageExport, export.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
		if seen[export.Metadata.Name] {
			errs = append(errs, fmt.Sprintf("duplicate StorageExport %q", export.Metadata.Name))
		}
		seen[export.Metadata.Name] = true
		prefix := fmt.Sprintf("StorageExport/%s spec", export.Metadata.Name)
		cluster, clusterOK := clusters[export.Spec.StorageClusterRef.Name]
		if export.Spec.StorageClusterRef.Name == "" {
			errs = append(errs, prefix+".storageClusterRef.name is required")
		} else if !clusterOK {
			errs = append(errs, fmt.Sprintf("%s.storageClusterRef.name %q does not match any StorageCluster", prefix, export.Spec.StorageClusterRef.Name))
		}
		switch export.Spec.Type {
		case v1alpha1.StorageExportTypeDataFoundation:
		case "":
			errs = append(errs, prefix+".type is required")
			continue
		default:
			errs = append(errs, fmt.Sprintf("%s.type %q must be %q", prefix, export.Spec.Type, v1alpha1.StorageExportTypeDataFoundation))
			continue
		}
		df := export.Spec.DataFoundation
		if clusterOK {
			errs = append(errs, validateStorageExportExternalDetails(state, export, cluster, hosts)...)
		}
		if clusterOK && storageClusterExternal(cluster) {
			if df != nil {
				errs = append(errs, fmt.Sprintf("%s.dataFoundation must be empty when storageClusterRef points to StorageCluster/%s with spec.management=external", prefix, cluster.Metadata.Name))
			}
			continue
		}
		if df == nil {
			errs = append(errs, prefix+".dataFoundation is required when storageClusterRef points to managed Ceph")
			continue
		}
		if df.RBDPoolRef.Name == "" {
			errs = append(errs, prefix+".dataFoundation.rbdPoolRef.name is required")
		} else if pool, ok := pools[df.RBDPoolRef.Name]; !ok {
			errs = append(errs, fmt.Sprintf("%s.dataFoundation.rbdPoolRef.name %q does not match any StoragePool", prefix, df.RBDPoolRef.Name))
		} else if pool.Spec.StorageClusterRef.Name != export.Spec.StorageClusterRef.Name {
			errs = append(errs, fmt.Sprintf("%s.dataFoundation.rbdPoolRef.name %q belongs to StorageCluster/%s, want StorageCluster/%s", prefix, pool.Metadata.Name, pool.Spec.StorageClusterRef.Name, export.Spec.StorageClusterRef.Name))
		}
		if df.CephFSRef.Name == "" {
			errs = append(errs, prefix+".dataFoundation.cephFSRef.name is required")
		} else if fs, ok := filesystems[df.CephFSRef.Name]; !ok {
			errs = append(errs, fmt.Sprintf("%s.dataFoundation.cephFSRef.name %q does not match any StorageFilesystem", prefix, df.CephFSRef.Name))
		} else if fs.Spec.StorageClusterRef.Name != export.Spec.StorageClusterRef.Name {
			errs = append(errs, fmt.Sprintf("%s.dataFoundation.cephFSRef.name %q belongs to StorageCluster/%s, want StorageCluster/%s", prefix, fs.Metadata.Name, fs.Spec.StorageClusterRef.Name, export.Spec.StorageClusterRef.Name))
		}
		if df.ObjectGatewayRef.Name != "" {
			if gw, ok := gateways[df.ObjectGatewayRef.Name]; !ok {
				errs = append(errs, fmt.Sprintf("%s.dataFoundation.objectGatewayRef.name %q does not match any StorageObjectGateway", prefix, df.ObjectGatewayRef.Name))
			} else if gw.Spec.StorageClusterRef.Name != export.Spec.StorageClusterRef.Name {
				errs = append(errs, fmt.Sprintf("%s.dataFoundation.objectGatewayRef.name %q belongs to StorageCluster/%s, want StorageCluster/%s", prefix, gw.Metadata.Name, gw.Spec.StorageClusterRef.Name, export.Spec.StorageClusterRef.Name))
			}
		}
	}
	return errs
}

func validateStorageExportExternalDetails(state v1alpha1.State, export v1alpha1.StorageExport, cluster v1alpha1.StorageCluster, hosts map[string]v1alpha1.Host) []string {
	var errs []string
	prefix := fmt.Sprintf("StorageExport/%s spec.externalDetails", export.Metadata.Name)
	details := export.Spec.ExternalDetails
	if details == nil {
		if storageClusterExternal(cluster) {
			return []string{prefix + " is required when storageClusterRef points to external Ceph"}
		}
		return nil
	}
	sourceCount := 0
	if strings.TrimSpace(details.FromSecret) != "" {
		sourceCount++
	}
	if details.Generated != nil {
		sourceCount++
	}
	if details.SSHExecution != nil {
		sourceCount++
	}
	if sourceCount != 1 {
		errs = append(errs, prefix+" must set exactly one of fromSecret, generated, or sshExecution")
	}
	if storageClusterExternal(cluster) && details.Generated != nil {
		errs = append(errs, prefix+".generated must be empty when storageClusterRef points to external Ceph")
	}
	if strings.TrimSpace(details.FromSecret) != "" && !environmentDeclaresSecret(state, details.FromSecret) {
		errs = append(errs, fmt.Sprintf("%s.fromSecret %q is not declared in Environment spec.secrets", prefix, details.FromSecret))
	}
	if details.SSHExecution != nil {
		errs = append(errs, validateStorageExportSSHExecution(state, prefix+".sshExecution", cluster, details.SSHExecution, hosts)...)
	}
	return errs
}

func validateStorageExportSSHExecution(state v1alpha1.State, prefix string, cluster v1alpha1.StorageCluster, spec *v1alpha1.StorageExportExternalDetailsSSHExecution, hosts map[string]v1alpha1.Host) []string {
	var errs []string
	if storageClusterExternal(cluster) && len(spec.HostRefs) == 0 {
		errs = append(errs, prefix+".hostRefs is required when storageClusterRef points to external Ceph")
	}
	for i, ref := range spec.HostRefs {
		owner := fmt.Sprintf("%s.hostRefs[%d].name", prefix, i)
		if ref.Name == "" {
			errs = append(errs, owner+" is required")
			continue
		}
		host, ok := hosts[ref.Name]
		if !ok {
			errs = append(errs, fmt.Sprintf("%s %q does not match any Host", owner, ref.Name))
			continue
		}
		if !hostHasCapability(host, v1alpha1.HostCapabilityCephAdmin) {
			errs = append(errs, fmt.Sprintf("%s %q must reference a Host with capability %q", owner, ref.Name, v1alpha1.HostCapabilityCephAdmin))
		}
	}
	if spec.KnownHostsRef.Name == "" {
		errs = append(errs, prefix+".knownHostsRef.name is required")
	} else if !environmentDeclaresSecret(state, spec.KnownHostsRef.Name) {
		errs = append(errs, fmt.Sprintf("%s.knownHostsRef.name %q is not declared in Environment spec.secrets", prefix, spec.KnownHostsRef.Name))
	}
	if spec.Timeout != "" {
		if _, err := time.ParseDuration(spec.Timeout); err != nil {
			errs = append(errs, fmt.Sprintf("%s.timeout %q must be a Go duration such as 10m, 30m, or 1h", prefix, spec.Timeout))
		}
	}
	if spec.Exporter.Source == "" {
		errs = append(errs, prefix+".exporter.source is required")
	} else if spec.Exporter.Source != v1alpha1.StorageExportExternalDetailsExporterBoundDataFoundationAddon {
		errs = append(errs, fmt.Sprintf("%s.exporter.source %q must be %q", prefix, spec.Exporter.Source, v1alpha1.StorageExportExternalDetailsExporterBoundDataFoundationAddon))
	}
	if spec.Config.RBDDataPoolName == "" {
		errs = append(errs, prefix+".config.rbdDataPoolName is required")
	}
	if spec.Config.Format != "" && spec.Config.Format != "json" {
		errs = append(errs, fmt.Sprintf("%s.config.format %q must be %q when set", prefix, spec.Config.Format, "json"))
	}
	if spec.Config.RestrictedAuthPermission && spec.Config.ClusterName == "" {
		errs = append(errs, prefix+".config.clusterName is required when restrictedAuthPermission is true")
	}
	for i, endpoint := range spec.Config.MonitoringEndpoint {
		if strings.TrimSpace(endpoint) == "" {
			errs = append(errs, fmt.Sprintf("%s.config.monitoringEndpoint[%d] must not be empty", prefix, i))
		}
	}
	if spec.Config.MonitoringEndpointPort < 0 || spec.Config.MonitoringEndpointPort > 65535 {
		errs = append(errs, fmt.Sprintf("%s.config.monitoringEndpointPort must be between 0 and 65535", prefix))
	}
	return errs
}

func environmentDeclaresSecret(state v1alpha1.State, name string) bool {
	for _, env := range state.Environments {
		if _, ok := env.Spec.Secrets[name]; ok {
			return true
		}
	}
	return false
}

func validateStorageExportAttachmentEffects(state v1alpha1.State, exports map[string]v1alpha1.StorageExport) []string {
	var errs []string
	for _, effect := range addoninputs.EffectBindings(state, v1alpha1.ClusterAddonInputEffectStorageExportAttachment, v1alpha1.ClusterAddonProvidesDataFoundation) {
		prefix := fmt.Sprintf("ClusterAddonBinding/%s ClusterAddon/%s input[%s]", effect.Binding.Metadata.Name, effect.Addon.Name, effect.Input.Name)
		if !addonProvides(effect.Extension, v1alpha1.ClusterAddonProvidesDataFoundation) {
			errs = append(errs, fmt.Sprintf("%s effect %q with provider %q requires ClusterAddon/%s to provide %q", prefix, effect.Effect.Type, effect.Effect.Provider, effect.Addon.Name, v1alpha1.ClusterAddonProvidesDataFoundation))
		}
		exportRef := addoninputs.LocalObjectReferenceValue(effect.Input.Values, "exportRef")
		if exportRef.Name == "" {
			continue
		}
		export, exportOK := exports[exportRef.Name]
		if !exportOK {
			errs = append(errs, fmt.Sprintf("%s.values.exportRef.name %q does not match any StorageExport", prefix, exportRef.Name))
			continue
		}
		if export.Spec.Type != v1alpha1.StorageExportTypeDataFoundation {
			errs = append(errs, fmt.Sprintf("%s.values.exportRef.name %q must reference a data-foundation StorageExport", prefix, exportRef.Name))
			continue
		}
	}
	return errs
}

func addonProvides(extension v1alpha1.ClusterAddon, capability string) bool {
	for _, item := range extension.Spec.Provides {
		if item == capability {
			return true
		}
	}
	return false
}

func validateStoragePlacementHosts(prefix string, placement v1alpha1.StoragePlacement, cluster v1alpha1.StorageCluster, clusterOK bool, role string) []string {
	var errs []string
	if len(placement.Hosts) == 0 {
		return []string{prefix + ".hosts is required"}
	}
	seen := map[string]bool{}
	for i, host := range placement.Hosts {
		owner := fmt.Sprintf("%s.hosts[%d]", prefix, i)
		if host == "" {
			errs = append(errs, owner+" must not be empty")
			continue
		}
		if seen[host] {
			errs = append(errs, fmt.Sprintf("%s %q is duplicated", owner, host))
		}
		seen[host] = true
		if clusterOK {
			node, ok := storageCephNodeByName(cluster, host)
			if !ok {
				errs = append(errs, fmt.Sprintf("%s %q is not listed in StorageCluster/%s spec.ceph.topology.nodes", owner, host, cluster.Metadata.Name))
			} else if role != "" && !storageCephNodeHasRole(node, role) {
				errs = append(errs, fmt.Sprintf("%s %q does not have role %q in StorageCluster/%s", owner, host, role, cluster.Metadata.Name))
			}
		}
	}
	if placement.CountPerHost < 0 {
		errs = append(errs, fmt.Sprintf("%s.countPerHost must be non-negative", prefix))
	}
	return errs
}

func validatePlacementCoversDataSites(prefix string, hosts []string, cluster v1alpha1.StorageCluster, role string) []string {
	stretch := cluster.Spec.Ceph.Topology.Stretch
	if stretch == nil || !stretch.Enabled {
		return nil
	}
	counts := map[string]int{}
	for _, host := range hosts {
		node, ok := storageCephNodeByName(cluster, host)
		if ok && storageCephNodeHasRole(node, role) {
			counts[node.Site]++
		}
	}
	var errs []string
	for _, site := range stretch.DataSites {
		if counts[site] < 2 {
			errs = append(errs, fmt.Sprintf("%s must include at least two %s-capable hosts in data site %q for stretch-mode availability", prefix, role, site))
		}
	}
	return errs
}

func validateCIDR(owner, value string) []string {
	if value == "" {
		return []string{owner + " must not be empty"}
	}
	if _, _, err := net.ParseCIDR(value); err != nil {
		return []string{fmt.Sprintf("%s %q is not a valid CIDR", owner, value)}
	}
	return nil
}

func clusterInfraMachineExists(infra v1alpha1.ClusterInfra, name string) bool {
	for _, machine := range infra.Spec.Components.Machines {
		if machine.Name == name {
			return true
		}
	}
	return false
}

func storageClusterStretchEnabled(cluster v1alpha1.StorageCluster) bool {
	return cluster.Spec.Ceph != nil && cluster.Spec.Ceph.Topology.Stretch != nil && cluster.Spec.Ceph.Topology.Stretch.Enabled
}

func storageClusterManagement(cluster v1alpha1.StorageCluster) string {
	if cluster.Spec.Management == "" {
		return v1alpha1.StorageClusterManagementManaged
	}
	return cluster.Spec.Management
}

func storageClusterExternal(cluster v1alpha1.StorageCluster) bool {
	return storageClusterManagement(cluster) == v1alpha1.StorageClusterManagementExternal
}

func storageCephNodeExists(cluster v1alpha1.StorageCluster, name string) bool {
	_, ok := storageCephNodeByName(cluster, name)
	return ok
}

func storageCephNodeByName(cluster v1alpha1.StorageCluster, name string) (v1alpha1.StorageCephNode, bool) {
	if cluster.Spec.Ceph == nil {
		return v1alpha1.StorageCephNode{}, false
	}
	for _, node := range cluster.Spec.Ceph.Topology.Nodes {
		if node.Name == name {
			return node, true
		}
	}
	return v1alpha1.StorageCephNode{}, false
}

func storageCephNodeHasRole(node v1alpha1.StorageCephNode, role string) bool {
	for _, item := range node.Roles {
		if item == role {
			return true
		}
	}
	return false
}

func storageCephNodeRolesOnly(node v1alpha1.StorageCephNode, role string) bool {
	return len(node.Roles) == 1 && node.Roles[0] == role
}

func validStorageCephRole(role string) bool {
	for _, item := range storageCephRoles() {
		if item == role {
			return true
		}
	}
	return false
}

func storageCephRoles() []string {
	return []string{
		v1alpha1.StorageCephRoleMON,
		v1alpha1.StorageCephRoleMGR,
		v1alpha1.StorageCephRoleOSD,
		v1alpha1.StorageCephRoleMDS,
		v1alpha1.StorageCephRoleRGW,
		v1alpha1.StorageCephRoleIngress,
	}
}
