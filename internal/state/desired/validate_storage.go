package desiredstate

import (
	"fmt"
	"net"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	stateview "github.com/crmarques/bootwright/internal/state/view"
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

	errs = append(errs, validateStorageClusters(state, machines, installProfiles, env)...)
	errs = append(errs, validateStoragePlacementPolicies(state.StoragePlacementPolicies, clusters)...)
	errs = append(errs, validateStoragePools(state.StoragePools, clusters, policies)...)
	errs = append(errs, validateStorageFilesystems(state.StorageFilesystems, clusters, pools)...)
	errs = append(errs, validateStorageObjectGateways(state.StorageObjectGateways, clusters)...)
	errs = append(errs, validateStorageNFSExports(state.StorageNFSExports, clusters, filesystems)...)
	errs = append(errs, validateStorageServiceIDUniqueness(state)...)
	errs = append(errs, validateStorageIngressVRRPCollisions(state)...)
	errs = append(errs, validateStorageGatewayIngressTLS(state)...)
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
	errs = append(errs, validateStorageCephDistributionFamily(prefix, cluster, state)...)
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
	errs = append(errs, validateStorageCephMgmtGateway(prefix+".management", cluster, state)...)
	errs = append(errs, validateStorageCephServices(prefix+".services", cluster)...)
	errs = append(errs, validateStorageCephNodes(prefix+".topology.nodes", cluster, machines, storageSiteRequirement(state, cluster))...)
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
	if len(cluster.Spec.Ceph.Topology.Nodes) != 1 {
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
	if _, ok := cluster.Spec.Ceph.Config["mgr"]["mgr_standby_modules"]; ok {
		errs = append(errs, fmt.Sprintf("%s owns mgr_standby_modules at bootstrap; remove it from spec.ceph.config[mgr]", prefix))
	}
	return errs
}

func validateStorageCephadm(prefix string, cluster v1alpha1.StorageCluster, machines map[string]v1alpha1.Machine, state v1alpha1.State) []string {
	var errs []string
	adm := cluster.Spec.Ceph.Cephadm
	if adm.Bootstrap.Node == "" {
		errs = append(errs, prefix+".bootstrap.node is required")
	} else if !storageCephNodeExists(cluster, adm.Bootstrap.Node) {
		msg := fmt.Sprintf("%s.bootstrap.node %q does not match any node name in spec.ceph.topology.nodes", prefix, adm.Bootstrap.Node)
		if storageCephMachineRefExists(cluster, adm.Bootstrap.Node) {
			msg += "; it names the bound Machine, but clusters reference nodes — use the node's name"
		}
		errs = append(errs, msg)
	} else {
		errs = append(errs, validateStorageNodeMachineAddress(prefix+".bootstrap.addressRef", cluster, adm.Bootstrap.Node, adm.Bootstrap.AddressRef.Name, machines, adm.AddressRef.Name)...)
	}
	if ref := adm.ClusterSSH.KeyRef.Name; ref != "" {
		if s, ok := stateview.Secret(state, ref); !ok {
			errs = append(errs, fmt.Sprintf("%s.clusterSSH.keyRef %q is not a declared Secret", prefix, ref))
		} else if s.Spec.Type != v1alpha1.SecretTypeSSHKeyPair {
			errs = append(errs, fmt.Sprintf("%s.clusterSSH.keyRef %q must reference an sshKeyPair Secret", prefix, ref))
		}
	}
	errs = append(errs, validateStorageCephadmSSHPosture(prefix, cluster, machines, state)...)
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

func validateStorageTLSSecretRef(owner, ref string, state v1alpha1.State) []string {
	if ref == "" {
		return []string{owner + " is required"}
	}
	s, ok := stateview.Secret(state, ref)
	if !ok {
		return []string{fmt.Sprintf("%s %q is not a declared Secret", owner, ref)}
	}
	if s.Spec.Type != v1alpha1.SecretTypeTLSCertificate {
		return []string{fmt.Sprintf("%s %q is a %s Secret but a %s Secret is required", owner, ref, s.Spec.Type, v1alpha1.SecretTypeTLSCertificate)}
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
	nodes := cluster.Spec.Ceph.Topology.Nodes
	if len(nodes) == 0 {
		return []string{prefix + " is required"}
	}
	fleetCovered := storageFleetCoveredHosts(cluster)
	seen := map[string]string{}
	for i, node := range nodes {
		owner := fmt.Sprintf("%s[%d]", prefix, i)
		if node.Name == "" {
			errs = append(errs, owner+".name is required")
		} else {
			short := stateview.NodeShortName(node.Name)
			if prev, ok := seen[short]; ok {
				errs = append(errs, fmt.Sprintf("%s.name %q shares node short name %q with %q; node tokens, OSD service ids, and DNS labels key on the short name, so it must be unique within the cluster", owner, node.Name, short, prev))
			}
			seen[short] = node.Name
			if len(node.Name) > 253 || !dnsSubdomain.MatchString(node.Name) {
				errs = append(errs, fmt.Sprintf("%s.name %q is not a valid DNS name (<=253 chars, lowercase labels); the composed <node>.<cluster>.<storageClustersDomain> would be too long, or the node name is malformed", owner, node.Name))
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
					errs = append(errs, fmt.Sprintf("Machine/%s declares no login for %s.machineRef; a storage node is reached over SSH, so it cannot set spec.access.local, and a machine the cluster installs derives its login from spec.ceph.cephadm.clusterSSH", machine.Metadata.Name, owner))
				}
				errs = append(errs, validateStorageNodeMachineAddress(fmt.Sprintf("StorageCluster/%s spec.ceph.cephadm.addressRef", cluster.Metadata.Name), cluster, node.Name, cluster.Spec.Ceph.Cephadm.AddressRef.Name, machines, "")...)
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
		errs = append(errs, validateStorageCephNodeOSD(owner, node, fleetCovered[node.Name])...)
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

func validateStorageCephMgmtGateway(prefix string, cluster v1alpha1.StorageCluster, state v1alpha1.State) []string {
	mgmt := cluster.Spec.Ceph.MgmtGateway
	if mgmt == nil {
		return nil
	}
	var errs []string
	if mgmt.DNSLabel != "" && !IsDNSLabel(mgmt.DNSLabel) {
		errs = append(errs, fmt.Sprintf("%s.dnsLabel %q is not a valid DNS label", prefix, mgmt.DNSLabel))
	}
	if mgmt.Port < 0 || mgmt.Port > 65535 {
		errs = append(errs, fmt.Sprintf("%s.port %d out of range", prefix, mgmt.Port))
	}
	ingress := mgmt.Ingress
	if ingress.Name == "" {
		errs = append(errs, prefix+".ingress.name is required")
	}
	errs = append(errs, validateStorageIngressVIP(prefix+".ingress", ingress.Address, ingress.PrefixLength)...)
	errs = append(errs, validateStorageIngressVRRPID(prefix+".ingress", ingress.FirstVirtualRouterID)...)
	errs = append(errs, validateStoragePlacementHosts(prefix+".ingress.placement", ingress.Placement, cluster, true, v1alpha1.StorageCephRoleIngress)...)
	if storageClusterStretchEnabled(cluster) {
		errs = append(errs, validatePlacementCoversDataSites(prefix+".ingress.placement", topology.ResolvePlacement(cluster, ingress.Placement, v1alpha1.StorageCephRoleIngress), cluster, v1alpha1.StorageCephRoleIngress, nil)...)
	}
	if mgmt.TLS != nil {
		errs = append(errs, validateStorageTLSSecretRef(prefix+".tls.certificateRef", mgmt.TLS.CertificateRef.Name, state)...)
		errs = append(errs, validateStorageTLSSecretRef(prefix+".tls.keyRef", mgmt.TLS.KeyRef.Name, state)...)
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
			errs = append(errs, fmt.Sprintf("%s %q is not a site of any StorageCluster/%s spec.ceph.topology.nodes[] entry", owner, site, cluster.Metadata.Name))
		}
	}
	if clusterOK && len(topology.ResolvePlacement(cluster, placement, role)) == 0 {
		errs = append(errs, fmt.Sprintf("%s resolves to no hosts: no StorageCluster/%s spec.ceph.topology.nodes[] entry carries role %q within the selection", prefix, cluster.Metadata.Name, role))
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
				msg := fmt.Sprintf("%s %q does not match any node name in StorageCluster/%s spec.ceph.topology.nodes", owner, host, cluster.Metadata.Name)
				if storageCephMachineRefExists(cluster, host) {
					msg += "; it names the bound Machine, but clusters reference nodes — use the node's name"
				}
				errs = append(errs, msg)
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

func validatePlacementCoversDataSites(prefix string, hosts []string, cluster v1alpha1.StorageCluster, role string, targetSites []string) []string {
	stretch := cluster.Spec.Ceph.Topology.Stretch
	if stretch == nil {
		return nil
	}
	dataSites := stretch.DataSites
	if len(targetSites) > 0 {
		dataSites = targetSites
	}
	counts := map[string]int{}
	for _, host := range hosts {
		node, ok := storageCephNodeByName(cluster, host)
		if ok && topology.NodeHasRole(node, role) {
			counts[node.Site]++
		}
	}
	var errs []string
	for _, site := range dataSites {
		if counts[site] < 2 {
			errs = append(errs, fmt.Sprintf("%s must include at least two %s-capable hosts in data site %q for stretch-mode availability", prefix, role, site))
		}
	}
	return errs
}

func validateStorageCephBootstrapPublicNetwork(prefix string, cluster v1alpha1.StorageCluster, machines map[string]v1alpha1.Machine) []string {
	ceph := cluster.Spec.Ceph
	if ceph == nil || len(ceph.Networks.PublicCIDRs) == 0 {
		return nil
	}
	host := ceph.Cephadm.Bootstrap.Node
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
	for _, node := range ceph.Topology.Nodes {
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
		errs = append(errs, fmt.Sprintf("%s.networks.clusterCIDRs: Ceph OSD host %q (Machine/%s) configures no interface address inside the declared cluster network (%s); ceph-osd binds its replication socket inside cluster_network and, finding no local address there, aborts with 'unable to find any IP address in network(s)', so every OSD on the host stays down and stray outside the CRUSH map; add an interfaceAddresses entry (matching prefixLength) that places the node on the cluster network, or drop clusterCIDRs to run replication over the public network", prefix, node.Name, machine.Metadata.Name, strings.Join(clusters, ",")))
	}
	return errs
}

func storageCephHostRunsOSD(node v1alpha1.StorageCephNode) bool {
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
	for _, cluster := range state.StorageClusters {
		if cluster.Spec.Ceph == nil {
			continue
		}
		for i, node := range cluster.Spec.Ceph.Topology.Nodes {
			if !topology.NodeHasRole(node, v1alpha1.StorageCephRoleOSD) || (len(node.Devices) == 0 && node.OSD == nil) {
				continue
			}
			mark(cluster.Metadata.Name, "osd", "data-"+stateview.NodeShortName(node.Name), fmt.Sprintf("StorageCluster/%s spec.ceph.topology.nodes[%d] per-host OSD service", cluster.Metadata.Name, i))
		}
		for i, dg := range cluster.Spec.Ceph.Topology.OSDDrivegroups {
			mark(cluster.Metadata.Name, "osd", dg.ServiceID, fmt.Sprintf("StorageCluster/%s spec.ceph.topology.osdDrivegroups[%d].serviceID", cluster.Metadata.Name, i))
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
	for _, host := range cluster.Spec.Ceph.Topology.Nodes {
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

func storageCephNodeByName(cluster v1alpha1.StorageCluster, name string) (v1alpha1.StorageCephNode, bool) {
	if cluster.Spec.Ceph == nil {
		return v1alpha1.StorageCephNode{}, false
	}
	for _, node := range cluster.Spec.Ceph.Topology.Nodes {
		if node.Name == name || stateview.NodeShortName(node.Name) == name {
			return node, true
		}
	}
	return v1alpha1.StorageCephNode{}, false
}

func storageCephMachineRefExists(cluster v1alpha1.StorageCluster, name string) bool {
	if cluster.Spec.Ceph == nil {
		return false
	}
	for _, node := range cluster.Spec.Ceph.Topology.Nodes {
		if node.MachineRef.Name == name {
			return true
		}
	}
	return false
}

func storageCephNodeRolesOnly(node v1alpha1.StorageCephNode, role string) bool {
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
