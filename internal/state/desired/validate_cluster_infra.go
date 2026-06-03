package desiredstate

import (
	"fmt"
	"net"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func validateClusterInfras(state v1alpha1.State) []string {
	var errs []string
	providers := indexProviders(state.InfraProviders)
	hosts := indexHosts(state.Hosts)
	networkConfigs := indexNetworkConfigs(state.NetworkConfigs)
	dnsRefs := networkConfigDNSRefs(state)
	components := indexInfraComponents(state.InfraComponents)
	env := primaryEnvironment(&state)
	containerInfraNames := referencedContainerClusterInfraNames(state)
	seen := map[string]bool{}
	for _, ci := range state.ClusterInfras {
		if e := validateName(v1alpha1.KindClusterInfra, ci.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
		if seen[ci.Metadata.Name] {
			errs = append(errs, fmt.Sprintf("duplicate ClusterInfra %q", ci.Metadata.Name))
		}
		seen[ci.Metadata.Name] = true
		errs = append(errs, validateClusterPlatform(ci, containerInfraNames[ci.Metadata.Name])...)
		if containerInfraNames[ci.Metadata.Name] || len(ci.Spec.Endpoints) > 0 {
			errs = append(errs, validateClusterEndpoints(ci, components, networkConfigs, containerInfraNames[ci.Metadata.Name])...)
		}
		errs = append(errs, validateClusterArtifactAccess(ci, env, components)...)
		errs = append(errs, validateClusterNetworkBindings(ci, providers, networkConfigs)...)
		errs = append(errs, validateClusterNodes(ci, providers, hosts, networkConfigs, dnsRefs)...)
		errs = append(errs, validateClusterServices(ci, providers)...)
	}
	return errs
}

func validateClusterArtifactAccess(ci v1alpha1.ClusterInfra, env *v1alpha1.Environment, components map[string]v1alpha1.InfraComponent) []string {
	access := ci.Spec.ArtifactAccess
	if access.ServerRef.Name == "" &&
		access.RedfishVirtualMedia.EndpointRef.Name == "" &&
		access.ContainerClusterInstall.EndpointRef.Name == "" {
		return nil
	}
	prefix := fmt.Sprintf("ClusterInfra/%s spec.artifactAccess", ci.Metadata.Name)
	var errs []string
	if access.ServerRef.Name == "" {
		return []string{prefix + ".serverRef.name is required when artifactAccess endpoints are set"}
	}
	if env == nil {
		return []string{prefix + ".serverRef.name requires an Environment with spec.infraComponents.artifactServers"}
	}
	entry, ok := environmentArtifactServerByName(env, access.ServerRef.Name)
	if !ok {
		return []string{fmt.Sprintf("%s.serverRef.name %q does not resolve to Environment/%s spec.infraComponents.artifactServers[].name", prefix, access.ServerRef.Name, env.Metadata.Name)}
	}
	endpoints := artifactServerEndpointNames(entry, components)
	if entry.Type == v1alpha1.EnvironmentComponentManaged && len(endpoints) == 0 {
		errs = append(errs, fmt.Sprintf("%s.serverRef.name %q does not resolve to managed artifact server endpoints", prefix, access.ServerRef.Name))
	}
	errs = append(errs, validateClusterArtifactEndpointRef(prefix+".redfishVirtualMedia.endpointRef.name", access.RedfishVirtualMedia.EndpointRef.Name, endpoints)...)
	errs = append(errs, validateClusterArtifactEndpointRef(prefix+".containerClusterInstall.endpointRef.name", access.ContainerClusterInstall.EndpointRef.Name, endpoints)...)
	return errs
}

func environmentArtifactServerByName(env *v1alpha1.Environment, name string) (v1alpha1.EnvironmentArtifactServerComponent, bool) {
	for _, entry := range env.Spec.InfraComponents.ArtifactServers {
		if entry.Name == name {
			return entry, true
		}
	}
	return v1alpha1.EnvironmentArtifactServerComponent{}, false
}

func artifactServerEndpointNames(entry v1alpha1.EnvironmentArtifactServerComponent, components map[string]v1alpha1.InfraComponent) map[string]bool {
	out := map[string]bool{}
	switch entry.Type {
	case v1alpha1.EnvironmentComponentExternal:
		for _, endpoint := range entry.Endpoints {
			out[endpoint.Name] = true
		}
	case v1alpha1.EnvironmentComponentManaged:
		component, ok := components[entry.ComponentRef.Name]
		if !ok || component.Spec.ArtifactServer == nil {
			return out
		}
		for _, endpoint := range component.Spec.ArtifactServer.Endpoints {
			out[endpoint.Name] = true
		}
	}
	return out
}

func validateClusterArtifactEndpointRef(owner, name string, endpoints map[string]bool) []string {
	if name == "" {
		return nil
	}
	if !endpoints[name] {
		return []string{fmt.Sprintf("%s %q does not resolve to the selected artifact server endpoints", owner, name)}
	}
	return nil
}

func validateClusterPlatform(ci v1alpha1.ClusterInfra, required bool) []string {
	var errs []string
	prefix := fmt.Sprintf("ClusterInfra/%s spec.platform", ci.Metadata.Name)
	switch ci.Spec.Platform.Type {
	case "":
		if required || ci.Spec.Platform.BareMetal != nil || ci.Spec.Platform.VSphere != nil || ci.Spec.Platform.External != nil {
			errs = append(errs, fmt.Sprintf("%s.type is required", prefix))
		}
	case v1alpha1.PlatformTypeBareMetal:
		if ci.Spec.Platform.VSphere != nil || ci.Spec.Platform.External != nil {
			errs = append(errs, fmt.Sprintf("%s.type=baremetal must not set vsphere or external", prefix))
		}
	case v1alpha1.PlatformTypeVSphere:
		if ci.Spec.Platform.BareMetal != nil || ci.Spec.Platform.External != nil {
			errs = append(errs, fmt.Sprintf("%s.type=vsphere must not set baremetal or external", prefix))
		}
	case v1alpha1.PlatformTypeNone:
		if ci.Spec.Platform.BareMetal != nil || ci.Spec.Platform.VSphere != nil || ci.Spec.Platform.External != nil {
			errs = append(errs, fmt.Sprintf("%s.type=none must not set platform-specific config", prefix))
		}
	case v1alpha1.PlatformTypeExternal:
		if ci.Spec.Platform.BareMetal != nil || ci.Spec.Platform.VSphere != nil {
			errs = append(errs, fmt.Sprintf("%s.type=external must not set baremetal or vsphere", prefix))
		}
	default:
		errs = append(errs, fmt.Sprintf("%s.type %q must be one of {%s, %s, %s, %s}",
			prefix, ci.Spec.Platform.Type,
			v1alpha1.PlatformTypeBareMetal, v1alpha1.PlatformTypeVSphere, v1alpha1.PlatformTypeNone, v1alpha1.PlatformTypeExternal))
	}
	if bm := ci.Spec.Platform.BareMetal; bm != nil && bm.ProvisioningNetwork != "" {
		switch bm.ProvisioningNetwork {
		case v1alpha1.ProvisioningNetworkDisabled, v1alpha1.ProvisioningNetworkManaged, v1alpha1.ProvisioningNetworkUnmanaged:
		default:
			errs = append(errs, fmt.Sprintf("%s.baremetal.provisioningNetwork %q must be one of {%s, %s, %s}",
				prefix, bm.ProvisioningNetwork,
				v1alpha1.ProvisioningNetworkDisabled, v1alpha1.ProvisioningNetworkManaged, v1alpha1.ProvisioningNetworkUnmanaged))
		}
	}
	return errs
}

func validateClusterEndpoints(ci v1alpha1.ClusterInfra, components map[string]v1alpha1.InfraComponent, networkConfigs map[string]v1alpha1.NetworkConfig, requireOpenShiftEndpoints bool) []string {
	var errs []string
	if len(ci.Spec.Endpoints) == 0 {
		if requireOpenShiftEndpoints {
			errs = append(errs, fmt.Sprintf("ClusterInfra/%s spec.endpoints is required", ci.Metadata.Name))
		}
		return errs
	}
	for name, endpoint := range ci.Spec.Endpoints {
		if !dnsLabel.MatchString(name) {
			errs = append(errs, fmt.Sprintf("ClusterInfra/%s spec.endpoints.%s name is not a DNS label", ci.Metadata.Name, name))
		}
		errs = append(errs, validateClusterEndpoint(ci, components, name, endpoint, networkConfigs)...)
	}
	return errs
}

func referencedContainerClusterInfraNames(state v1alpha1.State) map[string]bool {
	out := map[string]bool{}
	for _, cluster := range state.ContainerClusters {
		for _, node := range cluster.Spec.Nodes {
			if node.InfraNodeRef.ClusterInfra != "" {
				out[node.InfraNodeRef.ClusterInfra] = true
			}
		}
	}
	return out
}

func validateClusterEndpoint(ci v1alpha1.ClusterInfra, components map[string]v1alpha1.InfraComponent, name string, endpoint v1alpha1.Endpoint, networkConfigs map[string]v1alpha1.NetworkConfig) []string {
	var errs []string
	prefix := fmt.Sprintf("ClusterInfra/%s spec.endpoints.%s", ci.Metadata.Name, name)
	if endpoint.Address != "" {
		ip := net.ParseIP(endpoint.Address)
		if ip == nil {
			errs = append(errs, fmt.Sprintf("%s.address %q is not a valid IP", prefix, endpoint.Address))
		} else {
			errs = append(errs, validateEndpointAddressNetwork(prefix+".address", ci, networkConfigs, endpoint.Address, ip)...)
			errs = append(errs, validateEndpointPrefixLength(prefix+".prefixLength", endpoint.PrefixLength, ip)...)
		}
	} else if endpoint.PrefixLength != 0 {
		errs = append(errs, prefix+".prefixLength requires address")
	}
	if endpoint.DNSName != "" && !dnsSubdomain.MatchString(endpoint.DNSName) {
		errs = append(errs, fmt.Sprintf("%s.dnsName %q is not a valid DNS subdomain", prefix, endpoint.DNSName))
	}
	if endpoint.Port < 0 || endpoint.Port > 65535 {
		errs = append(errs, fmt.Sprintf("%s.port %d out of range", prefix, endpoint.Port))
	}
	switch endpoint.Scheme {
	case "", "http", "https":
	default:
		errs = append(errs, fmt.Sprintf("%s.scheme %q must be http or https", prefix, endpoint.Scheme))
	}
	for i, cidr := range endpoint.InterfaceNetworks {
		errs = append(errs, validateCIDR(fmt.Sprintf("%s.interfaceNetworks[%d]", prefix, i), cidr)...)
	}
	switch endpoint.Source.Type {
	case "":
		if endpoint.Source.ComponentRef.Name != "" || endpoint.Source.BindAddress != "" {
			errs = append(errs, prefix+".source.type is required when source component fields are set")
		}
	case v1alpha1.EndpointSourceOpenShift, v1alpha1.EndpointSourceCephadm, v1alpha1.EndpointSourceExternal:
		if endpoint.Source.ComponentRef.Name != "" || endpoint.Source.BindAddress != "" {
			errs = append(errs, fmt.Sprintf("%s.source.type=%s must not set componentRef or bindAddress", prefix, endpoint.Source.Type))
		}
	case v1alpha1.EndpointSourceInfraComponent:
		if endpoint.Address != "" {
			errs = append(errs, prefix+".address must be empty when source.type=infraComponent; use source.bindAddress")
		}
		errs = append(errs, validateEndpointProvider(prefix, endpoint.Source, components)...)
	default:
		errs = append(errs, fmt.Sprintf("%s.source.type %q must be one of {%s, %s, %s, %s}",
			prefix, endpoint.Source.Type,
			v1alpha1.EndpointSourceOpenShift, v1alpha1.EndpointSourceCephadm, v1alpha1.EndpointSourceExternal, v1alpha1.EndpointSourceInfraComponent))
	}
	if endpoint.Source.Type == v1alpha1.EndpointSourceInfraComponent {
		if address, ok := endpointAddress(components, endpoint); ok {
			ip := net.ParseIP(address)
			if ip == nil {
				errs = append(errs, fmt.Sprintf("%s effective address %q is not a valid IP", prefix, address))
			} else {
				errs = append(errs, validateEndpointAddressNetwork(prefix+" effective address", ci, networkConfigs, address, ip)...)
			}
		}
	}
	if endpoint.Address == "" && endpoint.DNSName == "" && endpoint.Source.Type != v1alpha1.EndpointSourceInfraComponent {
		errs = append(errs, prefix+" must set address, dnsName, or source.type=infraComponent")
	}
	return errs
}

func validateEndpointAddressNetwork(prefix string, ci v1alpha1.ClusterInfra, networkConfigs map[string]v1alpha1.NetworkConfig, address string, ip net.IP) []string {
	var errs []string
	if !clusterInfraHasSelectedInstallNetwork(ci) {
		return errs
	}
	matches := endpointNetworkMatches(ci, networkConfigs, ip)
	switch len(matches) {
	case 0:
		errs = append(errs, fmt.Sprintf("%s %q is outside selected NetworkConfig machine networks", prefix, address))
	case 1:
	default:
		errs = append(errs, fmt.Sprintf("%s %q matches multiple selected NetworkConfigs (%s)",
			prefix, address, joinSortedNames(matches)))
	}
	return errs
}

func validateEndpointPrefixLength(prefix string, prefixLength int, ip net.IP) []string {
	if prefixLength == 0 {
		return nil
	}
	if ip.To4() != nil {
		if prefixLength < 1 || prefixLength > 32 {
			return []string{fmt.Sprintf("%s %d must be between 1 and 32 for IPv4", prefix, prefixLength)}
		}
		return nil
	}
	if prefixLength < 1 || prefixLength > 128 {
		return []string{fmt.Sprintf("%s %d must be between 1 and 128 for IPv6", prefix, prefixLength)}
	}
	return nil
}

func validateEndpointProvider(prefix string, ref v1alpha1.EndpointSource, components map[string]v1alpha1.InfraComponent) []string {
	var errs []string
	if ref.ComponentRef.Name == "" {
		return []string{fmt.Sprintf("%s.source.componentRef.name is required when source.type=infraComponent", prefix)}
	}
	component, ok := components[ref.ComponentRef.Name]
	if !ok || component.Spec.LoadBalancer == nil {
		return []string{fmt.Sprintf("%s.source.componentRef.name %q does not resolve to an InfraComponent loadBalancer", prefix, ref.ComponentRef.Name)}
	}
	lb := component.Spec.LoadBalancer
	if len(lb.BindAddresses) == 0 {
		return []string{fmt.Sprintf("InfraComponent/%s spec.loadBalancer.bindAddresses is required for infraComponent endpoints", component.Metadata.Name)}
	}
	if len(lb.BindAddresses) > 1 && ref.BindAddress == "" {
		errs = append(errs, fmt.Sprintf("%s.source.bindAddress is required because InfraComponent/%s spec.loadBalancer has multiple bindAddresses",
			prefix, component.Metadata.Name))
	}
	if ref.BindAddress != "" && !loadBalancerHasBindAddress(lb, ref.BindAddress) {
		errs = append(errs, fmt.Sprintf("%s.source.bindAddress %q does not match any InfraComponent/%s spec.loadBalancer.bindAddresses[].name",
			prefix, ref.BindAddress, component.Metadata.Name))
	}
	return errs
}

func endpointAddress(components map[string]v1alpha1.InfraComponent, endpoint v1alpha1.Endpoint) (string, bool) {
	if endpoint.Source.Type != v1alpha1.EndpointSourceInfraComponent {
		return endpoint.Address, endpoint.Address != ""
	}
	component, ok := components[endpoint.Source.ComponentRef.Name]
	if !ok || component.Spec.LoadBalancer == nil || len(component.Spec.LoadBalancer.BindAddresses) == 0 {
		return "", false
	}
	lb := component.Spec.LoadBalancer
	if endpoint.Source.BindAddress == "" {
		if len(lb.BindAddresses) == 1 {
			return lb.BindAddresses[0].IP, true
		}
		return "", false
	}
	for _, bind := range lb.BindAddresses {
		if bind.Name == endpoint.Source.BindAddress {
			return bind.IP, true
		}
	}
	return "", false
}

func loadBalancerHasBindAddress(lb *v1alpha1.LoadBalancerComponent, name string) bool {
	for _, bind := range lb.BindAddresses {
		if bind.Name == name {
			return true
		}
	}
	return false
}

func validateClusterNetworkBindings(ci v1alpha1.ClusterInfra, providers map[string]v1alpha1.InfraProvider, networkConfigs map[string]v1alpha1.NetworkConfig) []string {
	var errs []string
	seen := map[string]bool{}
	for i, binding := range ci.Spec.NetworkBindings {
		prefix := fmt.Sprintf("ClusterInfra/%s spec.networkBindings[%d]", ci.Metadata.Name, i)
		if binding.NetworkConfigRef.Name == "" {
			errs = append(errs, fmt.Sprintf("%s.networkConfigRef.name is required", prefix))
		} else if _, ok := networkConfigs[binding.NetworkConfigRef.Name]; !ok {
			errs = append(errs, fmt.Sprintf("%s.networkConfigRef.name %q does not match any NetworkConfig", prefix, binding.NetworkConfigRef.Name))
		}
		provider, hasProvider := providers[binding.ProviderRef.Name]
		if binding.ProviderRef.Name == "" {
			errs = append(errs, fmt.Sprintf("%s.providerRef.name is required", prefix))
		} else if !hasProvider {
			errs = append(errs, fmt.Sprintf("%s.providerRef.name %q does not match any InfraProvider", prefix, binding.ProviderRef.Name))
		}
		if binding.AttachmentRef.Name == "" {
			errs = append(errs, fmt.Sprintf("%s.attachmentRef.name is required", prefix))
		} else if hasProvider {
			if _, ok := lookupNetworkAttachment(provider, binding.AttachmentRef.Name); !ok {
				errs = append(errs, fmt.Sprintf("%s.attachmentRef.name %q does not match any InfraProvider/%s spec.networkAttachments[].name",
					prefix, binding.AttachmentRef.Name, provider.Metadata.Name))
			}
		}
		key := binding.ProviderRef.Name + "\x00" + binding.NetworkConfigRef.Name
		if seen[key] {
			errs = append(errs, fmt.Sprintf("%s duplicates providerRef.name %q and networkConfigRef.name %q",
				prefix, binding.ProviderRef.Name, binding.NetworkConfigRef.Name))
		}
		seen[key] = true
	}
	return errs
}

func validateClusterNodes(ci v1alpha1.ClusterInfra, providers map[string]v1alpha1.InfraProvider, hosts map[string]v1alpha1.Host, networkConfigs map[string]v1alpha1.NetworkConfig, dnsRefs map[string]bool) []string {
	var errs []string
	seen := map[string]bool{}
	for i, m := range ci.Spec.Components.Nodes {
		prefix := fmt.Sprintf("ClusterInfra/%s spec.components.nodes[%d]", ci.Metadata.Name, i)
		if m.Name == "" {
			errs = append(errs, fmt.Sprintf("%s.name is required", prefix))
			continue
		}
		prefix = fmt.Sprintf("ClusterInfra/%s spec.components.nodes[%s]", ci.Metadata.Name, m.Name)
		if seen[m.Name] {
			errs = append(errs, fmt.Sprintf("%s has duplicate name", prefix))
		}
		seen[m.Name] = true
		sourceKind, sourceErrs := validateClusterNodeSource(prefix, ci, m, providers, hosts)
		errs = append(errs, sourceErrs...)
		if sourceKind == "provider" {
			errs = append(errs, validateClusterNodeNetwork(prefix, m, networkConfigs, dnsRefs)...)
			errs = append(errs, validateClusterNodeNetworkBinding(prefix, ci, m, providers)...)
			errs = append(errs, validateBareMetalNodeNetworkInterfaces(prefix, m, providers, networkConfigs)...)
		} else if sourceKind == "host" {
			errs = append(errs, validateHostSourcedNode(prefix, m)...)
		}
	}
	return errs
}

func validateClusterNodeSource(prefix string, ci v1alpha1.ClusterInfra, m v1alpha1.ClusterNodeComponent, providers map[string]v1alpha1.InfraProvider, hosts map[string]v1alpha1.Host) (string, []string) {
	var errs []string
	hasHost := m.Source.HostRef.Name != ""
	hasProvider := m.Source.ProviderRef.Name != ""
	if hasHost == hasProvider {
		errs = append(errs, fmt.Sprintf("%s.source must set exactly one of {hostRef, providerRef}", prefix))
		return "", errs
	}
	if hasHost {
		if m.Source.MachineRef.Name != "" || m.Source.ProfileRef.Name != "" {
			errs = append(errs, fmt.Sprintf("%s.source.hostRef must not set machineRef or profileRef", prefix))
		}
		if _, ok := hosts[m.Source.HostRef.Name]; !ok {
			errs = append(errs, fmt.Sprintf("%s.source.hostRef.name %q does not match any Host", prefix, m.Source.HostRef.Name))
		}
		return "host", errs
	}
	provider, ok := providers[m.Source.ProviderRef.Name]
	if !ok {
		errs = append(errs, fmt.Sprintf("%s.source.providerRef.name %q does not match any InfraProvider", prefix, m.Source.ProviderRef.Name))
		return "provider", errs
	}
	hasProfile := m.Source.ProfileRef.Name != ""
	hasName := m.Source.MachineRef.Name != ""
	if hasProfile == hasName {
		errs = append(errs, fmt.Sprintf("%s.source.providerRef must set exactly one of {machineRef, profileRef}", prefix))
		return "provider", errs
	}
	if hasProfile {
		profile, ok := lookupMachineProfile(provider, m.Source.ProfileRef.Name)
		if !ok {
			return "provider", append(errs, fmt.Sprintf("%s.source.profileRef.name %q is not defined on InfraProvider/%s spec.machineProfiles",
				prefix, m.Source.ProfileRef.Name, provider.Metadata.Name))
		}
		profileKind := v1alpha1.ProfileProvisionerKind(profile)
		if ci.Spec.Platform.Type == v1alpha1.PlatformTypeVSphere && profileKind != v1alpha1.ProvisionerVSphere {
			errs = append(errs, fmt.Sprintf("%s.source.profileRef.name %q uses %q but ClusterInfra/%s spec.platform.type is %q",
				prefix, m.Source.ProfileRef.Name, profileKind, ci.Metadata.Name, ci.Spec.Platform.Type))
		}
		if ci.Spec.Platform.Type == v1alpha1.PlatformTypeVSphere && profile.VSphere != nil {
			errs = append(errs, validateVSphereMultiNICNodeNetworking(prefix, ci, profile)...)
		}
		return "provider", errs
	}
	server, ok := lookupMachine(provider, m.Source.MachineRef.Name)
	if !ok {
		return "provider", append(errs, fmt.Sprintf("%s.source.machineRef.name %q is not defined on InfraProvider/%s spec.machines",
			prefix, m.Source.MachineRef.Name, provider.Metadata.Name))
	}
	if v1alpha1.MachineProvisionerKind(server) == v1alpha1.ProvisionerBareMetal && ci.Spec.Platform.Type != "" && ci.Spec.Platform.Type != v1alpha1.PlatformTypeBareMetal {
		errs = append(errs, fmt.Sprintf("%s.source.machineRef.name %q is baremetal but ClusterInfra/%s spec.platform.type is %q",
			prefix, m.Source.MachineRef.Name, ci.Metadata.Name, ci.Spec.Platform.Type))
	}
	return "provider", errs
}

func validateHostSourcedNode(prefix string, m v1alpha1.ClusterNodeComponent) []string {
	var errs []string
	if m.Network.NetworkConfigRef.Name != "" || m.Network.Spec != nil || len(m.Network.Overrides) > 0 {
		errs = append(errs, prefix+".network must be empty when source.hostRef is set")
	}
	if m.RootDeviceHints != nil {
		errs = append(errs, prefix+".rootDeviceHints must be empty when source.hostRef is set")
	}
	return errs
}

func validateVSphereMultiNICNodeNetworking(prefix string, ci v1alpha1.ClusterInfra, profile v1alpha1.MachineProfileCapability) []string {
	if profile.VSphere == nil {
		return nil
	}
	hasNodeNetworking := profile.VSphere.NodeNetworking != nil ||
		(ci.Spec.Platform.VSphere != nil && ci.Spec.Platform.VSphere.NodeNetworking != nil)
	if hasNodeNetworking {
		return nil
	}
	for _, fd := range profile.VSphere.FailureDomains {
		if len(fd.Topology.Networks) > 1 {
			return []string{fmt.Sprintf("%s.source.profileRef.name %q declares multiple vSphere topology networks; set ClusterInfra/%s spec.platform.vsphere.nodeNetworking or InfraProvider profile nodeNetworking so machineNetwork mapping is explicit",
				prefix, profile.Name, ci.Metadata.Name)}
		}
	}
	return nil
}

func validateClusterNodeNetwork(prefix string, m v1alpha1.ClusterNodeComponent, networkConfigs map[string]v1alpha1.NetworkConfig, dnsRefs map[string]bool) []string {
	var errs []string
	hasRef := m.Network.NetworkConfigRef.Name != ""
	hasSpec := m.Network.Spec != nil
	if hasRef == hasSpec {
		errs = append(errs, fmt.Sprintf("%s.network must set exactly one of {networkConfigRef, spec}", prefix))
		return errs
	}
	if hasSpec {
		if len(m.Network.Overrides) > 0 {
			errs = append(errs, fmt.Sprintf("%s.network.overrides is only valid with network.networkConfigRef", prefix))
		}
		errs = append(errs, validateNetworkConfigSpec(prefix+".network.spec", *m.Network.Spec, dnsRefs)...)
		return errs
	}
	networkConfig, ok := networkConfigs[m.Network.NetworkConfigRef.Name]
	if !ok {
		errs = append(errs, fmt.Sprintf("%s.network.networkConfigRef.name %q does not match any NetworkConfig", prefix, m.Network.NetworkConfigRef.Name))
		return errs
	}
	if len(m.Network.Overrides) > 0 {
		errs = append(errs, validateNetworkConfigOverrides(prefix+".network.overrides", m.Network.Overrides, networkConfig)...)
	}
	return errs
}

func validateClusterNodeNetworkBinding(prefix string, ci v1alpha1.ClusterInfra, m v1alpha1.ClusterNodeComponent, providers map[string]v1alpha1.InfraProvider) []string {
	if m.Source.ProviderRef.Name == "" || m.Network.NetworkConfigRef.Name == "" {
		return nil
	}
	provider, ok := providers[m.Source.ProviderRef.Name]
	if !ok {
		return nil
	}
	machineKind, ok := clusterMachineProvisionerKind(m, provider)
	if !ok {
		return nil
	}
	binding, ok := clusterNetworkBinding(ci, m.Source.ProviderRef.Name, m.Network.NetworkConfigRef.Name)
	if !ok {
		if machineKind == v1alpha1.ProvisionerBareMetal {
			return nil
		}
		return []string{fmt.Sprintf("%s.network.networkConfigRef.name %q has no ClusterInfra/%s spec.networkBindings entry for InfraProvider/%s",
			prefix, m.Network.NetworkConfigRef.Name, ci.Metadata.Name, m.Source.ProviderRef.Name)}
	}
	attachment, ok := lookupNetworkAttachment(provider, binding.AttachmentRef.Name)
	if !ok {
		return nil
	}
	attachmentKind := v1alpha1.NetworkAttachmentKind(attachment)
	if attachmentKind != machineKind {
		return []string{fmt.Sprintf("%s.network.networkConfigRef.name %q binds to InfraProvider/%s networkAttachment %q of kind %q, but node substrate is %q",
			prefix, m.Network.NetworkConfigRef.Name, provider.Metadata.Name, attachment.Name, attachmentKind, machineKind)}
	}
	return nil
}

func clusterMachineProvisionerKind(m v1alpha1.ClusterNodeComponent, provider v1alpha1.InfraProvider) (string, bool) {
	if m.Source.ProfileRef.Name != "" {
		profile, ok := lookupMachineProfile(provider, m.Source.ProfileRef.Name)
		if !ok {
			return "", false
		}
		return v1alpha1.ProfileProvisionerKind(profile), true
	}
	if m.Source.MachineRef.Name != "" {
		machine, ok := lookupMachine(provider, m.Source.MachineRef.Name)
		if !ok {
			return "", false
		}
		return v1alpha1.MachineProvisionerKind(machine), true
	}
	return "", false
}

func clusterNetworkBinding(ci v1alpha1.ClusterInfra, providerName, networkConfigName string) (v1alpha1.ClusterNetworkBinding, bool) {
	for _, binding := range ci.Spec.NetworkBindings {
		if binding.ProviderRef.Name == providerName && binding.NetworkConfigRef.Name == networkConfigName {
			return binding, true
		}
	}
	return v1alpha1.ClusterNetworkBinding{}, false
}

func validateBareMetalNodeNetworkInterfaces(prefix string, m v1alpha1.ClusterNodeComponent, providers map[string]v1alpha1.InfraProvider, networkConfigs map[string]v1alpha1.NetworkConfig) []string {
	if m.Source.ProviderRef.Name == "" || m.Source.MachineRef.Name == "" || m.Source.ProfileRef.Name != "" {
		return nil
	}
	provider, ok := providers[m.Source.ProviderRef.Name]
	if !ok {
		return nil
	}
	machine, ok := lookupMachine(provider, m.Source.MachineRef.Name)
	if !ok || machine.BareMetal == nil {
		return nil
	}
	networkConfig, ok := clusterMachineNetworkConfigForValidation(m, networkConfigs)
	if !ok {
		return nil
	}
	required := templateBareMetalInterfaceNames(networkConfig)
	for _, name := range overrideBareMetalInterfaceNames(m.Network.Overrides) {
		if !containsString(required, name) {
			required = append(required, name)
		}
	}
	if len(required) == 0 {
		return nil
	}
	declared := map[string]bool{}
	for _, iface := range machine.BareMetal.Interfaces {
		if iface.Name != "" {
			declared[iface.Name] = true
		}
	}
	var errs []string
	networkConfigField := "network.spec"
	if m.Network.NetworkConfigRef.Name != "" {
		networkConfigField = "network.networkConfigRef"
	}
	for _, name := range required {
		if declared[name] {
			continue
		}
		errs = append(errs, fmt.Sprintf("%s.%s %q requires baremetal interface %q but InfraProvider/%s spec.machines[%s].baremetal.interfaces does not declare it",
			prefix, networkConfigField, networkConfig.Metadata.Name, name, provider.Metadata.Name, machine.Name))
	}
	return errs
}

func clusterMachineNetworkConfigForValidation(m v1alpha1.ClusterNodeComponent, networkConfigs map[string]v1alpha1.NetworkConfig) (v1alpha1.NetworkConfig, bool) {
	if m.Network.Spec != nil {
		return v1alpha1.NetworkConfig{
			Metadata: v1alpha1.Metadata{Name: "inline"},
			Spec:     *m.Network.Spec,
		}, true
	}
	if m.Network.NetworkConfigRef.Name == "" {
		return v1alpha1.NetworkConfig{}, false
	}
	networkConfig, ok := networkConfigs[m.Network.NetworkConfigRef.Name]
	return networkConfig, ok
}

func validateNetworkConfigOverrides(owner string, config map[string]any, n v1alpha1.NetworkConfig) []string {
	rawInterfaces, ok := config["interfaces"].([]any)
	if !ok {
		return nil
	}
	var errs []string
	for i, raw := range rawInterfaces {
		entry, ok := raw.(map[string]any)
		if !ok {
			errs = append(errs, fmt.Sprintf("%s.interfaces[%d] must be an NMState interface object", owner, i))
			continue
		}
		name, _ := entry["name"].(string)
		label := fmt.Sprintf("%s.interfaces[%d]", owner, i)
		if name != "" {
			label = fmt.Sprintf("%s.interfaces[%s]", owner, name)
		}
		hasAddresses := false
		for _, family := range []string{"ipv4", "ipv6"} {
			addresses := networkConfigInterfaceAddresses(entry, family)
			if len(addresses) == 0 {
				continue
			}
			hasAddresses = true
			for j, address := range addresses {
				errs = append(errs, validateNMStateAddress(fmt.Sprintf("%s.%s.address[%d]", label, family, j), address, n)...)
			}
		}
		if !hasAddresses {
			continue
		}
		if name == "" {
			errs = append(errs, fmt.Sprintf("%s.name is required when addresses are set", label))
		}
	}
	return errs
}

func networkConfigInterfaceAddresses(entry map[string]any, family string) []any {
	familyConfig, ok := entry[family].(map[string]any)
	if !ok {
		return nil
	}
	addresses, ok := familyConfig["address"].([]any)
	if !ok {
		return nil
	}
	return addresses
}

func validateNMStateAddress(owner string, raw any, n v1alpha1.NetworkConfig) []string {
	var errs []string
	addr, ok := raw.(map[string]any)
	if !ok {
		return []string{fmt.Sprintf("%s must be an NMState address object", owner)}
	}
	ipValue, _ := addr["ip"].(string)
	ip := net.ParseIP(ipValue)
	if ipValue == "" {
		errs = append(errs, fmt.Sprintf("%s.ip is required", owner))
	} else if ip == nil {
		errs = append(errs, fmt.Sprintf("%s.ip %q is not a valid IP", owner, ipValue))
	} else if !networkConfigContainsIP(n, ip) {
		errs = append(errs, fmt.Sprintf("%s.ip %q is outside NetworkConfig/%s spec.machineNetwork", owner, ipValue, n.Metadata.Name))
	}
	prefixLength, ok := nmStatePrefixLength(addr["prefix-length"])
	if !ok {
		errs = append(errs, fmt.Sprintf("%s.prefix-length must be an integer", owner))
		return errs
	}
	if prefixLength < 0 || prefixLength > 128 {
		errs = append(errs, fmt.Sprintf("%s.prefix-length %d must be 0..128", owner, prefixLength))
	}
	if ip != nil && ip.To4() != nil && prefixLength > 32 {
		errs = append(errs, fmt.Sprintf("%s.prefix-length %d must be 0..32 for IPv4", owner, prefixLength))
	}
	return errs
}

func nmStatePrefixLength(raw any) (int, bool) {
	switch v := raw.(type) {
	case nil:
		return 0, true
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		if v != float64(int(v)) {
			return 0, false
		}
		return int(v), true
	default:
		return 0, false
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
