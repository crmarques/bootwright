package render

import (
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/proxy"
)

// Vars produces the per-cluster Ansible variables consumed by the
// layers under ansible/playbooks/. The contract is documented in
// ADR-0002: each machine component carries diagnostic dispatch labels
// plus exact rendered role names; provider services carry their own
// apply/destroy role names.
//
// Vars is the public entry point; the implementation is split across
// vars_provider.go, vars_networks.go, vars_components.go, and
// vars_dispatch.go so each concern stays under the size lint and
// editing one shape (e.g. networking) doesn't touch the rest.
func Vars(state v1alpha1.State) map[string]any {
	return VarsWithSecretsDir(state, "")
}

func VarsWithSecretsDir(state v1alpha1.State, secretsDir string) map[string]any {
	env := primaryEnvironment(state)
	clusters := make([]any, 0, len(state.ContainerClusters))
	for _, ocp := range state.ContainerClusters {
		ci, err := clusterInfraForOCP(state, ocp)
		if err != nil {
			continue
		}
		entry := map[string]any{
			"name":                   ocp.Metadata.Name,
			"installMode":            v1alpha1.InstallMode(ocp),
			"installMethod":          ocp.Spec.Install.Method,
			"baseDomain":             environmentBaseDomain(env),
			"endpoints":              endpointsVars(state, ci),
			"networks":               clusterNetworksVars(state, ci),
			"components":             componentsVars(state, ci, ocp, secretsDir),
			"nodes":                  nodesVars(ocp),
			"agentIsoPublishTargets": agentISOPublishTargets(state, ci, ocp),
		}
		if keyPath := nodeSSHPrivateKeyPath(env, ocp, secretsDir); keyPath != "" {
			entry["nodeSSHPrivateKeyPath"] = keyPath
		}
		if ocp.Spec.Distribution.Type != "" || ocp.Spec.Distribution.Release.Version != "" || ocp.Spec.Distribution.Release.Image != "" {
			entry["distribution"] = distributionVars(ocp)
		}
		clusters = append(clusters, entry)
	}
	out := map[string]any{
		"bootwright_environment":      environmentVars(env),
		"bootwright_hosts":            hostsVars(state),
		"bootwright_providers":        providersVars(state),
		"bootwright_infra_components": infraComponentsVars(state),
		"bootwright_clusters":         clusters,
		"bootwright_component_pins":   componentPinsVars(state),
	}
	if services := providerServicesVars(state); len(services) > 0 {
		out["bootwright_provider_services"] = services
	}
	if setups := providerHostSetupsVars(state); len(setups) > 0 {
		out["bootwright_provider_host_setups"] = setups
	}
	if proxyVars := bootwrightProxyVars(state, env); len(proxyVars) > 0 {
		out["bootwright_proxy"] = proxyVars
	}
	return out
}

// componentPinsVars projects the lock's component pins into the
// Ansible vars shape for operator-visible diagnostics. Runtime roles
// consume renderer-projected image/tool fields on their components.
// Same data as render.Lock(); kept in sync via ComponentPins().
func componentPinsVars(state v1alpha1.State) []any {
	pins := ComponentPins(state)
	out := make([]any, 0, len(pins))
	for _, p := range pins {
		out = append(out, map[string]any{
			"name":       p.Name,
			"version":    p.Version,
			"source":     p.Source,
			"lookupDate": p.LookupDate,
		})
	}
	return out
}

func environmentVars(env *v1alpha1.Environment) map[string]any {
	if env == nil {
		return map[string]any{}
	}
	out := map[string]any{
		"name":       env.Metadata.Name,
		"baseDomain": env.Spec.BaseDomain,
	}
	if len(env.Spec.ContainerClusters) > 0 {
		out["containerClusters"] = stringSliceAny(env.Spec.ContainerClusters)
	}
	proxyFor := map[string]any{}
	if env.Spec.ProxyFor.Bootwright != "" {
		proxyFor["bootwright"] = env.Spec.ProxyFor.Bootwright
	}
	if env.Spec.ProxyFor.ClusterInstall != "" {
		proxyFor["clusterInstall"] = env.Spec.ProxyFor.ClusterInstall
	}
	if len(proxyFor) > 0 {
		out["proxyFor"] = proxyFor
	}
	if infra := environmentInfraComponentsVars(env); len(infra) > 0 {
		out["infraComponents"] = infra
	}
	if env.Spec.Registries != nil && env.Spec.Registries.Mirror != nil {
		mirror := map[string]any{}
		if env.Spec.Registries.Mirror.URL != "" {
			mirror["url"] = env.Spec.Registries.Mirror.URL
		}
		if env.Spec.Registries.Mirror.CredentialsRef.Name != "" {
			mirror["credentialsRef"] = env.Spec.Registries.Mirror.CredentialsRef.Name
		}
		if env.Spec.Registries.Mirror.TrustBundleRef.Name != "" {
			mirror["trustBundleRef"] = env.Spec.Registries.Mirror.TrustBundleRef.Name
		}
		out["mirror"] = mirror
	}
	if len(env.Spec.ComponentImages) > 0 {
		out["componentImages"] = componentImagesVars(env.Spec.ComponentImages)
	}
	return out
}

func componentImagesVars(images map[string]map[string]v1alpha1.ComponentImageSpec) map[string]any {
	out := map[string]any{}
	for category, byType := range images {
		categoryOut := map[string]any{}
		for typ, image := range byType {
			entry := map[string]any{}
			if image.Local != "" {
				entry["local"] = image.Local
			}
			if image.Public != "" {
				entry["public"] = image.Public
			}
			categoryOut[typ] = entry
		}
		out[category] = categoryOut
	}
	return out
}

func hostsVars(state v1alpha1.State) []any {
	out := make([]any, 0, len(state.Hosts))
	for _, h := range state.Hosts {
		entry := map[string]any{
			"name":         h.Metadata.Name,
			"addresses":    hostAddressesVars(h),
			"capabilities": h.Spec.Capabilities,
		}
		if h.Spec.SSH != nil {
			entry["sshAddress"] = v1alpha1.HostSSHAddress(h)
			entry["sshAddressName"] = h.Spec.SSH.AddressName
			entry["sshUser"] = h.Spec.SSH.User
			entry["sshKeyName"] = h.Spec.SSH.KeyRef.Name
		}
		out = append(out, entry)
	}
	return out
}

func hostAddressesVars(host v1alpha1.Host) []any {
	out := make([]any, 0, len(host.Spec.Addresses))
	for _, address := range host.Spec.Addresses {
		out = append(out, map[string]any{
			"name":    address.Name,
			"address": address.Address,
		})
	}
	return out
}

func distributionVars(ocp v1alpha1.ContainerCluster) map[string]any {
	release := map[string]any{}
	if ocp.Spec.Distribution.Release.Version != "" {
		release["version"] = ocp.Spec.Distribution.Release.Version
	}
	if channel := v1alpha1.ReleaseChannel(ocp); channel != "" {
		release["channel"] = channel
	}
	if ocp.Spec.Distribution.Release.Image != "" {
		release["image"] = ocp.Spec.Distribution.Release.Image
	}
	return map[string]any{
		"type":    v1alpha1.DistributionType(ocp),
		"release": release,
	}
}

func nodesVars(ocp v1alpha1.ContainerCluster) map[string]any {
	nodes := sortedNodes(ocp.Spec.Nodes)
	out := map[string]any{}
	for _, node := range nodes {
		name := node.Hostname
		entry := map[string]any{"role": node.Role}
		if node.MachineRef.Name != "" || node.MachineRef.ClusterInfra != "" {
			entry["machineRef"] = map[string]any{
				"clusterInfra": node.MachineRef.ClusterInfra,
				"name":         node.MachineRef.Name,
			}
		}
		out[name] = entry
	}
	return out
}

func effectiveProxyVars(eff *proxy.Effective) map[string]any {
	out := map[string]any{}
	if eff.HTTP != "" {
		out["http"] = eff.HTTP
	}
	if eff.HTTPS != "" {
		out["https"] = eff.HTTPS
	}
	if len(eff.NoProxy) > 0 {
		// Stable order so downstream tests can diff.
		sorted := append([]string(nil), eff.NoProxy...)
		sort.SliceStable(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
		out["noProxy"] = sorted
	}
	if eff.Auth.Name != "" {
		out["auth"] = map[string]any{"proxyAuthRef": eff.Auth.Name}
	}
	return out
}

func bootwrightProxyVars(state v1alpha1.State, env *v1alpha1.Environment) map[string]any {
	eff := proxy.Resolve(state, env)
	if eff == nil {
		return nil
	}
	return effectiveProxyVars(eff)
}
