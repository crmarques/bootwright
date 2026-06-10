package desiredstate

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/entitlements"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

var (
	// cephOSSReleaseNamePattern matches an upstream Ceph release codename (squid).
	cephOSSReleaseNamePattern = regexp.MustCompile(`^[a-z][a-z0-9]+$`)
	// cephOSSReleaseVersionPattern matches a full upstream x.y.z version.
	cephOSSReleaseVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	// cephSubscriptionStreamPattern matches a redhat/ibm product stream (9, 9.1).
	cephSubscriptionStreamPattern = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)
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
	errs = append(errs, validateStorageCephRelease(prefix, storageCephDistribution(cluster), ceph.Release)...)
	errs = append(errs, validateStorageCephImage(prefix, ceph.Image)...)
	errs = append(errs, validateStorageCephCommunity(prefix+".community", cluster)...)
	errs = append(errs, validateStorageCephManagedOS(cluster, machines, installProfiles)...)
	errs = append(errs, validateStorageCephadm(prefix+".cephadm", cluster, machines)...)
	for i, cidr := range ceph.Networks.PublicCIDRs {
		errs = append(errs, validateCIDR(fmt.Sprintf("%s.networks.publicCIDRs[%d]", prefix, i), cidr)...)
	}
	for i, cidr := range ceph.Networks.ClusterCIDRs {
		errs = append(errs, validateCIDR(fmt.Sprintf("%s.networks.clusterCIDRs[%d]", prefix, i), cidr)...)
	}
	errs = append(errs, validateStorageCephConfig(prefix+".config", ceph.Config)...)
	errs = append(errs, validateStorageCephNodes(prefix+".topology.hosts", cluster, machines)...)
	if ceph.Topology.Stretch != nil {
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
			return []string{prefix + ".entitlementRef must be empty when distribution=oss"}
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
		return []string{prefix + ".entitlementRef is required when distribution requires subscription or license handling"}
	}
	entitlement, ok := entitlements.Find(env, ref)
	if !ok {
		return []string{fmt.Sprintf("%s.entitlementRef %q does not match any Environment.spec.entitlements[].name", prefix, ref)}
	}
	var errs []string
	if entitlement.Provider != provider {
		errs = append(errs, fmt.Sprintf("%s.entitlementRef %q resolves to provider %q, want %q", prefix, ref, entitlement.Provider, provider))
	}
	if entitlement.Product != product {
		errs = append(errs, fmt.Sprintf("%s.entitlementRef %q resolves to product %q, want %q", prefix, ref, entitlement.Product, product))
	}
	return errs
}

// validateStorageCephRelease checks spec.ceph.release against the meaning the
// chosen distribution gives it: an upstream release name or x.y.z version for
// oss, a product stream version for the subscription-backed distributions.
func validateStorageCephRelease(prefix, distribution, release string) []string {
	if release == "" {
		return nil
	}
	switch distribution {
	case v1alpha1.StorageCephDistributionOSS:
		if !cephOSSReleaseNamePattern.MatchString(release) && !cephOSSReleaseVersionPattern.MatchString(release) {
			return []string{fmt.Sprintf("%s.release %q must be an upstream Ceph release name (e.g. squid) or an x.y.z version (e.g. 19.2.1)", prefix, release)}
		}
	case v1alpha1.StorageCephDistributionRedHat, v1alpha1.StorageCephDistributionIBM:
		if !cephSubscriptionStreamPattern.MatchString(release) {
			return []string{fmt.Sprintf("%s.release %q must be a product stream version such as 9 or 9.1", prefix, release)}
		}
	}
	return nil
}

// validateStorageCephImage checks spec.ceph.image pins a reproducible reference,
// reusing the same tag/digest rules enforced for pinned component images.
func validateStorageCephImage(prefix, image string) []string {
	if image == "" {
		return nil
	}
	if err := validatePinnedImageReference(image); err != "" {
		return []string{fmt.Sprintf("%s.image %q %s", prefix, image, err)}
	}
	return nil
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
	if mirror := community.Mirror; mirror != "" && !isHTTPURL(mirror) {
		errs = append(errs, fmt.Sprintf("%s.mirror %q must be an http or https URL", prefix, mirror))
	}
	return errs
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
	for _, node := range cluster.Spec.Ceph.Topology.Hosts {
		machine, ok := machines[node.MachineRef.Name]
		if !ok || machine.Spec.OS.InstallProfileRef.Name == "" {
			continue
		}
		profile, ok := installProfiles[machine.Spec.OS.InstallProfileRef.Name]
		if !ok {
			continue
		}
		owner := fmt.Sprintf("StorageCluster/%s spec.ceph.topology.nodes[%s] MachineInstallProfile/%s spec.os", cluster.Metadata.Name, node.Hostname, profile.Metadata.Name)
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
	if adm.Bootstrap.Host == "" {
		errs = append(errs, prefix+".bootstrap.host is required")
	} else if !storageCephNodeExists(cluster, adm.Bootstrap.Host) {
		errs = append(errs, fmt.Sprintf("%s.bootstrap.host %q is not listed in spec.ceph.topology.hosts", prefix, adm.Bootstrap.Host))
	} else {
		errs = append(errs, validateStorageNodeMachineAddress(prefix+".bootstrap.addressRef", cluster, adm.Bootstrap.Host, adm.Bootstrap.AddressRef.Name, machines, adm.AddressRef.Name)...)
	}
	return errs
}

func validateStorageCephNodes(prefix string, cluster v1alpha1.StorageCluster, machines map[string]v1alpha1.Machine) []string {
	var errs []string
	nodes := cluster.Spec.Ceph.Topology.Hosts
	if len(nodes) == 0 {
		return []string{prefix + " is required"}
	}
	seen := map[string]bool{}
	sshUser := ""
	sshKeyRef := ""
	sshSeen := false
	for i, node := range nodes {
		owner := fmt.Sprintf("%s[%d]", prefix, i)
		if node.Hostname == "" {
			errs = append(errs, owner+".hostname is required")
		} else {
			if seen[node.Hostname] {
				errs = append(errs, fmt.Sprintf("%s.hostname %q is duplicated", owner, node.Hostname))
			}
			seen[node.Hostname] = true
		}
		if node.MachineRef.Name == "" {
			errs = append(errs, owner+".machineRef is required")
		} else {
			machine, ok := machines[node.MachineRef.Name]
			if !ok {
				errs = append(errs, fmt.Sprintf("%s.machineRef %q does not match any Machine", owner, node.MachineRef.Name))
			} else {
				if !machineHasCapability(machine, v1alpha1.MachineCapabilityCephNode) {
					errs = append(errs, fmt.Sprintf("%s.machineRef %q lacks capability %q", owner, node.MachineRef.Name, v1alpha1.MachineCapabilityCephNode))
				}
				if machine.Spec.Access.SSH == nil {
					errs = append(errs, fmt.Sprintf("Machine/%s spec.access.ssh is required for %s.machineRef", machine.Metadata.Name, owner))
				} else {
					if !sshSeen {
						sshUser = machine.Spec.Access.SSH.User
						sshKeyRef = machine.Spec.Access.SSH.KeyRef.Name
						sshSeen = true
					} else if machine.Spec.Access.SSH.User != sshUser {
						errs = append(errs, fmt.Sprintf("%s.machineRef %q resolves to Machine/%s with ssh.user %q; all storage node Machines in one StorageCluster must use %q", owner, node.MachineRef.Name, machine.Metadata.Name, machine.Spec.Access.SSH.User, sshUser))
					} else if machine.Spec.Access.SSH.KeyRef.Name != sshKeyRef {
						errs = append(errs, fmt.Sprintf("%s.machineRef %q resolves to Machine/%s with ssh.keyRef %q; all storage node Machines in one StorageCluster must use %q", owner, node.MachineRef.Name, machine.Metadata.Name, machine.Spec.Access.SSH.KeyRef.Name, sshKeyRef))
					}
				}
				errs = append(errs, validateStorageNodeMachineAddress(fmt.Sprintf("StorageCluster/%s spec.ceph.cephadm.addressRef", cluster.Metadata.Name), cluster, node.Hostname, cluster.Spec.Ceph.Cephadm.AddressRef.Name, machines, "")...)
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
		for j, label := range node.Labels {
			labelOwner := fmt.Sprintf("%s.labels[%d]", owner, j)
			if label == "" {
				errs = append(errs, labelOwner+" must not be empty")
			} else if roleSeen[label] {
				errs = append(errs, fmt.Sprintf("%s %q duplicates a role; roles always become host labels", labelOwner, label))
			}
		}
		errs = append(errs, validateStorageCephHostOSD(owner, node)...)
	}
	return errs
}

// validateStorageCephConfig checks the declared ceph config options: sections
// must be valid `ceph config set` who-targets, and keys owned by other spec
// fields (the networks CIDRs) are rejected — one owner per fact.
func validateStorageCephConfig(prefix string, config map[string]map[string]string) []string {
	var errs []string
	for _, section := range sortedStringKeys(config) {
		owner := fmt.Sprintf("%s[%s]", prefix, section)
		if !validCephConfigSection(section) {
			errs = append(errs, fmt.Sprintf("%s is not a valid ceph config section (accepted: global, mon, mgr, osd, mds, client, or <type>.<id>)", owner))
		}
		options := config[section]
		for _, key := range sortedStringKeys2(options) {
			keyOwner := fmt.Sprintf("%s.%s", owner, key)
			if key == "" {
				errs = append(errs, owner+" contains an empty option key")
				continue
			}
			if key == "public_network" || key == "cluster_network" {
				errs = append(errs, fmt.Sprintf("%s is owned by spec.ceph.networks (publicCIDRs/clusterCIDRs); declare it there", keyOwner))
			}
			if options[key] == "" {
				errs = append(errs, keyOwner+" must not be empty")
			}
		}
	}
	return errs
}

func validCephConfigSection(section string) bool {
	switch section {
	case "global", "mon", "mgr", "osd", "mds", "client":
		return true
	}
	for _, daemon := range []string{"mon.", "mgr.", "osd.", "mds.", "client."} {
		if strings.HasPrefix(section, daemon) && len(section) > len(daemon) {
			return true
		}
	}
	return false
}

func sortedStringKeys(m map[string]map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedStringKeys2(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func validateStoragePlacementHosts(prefix string, placement v1alpha1.StoragePlacement, cluster v1alpha1.StorageCluster, clusterOK bool, role string) []string {
	var errs []string
	seenSites := map[string]bool{}
	for i, site := range placement.Sites {
		owner := fmt.Sprintf("%s.sites[%d]", prefix, i)
		if site == "" {
			errs = append(errs, owner+" must not be empty")
			continue
		}
		if seenSites[site] {
			errs = append(errs, fmt.Sprintf("%s %q is duplicated", owner, site))
		}
		seenSites[site] = true
		if clusterOK && !storageTopologyHasSite(cluster, site) {
			errs = append(errs, fmt.Sprintf("%s %q is not a site of any StorageCluster/%s spec.ceph.topology.hosts[] entry", owner, site, cluster.Metadata.Name))
		}
	}
	if clusterOK && len(topology.ResolvePlacement(cluster, placement, role)) == 0 {
		errs = append(errs, fmt.Sprintf("%s resolves to no hosts: no StorageCluster/%s spec.ceph.topology.hosts[] entry carries role %q within the selection", prefix, cluster.Metadata.Name, role))
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
	if stretch == nil {
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
	return cluster.Spec.Ceph != nil && cluster.Spec.Ceph.Topology.Stretch != nil
}

func storageTopologyHasSite(cluster v1alpha1.StorageCluster, site string) bool {
	if cluster.Spec.Ceph == nil {
		return false
	}
	for _, host := range cluster.Spec.Ceph.Topology.Hosts {
		if host.Site == site {
			return true
		}
	}
	return false
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
		return []string{fmt.Sprintf("%s must set addressRef or Machine/%s spec.access.ssh.addressRef", owner, machine.Metadata.Name)}
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

func storageCephNodeByName(cluster v1alpha1.StorageCluster, name string) (v1alpha1.StorageCephHost, bool) {
	if cluster.Spec.Ceph == nil {
		return v1alpha1.StorageCephHost{}, false
	}
	for _, node := range cluster.Spec.Ceph.Topology.Hosts {
		if node.Hostname == name {
			return node, true
		}
	}
	return v1alpha1.StorageCephHost{}, false
}

// validateStorageCephHostOSD checks the OSD device selection: the lean
// devices shorthand and the drivegroup-shaped osd object are mutually
// exclusive, and each device selection must select something coherent.
func validateStorageCephHostOSD(owner string, node v1alpha1.StorageCephHost) []string {
	var errs []string
	if len(node.Devices) > 0 && node.OSD != nil {
		errs = append(errs, fmt.Sprintf("%s sets both devices and osd; devices is the shorthand for osd.dataDevices.paths — use one", owner))
	}
	if node.OSD == nil {
		return errs
	}
	if !topology.NodeHasRole(node, v1alpha1.StorageCephRoleOSD) {
		errs = append(errs, fmt.Sprintf("%s.osd requires the %q role", owner, v1alpha1.StorageCephRoleOSD))
	}
	if node.OSD.DataDevices == nil {
		errs = append(errs, owner+".osd.dataDevices is required")
	}
	for _, selection := range []struct {
		field string
		value *v1alpha1.StorageCephDeviceSelection
	}{
		{"dataDevices", node.OSD.DataDevices},
		{"dbDevices", node.OSD.DBDevices},
		{"walDevices", node.OSD.WALDevices},
	} {
		if selection.value == nil {
			continue
		}
		selectionOwner := owner + ".osd." + selection.field
		if len(selection.value.Paths) > 0 && selection.value.All {
			errs = append(errs, selectionOwner+" must not set both paths and all")
		}
		if len(selection.value.Paths) == 0 && !selection.value.All && selection.value.Rotational == nil && selection.value.Size == "" && selection.value.Limit == 0 {
			errs = append(errs, selectionOwner+" must select devices (paths, all, rotational, size, or limit)")
		}
		if selection.value.Limit < 0 {
			errs = append(errs, selectionOwner+".limit must be non-negative")
		}
	}
	if node.OSD.OSDsPerDevice < 0 {
		errs = append(errs, owner+".osd.osdsPerDevice must be non-negative")
	}
	return errs
}

func storageCephNodeRolesOnly(node v1alpha1.StorageCephHost, role string) bool {
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
