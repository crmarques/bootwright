package desiredstate

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
)

func normalizeMachineAccess(state *v1alpha1.State) {
	claims := storageNodeClaims(*state)
	for i := range state.Machines {
		machine := &state.Machines[i]
		if machine.Spec.Access.Local {
			continue
		}
		if machine.Spec.Access.SSH == nil {
			deriveMachineAccess(machine, claims[machine.Metadata.Name])
		}
		ssh := machine.Spec.Access.SSH
		if ssh == nil {
			continue
		}
		if ssh.AddressRef.Name == "" {
			ssh.AddressRef.Name = defaultMachineAddressName(*machine)
		}
		if ssh.Auth.IsZero() && !v1alpha1.MachineInstallsOS(*machine) {
			ssh.Auth.ControllerIdentity = &v1alpha1.MachineSSHControllerIdentity{}
		}
		if machine.Spec.Access.RootLogin == "" {
			machine.Spec.Access.RootLogin = v1alpha1.MachineRootLoginKeep
		}
	}
}

func deriveMachineAccess(machine *v1alpha1.Machine, cluster *v1alpha1.StorageCluster) {
	switch {
	case cluster != nil && v1alpha1.MachineInstallsOS(*machine):
		user := v1alpha1.StorageClusterCephadmSSHUser(*cluster)
		machine.Spec.Access.SSH = &v1alpha1.MachineSSHSpec{
			User: user,
			Auth: v1alpha1.MachineSSHAuth{
				Provision: &v1alpha1.MachineSSHProvision{KeyRef: v1alpha1.SecretRef{Name: cluster.Spec.Ceph.Cephadm.ClusterSSH.KeyRef.Name}},
			},
		}
		machine.DefaultedRefs.Access = true
		if user != v1alpha1.RootSSHUser && machine.Spec.Access.RootLogin == "" {
			machine.Spec.Access.RootLogin = v1alpha1.MachineRootLoginRevoke
		}
	case v1alpha1.MachineOSProvided(*machine):
		machine.Spec.Access.SSH = &v1alpha1.MachineSSHSpec{
			Auth: v1alpha1.MachineSSHAuth{ControllerIdentity: &v1alpha1.MachineSSHControllerIdentity{}},
		}
		machine.DefaultedRefs.Access = true
	}
}

func defaultMachineAddressName(machine v1alpha1.Machine) string {
	for _, address := range machine.Spec.Addresses {
		if address.Name == "ssh" {
			return "ssh"
		}
	}
	if _, ok := v1alpha1.MachineAddressByName(machine, v1alpha1.MachineAddressFQDN); ok {
		return v1alpha1.MachineAddressFQDN
	}
	return ""
}

func storageNodeClaims(state v1alpha1.State) map[string]*v1alpha1.StorageCluster {
	claims := map[string]*v1alpha1.StorageCluster{}
	for i := range state.StorageClusters {
		cluster := &state.StorageClusters[i]
		if !v1alpha1.StorageClusterManaged(*cluster) || cluster.Spec.Ceph == nil {
			continue
		}
		for _, node := range cluster.Spec.Ceph.Topology.Nodes {
			if node.MachineRef.Name != "" {
				claims[node.MachineRef.Name] = cluster
			}
		}
	}
	return claims
}
