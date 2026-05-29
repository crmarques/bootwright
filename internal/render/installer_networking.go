package render

import "github.com/crmarques/bootwright/api/v1alpha1"

// networkingConfig fills install-config.yaml's networking block. The
// renderer owns machineNetwork (derived from each referenced NetworkConfig);
// the user owns clusterNetwork / serviceNetwork (declared on
// ContainerCluster.spec.networking).
func networkingConfig(state v1alpha1.State, ci v1alpha1.ClusterInfra, ocp v1alpha1.ContainerCluster) map[string]any {
	result := map[string]any{
		"machineNetwork": machineNetworkConfig(state, ci),
	}
	if ocp.Spec.Networking == nil {
		return result
	}
	if ocp.Spec.Networking.NetworkType != "" {
		result["networkType"] = ocp.Spec.Networking.NetworkType
	}
	if len(ocp.Spec.Networking.ClusterNetwork) > 0 {
		result["clusterNetwork"] = clusterNetworkConfig(ocp.Spec.Networking.ClusterNetwork)
	}
	if len(ocp.Spec.Networking.ServiceNetwork) > 0 {
		result["serviceNetwork"] = serviceNetworkConfig(ocp.Spec.Networking.ServiceNetwork)
	}
	return result
}

func machineNetworkConfig(state v1alpha1.State, ci v1alpha1.ClusterInfra) []any {
	names := clusterNetworkConfigNames(ci)
	out := make([]any, 0, len(names))
	for _, name := range names {
		n, ok := findNetworkConfig(state, name)
		if !ok {
			continue
		}
		for _, mn := range n.Spec.MachineNetwork {
			out = append(out, map[string]any{"cidr": mn.CIDR})
		}
	}
	return out
}

func clusterNetworkConfig(networks []v1alpha1.ContainerClusterNetworkCIDR) []any {
	out := make([]any, 0, len(networks))
	for _, n := range networks {
		entry := map[string]any{"cidr": n.CIDR}
		if n.HostPrefix > 0 {
			entry["hostPrefix"] = n.HostPrefix
		}
		out = append(out, entry)
	}
	return out
}

func serviceNetworkConfig(networks []string) []any {
	out := make([]any, 0, len(networks))
	for _, c := range networks {
		out = append(out, c)
	}
	return out
}
