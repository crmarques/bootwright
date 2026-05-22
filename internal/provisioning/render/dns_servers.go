package render

import "github.com/crmarques/bootwright/api/v1alpha1"

// resolveClusterDNSServers returns DNS servers declared in the NMState
// NetworkConfig template, with the managed dnsmasq service IP prepended
// when it can be derived.
func resolveClusterDNSServers(state v1alpha1.State, ci v1alpha1.ClusterInfra, network v1alpha1.NetworkConfig) []string {
	base := templateDNSServers(network)
	nr := ci.Spec.Components.NameResolution
	if nr == nil {
		return base
	}
	d, ok := resolveDNS(state, nr.From)
	if !ok || d.Dnsmasq == nil {
		return base
	}
	ip := v1alpha1.DNSServiceIP(nr, network)
	if ip == "" {
		return base
	}
	for _, s := range base {
		if s == ip {
			return base
		}
	}
	return append([]string{ip}, base...)
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
