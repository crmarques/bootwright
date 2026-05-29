package render

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/state/view"
)

// clusterNetworksVars exposes the NetworkConfig templates a cluster
// consumes so Ansible roles can resolve substrate hints and machine
// network CIDRs without scanning the global state.
func clusterNetworksVars(state v1alpha1.State, ci v1alpha1.ClusterInfra) []any {
	names := clusterNetworkConfigNames(ci)
	out := make([]any, 0, len(names))
	for _, name := range names {
		n, ok := findNetworkConfig(state, name)
		if !ok {
			continue
		}
		entry := map[string]any{
			"name":           n.Metadata.Name,
			"cidr":           firstMachineNetworkCIDR(n),
			"gateway":        networkConfigGateway(n),
			"machineNetwork": machineNetworkCIDRVars(n.Spec.MachineNetwork),
			"dnsServers":     resolveClusterDNSServers(state, ci, n),
			"template":       n.Spec.Template.NetworkConfig,
			"substrate":      networkConfigSubstrateVars(n),
		}
		out = append(out, entry)
	}
	return out
}

func clusterNetworkConfigNames(ci v1alpha1.ClusterInfra) []string {
	return stateview.ClusterConsumedNetworkConfigs(ci)
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

func networkConfigSubstrateVars(n v1alpha1.NetworkConfig) map[string]any {
	out := map[string]any{}
	switch {
	case n.Spec.Libvirt != nil:
		out["kind"] = v1alpha1.ProvisionerLibvirt
		out["libvirt"] = map[string]any{"bridge": n.Spec.Libvirt.Bridge}
	case n.Spec.VSphere != nil:
		out["kind"] = v1alpha1.ProvisionerVSphere
		out["vsphere"] = map[string]any{"portgroup": n.Spec.VSphere.Portgroup}
	case n.Spec.KubeVirt != nil:
		out["kind"] = v1alpha1.ProvisionerKubeVirt
		out["kubevirt"] = map[string]any{"nad": n.Spec.KubeVirt.NAD}
	case n.Spec.Physical != nil:
		out["kind"] = v1alpha1.ProvisionerBareMetal
		if n.Spec.Physical.VLAN != 0 {
			out["physical"] = map[string]any{"vlan": n.Spec.Physical.VLAN}
		} else {
			out["physical"] = map[string]any{}
		}
	}
	return out
}
