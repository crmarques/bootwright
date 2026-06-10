package stateview

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func ContainerCluster(state v1alpha1.State, name string) (v1alpha1.ContainerCluster, bool) {
	for _, cluster := range state.ContainerClusters {
		if cluster.Metadata.Name == name {
			return cluster, true
		}
	}
	return v1alpha1.ContainerCluster{}, false
}

func MachineImage(state v1alpha1.State, name string) (v1alpha1.MachineImage, bool) {
	for _, image := range state.MachineImages {
		if image.Metadata.Name == name {
			return image, true
		}
	}
	return v1alpha1.MachineImage{}, false
}

func MachineInstallProfile(state v1alpha1.State, name string) (v1alpha1.MachineInstallProfile, bool) {
	for _, profile := range state.MachineInstallProfiles {
		if profile.Metadata.Name == name {
			return profile, true
		}
	}
	return v1alpha1.MachineInstallProfile{}, false
}

func NetworkAttachment(p v1alpha1.InfraProvider, name string) (v1alpha1.NetworkAttachmentCapability, bool) {
	for _, attachment := range p.Spec.NetworkAttachments {
		if attachment.Name == name {
			return attachment, true
		}
	}
	return v1alpha1.NetworkAttachmentCapability{}, false
}

func MachineNetworkBinding(ci v1alpha1.ClusterInstall, providerName, networkName string) (v1alpha1.MachineNetworkBinding, bool) {
	for _, binding := range ci.NetworkBindings {
		if binding.ProviderRef.Name == providerName && binding.NetworkConfigRef.Name == networkName {
			return binding, true
		}
	}
	return v1alpha1.MachineNetworkBinding{}, false
}

func InstallMachine(ci v1alpha1.ClusterInstall, name string) (v1alpha1.InstallMachine, bool) {
	for _, m := range ci.Machines {
		if m.Name == name {
			return m, true
		}
	}
	return v1alpha1.InstallMachine{}, false
}

func MachineSSHAddressByName(state v1alpha1.State, name string) string {
	if m, ok := Machine(state, name); ok && m.Spec.Access.SSH != nil {
		return v1alpha1.MachineSSHAddress(m)
	}
	return ""
}

func MachineNetworkDefinition(state v1alpha1.State, ci v1alpha1.ClusterInstall, machine v1alpha1.InstallMachine) (v1alpha1.NetworkConfig, bool) {
	if machine.Network.Spec != nil {
		return v1alpha1.NetworkConfig{
			Metadata: v1alpha1.Metadata{Name: fmt.Sprintf("%s/%s", ci.Metadata.Name, machine.Name)},
			Spec:     *machine.Network.Spec,
		}, true
	}
	if machine.Network.NetworkConfigRef.Name != "" {
		return NetworkConfig(state, machine.Network.NetworkConfigRef.Name)
	}
	return v1alpha1.NetworkConfig{}, false
}

func InstallMachineAddress(machine v1alpha1.InstallMachine, name string) string {
	for _, address := range machine.Addresses {
		if address.Name == name {
			return address.Address
		}
	}
	return ""
}
