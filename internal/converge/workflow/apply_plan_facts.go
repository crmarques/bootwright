package workflow

import (
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

func clusterHostNeedsSubstratePrepare(state v1alpha1.State, clusterName, host string) bool {
	cluster, ok := containerClusterByName(state, clusterName)
	if !ok {
		return false
	}
	for _, node := range cluster.Spec.Nodes {
		if node.MachineRef.Name == "" || applyMachineHost(state, node.MachineRef.Name) != host {
			continue
		}
		if applyMachineNeedsSubstratePrepare(state, node.MachineRef.Name) {
			return true
		}
	}
	return false
}

func applyMachineHost(state v1alpha1.State, machineName string) string {
	machine, ok := stateview.Machine(state, machineName)
	if !ok || machine.Spec.Substrate.ProviderRef.Name == "" || machine.Spec.Substrate.ProfileRef.Name == "" {
		return ""
	}
	provider, ok := stateview.Provider(state, machine.Spec.Substrate.ProviderRef.Name)
	if !ok {
		return ""
	}
	switch provider.Spec.Type {
	case v1alpha1.ProvisionerLibvirt:
		if provider.Spec.Libvirt != nil {
			return provider.Spec.Libvirt.MachineRef.Name
		}
	case v1alpha1.ProvisionerKubeVirt, v1alpha1.ProvisionerVSphere:
		return "localhost"
	}
	return ""
}

func applyMachineNeedsSubstratePrepare(state v1alpha1.State, machineName string) bool {
	machine, ok := stateview.Machine(state, machineName)
	if !ok {
		return false
	}
	provider, ok := stateview.Provider(state, machine.Spec.Substrate.ProviderRef.Name)
	return ok && provider.Spec.Type == v1alpha1.ProvisionerLibvirt
}

func applyMachineHostSlotKey(state v1alpha1.State, machineName string) string {
	host := applyMachineHost(state, machineName)
	if host == "" || applyMachineProviderType(state, machineName) != v1alpha1.ProvisionerLibvirt {
		return ""
	}
	return "host:" + host + ":machine"
}

func applyMachineExclusiveResourceKeys(state v1alpha1.State, clusterName, machineName string) []string {
	machine, ok := stateview.Machine(state, machineName)
	if !ok {
		return nil
	}
	provider, ok := stateview.Provider(state, machine.Spec.Substrate.ProviderRef.Name)
	if ok && provider.Spec.Type == v1alpha1.ProvisionerKubeVirt && provider.Spec.KubeVirt != nil {
		return []string{kubeVirtResourceKey(provider.Spec.KubeVirt)}
	}
	if ok && provider.Spec.Type == v1alpha1.ProvisionerVSphere && provider.Spec.VSphere != nil {
		return []string{vsphereResourceKey(provider, machine)}
	}
	if machine.Spec.Hardware.Management.BMC.Address != "" {
		return []string{applyNodeRedfishResource(state, clusterName, machineName)}
	}
	return nil
}

func applyManagedOSResourceKeys(state v1alpha1.State, clusterName string, machineNames []string) []string {
	var out []string
	for _, machineName := range machineNames {
		for _, key := range applyMachineExclusiveResourceKeys(state, clusterName, machineName) {
			out = appendUniqueString(out, key)
		}
		if applyMachineProviderType(state, machineName) == v1alpha1.ProvisionerLibvirt {
			if host := applyMachineHost(state, machineName); host != "" {
				out = appendUniqueString(out, hostMutationResource(host))
			}
		}
	}
	return out
}

func applyMachineProviderType(state v1alpha1.State, machineName string) string {
	machine, ok := stateview.Machine(state, machineName)
	if !ok {
		return ""
	}
	provider, ok := stateview.Provider(state, machine.Spec.Substrate.ProviderRef.Name)
	if !ok {
		return ""
	}
	return provider.Spec.Type
}

func applyClusterNames(state v1alpha1.State) []string {
	names := make([]string, 0, len(state.ContainerClusters))
	for _, cluster := range state.ContainerClusters {
		names = append(names, cluster.Metadata.Name)
	}
	sort.Strings(names)
	return names
}

func storageClusterSelectedForTarget(target ApplyTarget, name string) bool {
	if target.StorageClusterNames == nil {
		return true
	}
	for _, selected := range target.StorageClusterNames {
		if selected == name {
			return true
		}
	}
	return false
}

func applyClusterMachineNames(state v1alpha1.State, clusterName string) []string {
	var names []string
	for _, cluster := range state.ContainerClusters {
		if cluster.Metadata.Name != clusterName {
			continue
		}
		seen := map[string]bool{}
		for _, node := range cluster.Spec.Nodes {
			if node.MachineRef.Name == "" || seen[node.MachineRef.Name] {
				continue
			}
			seen[node.MachineRef.Name] = true
			names = append(names, node.MachineRef.Name)
		}
		break
	}
	sort.Strings(names)
	return names
}

func applyNodeRedfishResource(state v1alpha1.State, clusterName, machineName string) string {
	for _, machine := range state.Machines {
		if machine.Metadata.Name != machineName {
			continue
		}
		if machine.Spec.Hardware.Management.BMC.Address != "" {
			return "redfish:" + machine.Spec.Hardware.Management.BMC.Address
		}
	}
	return "redfish:" + clusterName + "/" + machineName
}

func hostMutationResource(host string) string {
	if host == "" {
		return ""
	}
	return "host:" + host + ":mutating"
}
