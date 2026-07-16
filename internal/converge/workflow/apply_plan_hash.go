package workflow

import (
	"encoding/json"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func containerClusterInstallStructuralHashVars(clusterState v1alpha1.State) v1alpha1.State {
	var clone v1alpha1.State
	data, err := json.Marshal(hashScopedState(clusterState))
	if err != nil {
		return clusterState
	}
	if err := json.Unmarshal(data, &clone); err != nil {
		return clusterState
	}
	clone.ClusterAddons = nil
	clone.ClusterAddonBindings = nil
	clone.ClusterAddonProfiles = nil
	clone.InfraProviders = nil
	clone.InfraComponents = nil
	clone.NetworkConfigs = nil
	clone.Entitlements = nil
	clone.MachineImages = nil
	clone.MachineInstallProfiles = nil
	clone.ProvisioningPlaybooks = nil
	for i := range clone.ContainerClusters {
		for j := range clone.ContainerClusters[i].Spec.Hosts {
			clone.ContainerClusters[i].Spec.Hosts[j].Labels = nil
			clone.ContainerClusters[i].Spec.Hosts[j].Taints = nil
			clone.ContainerClusters[i].Spec.Hosts[j].Role = installTimeNodeRole(clone.ContainerClusters[i].Spec.Hosts[j].Role)
		}
	}
	for i := range clone.Machines {
		clone.Machines[i].Spec.Hardware.Management = v1alpha1.MachineHardwareManagement{}
	}
	return clone
}

func installTimeNodeRole(role string) string {
	if role == v1alpha1.NodeRoleWorker || role == v1alpha1.NodeRoleInfra {
		return v1alpha1.NodeRoleWorker
	}
	return v1alpha1.NodeRoleMaster
}
