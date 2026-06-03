package workflow

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionplan "github.com/crmarques/bootwright/internal/addons/plan"
)

func kubeVirtHostClusterApplyDeps(state v1alpha1.State) (map[string][]string, error) {
	selected := map[string]bool{}
	for _, cluster := range state.ContainerClusters {
		selected[cluster.Metadata.Name] = true
	}
	providers := providerIndex(state)
	infras := clusterInfraIndex(state)
	provides, err := kubeVirtProviderExtensionWaitTasks(state)
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, cluster := range state.ContainerClusters {
		infra, ok := clusterInfraForCluster(cluster, infras)
		if !ok {
			continue
		}
		for _, machine := range infra.Spec.Components.Machines {
			profile, ok := machineProfileForComponent(providers, machine)
			if !ok || profile.KubeVirt == nil || profile.KubeVirt.HostContainerClusterRef == nil || profile.KubeVirt.HostContainerClusterRef.Name == "" {
				continue
			}
			parent := profile.KubeVirt.HostContainerClusterRef.Name
			if !selected[parent] {
				continue
			}
			out[cluster.Metadata.Name] = appendUniqueString(out[cluster.Metadata.Name], "wait."+parent)
			waitTasks := provides[parent]
			if len(waitTasks) == 0 {
				return nil, fmt.Errorf("ContainerCluster/%s uses KubeVirt hostContainerClusterRef %q but no selected ClusterAddon providing %q is bound to that host cluster",
					cluster.Metadata.Name, parent, v1alpha1.ClusterAddonProvidesKubeVirt)
			}
			for _, taskID := range waitTasks {
				out[cluster.Metadata.Name] = appendUniqueString(out[cluster.Metadata.Name], taskID)
			}
		}
	}
	return out, nil
}

func kubeVirtProviderExtensionWaitTasks(state v1alpha1.State) (map[string][]string, error) {
	plans, err := extensionplan.BindingPlans(state)
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, binding := range plans {
		for _, extension := range binding.Addons {
			if !extensionProvides(extension.Extension, v1alpha1.ClusterAddonProvidesKubeVirt) {
				continue
			}
			taskID := "addon." + binding.Cluster + "." + extension.Name + ".wait"
			out[binding.Cluster] = appendUniqueString(out[binding.Cluster], taskID)
		}
	}
	return out, nil
}

func extensionProvides(extension v1alpha1.ClusterAddon, capability string) bool {
	for _, item := range extension.Spec.Provides {
		if item == capability {
			return true
		}
	}
	return false
}

func kubeVirtResourceKeys(state v1alpha1.State, clusterName string) []string {
	providers := providerIndex(state)
	infras := clusterInfraIndex(state)
	var cluster v1alpha1.ContainerCluster
	foundCluster := false
	for _, item := range state.ContainerClusters {
		if item.Metadata.Name == clusterName {
			cluster = item
			foundCluster = true
			break
		}
	}
	if !foundCluster {
		return nil
	}
	infra, ok := clusterInfraForCluster(cluster, infras)
	if !ok {
		return nil
	}
	var out []string
	for _, machine := range infra.Spec.Components.Machines {
		profile, ok := machineProfileForComponent(providers, machine)
		if !ok || profile.KubeVirt == nil {
			continue
		}
		out = appendUniqueString(out, kubeVirtResourceKey(profile.KubeVirt))
	}
	return out
}

func kubeVirtResourceKey(profile *v1alpha1.MachineProfileKubeVirtProvisioner) string {
	owner := "external"
	if profile.HostContainerClusterRef != nil && profile.HostContainerClusterRef.Name != "" {
		owner = profile.HostContainerClusterRef.Name
	} else if profile.KubeconfigRef != nil && profile.KubeconfigRef.Name != "" {
		owner = "kubeconfig:" + profile.KubeconfigRef.Name
	}
	return "kubevirt:" + owner + ":" + profile.Namespace
}

func providerIndex(state v1alpha1.State) map[string]v1alpha1.InfraProvider {
	out := make(map[string]v1alpha1.InfraProvider, len(state.InfraProviders))
	for _, provider := range state.InfraProviders {
		out[provider.Metadata.Name] = provider
	}
	return out
}

func clusterInfraIndex(state v1alpha1.State) map[string]v1alpha1.ClusterInfra {
	out := make(map[string]v1alpha1.ClusterInfra, len(state.ClusterInfras))
	for _, infra := range state.ClusterInfras {
		out[infra.Metadata.Name] = infra
	}
	return out
}

func clusterInfraForCluster(cluster v1alpha1.ContainerCluster, infras map[string]v1alpha1.ClusterInfra) (v1alpha1.ClusterInfra, bool) {
	if len(cluster.Spec.Nodes) == 0 {
		return v1alpha1.ClusterInfra{}, false
	}
	name := cluster.Spec.Nodes[0].MachineRef.ClusterInfra
	if name == "" {
		return v1alpha1.ClusterInfra{}, false
	}
	infra, ok := infras[name]
	return infra, ok
}

func machineProfileForComponent(providers map[string]v1alpha1.InfraProvider, machine v1alpha1.ClusterMachineComponent) (v1alpha1.MachineProfileCapability, bool) {
	if machine.From.Profile == "" {
		return v1alpha1.MachineProfileCapability{}, false
	}
	provider, ok := providers[machine.From.Provider]
	if !ok {
		return v1alpha1.MachineProfileCapability{}, false
	}
	for _, profile := range provider.Spec.MachineProfiles {
		if profile.Name == machine.From.Profile {
			return profile, true
		}
	}
	return v1alpha1.MachineProfileCapability{}, false
}

func appendUniqueString(items []string, value string) []string {
	for _, item := range items {
		if item == value {
			return items
		}
	}
	return append(items, value)
}

func applyNodeBootResourceKeys(state v1alpha1.State, clusterName string, machineNames []string) []string {
	providers := providerIndex(state)
	infras := clusterInfraIndex(state)
	cluster, ok := containerClusterByName(state, clusterName)
	if !ok {
		return nil
	}
	infra, ok := clusterInfraForCluster(cluster, infras)
	if !ok {
		return nil
	}
	machines := map[string]v1alpha1.ClusterMachineComponent{}
	for _, machine := range infra.Spec.Components.Machines {
		machines[machine.Name] = machine
	}
	var out []string
	for _, machineName := range machineNames {
		machine, ok := machines[machineName]
		if !ok {
			continue
		}
		if profile, ok := machineProfileForComponent(providers, machine); ok && profile.KubeVirt != nil {
			out = appendUniqueString(out, kubeVirtResourceKey(profile.KubeVirt))
			continue
		}
		out = appendUniqueString(out, applyNodeRedfishResource(state, clusterName, machineName))
	}
	return out
}

func containerClusterByName(state v1alpha1.State, name string) (v1alpha1.ContainerCluster, bool) {
	for _, cluster := range state.ContainerClusters {
		if cluster.Metadata.Name == name {
			return cluster, true
		}
	}
	return v1alpha1.ContainerCluster{}, false
}
