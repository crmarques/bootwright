package cli

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func machineNodeSSHTarget(state v1alpha1.State, machine v1alpha1.Machine) (sshTarget, error) {
	name := machine.Metadata.Name
	cluster, ok := machineContainerCluster(state, name)
	if !ok {
		return sshTarget{}, fmt.Errorf("machine %q declares no SSH access (spec.access.ssh) and no ContainerCluster lists it as a node, so there is no spec.install.nodeSSH key to reach it with; declare spec.access.ssh on the Machine", name)
	}
	if cluster.Spec.Install.NodeSSH.PrivateMaterialRef().Name == "" {
		return sshTarget{}, fmt.Errorf("machine %q declares no SSH access (spec.access.ssh), and ContainerCluster %q holds no node private key (spec.install.nodeSSH keyPairRef or privateKeyRef), so Bootwright holds no credential for it; declare spec.access.ssh on the Machine, or set spec.install.nodeSSH.keyPairRef on the cluster", name, cluster.Metadata.Name)
	}
	return containerNodeSSHTarget(state, cluster, name)
}

func machineContainerCluster(state v1alpha1.State, machineName string) (v1alpha1.ContainerCluster, bool) {
	for _, cluster := range state.ContainerClusters {
		for _, node := range cluster.Spec.Nodes {
			if node.MachineRef.Name == machineName {
				return cluster, true
			}
		}
	}
	return v1alpha1.ContainerCluster{}, false
}
