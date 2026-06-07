package render

import (
	"fmt"
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/nmstate"
)

func machineNetworkDefinition(state v1alpha1.State, ci v1alpha1.ClusterInstall, machine v1alpha1.InstallMachine) (v1alpha1.NetworkConfig, bool) {
	if machine.Network.Spec != nil {
		return v1alpha1.NetworkConfig{
			Metadata: v1alpha1.Metadata{Name: fmt.Sprintf("%s/%s", ci.Metadata.Name, machine.Name)},
			Spec:     *machine.Network.Spec,
		}, true
	}
	if machine.Network.NetworkConfigRef.Name != "" {
		return findNetworkConfig(state, machine.Network.NetworkConfigRef.Name)
	}
	return v1alpha1.NetworkConfig{}, false
}

func machineNetworkConfigTemplate(state v1alpha1.State, ci v1alpha1.ClusterInstall, machine v1alpha1.InstallMachine) map[string]any {
	network, ok := machineNetworkDefinition(state, ci, machine)
	if !ok {
		return nil
	}
	return nmstate.EffectiveConfig(network.Spec.Template.NetworkConfig, machine.Network.Overrides, machineInterfaceAddresses(machine))
}

func machineInterfaceAddresses(machine v1alpha1.InstallMachine) []nmstate.InterfaceAddress {
	var out []nmstate.InterfaceAddress
	for _, ia := range machine.Network.InterfaceAddresses {
		ip := installMachineAddress(machine, ia.AddressRef.Name)
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

func installMachineAddress(machine v1alpha1.InstallMachine, name string) string {
	for _, address := range machine.Addresses {
		if address.Name == name {
			return address.Address
		}
	}
	return ""
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

func networkConfigPrimaryIP(config map[string]any) string {
	if ip := networkConfigInterfaceIP(config, "", "ipv4"); ip != "" {
		return ip
	}
	return networkConfigInterfaceIP(config, "", "ipv6")
}

func networkConfigInterfaceIP(config map[string]any, interfaceName string, family string) string {
	raw, ok := config["interfaces"].([]any)
	if !ok {
		return ""
	}
	if family == "" {
		family = "ipv4"
	}
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := entry["name"].(string)
		if interfaceName != "" && name != interfaceName {
			continue
		}
		if ip := networkConfigFamilyIP(entry, family); ip != "" {
			return ip
		}
	}
	return ""
}

func networkConfigFamilyIP(entry map[string]any, family string) string {
	familyConfig, ok := entry[family].(map[string]any)
	if !ok {
		return ""
	}
	rawAddresses, ok := familyConfig["address"].([]any)
	if !ok {
		return ""
	}
	for _, raw := range rawAddresses {
		address, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		ip, _ := address["ip"].(string)
		if ip != "" {
			return ip
		}
	}
	return ""
}
