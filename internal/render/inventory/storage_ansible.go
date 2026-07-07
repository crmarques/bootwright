package inventory

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	addoninputs "github.com/crmarques/bootwright/internal/addons/inputs"
	"github.com/crmarques/bootwright/internal/infra/locality"
	cephrender "github.com/crmarques/bootwright/internal/render/ceph"
	secret "github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/sshtrust"
	stateview "github.com/crmarques/bootwright/internal/state/view"
	"github.com/crmarques/bootwright/internal/storage/cephprovider"
	"github.com/crmarques/bootwright/internal/storage/datafoundation"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

// StorageSeedHostName is the inventory host name of the cephadm bootstrap seed
// node. It is the seed node's regular per-node host name; the seed is not named
// differently from the other storage nodes.
func StorageSeedHostName(cluster v1alpha1.StorageCluster) string {
	seedNode := ""
	if cluster.Spec.Ceph != nil {
		seedNode = cluster.Spec.Ceph.Cephadm.Bootstrap.Host
	}
	return StorageNodeHostName(cluster.Metadata.Name, seedNode)
}

func StorageNodeHostName(clusterName, nodeName string) string {
	return "storage__" + clusterName + "__" + nodeName
}

func StorageClusterGroupName(clusterName string) string {
	return GroupStorageHosts + "_" + inventoryGroupToken(clusterName)
}

func ManagedStorageClusters(state v1alpha1.State) []v1alpha1.StorageCluster {
	var out []v1alpha1.StorageCluster
	for _, cluster := range state.StorageClusters {
		if v1alpha1.StorageClusterManaged(cluster) && cluster.Spec.Ceph != nil {
			out = append(out, cluster)
		}
	}
	return out
}

func storageReferencedHosts(state v1alpha1.State) map[string]bool {
	out := map[string]bool{}
	for _, cluster := range ManagedStorageClusters(state) {
		for _, node := range cluster.Spec.Ceph.Topology.Hosts {
			out[storageInventoryHostName(cluster, node.MachineRef.Name)] = true
		}
	}
	return out
}

func storageClusterHostSets(state v1alpha1.State) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, cluster := range ManagedStorageClusters(state) {
		set := map[string]bool{}
		for _, node := range cluster.Spec.Ceph.Topology.Hosts {
			set[storageInventoryHostName(cluster, node.MachineRef.Name)] = true
		}
		out[StorageClusterGroupName(cluster.Metadata.Name)] = set
	}
	return out
}

func storageInventoryHostName(cluster v1alpha1.StorageCluster, nodeName string) string {
	return StorageNodeHostName(cluster.Metadata.Name, nodeName)
}

// storageOSDReadinessVars renders what the seed's post-apply readiness poll can
// assert about OSD creation: an exact expected OSD count when every managed OSD
// selection names explicit devices, an "at least one" floor when a filter/all
// selection makes the count host-resolved, or skip when no managed OSD service
// creates OSDs. It converts a fire-and-forget `ceph orch apply` into a checked
// step so a zero/short-OSD cluster fails the apply instead of reporting success.
func storageOSDReadinessVars(cluster v1alpha1.StorageCluster) map[string]any {
	mode, count := cephrender.OSDReadinessExpectation(cluster)
	return map[string]any{
		"mode":          mode,
		"expectedCount": count,
	}
}

func storageNodeInventoryEntry(state v1alpha1.State, cluster v1alpha1.StorageCluster, node v1alpha1.StorageCephHost, env *v1alpha1.Environment, paths PathOptions, localPolicy locality.Policy) map[string]any {
	// The Ansible inventory identifies nodes by machine name (stable, and the
	// token the bootstrap seedHost is authored with); cephadm's fully-qualified
	// hostname lives only in the rendered cephadm specs.
	nodeName := node.MachineRef.Name
	entry := map[string]any{}
	if machine, ok := topology.NodeMachine(state, cluster, nodeName); ok && machine.Spec.Access.SSH != nil {
		entry = machineInventoryEntry(machine, env, paths, localPolicy)
	} else {
		entry["ansible_host"] = topology.NodeAddress(state, cluster, nodeName)
	}
	entry["bootwright_host_name"] = storageInventoryHostName(cluster, nodeName)
	entry["bootwright_storage_cluster_name"] = cluster.Metadata.Name
	entry["bootwright_storage_node_name"] = nodeName
	entry["bootwright_storage_seed_host_name"] = StorageSeedHostName(cluster)
	return entry
}

func storageClustersVars(state v1alpha1.State, paths PathOptions) []any {
	env := stateview.Environment(state)
	var out []any
	for _, cluster := range ManagedStorageClusters(state) {
		ceph := cluster.Spec.Ceph
		provider := cephprovider.Select(cluster, state.Entitlements, env, paths.SecretsDir)
		asset := cephrender.StorageAssets("{{ bootwright_rendered_dir }}", v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}})[0]
		entry := map[string]any{
			"name":                cluster.Metadata.Name,
			"seedHost":            StorageSeedHostName(cluster),
			"storageGroup":        StorageClusterGroupName(cluster.Metadata.Name),
			"provider":            cephprovider.Vars(provider),
			"remoteWorkDir":       "/tmp/bootwright-storage-" + cluster.Metadata.Name,
			"resultPath":          "{{ bootwright_ansible_artifacts_dir }}/storage-result.json",
			"clusterNetworkCIDRs": append([]string(nil), ceph.Networks.ClusterCIDRs...),
			"hosts":               storageHostsVars(state, cluster),
			"bootstrap": map[string]any{
				"host":               ceph.Cephadm.Bootstrap.Host,
				"monIP":              topology.NodeAddressByRef(state, cluster, ceph.Cephadm.Bootstrap.Host, ceph.Cephadm.Bootstrap.AddressRef.Name),
				"singleHostDefaults": ceph.Cephadm.Bootstrap.SingleHostDefaults,
			},
			"ceph": map[string]any{
				"bootstrapConfPath":    asset.BootstrapConfPath,
				"bootstrapSpecPath":    asset.BootstrapSpecPath,
				"coreServicesSpecPath": asset.CoreServicesSpecPath,
				"lateServicesSpecPath": asset.LateServicesSpecPath,
				"operationsPath":       asset.OperationsPath,
			},
			"dataFoundationBindings": storageDataFoundationBindingsVars(state, cluster.Metadata.Name),
			"osdReadiness":           storageOSDReadinessVars(cluster),
		}
		if !cephrender.MonitoringEnabled(cluster) {
			entry["skipMonitoringStack"] = true
		}
		if clusterSSH := storageClusterSSHVars(state, cluster, env, paths); len(clusterSSH) > 0 {
			entry["clusterSSH"] = clusterSSH
		}
		if management := storageManagementVars(cluster, env, paths); management != nil {
			entry["management"] = management
		}
		out = append(out, entry)
	}
	return out
}

// storageClusterSSHVars renders the cephadm cluster SSH identity: the key
// cephadm distributes and reaches every host with. When spec.ceph.cephadm
// .clusterSSHKeyRef is set it is the explicit source (independent of any node's
// access key), and the controller-side known_hosts is the cluster-wide managed
// trust store so it covers every host cephadm adds. Omitted, it falls back to
// the first topology host's access SSH key — the legacy behavior the
// uniform-key validation backs.
func storageClusterSSHVars(state v1alpha1.State, cluster v1alpha1.StorageCluster, env *v1alpha1.Environment, paths PathOptions) map[string]any {
	if len(cluster.Spec.Ceph.Topology.Hosts) == 0 {
		return nil
	}
	if ref := cluster.Spec.Ceph.Cephadm.ClusterSSHKeyRef.Name; ref != "" {
		out := map[string]any{}
		if user := cluster.Spec.Ceph.Cephadm.ClusterSSHUser; user != "" {
			out["user"] = user
		}
		if privatePath := secret.ResolveSSHPrivateKeyPath(ref, env, paths.SecretsDir); privatePath != "" {
			out["privateKeyPath"] = privatePath
		}
		if publicPath := secret.ResolveSSHPublicKeyPath(ref, env, paths.SecretsDir); publicPath != "" {
			out["publicKeyPath"] = publicPath
		}
		if knownHostsPath := sshtrust.KnownHostsPathForSecrets(paths.trustSecretsDir()); knownHostsPath != "" {
			out["knownHostsPath"] = knownHostsPath
		}
		return out
	}
	machine, ok := topology.NodeMachine(state, cluster, cluster.Spec.Ceph.Topology.Hosts[0].Hostname)
	if !ok || machine.Spec.Access.SSH == nil {
		return nil
	}
	out := map[string]any{}
	if machine.Spec.Access.SSH.User != "" {
		out["user"] = machine.Spec.Access.SSH.User
	}
	if privatePath := secret.ResolveSSHPrivateKeyPath(machine.Spec.Access.SSH.KeyRef.Name, env, paths.SecretsDir); privatePath != "" {
		out["privateKeyPath"] = privatePath
	}
	if publicPath := secret.ResolveSSHPublicKeyPath(machine.Spec.Access.SSH.KeyRef.Name, env, paths.SecretsDir); publicPath != "" {
		out["publicKeyPath"] = publicPath
	}
	if knownHostsPath := machineKnownHostsPath(machine, env, paths); knownHostsPath != "" {
		out["knownHostsPath"] = knownHostsPath
	}
	return out
}

// storageManagementVars renders the secret-bearing management gateway
// (TLS and/or oauth2-proxy) the static render defers: the resolved gateway
// placement and VIP plus the staged secret file paths. The dedicated
// management-services apply step assembles the cephadm spec from these, inlining
// the secrets, so they never appear in a locally-rendered file. Returns nil when
// the gateway carries no secrets (the static render handles it).
func storageManagementVars(cluster v1alpha1.StorageCluster, env *v1alpha1.Environment, paths PathOptions) map[string]any {
	if !cephrender.ManagementHasSecrets(cluster) {
		return nil
	}
	mgmt := cluster.Spec.Ceph.Management
	endpoint, ok := topology.ManagementIngressEndpoint(mgmt.Ingress)
	if !ok {
		return nil
	}
	hosts := topology.ResolvePlacement(cluster, mgmt.Ingress.Placement, v1alpha1.StorageCephRoleIngress)
	if len(hosts) == 0 {
		return nil
	}
	port := mgmt.Port
	if port == 0 {
		port = topology.CephManagementDefaultPort
	}
	out := map[string]any{
		"port":       port,
		"virtualIP":  endpoint.Address,
		"hosts":      hosts,
		"enableAuth": mgmt.EnableAuth != nil && *mgmt.EnableAuth,
	}
	if mgmt.TLS != nil {
		out["tls"] = map[string]any{
			"certificatePath": secret.ResolvePath(mgmt.TLS.CertificateRef.Name, env, paths.SecretsDir),
			"keyPath":         secret.ResolveTLSKeyPath(mgmt.TLS.KeyRef.Name, env, paths.SecretsDir),
		}
	}
	if o := mgmt.OAuth2Proxy; o != nil {
		oauth := map[string]any{
			"providerDisplayName": o.ProviderDisplayName,
			"clientId":            o.ClientID,
			"oidcIssuerURL":       o.OIDCIssuerURL,
			"clientSecretPath":    secret.ResolvePath(o.ClientSecretRef.Name, env, paths.SecretsDir),
		}
		if o.RedirectURL != "" {
			oauth["redirectURL"] = o.RedirectURL
		}
		if o.HTTPSAddress != "" {
			oauth["httpsAddress"] = o.HTTPSAddress
		}
		if len(o.AllowlistDomains) > 0 {
			oauth["allowlistDomains"] = append([]string(nil), o.AllowlistDomains...)
		}
		if o.CookieSecretRef.Name != "" {
			oauth["cookieSecretPath"] = secret.ResolvePath(o.CookieSecretRef.Name, env, paths.SecretsDir)
		}
		out["oauth2Proxy"] = oauth
	}
	return out
}

func storageHostsVars(state v1alpha1.State, cluster v1alpha1.StorageCluster) []any {
	var out []any
	for _, node := range cluster.Spec.Ceph.Topology.Hosts {
		out = append(out, map[string]any{
			"hostname":      node.MachineRef.Name,
			"inventoryHost": storageInventoryHostName(cluster, node.MachineRef.Name),
			"address":       topology.NodeAddress(state, cluster, node.MachineRef.Name),
			// devices carries every explicit OSD block-device path — the devices
			// shorthand AND the drivegroup osd: form's data/db/wal paths (and any
			// covering fleet osdDrivegroup) — so the device-empty gate, the OSD
			// ownership marker, and the destroy wipe cover the drivegroup form too,
			// not only the shorthand. It is gate/marker-only; the rendered OSD spec
			// reads node.Devices/node.OSD directly.
			"devices": cephrender.OSDGateDevicePaths(cluster, node),
		})
	}
	return out
}

func storageDataFoundationBindingsVars(state v1alpha1.State, storageCluster string) []any {
	var out []any
	for _, effect := range addoninputs.StorageExportAttachments(state) {
		export := effect.Export
		if export.Spec.StorageClusterRef.Name != storageCluster || export.Spec.DataFoundation == nil {
			continue
		}
		if cluster, ok := stateview.ClusterByName(state, storageCluster); ok && !datafoundation.ExternalDetailsSourceGenerated(export, cluster) {
			continue
		}
		entry := map[string]any{
			"cluster": effect.Binding.Spec.ClusterRef.Name,
			"addon":   effect.Addon.AddonRef.Name,
			"input":   effect.Input.Name,
			"export":  export.Metadata.Name,
		}
		if export.Spec.DataFoundation != nil && export.Spec.DataFoundation.ObjectGatewayRef.Name != "" {
			entry["objectGateway"] = export.Spec.DataFoundation.ObjectGatewayRef.Name
		}
		out = append(out, entry)
	}
	return out
}
