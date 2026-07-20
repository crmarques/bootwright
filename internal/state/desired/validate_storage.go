package desiredstate

import (
	"fmt"
	"net"
	"regexp"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	stateview "github.com/crmarques/bootwright/internal/state/view"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

var (
	cephOSSReleaseNamePattern      = regexp.MustCompile(`^[a-z][a-z0-9]+$`)
	cephOSSReleaseVersionPattern   = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	cephSubscriptionVersionPattern = regexp.MustCompile(`^[0-9]+(\.[0-9]+){0,3}$`)
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

	errs = append(errs, validateStorageClusters(state, machines, installProfiles, env)...)
	errs = append(errs, validateStoragePlacementPolicies(state.StoragePlacementPolicies, clusters)...)
	errs = append(errs, validateStoragePools(state.StoragePools, clusters, policies)...)
	errs = append(errs, validateStorageFilesystems(state.StorageFilesystems, clusters, pools)...)
	errs = append(errs, validateStorageObjectGateways(state.StorageObjectGateways, clusters)...)
	errs = append(errs, validateStorageNFSExports(state.StorageNFSExports, clusters, filesystems)...)
	errs = append(errs, validateStorageServiceIDUniqueness(state)...)
	errs = append(errs, validateStorageExports(state, clusters, pools, filesystems, gateways, machines)...)
	errs = append(errs, validateStorageExportAttachmentEffects(state, exports)...)
	return errs
}

func validateStorageClusters(state v1alpha1.State, machines map[string]v1alpha1.Machine, installProfiles map[string]v1alpha1.MachineInstallProfile, env *v1alpha1.Environment) []string {
	var errs []string
	for _, cluster := range state.StorageClusters {
		if e := validateName(v1alpha1.KindStorageCluster, cluster.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
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
			errs = append(errs, validateStorageClusterCeph(state, cluster, machines, installProfiles, env)...)
		}
	}
	return errs
}

func validateStorageClusterCeph(state v1alpha1.State, cluster v1alpha1.StorageCluster, machines map[string]v1alpha1.Machine, installProfiles map[string]v1alpha1.MachineInstallProfile, env *v1alpha1.Environment) []string {
	var errs []string
	ceph := cluster.Spec.Ceph
	prefix := fmt.Sprintf("StorageCluster/%s spec.ceph", cluster.Metadata.Name)
	errs = append(errs, validateStorageCephDistribution(prefix, cluster, state)...)
	errs = append(errs, validateStorageCephRelease(prefix, storageCephDistribution(cluster), ceph.Release)...)
	errs = append(errs, validateStorageCephImage(prefix, cluster, state)...)
	errs = append(errs, validateStorageCephCommunity(prefix+".community", cluster)...)
	errs = append(errs, validateStorageCephIBM(prefix+".ibm", cluster)...)
	errs = append(errs, validateStorageCephManagedOS(cluster, machines, installProfiles)...)
	errs = append(errs, validateStorageCephFIPS(cluster, machines, installProfiles)...)
	errs = append(errs, validateStorageCephadm(prefix+".cephadm", cluster, machines, state)...)
	for i, cidr := range ceph.Networks.PublicCIDRs {
		errs = append(errs, validateCIDR(fmt.Sprintf("%s.networks.publicCIDRs[%d]", prefix, i), cidr)...)
	}
	for i, cidr := range ceph.Networks.ClusterCIDRs {
		errs = append(errs, validateCIDR(fmt.Sprintf("%s.networks.clusterCIDRs[%d]", prefix, i), cidr)...)
	}
	errs = append(errs, validateStorageCephBootstrapPublicNetwork(prefix, cluster, machines)...)
	errs = append(errs, validateStorageCephClusterNetwork(prefix, cluster, machines)...)
	errs = append(errs, validateStorageCephConfig(prefix+".config", ceph.Config)...)
	errs = append(errs, validateStorageCephMgrModules(prefix+".mgrModules", ceph.MgrModules)...)
	errs = append(errs, validateStorageCephMonitoring(prefix+".monitoring", cluster)...)
	errs = append(errs, validateStorageCephManagement(prefix+".management", cluster, state)...)
	errs = append(errs, validateStorageCephServices(prefix+".services", cluster)...)
	errs = append(errs, validateStorageCephNodes(prefix+".topology.hosts", cluster, machines, storageSiteRequirement(state, cluster))...)
	errs = append(errs, validateStorageCephOSDDrivegroups(prefix+".topology.osdDrivegroups", cluster)...)
	if ceph.Topology.Stretch != nil {
		errs = append(errs, validateStorageCephStretch(cluster)...)
	}
	errs = append(errs, validateStorageCephSingleHostDefaults(prefix+".cephadm.bootstrap.singleHostDefaults", cluster)...)
	return errs
}

func validateStorageCephSingleHostDefaults(prefix string, cluster v1alpha1.StorageCluster) []string {
	if !cluster.Spec.Ceph.Cephadm.Bootstrap.SingleHostDefaults {
		return nil
	}
	var errs []string
	if len(cluster.Spec.Ceph.Topology.Hosts) != 1 {
		errs = append(errs, prefix+" is only valid for a single-host topology")
	}
	if cluster.Spec.Ceph.Topology.Stretch != nil {
		errs = append(errs, prefix+" must not be set with stretch mode")
	}
	if count, known := topology.DeclaredOSDCount(cluster); known && count < topology.SingleHostMinimumOSDs {
		errs = append(errs, fmt.Sprintf("%s requires at least %d OSDs; the statically declared topology creates %d", prefix, topology.SingleHostMinimumOSDs, count))
	}
	if global := cluster.Spec.Ceph.Config["global"]; global != nil {
		for _, key := range []string{"osd_pool_default_size", "osd_pool_default_min_size", "osd_crush_chooseleaf_type"} {
			if _, ok := global[key]; ok {
				errs = append(errs, fmt.Sprintf("%s owns %s at bootstrap; remove it from spec.ceph.config[global]", prefix, key))
			}
		}
	}
	return errs
}

func validateStorageCephadm(prefix string, cluster v1alpha1.StorageCluster, machines map[string]v1alpha1.Machine, state v1alpha1.State) []string {
	var errs []string
	adm := cluster.Spec.Ceph.Cephadm
	if adm.Bootstrap.Host == "" {
		errs = append(errs, prefix+".bootstrap.host is required")
	} else if !storageCephNodeExists(cluster, adm.Bootstrap.Host) {
		errs = append(errs, fmt.Sprintf("%s.bootstrap.host %q is not listed in spec.ceph.topology.hosts", prefix, adm.Bootstrap.Host))
	} else {
		errs = append(errs, validateStorageNodeMachineAddress(prefix+".bootstrap.addressRef", cluster, adm.Bootstrap.Host, adm.Bootstrap.AddressRef.Name, machines, adm.AddressRef.Name)...)
	}
	if ref := adm.ClusterSSHKeyRef.Name; ref != "" {
		if s, ok := stateview.Secret(state, ref); !ok {
			errs = append(errs, fmt.Sprintf("%s.clusterSSHKeyRef %q is not a declared Secret", prefix, ref))
		} else if s.Spec.Type != v1alpha1.SecretTypeSSHKeyPair {
			errs = append(errs, fmt.Sprintf("%s.clusterSSHKeyRef %q must reference an sshKeyPair Secret", prefix, ref))
		}
	}
	return errs
}

func validateStorageSecretRef(owner, ref string, state v1alpha1.State) []string {
	if ref == "" {
		return []string{owner + " is required"}
	}
	if _, ok := stateview.Secret(state, ref); !ok {
		return []string{fmt.Sprintf("%s %q is not a declared Secret", owner, ref)}
	}
	return nil
}

func storageSiteRequirement(state v1alpha1.State, cluster v1alpha1.StorageCluster) string {
	if cluster.Spec.Ceph == nil {
		return ""
	}
	if cluster.Spec.Ceph.Topology.Stretch != nil {
		return "spec.ceph.topology.stretch is set"
	}
	if monitoring := cluster.Spec.Ceph.Monitoring; monitoring != nil {
		for _, item := range []struct {
			field   string
			service *v1alpha1.StorageCephMonitoringService
		}{
			{"prometheus", monitoring.Prometheus},
			{"grafana", monitoring.Grafana},
			{"alertmanager", monitoring.Alertmanager},
			{"nodeExporter", monitoring.NodeExporter},
			{"loki", monitoring.Loki},
			{"promtail", monitoring.Promtail},
		} {
			if item.service != nil && len(item.service.Placement.Sites) > 0 {
				return fmt.Sprintf("spec.ceph.monitoring.%s.placement narrows by sites", item.field)
			}
		}
	}
	for i, service := range cluster.Spec.Ceph.Services {
		if len(service.Placement.Sites) > 0 {
			return fmt.Sprintf("spec.ceph.services[%d].placement narrows by sites", i)
		}
	}
	for _, fs := range state.StorageFilesystems {
		if fs.Spec.StorageClusterRef.Name != cluster.Metadata.Name {
			continue
		}
		if len(fs.Spec.CephFS.MDS.Placement.Sites) > 0 {
			return fmt.Sprintf("StorageFilesystem/%s spec.cephfs.mds.placement narrows by sites", fs.Metadata.Name)
		}
	}
	for _, gateway := range state.StorageObjectGateways {
		if gateway.Spec.StorageClusterRef.Name != cluster.Metadata.Name {
			continue
		}
		if len(gateway.Spec.Ceph.Placement.Sites) > 0 {
			return fmt.Sprintf("StorageObjectGateway/%s spec.ceph.placement narrows by sites", gateway.Metadata.Name)
		}
		for i, ingress := range gateway.Spec.Ceph.Ingresses {
			if len(ingress.Placement.Sites) > 0 {
				return fmt.Sprintf("StorageObjectGateway/%s spec.ceph.ingresses[%d].placement narrows by sites", gateway.Metadata.Name, i)
			}
		}
	}
	return ""
}

func validateStorageCephNodes(prefix string, cluster v1alpha1.StorageCluster, machines map[string]v1alpha1.Machine, siteRequiredBecause string) []string {
	var errs []string
	nodes := cluster.Spec.Ceph.Topology.Hosts
	if len(nodes) == 0 {
		return []string{prefix + " is required"}
	}
	fleetCovered := storageFleetCoveredHosts(cluster)
	seen := map[string]bool{}
	sshUser := ""
	sshKeyRef := ""
	sshSeen := false
	uniformAccessKeyRequired := cluster.Spec.Ceph.Cephadm.ClusterSSHKeyRef.Name == ""
	for i, node := range nodes {
		owner := fmt.Sprintf("%s[%d]", prefix, i)
		if node.Hostname == "" {
			errs = append(errs, owner+".hostname is required")
		} else {
			if seen[node.Hostname] {
				errs = append(errs, fmt.Sprintf("%s.hostname %q is duplicated", owner, node.Hostname))
			}
			seen[node.Hostname] = true
			if len(node.Hostname) > 253 || !dnsSubdomain.MatchString(node.Hostname) {
				errs = append(errs, fmt.Sprintf("%s.hostname %q is not a valid DNS name (<=253 chars, lowercase labels); the default <machine>.<cluster>.<baseDomain> would be too long, or an explicit hostname is malformed", owner, node.Hostname))
			}
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
				} else if uniformAccessKeyRequired {
					if !sshSeen {
						sshUser = machine.Spec.Access.SSH.User
						sshKeyRef = machine.Spec.Access.SSH.KeyRef.Name
						sshSeen = true
					} else if machine.Spec.Access.SSH.User != sshUser {
						errs = append(errs, fmt.Sprintf("%s.machineRef %q resolves to Machine/%s with ssh.user %q; all storage node Machines in one StorageCluster must use %q (set spec.ceph.cephadm.clusterSSHKeyRef to allow per-node access keys)", owner, node.MachineRef.Name, machine.Metadata.Name, machine.Spec.Access.SSH.User, sshUser))
					} else if machine.Spec.Access.SSH.KeyRef.Name != sshKeyRef {
						errs = append(errs, fmt.Sprintf("%s.machineRef %q resolves to Machine/%s with ssh.keyRef %q; all storage node Machines in one StorageCluster must use %q (set spec.ceph.cephadm.clusterSSHKeyRef to allow per-node access keys)", owner, node.MachineRef.Name, machine.Metadata.Name, machine.Spec.Access.SSH.KeyRef.Name, sshKeyRef))
					}
				}
				errs = append(errs, validateStorageNodeMachineAddress(fmt.Sprintf("StorageCluster/%s spec.ceph.cephadm.addressRef", cluster.Metadata.Name), cluster, node.Hostname, cluster.Spec.Ceph.Cephadm.AddressRef.Name, machines, "")...)
			}
		}
		if node.Site == "" && siteRequiredBecause != "" {
			errs = append(errs, fmt.Sprintf("%s.site is required when %s", owner, siteRequiredBecause))
		}
		if len(node.Roles) == 0 {
			errs = append(errs, owner+".roles is required")
		}
		roleSeen := map[string]bool{}
		for j, role := range node.Roles {
			roleOwner := fmt.Sprintf("%s.roles[%d]", owner, j)
			if !validStorageCephRole(role) {
				errs = append(errs, fmt.Sprintf("%s %q must be one of {%s}", roleOwner, role, strings.Join(v1alpha1.StorageCephRoles(), ", ")))
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
		errs = append(errs, validateStorageCephHostOSD(owner, node, fleetCovered[node.Hostname])...)
	}
	return errs
}

func validateStorageCephConfig(prefix string, config map[string]map[string]string) []string {
	var errs []string
	for _, section := range sortedKeys(config) {
		owner := fmt.Sprintf("%s[%s]", prefix, section)
		if !validCephConfigSection(section) {
			errs = append(errs, fmt.Sprintf("%s is not a valid ceph config section (accepted: global, mon, mgr, osd, mds, client, <type>.<id>, optionally with a /<mask> such as /class:ssd or /rack:r1)", owner))
		}
		options := config[section]
		for _, key := range sortedKeys(options) {
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
	base := section
	if idx := strings.Index(section, "/"); idx >= 0 {
		base = section[:idx]
		mask := section[idx+1:]
		if strings.Contains(mask, "/") {
			return false
		}
		colon := strings.Index(mask, ":")
		if colon <= 0 || colon == len(mask)-1 {
			return false
		}
	}
	return validCephConfigWho(base)
}

func validCephConfigWho(who string) bool {
	switch who {
	case "global", "mon", "mgr", "osd", "mds", "client":
		return true
	}
	for _, daemon := range []string{"mon.", "mgr.", "osd.", "mds.", "client."} {
		if strings.HasPrefix(who, daemon) && len(who) > len(daemon) {
			return true
		}
	}
	return false
}

func validateStorageCephMgrModules(prefix string, modules []string) []string {
	var errs []string
	seen := map[string]bool{}
	for i, module := range modules {
		owner := fmt.Sprintf("%s[%d]", prefix, i)
		if module == "" {
			errs = append(errs, owner+" must not be empty")
			continue
		}
		if seen[module] {
			errs = append(errs, fmt.Sprintf("%s %q is duplicated", owner, module))
		}
		seen[module] = true
	}
	return errs
}

func validateStorageCephMonitoring(prefix string, cluster v1alpha1.StorageCluster) []string {
	monitoring := cluster.Spec.Ceph.Monitoring
	if monitoring == nil {
		return nil
	}
	var errs []string
	if monitoring.Enabled != nil && !*monitoring.Enabled {
		for field, service := range map[string]*v1alpha1.StorageCephMonitoringService{
			"prometheus": monitoring.Prometheus, "grafana": monitoring.Grafana,
			"alertmanager": monitoring.Alertmanager, "nodeExporter": monitoring.NodeExporter,
			"loki": monitoring.Loki, "promtail": monitoring.Promtail,
		} {
			if service != nil {
				errs = append(errs, fmt.Sprintf("%s.%s must be empty when monitoring.enabled is false", prefix, field))
			}
		}
		return errs
	}
	for _, item := range []struct {
		field   string
		role    string
		service *v1alpha1.StorageCephMonitoringService
	}{
		{"prometheus", v1alpha1.StorageCephRolePrometheus, monitoring.Prometheus},
		{"grafana", v1alpha1.StorageCephRoleGrafana, monitoring.Grafana},
		{"alertmanager", v1alpha1.StorageCephRoleAlertmanager, monitoring.Alertmanager},
		{"nodeExporter", "", monitoring.NodeExporter},
		{"loki", "", monitoring.Loki},
		{"promtail", "", monitoring.Promtail},
	} {
		if item.service == nil {
			continue
		}
		owner := prefix + "." + item.field
		errs = append(errs, validateStoragePlacementHosts(owner+".placement", item.service.Placement, cluster, true, item.role)...)
		if item.service.Port < 0 || item.service.Port > 65535 {
			errs = append(errs, fmt.Sprintf("%s.port %d out of range", owner, item.service.Port))
		}
		if item.service.RetentionTime != "" && item.field != "prometheus" {
			errs = append(errs, owner+".retentionTime applies to prometheus only")
		}
		if item.service.RetentionSize != "" && item.field != "prometheus" {
			errs = append(errs, owner+".retentionSize applies to prometheus only")
		}
		for i, cidr := range item.service.Networks {
			errs = append(errs, validateCIDR(fmt.Sprintf("%s.networks[%d]", owner, i), cidr)...)
		}
	}
	return errs
}

func validateStorageCephManagement(prefix string, cluster v1alpha1.StorageCluster, state v1alpha1.State) []string {
	mgmt := cluster.Spec.Ceph.Management
	if mgmt == nil {
		return nil
	}
	var errs []string
	if mgmt.DNSName == "" {
		errs = append(errs, prefix+".dnsName is required")
	}
	if mgmt.Port < 0 || mgmt.Port > 65535 {
		errs = append(errs, fmt.Sprintf("%s.port %d out of range", prefix, mgmt.Port))
	}
	ingress := mgmt.Ingress
	if ingress.Name == "" {
		errs = append(errs, prefix+".ingress.name is required")
	}
	errs = append(errs, validateStorageIngressVIP(prefix+".ingress", ingress.Address, ingress.PrefixLength)...)
	errs = append(errs, validateStoragePlacementHosts(prefix+".ingress.placement", ingress.Placement, cluster, true, v1alpha1.StorageCephRoleIngress)...)
	if storageClusterStretchEnabled(cluster) {
		errs = append(errs, validatePlacementCoversDataSites(prefix+".ingress.placement", topology.ResolvePlacement(cluster, ingress.Placement, v1alpha1.StorageCephRoleIngress), cluster, v1alpha1.StorageCephRoleIngress)...)
	}
	if mgmt.TLS != nil {
		errs = append(errs, validateStorageSecretRef(prefix+".tls.certificateRef", mgmt.TLS.CertificateRef.Name, state)...)
		errs = append(errs, validateStorageSecretRef(prefix+".tls.keyRef", mgmt.TLS.KeyRef.Name, state)...)
	}
	authOn := mgmt.EnableAuth != nil && *mgmt.EnableAuth
	switch {
	case authOn && mgmt.OAuth2Proxy == nil:
		errs = append(errs, prefix+".enableAuth requires oauth2Proxy to be configured")
	case !authOn && mgmt.OAuth2Proxy != nil:
		errs = append(errs, prefix+".oauth2Proxy requires enableAuth: true")
	}
	if o := mgmt.OAuth2Proxy; o != nil {
		if o.ProviderDisplayName == "" {
			errs = append(errs, prefix+".oauth2Proxy.providerDisplayName is required")
		}
		if o.ClientID == "" {
			errs = append(errs, prefix+".oauth2Proxy.clientId is required")
		}
		if o.OIDCIssuerURL == "" {
			errs = append(errs, prefix+".oauth2Proxy.oidcIssuerUrl is required")
		}
		errs = append(errs, validateStorageSecretRef(prefix+".oauth2Proxy.clientSecretRef", o.ClientSecretRef.Name, state)...)
		if o.CookieSecretRef.Name != "" {
			errs = append(errs, validateStorageSecretRef(prefix+".oauth2Proxy.cookieSecretRef", o.CookieSecretRef.Name, state)...)
		}
	}
	return errs
}

func validateStorageCephServices(prefix string, cluster v1alpha1.StorageCluster) []string {
	reserved := map[string]bool{
		"host": true, "mon": true, "mgr": true, "osd": true, "mds": true,
		"rgw": true, "ingress": true, "prometheus": true, "grafana": true,
		"alertmanager": true, "node-exporter": true, "nfs": true,
		"loki": true, "promtail": true,
	}
	var errs []string
	seen := map[string]bool{}
	for i, service := range cluster.Spec.Ceph.Services {
		owner := fmt.Sprintf("%s[%d]", prefix, i)
		switch {
		case service.ServiceType == "":
			errs = append(errs, owner+".serviceType is required")
		case reserved[service.ServiceType]:
			errs = append(errs, fmt.Sprintf("%s.serviceType %q is owned by a first-class surface (topology roles, monitoring, gateways); declare it there", owner, service.ServiceType))
		}
		identity := service.ServiceType + "/" + service.ServiceID
		if seen[identity] {
			errs = append(errs, fmt.Sprintf("%s duplicates serviceType/serviceID %q", owner, identity))
		}
		seen[identity] = true
		if len(service.Placement.Hosts) == 0 && len(service.Placement.Sites) == 0 {
			errs = append(errs, owner+".placement requires hosts or sites for passthrough services")
		}
		errs = append(errs, validateStoragePlacementHosts(owner+".placement", service.Placement, cluster, true, "")...)
	}
	return errs
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
				errs = append(errs, fmt.Sprintf("%s %q is not listed in StorageCluster/%s spec.ceph.topology.hosts", owner, host, cluster.Metadata.Name))
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

func validateStorageIngressVIP(owner, address string, prefixLength int) []string {
	var errs []string
	ip := net.ParseIP(address)
	switch {
	case address == "":
		errs = append(errs, owner+".address is required")
	case ip == nil:
		errs = append(errs, fmt.Sprintf("%s.address %q is not a valid IP", owner, address))
	}
	maxPrefix := 128
	if ip != nil && ip.To4() != nil {
		maxPrefix = 32
	}
	switch {
	case prefixLength == 0:
		errs = append(errs, owner+".prefixLength is required")
	case prefixLength < 1 || prefixLength > maxPrefix:
		errs = append(errs, fmt.Sprintf("%s.prefixLength %d out of range (1-%d)", owner, prefixLength, maxPrefix))
	}
	return errs
}

func validateStorageCephBootstrapPublicNetwork(prefix string, cluster v1alpha1.StorageCluster, machines map[string]v1alpha1.Machine) []string {
	ceph := cluster.Spec.Ceph
	if ceph == nil || len(ceph.Networks.PublicCIDRs) == 0 {
		return nil
	}
	host := ceph.Cephadm.Bootstrap.Host
	node, ok := storageCephNodeByName(cluster, host)
	if host == "" || !ok {
		return nil
	}
	machine, ok := machines[node.MachineRef.Name]
	if !ok {
		return nil
	}
	addrName := ceph.Cephadm.Bootstrap.AddressRef.Name
	if addrName == "" && machine.Spec.Access.SSH != nil {
		addrName = machine.Spec.Access.SSH.AddressRef.Name
	}
	monIP, ok := v1alpha1.MachineAddressByName(machine, addrName)
	if addrName == "" || !ok || monIP == "" {
		return nil
	}
	publics := ceph.Networks.PublicCIDRs
	var subnets []string
	for _, ia := range machine.Spec.Network.Config.InterfaceAddresses {
		if ia.AddressRef.Name != addrName || ia.PrefixLength <= 0 {
			continue
		}
		if subnet, ok := storageConnectedSubnet(monIP, ia.PrefixLength); ok {
			subnets = append(subnets, subnet)
		}
	}
	if len(subnets) == 0 {
		if storageIPWithinAny(monIP, publics) {
			return nil
		}
		return []string{fmt.Sprintf("%s.networks.publicCIDRs: cephadm bootstrap host %q mon-ip %s falls outside every entry (%s); the bootstrap monitor cannot bind inside the declared public network", prefix, host, monIP, strings.Join(publics, ","))}
	}
	declared := storageCanonicalNetworks(publics)
	for _, subnet := range subnets {
		if declared[subnet] {
			return nil
		}
	}
	return []string{fmt.Sprintf("%s.networks.publicCIDRs: cephadm bootstrap host %q carries mon-ip %s on interface subnet %s, absent from the declared entries (%s); cephadm bootstrap aborts with \"public CIDR network ... is not configured locally\" unless a publicCIDRs entry exactly equals a locally-configured interface subnet — set the Machine/%s interfaceAddresses prefixLength and the publicCIDRs entry to the same CIDR", prefix, host, monIP, strings.Join(subnets, ","), strings.Join(publics, ","), machine.Metadata.Name)}
}

func validateStorageCephClusterNetwork(prefix string, cluster v1alpha1.StorageCluster, machines map[string]v1alpha1.Machine) []string {
	ceph := cluster.Spec.Ceph
	if ceph == nil || len(ceph.Networks.ClusterCIDRs) == 0 {
		return nil
	}
	clusters := ceph.Networks.ClusterCIDRs
	var errs []string
	for _, node := range ceph.Topology.Hosts {
		if !storageCephHostRunsOSD(node) {
			continue
		}
		machine, ok := machines[node.MachineRef.Name]
		if !ok || len(machine.Spec.Network.Config.InterfaceAddresses) == 0 {
			continue
		}
		if storageMachineHasClusterNetworkAddress(machine, clusters) {
			continue
		}
		errs = append(errs, fmt.Sprintf("%s.networks.clusterCIDRs: Ceph OSD host %q (Machine/%s) configures no interface address inside the declared cluster network (%s); ceph-osd binds its replication socket inside cluster_network and, finding no local address there, aborts with 'unable to find any IP address in network(s)', so every OSD on the host stays down and stray outside the CRUSH map; add an interfaceAddresses entry (matching prefixLength) that places the node on the cluster network, or drop clusterCIDRs to run replication over the public network", prefix, node.Hostname, machine.Metadata.Name, strings.Join(clusters, ",")))
	}
	return errs
}

func storageCephHostRunsOSD(node v1alpha1.StorageCephHost) bool {
	if node.OSD != nil {
		return true
	}
	for _, role := range node.Roles {
		if role == v1alpha1.StorageCephRoleOSD {
			return true
		}
	}
	return false
}

func storageMachineHasClusterNetworkAddress(machine v1alpha1.Machine, cidrs []string) bool {
	for _, ia := range machine.Spec.Network.Config.InterfaceAddresses {
		ip, ok := v1alpha1.MachineAddressByName(machine, ia.AddressRef.Name)
		if !ok || ip == "" {
			continue
		}
		if storageIPWithinAny(ip, cidrs) {
			return true
		}
	}
	return false
}

func storageConnectedSubnet(ip string, prefixLength int) (string, bool) {
	_, ipNet, err := net.ParseCIDR(fmt.Sprintf("%s/%d", ip, prefixLength))
	if err != nil {
		return "", false
	}
	return ipNet.String(), true
}

func storageCanonicalNetworks(cidrs []string) map[string]bool {
	out := make(map[string]bool, len(cidrs))
	for _, cidr := range cidrs {
		if _, ipNet, err := net.ParseCIDR(cidr); err == nil {
			out[ipNet.String()] = true
		}
	}
	return out
}

func storageIPWithinAny(ip string, cidrs []string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return true
	}
	for _, cidr := range cidrs {
		if _, ipNet, err := net.ParseCIDR(cidr); err == nil && ipNet.Contains(parsed) {
			return true
		}
	}
	return false
}

func validateStorageServiceIDUniqueness(state v1alpha1.State) []string {
	var errs []string
	seen := map[string]map[string]string{}
	mark := func(cluster, serviceType, serviceID, owner string) {
		if cluster == "" || serviceID == "" {
			return
		}
		if seen[cluster] == nil {
			seen[cluster] = map[string]string{}
		}
		identity := serviceType + "/" + serviceID
		if prev, ok := seen[cluster][identity]; ok {
			errs = append(errs, fmt.Sprintf("%s duplicates cephadm service %q already declared by %s on StorageCluster/%s; cephadm keeps only one, silently dropping the other", owner, identity, prev, cluster))
			return
		}
		seen[cluster][identity] = owner
	}
	for _, gw := range state.StorageObjectGateways {
		mark(gw.Spec.StorageClusterRef.Name, "rgw", gw.Spec.Ceph.ServiceID, fmt.Sprintf("StorageObjectGateway/%s spec.ceph.serviceID", gw.Metadata.Name))
	}
	for _, nfs := range state.StorageNFSExports {
		mark(nfs.Spec.StorageClusterRef.Name, "nfs", nfs.Spec.Ceph.ServiceID, fmt.Sprintf("StorageNFSExport/%s spec.ceph.serviceID", nfs.Metadata.Name))
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
		if node.Hostname == name || node.MachineRef.Name == name {
			return node, true
		}
	}
	return v1alpha1.StorageCephHost{}, false
}

func storageCephNodeRolesOnly(node v1alpha1.StorageCephHost, role string) bool {
	return len(node.Roles) == 1 && node.Roles[0] == role
}

func validStorageCephRole(role string) bool {
	for _, item := range v1alpha1.StorageCephRoles() {
		if item == role {
			return true
		}
	}
	return false
}
