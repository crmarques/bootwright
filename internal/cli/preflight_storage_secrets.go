package cli

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/runtime/secrets"
	"github.com/crmarques/bootwright/internal/storage/datafoundation"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

func collectStorageSecretRefRequirements(state v1alpha1.State) []secretRefRequirement {
	var out []secretRefRequirement
	for _, cluster := range state.StorageClusters {
		if cluster.Spec.Ceph == nil {
			continue
		}
		if ref := cluster.Spec.Ceph.Cephadm.Registry.CredentialsRef; ref.Name != "" {
			out = append(out, secretRefRequirement{
				refName: ref.Name,
				label:   cluster.Metadata.Name + " cephadm registry credentialsRef",
				phases:  []string{"storage-cluster"},
				role:    secret.MaterialPrimary,
			})
		}
		if ref := cluster.Spec.Ceph.Cephadm.Registry.TrustBundleRef; ref.Name != "" {
			out = append(out, secretRefRequirement{
				refName: ref.Name,
				label:   cluster.Metadata.Name + " cephadm registry trustBundleRef",
				phases:  []string{"storage-cluster"},
				role:    secret.MaterialPrimary,
			})
		}
		for _, node := range cluster.Spec.Ceph.Topology.Nodes {
			host, ok := topology.NodeHost(state, cluster, node.Name)
			if !ok {
				continue
			}
			out = append(out, hostSSHSecretRequirements(fmt.Sprintf("StorageCluster/%s node/%s Host/%s", cluster.Metadata.Name, node.Name, host.Metadata.Name), []string{"storage-cluster"}, host, true)...)
		}
	}
	clusterByName := map[string]v1alpha1.StorageCluster{}
	for _, cluster := range state.StorageClusters {
		clusterByName[cluster.Metadata.Name] = cluster
	}
	for _, export := range state.StorageExports {
		if fromSecret := datafoundation.ExternalDetailsSourceFromSecret(export); fromSecret != "" {
			out = append(out, secretRefRequirement{
				refName: fromSecret,
				label:   fmt.Sprintf("StorageExport/%s externalDetails.fromSecret", export.Metadata.Name),
				phases:  []string{"addons"},
				role:    secret.MaterialPrimary,
			})
		}
		ssh := datafoundation.ExternalDetailsSourceSSH(export)
		if ssh == nil {
			continue
		}
		for _, ref := range ssh.HostRefs {
			host, ok := hostByName(state, ref.Name)
			if !ok {
				continue
			}
			out = append(out, hostSSHSecretRequirements(fmt.Sprintf("StorageExport/%s externalDetails.sshExecution Host/%s", export.Metadata.Name, host.Metadata.Name), []string{"addons"}, host, false)...)
		}
		if len(ssh.HostRefs) == 0 {
			cluster, ok := clusterByName[export.Spec.StorageClusterRef.Name]
			if ok && cluster.Spec.Ceph != nil {
				for _, node := range cluster.Spec.Ceph.Topology.Nodes {
					if node.Name != cluster.Spec.Ceph.Cephadm.Bootstrap.SeedNode {
						continue
					}
					host, ok := topology.NodeHost(state, cluster, node.Name)
					if !ok {
						continue
					}
					out = append(out, hostSSHSecretRequirements(fmt.Sprintf("StorageExport/%s externalDetails.sshExecution seed Host/%s", export.Metadata.Name, host.Metadata.Name), []string{"addons"}, host, false)...)
				}
			}
		}
	}
	return out
}

func hostByName(state v1alpha1.State, name string) (v1alpha1.Host, bool) {
	for _, host := range state.Hosts {
		if host.Metadata.Name == name {
			return host, true
		}
	}
	return v1alpha1.Host{}, false
}

func hostSSHSecretRequirements(label string, phases []string, host v1alpha1.Host, requirePair bool) []secretRefRequirement {
	var out []secretRefRequirement
	if host.Spec.SSH == nil {
		return out
	}
	if host.Spec.SSH.KeyRef.Name != "" {
		req := secretRefRequirement{
			refName: host.Spec.SSH.KeyRef.Name,
			label:   label + " keyRef",
			phases:  phases,
			role:    secret.MaterialSSHPrivate,
		}
		if requirePair {
			req.role = secret.MaterialSSHPublic
			req.sshPair = true
		}
		out = append(out, req)
	}
	if host.Spec.SSH.KnownHostsRef.Name != "" {
		out = append(out, secretRefRequirement{
			refName: host.Spec.SSH.KnownHostsRef.Name,
			label:   label + " knownHostsRef",
			phases:  phases,
			role:    secret.MaterialPrimary,
		})
	}
	return out
}
