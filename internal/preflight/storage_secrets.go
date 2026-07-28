package preflight

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/storage/topology"
)

func collectStorageSecretRefRequirements(state v1alpha1.State) []secretRefRequirement {
	var out []secretRefRequirement
	for _, cluster := range state.StorageClusters {
		if cluster.Spec.Ceph == nil {
			continue
		}
		if ref := cluster.Spec.Ceph.Cephadm.ClusterSSH.KeyRef.Name; ref != "" {
			out = append(out, secretRefRequirement{
				refName: ref,
				label:   fmt.Sprintf("StorageCluster/%s spec.ceph.cephadm.clusterSSH.keyRef", cluster.Metadata.Name),
				phases:  []string{"deps", "base"},
				role:    secret.MaterialSSHPublic,
				sshPair: true,
				owner:   secretRefOwner{storageCluster: cluster.Metadata.Name},
			})
		}
		for _, node := range cluster.Spec.Ceph.Topology.Nodes {
			machine, ok := topology.NodeMachine(state, cluster, node.Name)
			if !ok {
				continue
			}
			out = append(out, machineSSHSecretRequirements(fmt.Sprintf("StorageCluster/%s node/%s Machine/%s", cluster.Metadata.Name, node.Name, machine.Metadata.Name), []string{"machines", "deps", "base"}, machine, true, secretRefOwner{storageCluster: cluster.Metadata.Name})...)
		}
	}
	for _, export := range state.StorageExports {
		if export.Spec.ExternalDetails == nil || export.Spec.ExternalDetails.FromSecretRef.Name == "" {
			continue
		}
		out = append(out, secretRefRequirement{
			refName: export.Spec.ExternalDetails.FromSecretRef.Name,
			label:   fmt.Sprintf("StorageExport/%s externalDetails.fromSecretRef", export.Metadata.Name),
			phases:  []string{"add-ons"},
			role:    secret.MaterialPrimary,
		})
	}
	for _, gateway := range state.StorageObjectGateways {
		for i, ingress := range gateway.Spec.Ceph.Ingresses {
			if ingress.TLS == nil {
				continue
			}
			owner := secretRefOwner{storageCluster: gateway.Spec.StorageClusterRef.Name}
			out = append(out,
				secretRefRequirement{
					refName: ingress.TLS.CertificateRef.Name,
					label:   fmt.Sprintf("StorageObjectGateway/%s spec.ceph.ingresses[%d].tls.certificateRef tls.crt", gateway.Metadata.Name, i),
					phases:  []string{"base"},
					role:    secret.MaterialPrimary,
					owner:   owner,
				},
				secretRefRequirement{
					refName: ingress.TLS.KeyRef.Name,
					label:   fmt.Sprintf("StorageObjectGateway/%s spec.ceph.ingresses[%d].tls.keyRef tls.key", gateway.Metadata.Name, i),
					phases:  []string{"base"},
					role:    secret.MaterialTLSKey,
					owner:   owner,
				},
			)
		}
	}
	return out
}

func machineSSHSecretRequirements(label string, phases []string, machine v1alpha1.Machine, requirePair bool, owner secretRefOwner) []secretRefRequirement {
	var out []secretRefRequirement
	if machine.Spec.Access.SSH == nil {
		return out
	}
	if keyRef := v1alpha1.MachineSSHKeyRef(machine); keyRef.Name != "" {
		req := secretRefRequirement{
			refName: keyRef.Name,
			label:   label + " keyRef",
			phases:  phases,
			role:    secret.MaterialSSHPrivate,
			owner:   owner,
		}
		if requirePair {
			req.role = secret.MaterialSSHPublic
			req.sshPair = true
		}
		out = append(out, req)
	}
	if passwordRef := v1alpha1.MachineSSHPasswordRef(machine); passwordRef.Name != "" {
		out = append(out, secretRefRequirement{
			refName: passwordRef.Name,
			label:   label + " passwordRef",
			phases:  phases,
			role:    secret.MaterialPrimary,
			owner:   owner,
		})
	}
	if machine.Spec.Access.SSH.SudoPasswordRef.Name != "" {
		out = append(out, secretRefRequirement{
			refName: machine.Spec.Access.SSH.SudoPasswordRef.Name,
			label:   label + " sudoPasswordRef",
			phases:  phases,
			role:    secret.MaterialPrimary,
			owner:   owner,
		})
	}
	if machine.Spec.Access.SSH.KnownHostsRef.Name != "" {
		out = append(out, secretRefRequirement{
			refName: machine.Spec.Access.SSH.KnownHostsRef.Name,
			label:   label + " knownHostsRef",
			phases:  phases,
			role:    secret.MaterialPrimary,
			owner:   owner,
		})
	}
	return out
}
