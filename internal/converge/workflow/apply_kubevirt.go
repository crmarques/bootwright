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
	machines := machineIndex(state)
	provides, err := kubeVirtProviderExtensionWaitTasks(state)
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, cluster := range state.ContainerClusters {
		for _, node := range cluster.Spec.Nodes {
			machine, ok := machines[node.MachineRef.Name]
			if !ok {
				continue
			}
			provider, ok := providers[machine.Spec.Substrate.ProviderRef.Name]
			if !ok || provider.Spec.Type != v1alpha1.ProvisionerKubeVirt || provider.Spec.KubeVirt == nil || provider.Spec.KubeVirt.HostClusterRef == nil || provider.Spec.KubeVirt.HostClusterRef.Name == "" {
				continue
			}
			parent := provider.Spec.KubeVirt.HostClusterRef.Name
			if !selected[parent] {
				continue
			}
			out[cluster.Metadata.Name] = appendUniqueString(out[cluster.Metadata.Name], "wait."+parent)
			waitTasks := provides[parent]
			if len(waitTasks) == 0 {
				return nil, fmt.Errorf("ContainerCluster/%s uses KubeVirt hostClusterRef %q but no selected ClusterAddon providing %q is bound to that host cluster",
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

func kubeVirtResourceKey(profile *v1alpha1.InfraProviderKubeVirt) string {
	owner := "external"
	if profile.HostClusterRef != nil && profile.HostClusterRef.Name != "" {
		owner = profile.HostClusterRef.Name
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

func machineIndex(state v1alpha1.State) map[string]v1alpha1.Machine {
	out := make(map[string]v1alpha1.Machine, len(state.Machines))
	for _, machine := range state.Machines {
		out[machine.Metadata.Name] = machine
	}
	return out
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
	machines := machineIndex(state)
	if _, ok := containerClusterByName(state, clusterName); !ok {
		return nil
	}
	var out []string
	for _, machineName := range machineNames {
		machine, ok := machines[machineName]
		if !ok {
			continue
		}
		provider, ok := providers[machine.Spec.Substrate.ProviderRef.Name]
		if ok && provider.Spec.Type == v1alpha1.ProvisionerKubeVirt && provider.Spec.KubeVirt != nil {
			out = appendUniqueString(out, kubeVirtResourceKey(provider.Spec.KubeVirt))
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
