package inventory

import (
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/proxy"
	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/render/installer"
	secret "github.com/crmarques/bootwright/internal/secrets"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

func Vars(state v1alpha1.State) map[string]any {
	return VarsWithSecretsDir(state, "")
}

func VarsWithSecretsDir(state v1alpha1.State, secretsDir string) map[string]any {
	return VarsWithSecretsDirAndOwnership(state, secretsDir, nil)
}

func VarsWithSecretsDirAndOwnership(state v1alpha1.State, secretsDir string, ownershipRecords []ownership.ResourceRecord) map[string]any {
	return VarsWithPathOptionsAndOwnership(state, PathOptions{SecretsDir: secretsDir}, ownershipRecords)
}

func VarsWithPathOptionsAndOwnership(state v1alpha1.State, paths PathOptions, ownershipRecords []ownership.ResourceRecord) map[string]any {
	paths.SecretIndex = secret.NewIndex(state)
	env := stateview.Environment(state)
	clusters := make([]any, 0, len(state.ContainerClusters))
	for _, ocp := range state.ContainerClusters {
		ci, err := clusterInstallForOCP(state, ocp)
		if err != nil {
			continue
		}
		entry := map[string]any{
			"name":                   ocp.Metadata.Name,
			"installMode":            v1alpha1.InstallMode(ocp),
			"installMethod":          ocp.Spec.Install.Method,
			"baseDomain":             installer.EnvironmentBaseDomain(env),
			"endpoints":              endpointsVars(state, ci),
			"networks":               clusterNetworksVars(state, ci),
			"components":             componentsVars(state, ci, ocp, paths.SecretsDir),
			"nodes":                  nodesVars(ocp),
			"agentIsoPublishTargets": agentISOPublishTargets(state, ci, ocp),
		}
		if keyPath := nodeSSHPrivateKeyPath(state, paths.SecretIndex, ocp, paths.SecretsDir); keyPath != "" {
			entry["nodeSSHPrivateKeyPath"] = keyPath
		}
		if resolvers := ClusterControllerNameResolvers(state, ci); len(resolvers) > 0 {
			entry["controllerNameResolvers"] = resolvers
		}
		if ocp.Spec.Distribution.Type != "" || ocp.Spec.Distribution.Release.Version != "" || ocp.Spec.Distribution.Release.Image != "" {
			entry["distribution"] = distributionVars(ocp)
		}
		if ocp.Spec.Security.FIPS.Enabled {
			entry["fips"] = true
		}
		clusters = append(clusters, entry)
	}
	out := map[string]any{
		"bootwright_environment":      environmentVars(env),
		"bootwright_machines":         machinesVars(state),
		"bootwright_providers":        providersVars(state),
		"bootwright_infra_components": infraComponentsVars(state),
		"bootwright_clusters":         clusters,
		"bootwright_component_pins":   componentPinsVars(state),
	}
	if services := providerServicesVars(state); len(services) > 0 {
		out["bootwright_provider_services"] = services
	}
	if services := infraComponentServicesVars(state); len(services) > 0 {
		out["bootwright_infra_component_services"] = services
	}
	if setups := providerMachineSetupsVars(state); len(setups) > 0 {
		out["bootwright_provider_machine_setups"] = setups
	}
	if storageClusters := storageClustersVars(state, paths); len(storageClusters) > 0 {
		out["bootwright_storage_clusters"] = storageClusters
	}
	if osInstallGroups := managedMachineOSInstallGroupsVars(state, paths); len(osInstallGroups) > 0 {
		out["bootwright_managed_os_install_groups"] = osInstallGroups
	}
	if proxyVars := bootwrightProxyVars(state, env); len(proxyVars) > 0 {
		out["bootwright_proxy"] = proxyVars
	}
	if ntpSources := stateview.ResolvedAdditionalNTPSources(state); len(ntpSources) > 0 {
		out["bootwright_resolved_ntp_sources"] = stringSliceAny(ntpSources)
	}
	if records := ownershipRecordsVars(ownershipRecords); len(records) > 0 {
		out["bootwright_ownership_records"] = records
		out["bootwright_ownership_records_by_kind"] = ownershipRecordsByKindVars(ownershipRecords)
	}
	return out
}

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
		"baseDomain": env.Spec.Domains.ContainerClustersDomain(),
	}
	if len(env.Spec.ContainerClusters) > 0 {
		out["containerClusters"] = stringSliceAny(env.Spec.ContainerClusters)
	}
	proxyFor := map[string]any{}
	for _, consumer := range []string{
		v1alpha1.ProxyConsumerBootwright,
		v1alpha1.ProxyConsumerContainerClusterInstall,
		v1alpha1.ProxyConsumerMachineOSInstall,
	} {
		if name := env.Spec.ProxyNameFor(consumer); name != "" {
			proxyFor[consumer] = name
		}
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

func machinesVars(state v1alpha1.State) []any {
	out := make([]any, 0, len(state.Machines))
	for _, h := range state.Machines {
		entry := map[string]any{
			"name":         h.Metadata.Name,
			"addresses":    machineAddressesVars(h),
			"capabilities": h.Spec.Capabilities,
		}
		if h.Spec.Access.SSH != nil {
			entry["sshAddress"] = v1alpha1.MachineSSHAddress(h)
			entry["sshAddressName"] = h.Spec.Access.SSH.AddressRef.Name
			entry["sshUser"] = h.Spec.Access.SSH.User
			entry["sshKeyName"] = h.Spec.Access.SSH.KeyRef.Name
		}
		out = append(out, entry)
	}
	return out
}

func machineAddressesVars(host v1alpha1.Machine) []any {
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
	nodes := installer.SortedNodes(ocp.Spec.Nodes)
	out := map[string]any{}
	for _, node := range nodes {
		name := node.Name
		entry := map[string]any{"role": installer.InstallerNodeRole(node.Role)}
		if node.MachineRef.Name != "" {
			entry["machineRef"] = map[string]any{
				"name": node.MachineRef.Name,
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
		sorted := append([]string(nil), eff.NoProxy...)
		sort.SliceStable(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
		out["noProxy"] = sorted
		if literal := proxy.NoProxyForLiteralMatchers(eff); len(literal) > 0 {
			sortedLiteral := append([]string(nil), literal...)
			sort.SliceStable(sortedLiteral, func(i, j int) bool { return sortedLiteral[i] < sortedLiteral[j] })
			out["noProxyLiteral"] = sortedLiteral
		}
	}
	if eff.Auth.Name != "" {
		out["auth"] = map[string]any{"proxyAuthRef": eff.Auth.Name}
	}
	if eff.TrustBundle.Name != "" {
		out["trustBundleRef"] = eff.TrustBundle.Name
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
