package render

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	secret "github.com/crmarques/bootwright/internal/runtime/secrets"
)

func StorageSeedHostName(clusterName string) string {
	return "storage__" + clusterName
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
		out[StorageSeedHostName(cluster.Metadata.Name)] = true
	}
	return out
}

func storageSeedHostInventoryEntry(state v1alpha1.State, cluster v1alpha1.StorageCluster, env *v1alpha1.Environment, secretsDir string) map[string]any {
	nodeSSH := cluster.Spec.Ceph.Cephadm.NodeSSH
	entry := map[string]any{
		"ansible_host":                      storageMachineIP(state, cluster, cluster.Spec.Ceph.Cephadm.Bootstrap.MonIP),
		"ansible_user":                      storageSSHUser(nodeSSH),
		"bootwright_host_name":              StorageSeedHostName(cluster.Metadata.Name),
		"bootwright_storage_cluster_name":   cluster.Metadata.Name,
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
			"remoteWorkDir":       "/tmp/bootwright-storage-" + cluster.Metadata.Name,
			"resultPath":          "{{ bootwright_ansible_artifacts_dir }}/storage-result.json",
			"clusterNetworkCIDRs": append([]string(nil), ceph.Networks.ClusterCIDRs...),
			"bootstrap": map[string]any{
				"seedNode": ceph.Cephadm.Bootstrap.SeedNode,
				"monIP":    storageMachineIP(state, cluster, ceph.Cephadm.Bootstrap.MonIP),
			},
			"ceph": map[string]any{
				"bootstrapSpecPath": asset.BootstrapSpecPath,
				"servicesSpecPath":  asset.ServicesSpecPath,
				"operationsPath":    asset.OperationsPath,
			},
			"dataFoundationBindings": storageDataFoundationBindingsVars(state, cluster.Metadata.Name),
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

func storageDataFoundationBindingsVars(state v1alpha1.State, storageCluster string) []any {
	exports := map[string]v1alpha1.StorageExport{}
	for _, export := range state.StorageExports {
		if export.Spec.StorageClusterRef.Name == storageCluster && export.Spec.DataFoundation != nil {
			exports[export.Metadata.Name] = export
		}
	}
	var out []any
	for _, binding := range state.ClusterAddonBindings {
		for _, storage := range binding.Spec.Storage {
			export, ok := exports[storage.ExportRef.Name]
			if !ok {
				continue
			}
			entry := map[string]any{
				"cluster": binding.Spec.ClusterRef.Name,
				"binding": binding.Metadata.Name,
				"storage": storage.Name,
				"export":  export.Metadata.Name,
			}
			if export.Spec.DataFoundation != nil && export.Spec.DataFoundation.ObjectGatewayRef.Name != "" {
				entry["objectGateway"] = export.Spec.DataFoundation.ObjectGatewayRef.Name
			}
			out = append(out, entry)
		}
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
			for _, address := range machine.NetworkConfig.Addresses {
				if ref.Interface != "" && address.Interface != ref.Interface {
					continue
				}
				if ref.Family == "ipv6" && len(address.IPv6) > 0 {
					return address.IPv6[0].IP
				}
				if ref.Family != "ipv6" && len(address.IPv4) > 0 {
					return address.IPv4[0].IP
				}
			}
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
