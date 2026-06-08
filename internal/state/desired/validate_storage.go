package desiredstate

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/entitlements"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

func validateStorage(state v1alpha1.State) []string {
	var errs []string
	clusters := indexStorageClusters(state.StorageClusters)
	policies := indexStoragePlacementPolicies(state.StoragePlacementPolicies)
	pools := indexStoragePools(state.StoragePools)
	filesystems := indexStorageFilesystems(state.StorageFilesystems)
	gateways := indexStorageObjectGateways(state.StorageObjectGateways)
	exports := indexStorageExports(state.StorageExports)
	machines := indexMachines(state.Machines)
	installProfiles := indexMachineInstallProfiles(state.MachineInstallProfiles)
	env := primaryEnvironment(&state)

	errs = append(errs, validateStorageClusters(state.StorageClusters, machines, installProfiles, env)...)
	errs = append(errs, validateStoragePlacementPolicies(state.StoragePlacementPolicies, clusters)...)
	errs = append(errs, validateStoragePools(state.StoragePools, clusters, policies)...)
	errs = append(errs, validateStorageFilesystems(state.StorageFilesystems, clusters, pools)...)
	errs = append(errs, validateStorageObjectGateways(state, state.StorageObjectGateways, clusters)...)
	errs = append(errs, validateStorageExports(state, clusters, pools, filesystems, gateways, machines)...)
	errs = append(errs, validateStorageExportAttachmentEffects(state, exports)...)
	return errs
}

func validateStorageClusters(items []v1alpha1.StorageCluster, machines map[string]v1alpha1.Machine, installProfiles map[string]v1alpha1.MachineInstallProfile, env *v1alpha1.Environment) []string {
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
		case v1alpha1.StorageClusterManagementManaged:
			if cluster.Spec.Ceph == nil {
				errs = append(errs, prefix+".ceph is required when spec.type=ceph")
				continue
			}
		case v1alpha1.StorageClusterManagementExternal:
			if cluster.Spec.Ceph != nil {
				errs = append(errs, prefix+".ceph must be empty when spec.management=external")
			}
			continue
		default:
			errs = append(errs, fmt.Sprintf("%s.management %q must be one of {%s, %s}", prefix, cluster.Spec.Management, v1alpha1.StorageClusterManagementManaged, v1alpha1.StorageClusterManagementExternal))
			continue
		}
		if cluster.Spec.Ceph != nil {
			errs = append(errs, validateStorageClusterCeph(cluster, machines, installProfiles, env)...)
		}
	}
	return errs
}

func validateStorageClusterCeph(cluster v1alpha1.StorageCluster, machines map[string]v1alpha1.Machine, installProfiles map[string]v1alpha1.MachineInstallProfile, env *v1alpha1.Environment) []string {
	var errs []string
	ceph := cluster.Spec.Ceph
	prefix := fmt.Sprintf("StorageCluster/%s spec.ceph", cluster.Metadata.Name)
	errs = append(errs, validateStorageCephDistribution(prefix, cluster, env)...)
	errs = append(errs, validateStorageCephCommunity(prefix+".community", cluster)...)
	errs = append(errs, validateStorageCephManagedOS(cluster, machines, installProfiles)...)
	errs = append(errs, validateStorageCephadm(prefix+".cephadm", cluster, machines)...)
	for i, cidr := range ceph.Networks.PublicCIDRs {
		errs = append(errs, validateCIDR(fmt.Sprintf("%s.networks.publicCIDRs[%d]", prefix, i), cidr)...)
	}
	for i, cidr := range ceph.Networks.ClusterCIDRs {
		errs = append(errs, validateCIDR(fmt.Sprintf("%s.networks.clusterCIDRs[%d]", prefix, i), cidr)...)
	}
	errs = append(errs, validateStorageCephNodes(prefix+".topology.nodes", cluster, machines)...)
	if ceph.Topology.Stretch != nil && ceph.Topology.Stretch.Enabled {
		errs = append(errs, validateStorageCephStretch(cluster)...)
	}
	return errs
}

func validateStorageCephDistribution(prefix string, cluster v1alpha1.StorageCluster, env *v1alpha1.Environment) []string {
	distribution := storageCephDistribution(cluster)
	if cluster.Spec.Ceph.Distribution != "" && distribution == "" {
		return []string{fmt.Sprintf("%s.distribution %q must be one of {%s, %s, %s}",
			prefix, cluster.Spec.Ceph.Distribution, v1alpha1.StorageCephDistributionOSS, v1alpha1.StorageCephDistributionRedHat, v1alpha1.StorageCephDistributionIBM)}
	}
	ref := cluster.Spec.Ceph.EntitlementRef.Name
	switch distribution {
	case v1alpha1.StorageCephDistributionOSS:
		if ref != "" {
			return []string{prefix + ".entitlementRef.name must be empty when distribution=oss"}
		}
		return nil
	case v1alpha1.StorageCephDistributionRedHat:
		return validateStorageCephDistributionEntitlement(prefix, env, ref, v1alpha1.EntitlementProviderRedHat, v1alpha1.EntitlementProductCeph)
	case v1alpha1.StorageCephDistributionIBM:
		return validateStorageCephDistributionEntitlement(prefix, env, ref, v1alpha1.EntitlementProviderIBM, v1alpha1.EntitlementProductIBMStorageCeph)
	default:
		return nil
	}
}

func validateStorageCephDistributionEntitlement(prefix string, env *v1alpha1.Environment, ref, provider, product string) []string {
	if ref == "" {
		return []string{prefix + ".entitlementRef.name is required when distribution requires subscription or license handling"}
	}
	entitlement, ok := entitlements.Find(env, ref)
	if !ok {
		return []string{fmt.Sprintf("%s.entitlementRef.name %q does not match any Environment.spec.entitlements[].name", prefix, ref)}
	}
	var errs []string
	if entitlement.Provider != provider {
		errs = append(errs, fmt.Sprintf("%s.entitlementRef.name %q resolves to provider %q, want %q", prefix, ref, entitlement.Provider, provider))
	}
	if entitlement.Product != product {
		errs = append(errs, fmt.Sprintf("%s.entitlementRef.name %q resolves to product %q, want %q", prefix, ref, entitlement.Product, product))
	}
	return errs
}

func validateStorageCephCommunity(prefix string, cluster v1alpha1.StorageCluster) []string {
	community := cluster.Spec.Ceph.Community
	if community == nil {
		return nil
	}
	if storageCephDistribution(cluster) != v1alpha1.StorageCephDistributionOSS {
		return []string{prefix + " must be empty unless distribution=oss"}
	}
	var errs []string
	if release := community.Release; release != "" && !isStorageCephCommunityRelease(release) {
		errs = append(errs, fmt.Sprintf("%s.release %q must be an upstream Ceph release name such as squid, reef, or quincy", prefix, release))
	}
	if mirror := community.Mirror; mirror != "" && !isHTTPURL(mirror) {
		errs = append(errs, fmt.Sprintf("%s.mirror %q must be an http or https URL", prefix, mirror))
	}
	return errs
}

func isStorageCephCommunityRelease(release string) bool {
	for _, r := range release {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '.' && r != '-' {
			return false
		}
	}
	return true
}

func isHTTPURL(value string) bool {
	u, err := url.Parse(value)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func validateStorageCephManagedOS(cluster v1alpha1.StorageCluster, machines map[string]v1alpha1.Machine, installProfiles map[string]v1alpha1.MachineInstallProfile) []string {
	distribution := storageCephDistribution(cluster)
	if distribution == v1alpha1.StorageCephDistributionOSS {
		return nil
	}
	var errs []string
	for _, node := range cluster.Spec.Ceph.Topology.Nodes {
		machine, ok := machines[node.MachineRef.Name]
		if !ok || machine.Spec.OS.InstallProfileRef.Name == "" {
			continue
		}
		profile, ok := installProfiles[machine.Spec.OS.InstallProfileRef.Name]
		if !ok {
			continue
		}
		owner := fmt.Sprintf("StorageCluster/%s spec.ceph.topology.nodes[%s] MachineInstallProfile/%s spec.os", cluster.Metadata.Name, node.Name, profile.Metadata.Name)
		if strings.ToLower(profile.Spec.OS.Family) != v1alpha1.MachineInstallOSFamilyRHEL {
			errs = append(errs, fmt.Sprintf("%s.family %q is incompatible with Ceph distribution %q; use RHEL", owner, profile.Spec.OS.Family, distribution))
			continue
		}
		if !storageCephDistributionSupportsRHELVersion(distribution, profile.Spec.OS.Version) {
			errs = append(errs, fmt.Sprintf("%s.version %q is incompatible with Ceph distribution %q; supported RHEL versions are 9.6, 9.7, 10, or 10.1", owner, profile.Spec.OS.Version, distribution))
		}
	}
	return errs
}

func storageCephDistributionSupportsRHELVersion(distribution, version string) bool {
	if distribution != v1alpha1.StorageCephDistributionRedHat && distribution != v1alpha1.StorageCephDistributionIBM {
		return true
	}
	switch version {
	case "9", "10", "9.6", "9.7", "10.0", "10.1":
		return true
	default:
		return false
	}
}

func validateStorageCephadm(prefix string, cluster v1alpha1.StorageCluster, machines map[string]v1alpha1.Machine) []string {
	var errs []string
	adm := cluster.Spec.Ceph.Cephadm
	if adm.Bootstrap.SeedNode == "" {
		errs = append(errs, prefix+".bootstrap.seedNode is required")
	} else if !storageCephNodeExists(cluster, adm.Bootstrap.SeedNode) {
		errs = append(errs, fmt.Sprintf("%s.bootstrap.seedNode %q is not listed in spec.ceph.topology.nodes", prefix, adm.Bootstrap.SeedNode))
	}
	mon := adm.Bootstrap.MonIP
	if mon.NodeRef.Name == "" {
		errs = append(errs, prefix+".bootstrap.monIP.nodeRef.name is required")
	} else {
		if !storageCephNodeExists(cluster, mon.NodeRef.Name) {
			errs = append(errs, fmt.Sprintf("%s.bootstrap.monIP.nodeRef.name %q is not listed in spec.ceph.topology.nodes", prefix, mon.NodeRef.Name))
		}
		errs = append(errs, validateStorageNodeMachineAddress(prefix+".bootstrap.monIP.addressRef.name", cluster, mon.NodeRef.Name, mon.AddressRef.Name, machines, adm.AddressRef.Name)...)
	}
	return errs
}

func validateStorageCephNodes(prefix string, cluster v1alpha1.StorageCluster, machines map[string]v1alpha1.Machine) []string {
	var errs []string
	nodes := cluster.Spec.Ceph.Topology.Nodes
	if len(nodes) == 0 {
		return []string{prefix + " is required"}
	}
	seen := map[string]bool{}
	sshUser := ""
	sshKeyRef := ""
	sshSeen := false
	for i, node := range nodes {
		owner := fmt.Sprintf("%s[%d]", prefix, i)
		if node.Name == "" {
			errs = append(errs, owner+".name is required")
		} else {
			if seen[node.Name] {
				errs = append(errs, fmt.Sprintf("%s.name %q is duplicated", owner, node.Name))
			}
			seen[node.Name] = true
		}
		if node.MachineRef.Name == "" {
			errs = append(errs, owner+".machineRef.name is required")
		} else {
			machine, ok := machines[node.MachineRef.Name]
			if !ok {
				errs = append(errs, fmt.Sprintf("%s.machineRef.name %q does not match any Machine", owner, node.MachineRef.Name))
			} else {
				if !machineHasCapability(machine, v1alpha1.MachineCapabilityCephNode) {
					errs = append(errs, fmt.Sprintf("%s.machineRef.name %q lacks capability %q", owner, node.MachineRef.Name, v1alpha1.MachineCapabilityCephNode))
				}
				if machine.Spec.Access.SSH == nil {
					errs = append(errs, fmt.Sprintf("Machine/%s spec.access.ssh is required for %s.machineRef.name", machine.Metadata.Name, owner))
				} else {
					if !sshSeen {
						sshUser = machine.Spec.Access.SSH.User
						sshKeyRef = machine.Spec.Access.SSH.KeyRef.Name
						sshSeen = true
					} else if machine.Spec.Access.SSH.User != sshUser {
						errs = append(errs, fmt.Sprintf("%s.machineRef.name %q resolves to Machine/%s with ssh.user %q; all storage node Machines in one StorageCluster must use %q", owner, node.MachineRef.Name, machine.Metadata.Name, machine.Spec.Access.SSH.User, sshUser))
					} else if machine.Spec.Access.SSH.KeyRef.Name != sshKeyRef {
						errs = append(errs, fmt.Sprintf("%s.machineRef.name %q resolves to Machine/%s with ssh.keyRef.name %q; all storage node Machines in one StorageCluster must use %q", owner, node.MachineRef.Name, machine.Metadata.Name, machine.Spec.Access.SSH.KeyRef.Name, sshKeyRef))
					}
				}
				errs = append(errs, validateStorageNodeMachineAddress(fmt.Sprintf("StorageCluster/%s spec.ceph.cephadm.addressRef.name", cluster.Metadata.Name), cluster, node.Name, cluster.Spec.Ceph.Cephadm.AddressRef.Name, machines, "")...)
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
			} else if role != "" && !topology.NodeHasRole(node, role) {
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
		if ok && topology.NodeHasRole(node, role) {
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

func storageClusterStretchEnabled(cluster v1alpha1.StorageCluster) bool {
	return cluster.Spec.Ceph != nil && cluster.Spec.Ceph.Topology.Stretch != nil && cluster.Spec.Ceph.Topology.Stretch.Enabled
}

func validateStorageNodeMachineAddress(owner string, cluster v1alpha1.StorageCluster, nodeName string, addressName string, machines map[string]v1alpha1.Machine, defaultAddressName string) []string {
	node, ok := storageCephNodeByName(cluster, nodeName)
	if !ok {
		return nil
	}
	machine, ok := machines[node.MachineRef.Name]
	if !ok {
		return nil
	}
	resolvedName := addressName
	if resolvedName == "" {
		resolvedName = defaultAddressName
	}
	if resolvedName == "" && machine.Spec.Access.SSH != nil {
		resolvedName = machine.Spec.Access.SSH.AddressRef.Name
	}
	if resolvedName == "" {
		return []string{fmt.Sprintf("%s must set addressRef.name or Machine/%s spec.access.ssh.addressRef.name", owner, machine.Metadata.Name)}
	}
	if _, ok := v1alpha1.MachineAddressByName(machine, resolvedName); !ok {
		return []string{fmt.Sprintf("%s %q does not resolve to Machine/%s spec.addresses[].name", owner, resolvedName, machine.Metadata.Name)}
	}
	return nil
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

func storageCephDistribution(cluster v1alpha1.StorageCluster) string {
	if cluster.Spec.Ceph == nil || cluster.Spec.Ceph.Distribution == "" {
		return v1alpha1.StorageCephDistributionOSS
	}
	switch cluster.Spec.Ceph.Distribution {
	case v1alpha1.StorageCephDistributionOSS, v1alpha1.StorageCephDistributionRedHat, v1alpha1.StorageCephDistributionIBM:
		return cluster.Spec.Ceph.Distribution
	default:
		return ""
	}
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
