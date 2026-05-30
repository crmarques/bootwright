package cli

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/runtime/secrets"
)

func collectStorageSecretRefRequirements(state v1alpha1.State) []secretRefRequirement {
	var out []secretRefRequirement
	for _, cluster := range state.StorageClusters {
		if cluster.Spec.Ceph == nil {
			continue
		}
		out = append(out, storageSSHRequirements(cluster.Metadata.Name, "nodeSSH", cluster.Spec.Ceph.Cephadm.NodeSSH)...)
		out = append(out, storageSSHRequirements(cluster.Metadata.Name, "clusterSSH", cluster.Spec.Ceph.Cephadm.ClusterSSH)...)
	}
	return out
}

func storageSSHRequirements(clusterName, field string, ssh v1alpha1.StorageSSHSpec) []secretRefRequirement {
	var out []secretRefRequirement
	if ssh.KeyPairRef.Name != "" {
		out = append(out, secretRefRequirement{
			refName: ssh.KeyPairRef.Name,
			label:   clusterName + " " + field + " keyPairRef",
			phases:  []string{"storage"},
			role:    secret.MaterialSSHPublic,
			sshPair: true,
		})
	}
	if ssh.PrivateKeyRef.Name != "" {
		out = append(out, secretRefRequirement{
			refName: ssh.PrivateKeyRef.Name,
			label:   clusterName + " " + field + " privateKeyRef",
			phases:  []string{"storage"},
			role:    secret.MaterialSSHPrivate,
		})
	}
	return out
}
