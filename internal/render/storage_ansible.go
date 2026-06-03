package render

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	addoninputs "github.com/crmarques/bootwright/internal/addons/inputs"
	secret "github.com/crmarques/bootwright/internal/runtime/secrets"
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

func storageNodeInventoryEntry(state v1alpha1.State, cluster v1alpha1.StorageCluster, nodeName string, env *v1alpha1.Environment, secretsDir string) map[string]any {
	nodeSSH := cluster.Spec.Ceph.Cephadm.NodeSSH
	entry := map[string]any{
		"ansible_host":                      storageNodeAddress(state, cluster, nodeName),
		"ansible_user":                      storageSSHUser(nodeSSH),
		"bootwright_host_name":              storageInventoryHostName(cluster, nodeName),
		"bootwright_storage_cluster_name":   cluster.Metadata.Name,
		"bootwright_storage_node_name":      nodeName,
		"bootwright_storage_seed_host_name": StorageSeedHostName(cluster.Metadata.Name),
	}
	if path := storageSSHPrivateKeyPath(nodeSSH, env, secretsDir); path != "" {
		entry["ansible_ssh_private_key_file"] = path
	}
	return entry
}

func storageClustersVars(state v1alpha1.State, secretsDir string) []any {
	env := primaryEnvironment(state)
	var out []any
	for _, cluster := range managedStorageClusters(state) {
		ceph := cluster.Spec.Ceph
		asset := StorageAssets("{{ bootwright_rendered_dir }}", v1alpha1.State{StorageClusters: []v1alpha1.StorageCluster{cluster}})[0]
		clusterSSH := ceph.Cephadm.ClusterSSH
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
				"monIP":    storageMachineIP(state, cluster, ceph.Cephadm.Bootstrap.MonIP),
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
		if user := storageSSHUser(clusterSSH); user != "" {
			entry["clusterSSH"] = map[string]any{"user": user}
		}
		if privatePath := storageSSHPrivateKeyPath(clusterSSH, env, secretsDir); privatePath != "" {
			clusterVars, _ := entry["clusterSSH"].(map[string]any)
			if clusterVars == nil {
				clusterVars = map[string]any{}
				entry["clusterSSH"] = clusterVars
			}
			clusterVars["privateKeyPath"] = privatePath
		}
		if publicPath := storageSSHPublicKeyPath(clusterSSH, env, secretsDir); publicPath != "" {
			clusterVars, _ := entry["clusterSSH"].(map[string]any)
			if clusterVars == nil {
				clusterVars = map[string]any{}
				entry["clusterSSH"] = clusterVars
			}
			clusterVars["publicKeyPath"] = publicPath
		}
		out = append(out, entry)
	}
	return out
}

func storageNodesVars(state v1alpha1.State, cluster v1alpha1.StorageCluster) []any {
	var out []any
	for _, node := range cluster.Spec.Ceph.Topology.Nodes {
		out = append(out, map[string]any{
			"name":          node.Name,
			"inventoryHost": storageInventoryHostName(cluster, node.Name),
			"address":       storageNodeAddress(state, cluster, node.Name),
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

func storageMachineIP(state v1alpha1.State, cluster v1alpha1.StorageCluster, ref v1alpha1.StorageMachineIPRef) string {
	for _, infra := range state.ClusterInfras {
		if infra.Metadata.Name != ref.MachineRef.ClusterInfra {
			continue
		}
		for _, machine := range infra.Spec.Components.Machines {
			if machine.Name != ref.MachineRef.Name {
				continue
			}
			family := "ipv4"
			if ref.Family == "ipv6" {
				family = "ipv6"
			}
			return networkConfigInterfaceIP(agentNetworkConfig(state, infra, machine, ""), ref.Interface, family)
		}
	}
	return ""
}

func storageSSHUser(ssh v1alpha1.StorageSSHSpec) string {
	if ssh.User != "" {
		return ssh.User
	}
	return "root"
}

func storageSSHPrivateKeyPath(ssh v1alpha1.StorageSSHSpec, env *v1alpha1.Environment, secretsDir string) string {
	name := ssh.PrivateKeyRef.Name
	if name == "" {
		name = ssh.KeyPairRef.Name
	}
	return secret.ResolveSSHPrivateKeyPath(name, env, secretsDir)
}

func storageSSHPublicKeyPath(ssh v1alpha1.StorageSSHSpec, env *v1alpha1.Environment, secretsDir string) string {
	if ssh.KeyPairRef.Name == "" {
		return ""
	}
	return secret.ResolveSSHPublicKeyPath(ssh.KeyPairRef.Name, env, secretsDir)
}
