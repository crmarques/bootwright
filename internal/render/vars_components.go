package render

import (
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/artifacts"
	"github.com/crmarques/bootwright/internal/infra/proxy"
	"github.com/crmarques/bootwright/internal/infra/support"
	secret "github.com/crmarques/bootwright/internal/runtime/secrets"
	"github.com/crmarques/bootwright/internal/state/view"
)

// componentsVars walks every component slot on a cluster and emits the
// per-component vars consumed by ansible/playbooks/layers/.
func componentsVars(state v1alpha1.State, ci v1alpha1.ClusterInfra, ocp v1alpha1.ContainerCluster, secretsDir string) []any {
	var out []any
	clusterName := ocp.Metadata.Name

	for _, m := range ci.Spec.Components.Machines {
		out = append(out, machineComponentVars(state, ci, m, clusterName, secretsDir))
	}
	for _, component := range loadBalancerComponentsForCluster(state, ci, ocp) {
		lb := loadBalancerComponentVars(state, component)
		lb["clusterName"] = clusterName
		lb["frontends"] = loadBalancerFrontends(state, ci, component.Metadata.Name, clusterName, ci.Spec.Components.Machines, clusterNodesForCI(state, ci))
		out = append(out, lb)
	}
	if artifacts.ClusterNeedsPublication(state, ci, ocp) {
		if server, ok := artifacts.Select(state, ci); ok && server.Config != nil {
			out = append(out, artifactServerComponentVars(state, ci, server))
		}
	}
	for _, selected := range proxyComponentsForCluster(state) {
		out = append(out, proxyComponentVars(state, selected.entry, selected.component))
	}
	for _, selected := range nameResolutionComponentsForCluster(state, ci) {
		out = append(out, nameResolutionComponentVars(state, selected.entry, selected.component))
	}
	for _, selected := range ntpComponentsForCluster(state) {
		out = append(out, ntpComponentVars(state, selected.entry, selected.component))
	}
	if selected, ok := registryComponentForCluster(state, ocp); ok {
		out = append(out, registryComponentVars(state, selected.entry, selected.component))
	}
	return out
}

type selectedProxyComponent struct {
	entry     v1alpha1.EnvironmentProxyComponent
	component v1alpha1.InfraComponent
}

type selectedNameResolutionComponent struct {
	entry     v1alpha1.EnvironmentNameResolutionComponent
	component v1alpha1.InfraComponent
}

type selectedNTPComponent struct {
	entry     v1alpha1.EnvironmentNTPSourceComponent
	component v1alpha1.InfraComponent
}

type selectedRegistryComponent struct {
	entry     v1alpha1.EnvironmentRegistryComponent
	component v1alpha1.InfraComponent
}

func loadBalancerComponentsForCluster(state v1alpha1.State, ci v1alpha1.ClusterInfra, ocp v1alpha1.ContainerCluster) []v1alpha1.InfraComponent {
	seen := map[string]bool{}
	out := []v1alpha1.InfraComponent{}
	for _, role := range standardEndpointNames {
		endpoint, ok := containerEndpoint(ci, ocp, role)
		if !ok || endpoint.Source.Type != v1alpha1.EndpointSourceInfraComponent || endpoint.Source.ComponentRef.Name == "" {
			continue
		}
		name := endpoint.Source.ComponentRef.Name
		if seen[name] {
			continue
		}
		seen[name] = true
		component, ok := findInfraComponent(state, name)
		if ok && component.Spec.LoadBalancer != nil {
			out = append(out, component)
		}
	}
	return out
}

func proxyComponentsForCluster(state v1alpha1.State) []selectedProxyComponent {
	env := primaryEnvironment(state)
	if env == nil {
		return nil
	}
	seen := map[string]bool{}
	out := []selectedProxyComponent{}
	for _, name := range []string{env.Spec.ProxyFor.Bootwright, env.Spec.ProxyFor.ContainerClusterInstall} {
		entry, ok := proxy.SelectedProxy(*env, name)
		if !ok || entry.Type != v1alpha1.EnvironmentComponentManaged || entry.ComponentRef.Name == "" {
			continue
		}
		if seen[entry.ComponentRef.Name] {
			continue
		}
		seen[entry.ComponentRef.Name] = true
		component, ok := findInfraComponent(state, entry.ComponentRef.Name)
		if ok && component.Spec.Proxy != nil {
			out = append(out, selectedProxyComponent{entry: entry, component: component})
		}
	}
	return out
}

func nameResolutionComponentsForCluster(state v1alpha1.State, ci v1alpha1.ClusterInfra) []selectedNameResolutionComponent {
	env := primaryEnvironment(state)
	if env == nil {
		return nil
	}
	seen := map[string]bool{}
	out := []selectedNameResolutionComponent{}
	for _, network := range stateview.ClusterNetworkConfigs(state, ci) {
		for _, ref := range network.Spec.DNSRefs {
			entry, ok := nameResolutionEntry(env, ref)
			if !ok || entry.Type != v1alpha1.EnvironmentComponentManaged || entry.ComponentRef.Name == "" {
				continue
			}
			if seen[entry.ComponentRef.Name] {
				continue
			}
			seen[entry.ComponentRef.Name] = true
			component, ok := findInfraComponent(state, entry.ComponentRef.Name)
			if ok && component.Spec.NameResolution != nil {
				out = append(out, selectedNameResolutionComponent{entry: entry, component: component})
			}
		}
	}
	return out
}

func ntpComponentsForCluster(state v1alpha1.State) []selectedNTPComponent {
	env := primaryEnvironment(state)
	if env == nil {
		return nil
	}
	seen := map[string]bool{}
	out := []selectedNTPComponent{}
	for _, entry := range env.Spec.InfraComponents.NTPSources {
		if entry.Type != v1alpha1.EnvironmentComponentManaged || entry.ComponentRef.Name == "" {
			continue
		}
		if seen[entry.ComponentRef.Name] {
			continue
		}
		seen[entry.ComponentRef.Name] = true
		component, ok := findInfraComponent(state, entry.ComponentRef.Name)
		if ok && component.Spec.NTP != nil {
			out = append(out, selectedNTPComponent{entry: entry, component: component})
		}
	}
	return out
}

func registryComponentForCluster(state v1alpha1.State, ocp v1alpha1.ContainerCluster) (selectedRegistryComponent, bool) {
	if v1alpha1.InstallMode(ocp) != v1alpha1.InstallModeDisconnected {
		return selectedRegistryComponent{}, false
	}
	entry, ok := selectedRegistryEntry(primaryEnvironment(state))
	if !ok || entry.Type != v1alpha1.EnvironmentComponentManaged || entry.ComponentRef.Name == "" {
		return selectedRegistryComponent{}, false
	}
	component, ok := findInfraComponent(state, entry.ComponentRef.Name)
	if !ok || component.Spec.Registry == nil {
		return selectedRegistryComponent{}, false
	}
	return selectedRegistryComponent{entry: entry, component: component}, true
}

func endpointsVars(state v1alpha1.State, ci v1alpha1.ClusterInfra) []any {
	out := make([]any, 0, len(ci.Spec.Endpoints))
	names := make([]string, 0, len(ci.Spec.Endpoints))
	for name := range ci.Spec.Endpoints {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		e := ci.Spec.Endpoints[name]
		entry := map[string]any{
			"name": name,
		}
		if address := endpointAddress(state, ci, name); address != "" {
			entry["address"] = address
		}
		if e.DNSName != "" {
			entry["dnsName"] = e.DNSName
		}
		if e.Port > 0 {
			entry["port"] = e.Port
		}
		if e.Scheme != "" {
			entry["scheme"] = e.Scheme
		}
		if e.PrefixLength > 0 {
			entry["prefixLength"] = e.PrefixLength
		}
		if len(e.InterfaceNetworks) > 0 {
			entry["interfaceNetworks"] = stringSliceAny(e.InterfaceNetworks)
		}
		if e.Source.Type != "" {
			source := map[string]any{"type": e.Source.Type}
			if e.Source.ComponentRef.Name != "" {
				source["componentRef"] = e.Source.ComponentRef.Name
			}
			if e.Source.BindAddress != "" {
				source["bindAddress"] = e.Source.BindAddress
			}
			entry["source"] = source
		}
		out = append(out, entry)
	}
	return out
}

func machineComponentVars(state v1alpha1.State, ci v1alpha1.ClusterInfra, m v1alpha1.ClusterMachineComponent, clusterName string, secretsDir string) map[string]any {
	driver := ProviderDriver(state, m)
	out := map[string]any{
		"kind":          v1alpha1.ComponentSlotMachines,
		"name":          m.Name,
		"providerName":  m.From.Provider,
		"substrateRole": driver.Dispatch.SubstrateRole,
		"bmcRole":       driver.Dispatch.BMCRole,
		"bootRole":      driver.Dispatch.BootRole,
		"networkConfig": clusterMachineNetworkConfigVars(m.NetworkConfig),
	}
	if attachment := clusterMachineNetworkAttachmentVars(state, ci, m); attachment != nil {
		out["networkAttachment"] = attachment
	}
	applyMachineRoleContract(out, driver.Roles)
	if ip := machinePrimaryIP(state, ci, m); ip != "" {
		out["primaryIPAddress"] = ip
	}
	if interfaces := machineInterfaces(state, m, clusterName); len(interfaces) > 0 {
		out["interfaces"] = machineInterfaceVars(interfaces)
	}
	if hostRef := machineHostRef(state, m); hostRef != "" {
		out["hostRef"] = hostRef
		out["hostAddress"] = lookupHostAddress(state, hostRef)
	}
	if m.From.Profile != "" {
		out["fromProfile"] = m.From.Profile
		// Inline the profile spec so Ansible roles do not need to
		// resolve back across the provider list.
		if provider, ok := findProvider(state, m.From.Provider); ok {
			if profile, ok := findProfile(provider, m.From.Profile); ok {
				out["profile"] = map[string]any{
					"name":      profile.Name,
					"cpu":       profile.CPU,
					"memoryMiB": profile.MemoryMiB,
					"diskGiB":   profile.DiskGiB,
				}
				if l := profile.Libvirt; l != nil {
					out["libvirt"] = map[string]any{
						"hostRef": l.HostRef.Name,
						"uri":     l.URI,
					}
					if be := machineEmulatedBMCVars(state, profile); be != nil && driver.Dispatch.BMCRole == "emulated" {
						out["bmcEmulated"] = be
					}
				}
				if profile.VSphere != nil {
					out["vsphere"] = machineProfileProvisionerVars(profile)
				}
				if k := profile.KubeVirt; k != nil {
					out["kubevirt"] = map[string]any{
						"namespace": k.Namespace,
					}
					if k.HostContainerClusterRef != nil {
						out["kubevirt"].(map[string]any)["hostContainerClusterRef"] = k.HostContainerClusterRef.Name
						out["kubevirt"].(map[string]any)["kubeconfig"] = "{{ bootwright_clusters_dir }}/" + k.HostContainerClusterRef.Name + "/secrets/kubeconfig"
					}
					if k.KubeconfigRef != nil {
						out["kubevirt"].(map[string]any)["kubeconfigRef"] = k.KubeconfigRef.Name
						if path := secret.ResolvePath(k.KubeconfigRef.Name, primaryEnvironment(state), secretsDir); path != "" {
							out["kubevirt"].(map[string]any)["kubeconfig"] = path
						}
					}
					if k.StorageClassRef != nil {
						out["kubevirt"].(map[string]any)["storageClassRef"] = k.StorageClassRef.Name
					}
				}
			}
		}
	}
	if m.From.Name != "" {
		out["fromName"] = m.From.Name
		if provider, ok := findProvider(state, m.From.Provider); ok {
			if server, ok := findProviderMachine(provider, m.From.Name); ok {
				serverVars := map[string]any{
					"name":       server.Name,
					"interfaces": providerInterfacesVars(server),
				}
				if len(server.Labels) > 0 {
					serverVars["labels"] = server.Labels
				}
				out["server"] = serverVars
				if server.BareMetal != nil {
					out["bmc"] = map[string]any{
						"address":                        server.BareMetal.BMC.Address,
						"protocol":                       server.BareMetal.BMC.Protocol,
						"credentialsRef":                 server.BareMetal.BMC.CredentialsRef.Name,
						"disableCertificateVerification": server.BareMetal.BMC.DisableCertificateVerification,
					}
				}
			}
		}
	}
	if hints := machineRootDeviceHints(state, m); hints != nil {
		out["rootDeviceHints"] = rootDeviceHintsConfig(hints)
	}
	if clusterName != "" {
		out["clusterName"] = clusterName
	}
	if boot := machineBootVars(state, ci, m, clusterName); boot != nil {
		out["boot"] = boot
	}
	return out
}

func applyMachineRoleContract(out map[string]any, roles support.RoleContract) {
	if len(roles.HostSetupRoles) > 0 {
		out["hostSetupRoles"] = stringSliceAny(roles.HostSetupRoles)
	}
	if roles.SubstrateApplyRole != "" {
		out["substrateApplyRole"] = roles.SubstrateApplyRole
	}
	if roles.SubstrateDestroyRole != "" {
		out["substrateDestroyRole"] = roles.SubstrateDestroyRole
	}
	if roles.BMCApplyRole != "" {
		out["bmcApplyRole"] = roles.BMCApplyRole
	}
	if roles.BMCDestroyRole != "" {
		out["bmcDestroyRole"] = roles.BMCDestroyRole
	}
	if roles.BootApplyRole != "" {
		out["bootApplyRole"] = roles.BootApplyRole
	}
	if roles.MediaPrepareRole != "" {
		out["mediaPrepareRole"] = roles.MediaPrepareRole
	}
	if roles.RequiresKVM {
		out["requiresKVM"] = true
	}
}

func machineHostRef(state v1alpha1.State, m v1alpha1.ClusterMachineComponent) string {
	if m.From.Profile == "" {
		return ""
	}
	provider, ok := findProvider(state, m.From.Provider)
	if !ok {
		return ""
	}
	profile, ok := findProfile(provider, m.From.Profile)
	if !ok {
		return ""
	}
	if profile.Libvirt != nil {
		return profile.Libvirt.HostRef.Name
	}
	if profile.KubeVirt != nil {
		return "localhost"
	}
	return ""
}
