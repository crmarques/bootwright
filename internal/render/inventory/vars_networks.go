package inventory

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render/installer"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

func clusterNetworksVars(state v1alpha1.State, ci v1alpha1.ClusterInstall) []any {
	networks := stateview.ClusterNetworkConfigs(state, ci)
	out := make([]any, 0, len(networks))
	for _, n := range networks {
		entry := map[string]any{
			"name":           n.Metadata.Name,
			"cidr":           firstMachineNetworkCIDR(n),
			"gateway":        networkConfigGateway(n),
			"machineNetwork": machineNetworkCIDRVars(n.Spec.MachineNetwork),
			"dnsServers":     installer.ResolveClusterDNSServers(state, ci, n),
			"template":       n.Spec.Template.NetworkConfig,
		}
		out = append(out, entry)
	}
	return out
}

func networkConfigGateway(n v1alpha1.NetworkConfig) string {
	routes, ok := n.Spec.Template.NetworkConfig["routes"].(map[string]any)
	if !ok {
		return ""
	}
	config, ok := routes["config"].([]any)
	if !ok {
		return ""
	}
	for _, item := range config {
		route, ok := item.(map[string]any)
		if !ok {
			continue
		}
		destination, _ := route["destination"].(string)
		if destination != "0.0.0.0/0" && destination != "::/0" {
			continue
		}
		nextHop, _ := route["next-hop-address"].(string)
		if nextHop != "" {
			return nextHop
		}
	}
	return ""
}

func machineNetworkCIDRVars(items []v1alpha1.MachineNetworkCIDR) []any {
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{"cidr": item.CIDR})
	}
	return out
}
