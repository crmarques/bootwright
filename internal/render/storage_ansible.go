package render

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	addoninputs "github.com/crmarques/bootwright/internal/addons/inputs"
	"github.com/crmarques/bootwright/internal/infra/locality"
	secret "github.com/crmarques/bootwright/internal/runtime/secrets"
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
		seedNode = cluster.Spec.Ceph.Cephadm.Bootstrap.SeedNode
	}
	return StorageNodeHostName(cluster.Metadata.Name, seedNode)
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
	return StorageNodeHostName(cluster.Metadata.Name, nodeName)
}

func storageNodeInventoryEntry(state v1alpha1.State, cluster v1alpha1.StorageCluster, node v1alpha1.StorageCephNode, env *v1alpha1.Environment, paths PathOptions, localPolicy locality.Policy) map[string]any {
	nodeName := node.Name
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
	env := primaryEnvironment(state)
	var out []any
	for _, cluster := range managedStorageClusters(state) {
		ceph := cluster.Spec.Ceph
		provider := cephprovider.Select(cluster, env, paths.SecretsDir)
		asset := StorageAssets("{{ bootwright_rendered_dir }}", v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}})[0]
		entry := map[string]any{
			"name":                cluster.Metadata.Name,
			"seedHost":            StorageSeedHostName(cluster),
			"storageGroup":        StorageClusterGroupName(cluster.Metadata.Name),
			"provider":            cephprovider.Vars(provider),
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
		if clusterSSH := storageClusterSSHVars(state, cluster, env, paths); len(clusterSSH) > 0 {
			entry["clusterSSH"] = clusterSSH
		}
		out = append(out, entry)
	}
	return out
}

func storageClusterSSHVars(state v1alpha1.State, cluster v1alpha1.StorageCluster, env *v1alpha1.Environment, paths PathOptions) map[string]any {
	if len(cluster.Spec.Ceph.Topology.Nodes) == 0 {
		return nil
	}
	machine, ok := topology.NodeMachine(state, cluster, cluster.Spec.Ceph.Topology.Nodes[0].Name)
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
