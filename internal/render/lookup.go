package render

import (
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/state/view"
)

// Lookup helpers shared by installer, vars, and inventory.

func findProvider(state v1alpha1.State, name string) (v1alpha1.InfraProvider, bool) {
	return stateview.Provider(state, name)
}

func findNetworkConfig(state v1alpha1.State, name string) (v1alpha1.NetworkConfig, bool) {
	return stateview.NetworkConfig(state, name)
}

func findMachine(state v1alpha1.State, name string) (v1alpha1.Machine, bool) {
	return stateview.Machine(state, name)
}

func findContainerCluster(state v1alpha1.State, name string) (v1alpha1.ContainerCluster, bool) {
	for _, cluster := range state.ContainerClusters {
		if cluster.Metadata.Name == name {
			return cluster, true
		}
	}
	return v1alpha1.ContainerCluster{}, false
}

func lookupMachineAddress(state v1alpha1.State, name string) string {
	if m, ok := findMachine(state, name); ok && m.Spec.Access.SSH != nil {
		return v1alpha1.MachineSSHAddress(m)
	}
	return ""
}

func findProfile(p v1alpha1.InfraProvider, name string) (v1alpha1.MachineProfile, bool) {
	return stateview.MachineProfile(p, name)
}

func findMachineImage(state v1alpha1.State, name string) (v1alpha1.MachineImage, bool) {
	for _, image := range state.MachineImages {
		if image.Metadata.Name == name {
			return image, true
		}
	}
	return v1alpha1.MachineImage{}, false
}

func findMachineInstallProfile(state v1alpha1.State, name string) (v1alpha1.MachineInstallProfile, bool) {
	for _, profile := range state.MachineInstallProfiles {
		if profile.Metadata.Name == name {
			return profile, true
		}
	}
	return v1alpha1.MachineInstallProfile{}, false
}

func findProviderMachine(state v1alpha1.State, name string) (v1alpha1.Machine, bool) {
	return stateview.Machine(state, name)
}

func findNetworkAttachment(p v1alpha1.InfraProvider, name string) (v1alpha1.NetworkAttachmentCapability, bool) {
	for _, attachment := range p.Spec.NetworkAttachments {
		if attachment.Name == name {
			return attachment, true
		}
	}
	return v1alpha1.NetworkAttachmentCapability{}, false
}

func findMachineNetworkBinding(ci v1alpha1.ClusterInstall, providerName, networkName string) (v1alpha1.MachineNetworkBinding, bool) {
	for _, binding := range ci.NetworkBindings {
		if binding.ProviderRef.Name == providerName && binding.NetworkConfigRef.Name == networkName {
			return binding, true
		}
	}
	return v1alpha1.MachineNetworkBinding{}, false
}

func findClusterMachine(ci v1alpha1.ClusterInstall, name string) (v1alpha1.InstallMachine, bool) {
	for _, m := range ci.Machines {
		if m.Name == name {
			return m, true
		}
	}
	return v1alpha1.InstallMachine{}, false
}

func primaryEnvironment(state v1alpha1.State) *v1alpha1.Environment {
	return stateview.Environment(state)
}

func sortedNodes(nodes []v1alpha1.OCPHostSpec) []v1alpha1.OCPHostSpec {
	out := append([]v1alpha1.OCPHostSpec(nil), nodes...)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Hostname < out[j].Hostname
	})
	return out
}

// clusterNodesForCI returns the ContainerCluster.spec.nodes map for the
// ContainerCluster bound to this ClusterInstall, or nil when no binding
// exists.
func clusterNodesForCI(state v1alpha1.State, ci v1alpha1.ClusterInstall) map[string]v1alpha1.OCPHostSpec {
	return stateview.ClusterNodesForInstall(state, ci)
}
