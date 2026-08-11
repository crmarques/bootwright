package desiredstate

import (
	"net"
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/state/view"
)

func endpointNetworkMatches(ci v1alpha1.ClusterInstall, networkConfigs map[string]v1alpha1.NetworkConfig, ip net.IP) []string {
	matched := map[string]bool{}
	for _, name := range stateview.ClusterConsumedNetworkConfigs(ci) {
		networkConfig, ok := networkConfigs[name]
		if !ok {
			continue
		}
		for _, machineNetwork := range networkConfig.Spec.MachineNetwork {
			if cidrContainsIP(machineNetwork.CIDR, ip) {
				matched[name] = true
			}
		}
	}
	for _, machine := range ci.Machines {
		if machine.Network.Spec == nil {
			continue
		}
		name := ci.Metadata.Name + "/" + machine.Name
		for _, machineNetwork := range machine.Network.Spec.MachineNetwork {
			if cidrContainsIP(machineNetwork.CIDR, ip) {
				matched[name] = true
			}
		}
	}
	matches := make([]string, 0, len(matched))
	for name := range matched {
		matches = append(matches, name)
	}
	sort.Strings(matches)
	return matches
}

func clusterInstallHasSelectedInstallNetwork(ci v1alpha1.ClusterInstall) bool {
	if len(stateview.ClusterConsumedNetworkConfigs(ci)) > 0 {
		return true
	}
	for _, node := range ci.Machines {
		if node.Network.Spec != nil {
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

func selectedMachineNetworkCIDRs(config v1alpha1.MachineNetworkConfig, networks map[string]v1alpha1.NetworkConfig) []string {
	var cidrs []string
	if config.NetworkConfigRef.Name != "" {
		if n, ok := networks[config.NetworkConfigRef.Name]; ok {
			for _, mn := range n.Spec.MachineNetwork {
				cidrs = append(cidrs, mn.CIDR)
			}
		}
	}
	if config.Spec != nil {
		for _, mn := range config.Spec.MachineNetwork {
			cidrs = append(cidrs, mn.CIDR)
		}
	}
	return cidrs
}

func addressInAnyCIDR(cidrs []string, address string) bool {
	ip := net.ParseIP(address)
	if ip == nil {
		return true
	}
	for _, cidr := range cidrs {
		if cidrContainsIP(cidr, ip) {
			return true
		}
	}
	return false
}
