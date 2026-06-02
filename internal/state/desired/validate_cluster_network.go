package desiredstate

import (
	"net"
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/state/view"
)

func endpointNetworkMatches(ci v1alpha1.ClusterInfra, networkConfigs map[string]v1alpha1.NetworkConfig, ip net.IP) []string {
	matchedCIDRs := map[string]string{}
	for _, name := range stateview.ClusterConsumedNetworkConfigs(ci) {
		networkConfig, ok := networkConfigs[name]
		if !ok {
			continue
		}
		for _, machineNetwork := range networkConfig.Spec.MachineNetwork {
			if cidrContainsIP(machineNetwork.CIDR, ip) {
				matchedCIDRs[machineNetwork.CIDR] = name
			}
		}
	}
	for _, machine := range ci.Spec.Components.Machines {
		if machine.NetworkConfig.Spec == nil {
			continue
		}
		name := ci.Metadata.Name + "/" + machine.Name
		for _, machineNetwork := range machine.NetworkConfig.Spec.MachineNetwork {
			if cidrContainsIP(machineNetwork.CIDR, ip) {
				matchedCIDRs[machineNetwork.CIDR] = name
			}
		}
	}
	matches := make([]string, 0, len(matchedCIDRs))
	for _, name := range matchedCIDRs {
		matches = append(matches, name)
	}
	sort.Strings(matches)
	return matches
}

func networkConfigContainsIP(networkConfig v1alpha1.NetworkConfig, ip net.IP) bool {
	for _, machineNetwork := range networkConfig.Spec.MachineNetwork {
		if cidrContainsIP(machineNetwork.CIDR, ip) {
			return true
		}
	}
	return false
}

func cidrContainsIP(cidr string, ip net.IP) bool {
	if _, parsed, err := net.ParseCIDR(cidr); err == nil && parsed.Contains(ip) {
		return true
	}
	return false
}

func templateBareMetalInterfaceNames(n v1alpha1.NetworkConfig) []string {
	return bareMetalInterfaceNames(n.Spec.Template.NetworkConfig)
}

func overrideBareMetalInterfaceNames(config map[string]any) []string {
	return bareMetalInterfaceNames(config)
}

func bareMetalInterfaceNames(config map[string]any) []string {
	raw, ok := config["interfaces"].([]any)
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	for _, item := range raw {
		iface, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := iface["name"].(string)
		if ifaceType, _ := iface["type"].(string); ifaceType == "ethernet" && name != "" {
			seen[name] = true
		}
		aggregation, _ := iface["link-aggregation"].(map[string]any)
		for _, port := range interfaceList(aggregation["port"]) {
			seen[port] = true
		}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func interfaceList(raw any) []string {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		name, _ := item.(string)
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}
