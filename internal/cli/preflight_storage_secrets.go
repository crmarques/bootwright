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
		for _, node := range cluster.Spec.Ceph.Topology.Hosts {
			machine, ok := topology.NodeMachine(state, cluster, node.Hostname)
			if !ok {
				continue
			}
			out = append(out, machineSSHSecretRequirements(fmt.Sprintf("StorageCluster/%s node/%s Machine/%s", cluster.Metadata.Name, node.Hostname, machine.Metadata.Name), []string{"deps", "base"}, machine, true)...)
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
				label:   fmt.Sprintf("StorageExport/%s externalDetails.fromSecretRef", export.Metadata.Name),
				phases:  []string{"addons"},
				role:    secret.MaterialPrimary,
			})
		}
		ssh := datafoundation.ExternalDetailsSourceSSH(export)
		if ssh == nil {
			continue
		}
		for _, ref := range ssh.MachineRefs {
			machine, ok := machineByName(state, ref.Name)
			if !ok {
				continue
			}
			out = append(out, machineSSHSecretRequirements(fmt.Sprintf("StorageExport/%s externalDetails.sshExecution Machine/%s", export.Metadata.Name, machine.Metadata.Name), []string{"addons"}, machine, false)...)
		}
		if len(ssh.MachineRefs) == 0 {
			cluster, ok := clusterByName[export.Spec.StorageClusterRef.Name]
			if ok && cluster.Spec.Ceph != nil {
				for _, node := range cluster.Spec.Ceph.Topology.Hosts {
					if node.Hostname != cluster.Spec.Ceph.Cephadm.Bootstrap.Host {
						continue
					}
					machine, ok := topology.NodeMachine(state, cluster, node.Hostname)
					if !ok {
						continue
					}
					out = append(out, machineSSHSecretRequirements(fmt.Sprintf("StorageExport/%s externalDetails.sshExecution seed Machine/%s", export.Metadata.Name, machine.Metadata.Name), []string{"addons"}, machine, false)...)
				}
			}
		}
	}
	return out
}

func machineByName(state v1alpha1.State, name string) (v1alpha1.Machine, bool) {
	for _, machine := range state.Machines {
		if machine.Metadata.Name == name {
			return machine, true
		}
	}
	return v1alpha1.Machine{}, false
}

func machineSSHSecretRequirements(label string, phases []string, machine v1alpha1.Machine, requirePair bool) []secretRefRequirement {
	var out []secretRefRequirement
	if machine.Spec.Access.SSH == nil {
		return out
	}
	if machine.Spec.Access.SSH.KeyRef.Name != "" {
		req := secretRefRequirement{
			refName: machine.Spec.Access.SSH.KeyRef.Name,
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
	if machine.Spec.Access.SSH.KnownHostsRef.Name != "" {
		out = append(out, secretRefRequirement{
			refName: machine.Spec.Access.SSH.KnownHostsRef.Name,
			label:   label + " knownHostsRef",
			phases:  phases,
			role:    secret.MaterialPrimary,
		})
	}
	return out
}
