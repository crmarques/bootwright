package desiredstate

import (
	"net"
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/stateview"
)

func endpointNetworkMatches(ci v1alpha1.ClusterInfra, networkConfigs map[string]v1alpha1.NetworkConfig, ip net.IP) []string {
	var matches []string
	for _, name := range stateview.ClusterConsumedNetworkConfigs(ci) {
		networkConfig, ok := networkConfigs[name]
		if ok && stateview.NetworkConfigContainsIP(networkConfig, ip) {
			matches = append(matches, name)
		}
	}
	sort.Strings(matches)
	return matches
}

func templateInterfaceNames(n v1alpha1.NetworkConfig) map[string]bool {
	raw, ok := n.Spec.Template.NetworkConfig["interfaces"].([]any)
	if !ok {
		return nil
	}
	out := map[string]bool{}
	for _, item := range raw {
		iface, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := iface["name"].(string)
		if name != "" {
			out[name] = true
		}
	}
	return out
}
