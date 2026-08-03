package desiredstate

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/nmstate"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

func validateMachineInstallCloneSubstrate(state v1alpha1.State) []string {
	var errs []string
	networks := indexNetworkConfigs(state.NetworkConfigs)
	for _, machine := range state.Machines {
		profileRef := machine.Spec.OS.InstallProfileRef.Name
		if profileRef == "" || !v1alpha1.MachineInstallsOS(machine) {
			continue
		}
		install, ok := stateview.MachineInstallProfile(state, profileRef)
		if !ok {
			continue
		}
		provider, ok := stateview.Provider(state, machine.Spec.Substrate.ProviderRef.Name)
		if !ok {
			continue
		}
		shape, hasShape := stateview.MachineProfile(provider, machine.Spec.Substrate.ProfileRef.Name)
		clone := install.Spec.Installer.TemplateClone != nil
		switch {
		case clone && provider.Spec.Type != v1alpha1.ProvisionerVSphere:
			errs = append(errs, fmt.Sprintf("Machine/%s spec.os.installProfileRef %q selects installer.templateClone, but its substrate provider InfraProvider/%s is type %q. Only the vsphere adapter clones a machine from a template today. Use installer.anaconda on this provider, or move the machine to a vsphere provider", machine.Metadata.Name, profileRef, provider.Metadata.Name, provider.Spec.Type))
		case clone && hasShape && shape.Template == "":
			errs = append(errs, fmt.Sprintf("Machine/%s spec.os.installProfileRef %q selects installer.templateClone, but InfraProvider/%s machineProfiles[%q].template is empty. Set machineProfiles[].template to the vCenter inventory path of the golden RHEL template to clone", machine.Metadata.Name, profileRef, provider.Metadata.Name, shape.Name))
		case !clone && hasShape && shape.Template != "":
			errs = append(errs, fmt.Sprintf("Machine/%s spec.os.installProfileRef %q selects installer.anaconda, but InfraProvider/%s machineProfiles[%q].template clones a template whose disk Anaconda then wipes. Clear machineProfiles[].template, or switch the profile to installer.templateClone", machine.Metadata.Name, profileRef, provider.Metadata.Name, shape.Name))
		}
		if clone {
			errs = append(errs, validateMachineCloneSeedNetwork(machine, networks)...)
		}
	}
	return errs
}

const machineCloneSeedNetworkNeed = "The cloud-init seed is the only thing that brings the clone onto the network before SSH answers, so a clone needs a static IPv4 primary."

func validateMachineCloneSeedNetwork(machine v1alpha1.Machine, networks map[string]v1alpha1.NetworkConfig) []string {
	config := machine.Spec.Network.Config
	effective, source := machineCloneEffectiveNetwork(machine, config, networks)
	if effective == nil {
		return []string{fmt.Sprintf("Machine/%s selects installer.templateClone but resolves no network config at all, so it declares no static IPv4 address on the interface carrying the default route. %s Declare one in a NetworkConfig named by spec.network.config.networkConfigRef or inline under spec.network.config.spec, or switch the profile to installer.anaconda", machine.Metadata.Name, machineCloneSeedNetworkNeed)}
	}
	routed, ok := nmstate.NetworkConfigRoutedInterface(effective)
	if !ok || routed.IPv4 == "" {
		return []string{fmt.Sprintf("Machine/%s selects installer.templateClone but its network config declares no static IPv4 address on the interface carrying the default route. %s Declare one in %s, or switch the profile to installer.anaconda", machine.Metadata.Name, machineCloneSeedNetworkNeed, source)}
	}
	if routed.Type != "" && routed.Type != "ethernet" {
		return []string{fmt.Sprintf("Machine/%s selects installer.templateClone but the interface carrying its default route is type %q. The cloud-init seed configures a single ethernet so SSH can answer; bonds and VLANs are applied afterwards by nmstate, which cannot run before Bootwright can log in. Put the machine's primary address on an ethernet (a vDS portgroup can carry the VLAN tag), or switch the profile to installer.anaconda", machine.Metadata.Name, routed.Type)}
	}
	return nil
}

func machineCloneEffectiveNetwork(machine v1alpha1.Machine, config v1alpha1.MachineNetworkConfig, networks map[string]v1alpha1.NetworkConfig) (map[string]any, string) {
	injects := machineConfigInterfaceAddresses(machine, config)
	switch {
	case config.NetworkConfigRef.Name != "":
		network, ok := networks[config.NetworkConfigRef.Name]
		if !ok {
			return nil, ""
		}
		return nmstate.EffectiveConfig(network.Spec.Template.NetworkConfig, config.Overrides, injects), "NetworkConfig/" + config.NetworkConfigRef.Name
	case config.Spec != nil:
		return nmstate.EffectiveConfig(config.Spec.Template.NetworkConfig, nil, injects), "spec.network.config.spec"
	}
	return nil, ""
}
