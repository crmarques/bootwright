package render

import "github.com/crmarques/bootwright/api/v1alpha1"

// dnsRefs are Bootwright-only input and must not be emitted into NMState.
func resolveClusterDNSServers(state v1alpha1.State, ci v1alpha1.ClusterInfra, network v1alpha1.NetworkConfig) []string {
	out := append([]string(nil), templateDNSServers(network)...)
	env := primaryEnvironment(state)
	if env == nil {
		return out
	}
	seen := map[string]bool{}
	for _, server := range out {
		seen[server] = true
	}
	for _, ref := range network.Spec.Template.DNSRefs {
		entry, ok := nameResolutionEntry(env, ref)
		if !ok {
			continue
		}
		ip := resolvedNameResolutionIP(state, ci, network, entry)
		if ip == "" || seen[ip] {
			continue
		}
		seen[ip] = true
		out = append(out, ip)
	}
	return out
}

func templateDNSServers(network v1alpha1.NetworkConfig) []string {
	rawResolver, ok := network.Spec.Template.NetworkConfig["dns-resolver"].(map[string]any)
	if !ok {
		return nil
	}
	rawConfig, ok := rawResolver["config"].(map[string]any)
	if !ok {
		return nil
	}
	rawServers, ok := rawConfig["server"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(rawServers))
	for _, raw := range rawServers {
		if server, ok := raw.(string); ok && server != "" {
			out = append(out, server)
		}
	}
	return out
}
