package installer

import (
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/nmstate"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

func machineNetworkConfigTemplate(state v1alpha1.State, ci v1alpha1.ClusterInstall, machine v1alpha1.InstallMachine) map[string]any {
	network, ok := stateview.MachineNetworkDefinition(state, ci, machine)
	if !ok {
		return nil
	}
	return nmstate.EffectiveConfig(network.Spec.Template.NetworkConfig, machine.Network.Overrides, machineInterfaceAddresses(machine))
}

func machineInterfaceAddresses(machine v1alpha1.InstallMachine) []nmstate.InterfaceAddress {
	var out []nmstate.InterfaceAddress
	for _, ia := range machine.Network.InterfaceAddresses {
		ip := stateview.InstallMachineAddress(machine, ia.AddressRef.Name)
		if ip == "" || ia.Interface == "" {
			continue
		}
		out = append(out, nmstate.InterfaceAddress{
			Interface:    ia.Interface,
			Family:       ia.Family,
			IP:           ip,
			PrefixLength: ia.PrefixLength,
		})
	}
	return out
}

func networkConfigInterfaceNames(config map[string]any) []string {
	raw, ok := config["interfaces"].([]any)
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := entry["name"].(string)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
