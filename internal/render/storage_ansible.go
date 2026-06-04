package render

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	addoninputs "github.com/crmarques/bootwright/internal/addons/inputs"
	"github.com/crmarques/bootwright/internal/infra/locality"
	secret "github.com/crmarques/bootwright/internal/runtime/secrets"
	"github.com/crmarques/bootwright/internal/storage/datafoundation"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

func StorageSeedHostName(clusterName string) string {
	return "storage__" + clusterName
}

func StorageNodeHostName(clusterName, nodeName string) string {
	return "storage__" + clusterName + "__" + nodeName
}

func StorageClusterGroupName(clusterName string) string {
	return GroupStorageHosts + "_" + inventoryGroupToken(clusterName)
}

func managedStorageClusters(state v1alpha1.State) []v1alpha1.StorageCluster {
	var out []v1alpha1.StorageCluster
	for _, cluster := range state.StorageClusters {
		if storageClusterManaged(cluster) && cluster.Spec.Ceph != nil {
			out = append(out, cluster)
		}
	}
	return out
}

func storageReferencedHosts(state v1alpha1.State) map[string]bool {
	out := map[string]bool{}
	for _, cluster := range managedStorageClusters(state) {
		for _, node := range cluster.Spec.Ceph.Topology.Nodes {
			out[storageInventoryHostName(cluster, node.Name)] = true
		}
	}
	return out
}

func storageClusterHostSets(state v1alpha1.State) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, cluster := range managedStorageClusters(state) {
		set := map[string]bool{}
		for _, node := range cluster.Spec.Ceph.Topology.Nodes {
			set[storageInventoryHostName(cluster, node.Name)] = true
		}
		out[StorageClusterGroupName(cluster.Metadata.Name)] = set
	}
	return out
}

func storageInventoryHostName(cluster v1alpha1.StorageCluster, nodeName string) string {
	if cluster.Spec.Ceph.Cephadm.Bootstrap.SeedNode == nodeName {
		return StorageSeedHostName(cluster.Metadata.Name)
	}
	return StorageNodeHostName(cluster.Metadata.Name, nodeName)
}

func storageNodeInventoryEntry(state v1alpha1.State, cluster v1alpha1.StorageCluster, node v1alpha1.StorageCephNode, env *v1alpha1.Environment, secretsDir string, localPolicy locality.Policy) map[string]any {
	nodeName := node.Name
	entry := map[string]any{}
	if machine, ok := topology.NodeMachine(state, cluster, nodeName); ok && machine.Spec.OS.SSH != nil {
		entry = machineInventoryEntry(machine, env, secretsDir, localPolicy)
	} else {
		entry["ansible_host"] = topology.NodeAddress(state, cluster, nodeName)
	}
	entry["bootwright_host_name"] = storageInventoryHostName(cluster, nodeName)
	entry["bootwright_storage_cluster_name"] = cluster.Metadata.Name
	entry["bootwright_storage_node_name"] = nodeName
	entry["bootwright_storage_seed_host_name"] = StorageSeedHostName(cluster.Metadata.Name)
	return entry
}

func storageClustersVars(state v1alpha1.State, secretsDir string) []any {
	env := primaryEnvironment(state)
	var out []any
	for _, cluster := range managedStorageClusters(state) {
		ceph := cluster.Spec.Ceph
		asset := StorageAssets("{{ bootwright_rendered_dir }}", v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}})[0]
		entry := map[string]any{
			"name":                cluster.Metadata.Name,
			"seedHost":            StorageSeedHostName(cluster.Metadata.Name),
			"storageGroup":        StorageClusterGroupName(cluster.Metadata.Name),
			"remoteWorkDir":       "/tmp/bootwright-storage-" + cluster.Metadata.Name,
			"resultPath":          "{{ bootwright_ansible_artifacts_dir }}/storage-result.json",
			"clusterNetworkCIDRs": append([]string(nil), ceph.Networks.ClusterCIDRs...),
			"nodes":               storageNodesVars(state, cluster),
			"bootstrap": map[string]any{
				"seedNode": ceph.Cephadm.Bootstrap.SeedNode,
				"monIP":    storageNodeIP(state, cluster, ceph.Cephadm.Bootstrap.MonIP),
			},
			"ceph": map[string]any{
				"bootstrapSpecPath":    asset.BootstrapSpecPath,
				"coreServicesSpecPath": asset.CoreServicesSpecPath,
				"lateServicesSpecPath": asset.LateServicesSpecPath,
				"operationsPath":       asset.OperationsPath,
			},
			"dataFoundationBindings": storageDataFoundationBindingsVars(state, cluster.Metadata.Name),
		}
		if registry := storageRegistryVars(ceph.Cephadm.Registry, env, secretsDir); len(registry) > 0 {
			entry["registry"] = registry
		}
		if clusterSSH := storageClusterSSHVars(state, cluster, env, secretsDir); len(clusterSSH) > 0 {
			entry["clusterSSH"] = clusterSSH
		}
		out = append(out, entry)
	}
	return out
}

func storageClusterSSHVars(state v1alpha1.State, cluster v1alpha1.StorageCluster, env *v1alpha1.Environment, secretsDir string) map[string]any {
	if len(cluster.Spec.Ceph.Topology.Nodes) == 0 {
		return nil
	}
	machine, ok := topology.NodeMachine(state, cluster, cluster.Spec.Ceph.Topology.Nodes[0].Name)
	if !ok || machine.Spec.OS.SSH == nil {
		return nil
	}
	out := map[string]any{}
	if machine.Spec.OS.SSH.User != "" {
		out["user"] = machine.Spec.OS.SSH.User
	}
	if privatePath := secret.ResolveSSHPrivateKeyPath(machine.Spec.OS.SSH.KeyRef.Name, env, secretsDir); privatePath != "" {
		out["privateKeyPath"] = privatePath
	}
	if publicPath := secret.ResolveSSHPublicKeyPath(machine.Spec.OS.SSH.KeyRef.Name, env, secretsDir); publicPath != "" {
		out["publicKeyPath"] = publicPath
	}
	if knownHostsPath := machineKnownHostsPath(machine, env, secretsDir); knownHostsPath != "" {
		out["knownHostsPath"] = knownHostsPath
	}
	return out
}

func storageNodesVars(state v1alpha1.State, cluster v1alpha1.StorageCluster) []any {
	var out []any
	for _, node := range cluster.Spec.Ceph.Topology.Nodes {
		out = append(out, map[string]any{
			"name":          node.Name,
			"inventoryHost": storageInventoryHostName(cluster, node.Name),
			"address":       topology.NodeAddress(state, cluster, node.Name),
			"devices":       append([]string(nil), node.Devices...),
		})
	}
	return out
}

func storageRegistryVars(registry v1alpha1.StorageCephadmRegistry, env *v1alpha1.Environment, secretsDir string) map[string]any {
	out := map[string]any{}
	if registry.URL != "" {
		out["url"] = registry.URL
	}
	if registry.CredentialsRef.Name != "" {
		out["credentialsPath"] = secret.ResolveMaterialPath(registry.CredentialsRef.Name, env, secretsDir, secret.MaterialPrimary)
	}
	if registry.TrustBundleRef.Name != "" {
		out["trustBundlePath"] = secret.ResolveMaterialPath(registry.TrustBundleRef.Name, env, secretsDir, secret.MaterialPrimary)
	}
	return out
}

func storageDataFoundationBindingsVars(state v1alpha1.State, storageCluster string) []any {
	exports := map[string]v1alpha1.StorageExport{}
	for _, export := range state.StorageExports {
		if export.Spec.StorageClusterRef.Name == storageCluster && export.Spec.DataFoundation != nil {
			if cluster, ok := topology.ClusterByName(state, storageCluster); ok && !datafoundation.ExternalDetailsSourceGenerated(export, cluster) {
				continue
			}
			exports[export.Metadata.Name] = export
		}
	}
	var out []any
	for _, effect := range addoninputs.EffectBindings(state, v1alpha1.ClusterAddonInputEffectStorageExportAttachment, v1alpha1.ClusterAddonProvidesDataFoundation) {
		exportRef := addoninputs.LocalObjectReferenceValue(effect.Input.Values, "exportRef")
		export, ok := exports[exportRef.Name]
		if !ok {
			continue
		}
		entry := map[string]any{
			"cluster": effect.Binding.Spec.ClusterRef.Name,
			"addon":   effect.Addon.Name,
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

func storageNodeIP(state v1alpha1.State, cluster v1alpha1.StorageCluster, ref v1alpha1.StorageNodeIPRef) string {
	addressName := ref.AddressRef.Name
	if addressName == "" {
		addressName = cluster.Spec.Ceph.Cephadm.AddressRef.Name
	}
	return topology.NodeAddressByRef(state, cluster, ref.NodeRef.Name, addressName)
}
