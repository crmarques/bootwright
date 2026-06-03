package cli

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/runtime/secrets"
	"github.com/crmarques/bootwright/internal/storage/datafoundation"
)

func collectStorageSecretRefRequirements(state v1alpha1.State) []secretRefRequirement {
	var out []secretRefRequirement
	for _, cluster := range state.StorageClusters {
		if cluster.Spec.Ceph == nil {
			continue
		}
		out = append(out, storageSSHRequirements(cluster.Metadata.Name, "nodeSSH", cluster.Spec.Ceph.Cephadm.NodeSSH)...)
		out = append(out, storageSSHRequirements(cluster.Metadata.Name, "clusterSSH", cluster.Spec.Ceph.Cephadm.ClusterSSH)...)
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
	}
	hostByName := map[string]v1alpha1.Host{}
	for _, host := range state.Hosts {
		hostByName[host.Metadata.Name] = host
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
		out = append(out, secretRefRequirement{
			refName: ssh.KnownHostsRef.Name,
			label:   fmt.Sprintf("StorageExport/%s externalDetails.sshExecution knownHostsRef", export.Metadata.Name),
			phases:  []string{"addons"},
			role:    secret.MaterialPrimary,
		})
		for _, ref := range ssh.HostRefs {
			host, ok := hostByName[ref.Name]
			if !ok || host.Spec.SSH == nil || host.Spec.SSH.KeyRef.Name == "" {
				continue
			}
			out = append(out, secretRefRequirement{
				refName: host.Spec.SSH.KeyRef.Name,
				label:   fmt.Sprintf("StorageExport/%s externalDetails.sshExecution Host/%s keyRef", export.Metadata.Name, host.Metadata.Name),
				phases:  []string{"addons"},
				role:    secret.MaterialSSHPrivate,
			})
		}
		if len(ssh.HostRefs) == 0 {
			cluster, ok := clusterByName[export.Spec.StorageClusterRef.Name]
			if ok && cluster.Spec.Ceph != nil {
				reqs := storageSSHRequirements(export.Metadata.Name, "externalDetails.sshExecution nodeSSH", cluster.Spec.Ceph.Cephadm.NodeSSH)
				for i := range reqs {
					reqs[i].phases = []string{"addons"}
				}
				out = append(out, reqs...)
			}
		}
	}
	return out
}

func storageSSHRequirements(clusterName, field string, ssh v1alpha1.StorageSSHSpec) []secretRefRequirement {
	var out []secretRefRequirement
	if ssh.KeyPairRef.Name != "" {
		out = append(out, secretRefRequirement{
			refName: ssh.KeyPairRef.Name,
			label:   clusterName + " " + field + " keyPairRef",
			phases:  []string{"storage-cluster"},
			role:    secret.MaterialSSHPublic,
			sshPair: true,
		})
	}
	if ssh.PrivateKeyRef.Name != "" {
		out = append(out, secretRefRequirement{
			refName: ssh.PrivateKeyRef.Name,
			label:   clusterName + " " + field + " privateKeyRef",
			phases:  []string{"storage-cluster"},
			role:    secret.MaterialSSHPrivate,
		})
	}
	return out
}
