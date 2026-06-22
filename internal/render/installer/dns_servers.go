package installer

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/nmstate"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

func ResolveClusterDNSServers(state v1alpha1.State, ci v1alpha1.ClusterInstall, network v1alpha1.NetworkConfig) []string {
	return resolveClusterDNSServersFromConfig(state, ci, network, network.Spec.Template.NetworkConfig)
}

func resolveClusterDNSServersFromConfig(state v1alpha1.State, ci v1alpha1.ClusterInstall, network v1alpha1.NetworkConfig, config map[string]any) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, server := range nmstate.NetworkConfigDNSServers(config) {
		if seen[server] {
			continue
		}
		seen[server] = true
		out = append(out, server)
	}
	env := stateview.Environment(state)
	if env == nil {
		return out
	}
	for _, ref := range network.Spec.NameResolutionRefs {
		entry, ok := stateview.NameResolutionEntry(env, ref.Name)
		if !ok {
			continue
		}
		ip := stateview.ResolvedNameResolutionIP(state, ci, network, entry)
		if ip == "" || seen[ip] {
			continue
		}
		seen[ip] = true
		out = append(out, ip)
	}
	return out
}

func renderDNSServers(config map[string]any, servers []string) {
	if len(servers) == 0 {
		return
	}
	resolver := ensureMap(config, "dns-resolver")
	resolverConfig := ensureMap(resolver, "config")
	rendered := make([]any, 0, len(servers))
	for _, server := range servers {
		rendered = append(rendered, server)
	}
	resolverConfig["server"] = rendered
}

func ensureMap(config map[string]any, key string) map[string]any {
	if out, ok := config[key].(map[string]any); ok {
		return out
	}
	out := map[string]any{}
	config[key] = out
	return out
}
