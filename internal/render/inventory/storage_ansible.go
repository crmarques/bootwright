package inventory

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/entitlements"
	"github.com/crmarques/bootwright/internal/infra/locality"
	cephrender "github.com/crmarques/bootwright/internal/render/ceph"
	secret "github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/sshtrust"
	stateview "github.com/crmarques/bootwright/internal/state/view"
	"github.com/crmarques/bootwright/internal/storage/cephprovider"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

func StorageSeedHostName(cluster v1alpha1.StorageCluster) string {
	seedNode := ""
	if cluster.Spec.Ceph != nil {
		seedNode = cluster.Spec.Ceph.Cephadm.Bootstrap.Node
		if node, ok := topology.CephNodeByName(cluster, seedNode); ok && node.MachineRef.Name != "" {
			seedNode = node.MachineRef.Name
		}
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
		for _, node := range cluster.Spec.Ceph.Topology.Nodes {
			out[storageInventoryHostName(cluster, node.MachineRef.Name)] = true
		}
	}
	return out
}

func storageClusterHostSets(state v1alpha1.State) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, cluster := range ManagedStorageClusters(state) {
		set := map[string]bool{}
		for _, node := range cluster.Spec.Ceph.Topology.Nodes {
			set[storageInventoryHostName(cluster, node.MachineRef.Name)] = true
		}
		out[StorageClusterGroupName(cluster.Metadata.Name)] = set
	}
	return out
}

func storageInventoryHostName(cluster v1alpha1.StorageCluster, nodeName string) string {
	return StorageNodeHostName(cluster.Metadata.Name, nodeName)
}

func storageOSDReadinessVars(cluster v1alpha1.StorageCluster) map[string]any {
	mode, count, dynamicHosts := cephrender.OSDReadinessExpectation(cluster)
	return map[string]any{
		"mode":          mode,
		"expectedCount": count,
		"dynamicHosts":  dynamicHosts,
	}
}

func storageNodeInventoryEntry(state v1alpha1.State, cluster v1alpha1.StorageCluster, node v1alpha1.StorageCephNode, env *v1alpha1.Environment, paths PathOptions, localPolicy locality.Policy) map[string]any {
	nodeName := node.MachineRef.Name
	entry := map[string]any{}
	machine, machineOK := topology.NodeMachine(state, cluster, nodeName)
	if machineOK && machine.Spec.Access.SSH != nil {
		entry = machineInventoryEntry(state, machine, env, paths, localPolicy)
	} else {
		entry["ansible_host"] = topology.NodeAddress(state, cluster, nodeName)
	}
	entry["bootwright_host_name"] = storageInventoryHostName(cluster, nodeName)
	entry["bootwright_storage_cluster_name"] = cluster.Metadata.Name
	entry["bootwright_storage_node_name"] = nodeName
	entry["bootwright_storage_seed_host_name"] = StorageSeedHostName(cluster)
	if machineOK {
		entry["bootwright_os_provided"] = v1alpha1.MachineOSProvided(machine)
	}
	return entry
}

func storageClustersVars(state v1alpha1.State, paths PathOptions) []any {
	env := stateview.Environment(state)
	idx := secret.NewIndex(state)
	var out []any
	for _, cluster := range ManagedStorageClusters(state) {
		ceph := cluster.Spec.Ceph
		provider := storageCephProvider(state, cluster, idx, paths.SecretsDir)
		asset := cephrender.StorageAssets("{{ bootwright_rendered_dir }}", v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}})[0]
		entry := map[string]any{
			"name":                cluster.Metadata.Name,
			"seedHost":            StorageSeedHostName(cluster),
			"storageGroup":        StorageClusterGroupName(cluster.Metadata.Name),
			"provider":            cephprovider.Vars(provider),
			"remoteWorkDir":       "/tmp/bootwright-storage-" + cluster.Metadata.Name,
			"clusterNetworkCIDRs": append([]string(nil), ceph.Networks.ClusterCIDRs...),
			"hosts":               storageHostsVars(state, cluster),
			"bootstrap": map[string]any{
				"host":               topology.CanonicalHostname(cluster, ceph.Cephadm.Bootstrap.Node),
				"monIP":              topology.NodeAddressByRef(state, cluster, ceph.Cephadm.Bootstrap.Node, ceph.Cephadm.Bootstrap.AddressRef.Name),
				"singleHostDefaults": ceph.Cephadm.Bootstrap.SingleHostDefaults,
			},
			"ceph": map[string]any{
				"bootstrapConfPath":    asset.BootstrapConfPath,
				"bootstrapSpecPath":    asset.BootstrapSpecPath,
				"coreServicesSpecPath": asset.CoreServicesSpecPath,
				"lateServicesSpecPath": asset.LateServicesSpecPath,
				"operationsPath":       asset.OperationsPath,
			},
			"osdReadiness": storageOSDReadinessVars(cluster),
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

func StorageCephProvider(state v1alpha1.State, cluster v1alpha1.StorageCluster) cephprovider.Provider {
	return storageCephProvider(state, cluster, secret.NewIndex(state), "")
}

func storageCephProvider(state v1alpha1.State, cluster v1alpha1.StorageCluster, idx secret.Index, secretsDir string) cephprovider.Provider {
	provider := cephprovider.Select(cluster, state.Entitlements, idx, secretsDir)
	if ent, ok := v1alpha1.StorageClusterOSSubscriptionEntitlement(cluster, state); ok {
		provider.OSRegistration, _ = entitlements.Resolve(state.Entitlements, idx, ent.Metadata.Name, "", secretsDir)
	}
	return provider
}

func storageClusterSSHVars(state v1alpha1.State, cluster v1alpha1.StorageCluster, env *v1alpha1.Environment, paths PathOptions) map[string]any {
	if len(cluster.Spec.Ceph.Topology.Nodes) == 0 {
		return nil
	}
	if ref := cluster.Spec.Ceph.Cephadm.ClusterSSHKeyRef.Name; ref != "" {
		out := map[string]any{}
		if user := cluster.Spec.Ceph.Cephadm.ClusterSSHUser; user != "" {
			out["user"] = user
		}
		if privatePath := secret.ResolveSSHPrivateKeyPath(ref, paths.SecretIndex, paths.SecretsDir); privatePath != "" {
			out["privateKeyPath"] = privatePath
		}
		if publicPath := secret.ResolveSSHPublicKeyPath(ref, paths.SecretIndex, paths.SecretsDir); publicPath != "" {
			out["publicKeyPath"] = publicPath
		}
		if knownHostsPath := sshtrust.KnownHostsPathForSecrets(paths.trustSecretsDir()); knownHostsPath != "" {
			out["knownHostsPath"] = knownHostsPath
		}
		return out
	}
	machine, ok := topology.NodeMachine(state, cluster, cluster.Spec.Ceph.Topology.Nodes[0].Name)
	if !ok || machine.Spec.Access.SSH == nil {
		return nil
	}
	out := map[string]any{}
	if machine.Spec.Access.SSH.User != "" {
		out["user"] = machine.Spec.Access.SSH.User
	}
	if privatePath := secret.ResolveSSHPrivateKeyPath(machine.Spec.Access.SSH.KeyRef.Name, paths.SecretIndex, paths.SecretsDir); privatePath != "" {
		out["privateKeyPath"] = privatePath
	}
	if publicPath := secret.ResolveSSHPublicKeyPath(machine.Spec.Access.SSH.KeyRef.Name, paths.SecretIndex, paths.SecretsDir); publicPath != "" {
		out["publicKeyPath"] = publicPath
	}
	if knownHostsPath := machineKnownHostsPath(machine, paths); knownHostsPath != "" {
		out["knownHostsPath"] = knownHostsPath
	}
	return out
}

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
			"certificatePath": secret.ResolvePath(mgmt.TLS.CertificateRef.Name, paths.SecretIndex, paths.SecretsDir),
			"keyPath":         secret.ResolveTLSKeyPath(mgmt.TLS.KeyRef.Name, paths.SecretIndex, paths.SecretsDir),
		}
	}
	if o := mgmt.OAuth2Proxy; o != nil {
		oauth := map[string]any{
			"providerDisplayName": o.ProviderDisplayName,
			"clientId":            o.ClientID,
			"oidcIssuerURL":       o.OIDCIssuerURL,
			"clientSecretPath":    secret.ResolvePath(o.ClientSecretRef.Name, paths.SecretIndex, paths.SecretsDir),
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
			oauth["cookieSecretPath"] = secret.ResolvePath(o.CookieSecretRef.Name, paths.SecretIndex, paths.SecretsDir)
		}
		out["oauth2Proxy"] = oauth
	}
	return out
}

func storageHostsVars(state v1alpha1.State, cluster v1alpha1.StorageCluster) []any {
	var out []any
	for _, node := range cluster.Spec.Ceph.Topology.Nodes {
		host := map[string]any{
			"hostname":      node.MachineRef.Name,
			"cephHostname":  node.Name,
			"inventoryHost": storageInventoryHostName(cluster, node.MachineRef.Name),
			"address":       topology.NodeAddress(state, cluster, node.MachineRef.Name),
			"devices":       cephrender.OSDGateDevicePaths(cluster, node),
		}
		if topology.OSDHostUsesAllDevices(cluster, node) {
			host["osdReclaimAll"] = true
		}
		out = append(out, host)
	}
	return out
}
