package stateview

import (
	"fmt"
	"strings"

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

func Secret(state v1alpha1.State, name string) (v1alpha1.Secret, bool) {
	for _, secret := range state.Secrets {
		if secret.Metadata.Name == name {
			return secret, true
		}
	}
	return v1alpha1.Secret{}, false
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

func MachineConnectionAddress(state v1alpha1.State, machine v1alpha1.Machine) string {
	fqdn := v1alpha1.MachineFQDNAddress(machine)
	if fqdn == "" {
		return v1alpha1.MachineSSHAddress(machine)
	}
	if !MachineReferencesNameResolution(state, machine) {
		return v1alpha1.MachineSSHAddress(machine)
	}
	if MachineHostsManagedNameResolution(state, machine) {
		return v1alpha1.MachineSSHAddress(machine)
	}
	return fqdn
}

func MachineConnectionAddressByName(state v1alpha1.State, name string) string {
	if m, ok := Machine(state, name); ok {
		return MachineConnectionAddress(state, m)
	}
	return ""
}

func MachineReferencesNameResolution(state v1alpha1.State, machine v1alpha1.Machine) bool {
	config := machine.Spec.Network.Config
	if config.Spec != nil && len(config.Spec.NameResolutionRefs) > 0 {
		return true
	}
	if config.NetworkConfigRef.Name == "" {
		return false
	}
	network, ok := NetworkConfig(state, config.NetworkConfigRef.Name)
	return ok && len(network.Spec.NameResolutionRefs) > 0
}

func MachineHostsManagedNameResolution(state v1alpha1.State, machine v1alpha1.Machine) bool {
	env := Environment(state)
	if env == nil {
		return false
	}
	for _, entry := range env.Spec.InfraComponents.NameResolution {
		if entry.Management != v1alpha1.EnvironmentComponentManaged {
			continue
		}
		component, ok := InfraComponent(state, entry.ComponentRef.Name)
		if !ok || component.Spec.NameResolution == nil {
			continue
		}
		if component.Spec.NameResolution.MachineRef.Name == machine.Metadata.Name {
			return true
		}
	}
	return false
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

func InstallMachineAddresses(machine v1alpha1.InstallMachine) []string {
	var out []string
	seen := map[string]bool{}
	for _, ia := range machine.Network.InterfaceAddresses {
		address := strings.TrimSpace(InstallMachineAddress(machine, ia.AddressRef.Name))
		if address == "" || seen[address] {
			continue
		}
		seen[address] = true
		out = append(out, address)
	}
	return out
}
