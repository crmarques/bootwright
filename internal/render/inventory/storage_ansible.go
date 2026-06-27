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
		provider := cephprovider.Select(cluster, env, paths.SecretsDir)
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
		}
		if !cephrender.MonitoringEnabled(cluster) {
			entry["skipMonitoringStack"] = true
		}
		if clusterSSH := storageClusterSSHVars(state, cluster, env, paths); len(clusterSSH) > 0 {
			entry["clusterSSH"] = clusterSSH
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

func storageHostsVars(state v1alpha1.State, cluster v1alpha1.StorageCluster) []any {
	var out []any
	for _, node := range cluster.Spec.Ceph.Topology.Hosts {
		out = append(out, map[string]any{
			"hostname":      node.MachineRef.Name,
			"inventoryHost": storageInventoryHostName(cluster, node.MachineRef.Name),
			"address":       topology.NodeAddress(state, cluster, node.MachineRef.Name),
			"devices":       append([]string(nil), node.Devices...),
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
