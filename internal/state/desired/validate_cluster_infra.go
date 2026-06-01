package desiredstate

import (
	"fmt"
	"net"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/state/view"
)

func validateClusterInfras(state v1alpha1.State) []string {
	var errs []string
	providers := indexProviders(state.InfraProviders)
	networkConfigs := indexNetworkConfigs(state.NetworkConfigs)
	components := indexInfraComponents(state.InfraComponents)
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
		errs = append(errs, validateClusterPlatform(ci)...)
		if containerInfraNames[ci.Metadata.Name] || len(ci.Spec.Endpoints) > 0 {
			errs = append(errs, validateClusterEndpoints(ci, components, networkConfigs, containerInfraNames[ci.Metadata.Name])...)
		}
		errs = append(errs, validateClusterNetworkBindings(ci, providers, networkConfigs)...)
		errs = append(errs, validateClusterMachines(ci, providers, networkConfigs)...)
		errs = append(errs, validateClusterServices(ci, providers)...)
	}
	return errs
}

func validateClusterPlatform(ci v1alpha1.ClusterInfra) []string {
	var errs []string
	prefix := fmt.Sprintf("ClusterInfra/%s spec.platform", ci.Metadata.Name)
	switch ci.Spec.Platform.Type {
	case "":
		errs = append(errs, fmt.Sprintf("%s.type is required", prefix))
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
			if node.MachineRef.ClusterInfra != "" {
				out[node.MachineRef.ClusterInfra] = true
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

func validateClusterMachines(ci v1alpha1.ClusterInfra, providers map[string]v1alpha1.InfraProvider, networkConfigs map[string]v1alpha1.NetworkConfig) []string {
	var errs []string
	if len(ci.Spec.Components.Machines) == 0 {
		errs = append(errs, fmt.Sprintf("ClusterInfra/%s spec.components.machines is required (at least one machine)", ci.Metadata.Name))
		return errs
	}
	seen := map[string]bool{}
	for i, m := range ci.Spec.Components.Machines {
		prefix := fmt.Sprintf("ClusterInfra/%s spec.components.machines[%d]", ci.Metadata.Name, i)
		if m.Name == "" {
			errs = append(errs, fmt.Sprintf("%s.name is required", prefix))
			continue
		}
		prefix = fmt.Sprintf("ClusterInfra/%s spec.components.machines[%s]", ci.Metadata.Name, m.Name)
		if seen[m.Name] {
			errs = append(errs, fmt.Sprintf("%s has duplicate name", prefix))
		}
		seen[m.Name] = true
		errs = append(errs, validateClusterMachineFrom(prefix, ci, m, providers)...)
		errs = append(errs, validateClusterMachineNetworkConfig(prefix, m, networkConfigs)...)
		errs = append(errs, validateClusterMachineNetworkBinding(prefix, ci, m, providers)...)
		errs = append(errs, validateBareMetalMachineNetworkInterfaces(prefix, m, providers, networkConfigs)...)
	}
	return errs
}

func validateClusterMachineFrom(prefix string, ci v1alpha1.ClusterInfra, m v1alpha1.ClusterMachineComponent, providers map[string]v1alpha1.InfraProvider) []string {
	var errs []string
	if m.From.Provider == "" {
		errs = append(errs, fmt.Sprintf("%s.from.provider is required", prefix))
		return errs
	}
	provider, ok := providers[m.From.Provider]
	if !ok {
		errs = append(errs, fmt.Sprintf("%s.from.provider %q does not match any InfraProvider", prefix, m.From.Provider))
		return errs
	}
	hasProfile := m.From.Profile != ""
	hasName := m.From.Name != ""
	if hasProfile == hasName {
		errs = append(errs, fmt.Sprintf("%s.from must set exactly one of {profile, name}", prefix))
		return errs
	}
	if hasProfile {
		profile, ok := lookupMachineProfile(provider, m.From.Profile)
		if !ok {
			return append(errs, fmt.Sprintf("%s.from.profile %q is not defined on InfraProvider/%s spec.machineProfiles",
				prefix, m.From.Profile, provider.Metadata.Name))
		}
		profileKind := v1alpha1.ProfileProvisionerKind(profile)
		if ci.Spec.Platform.Type == v1alpha1.PlatformTypeVSphere && profileKind != v1alpha1.ProvisionerVSphere {
			errs = append(errs, fmt.Sprintf("%s.from.profile %q uses %q but ClusterInfra/%s spec.platform.type is %q",
				prefix, m.From.Profile, profileKind, ci.Metadata.Name, ci.Spec.Platform.Type))
		}
		if ci.Spec.Platform.Type == v1alpha1.PlatformTypeVSphere && profile.VSphere != nil {
			errs = append(errs, validateVSphereMultiNICNodeNetworking(prefix, ci, profile)...)
		}
		return errs
	}
	server, ok := lookupMachine(provider, m.From.Name)
	if !ok {
		return append(errs, fmt.Sprintf("%s.from.name %q is not defined on InfraProvider/%s spec.machines",
			prefix, m.From.Name, provider.Metadata.Name))
	}
	if v1alpha1.MachineProvisionerKind(server) == v1alpha1.ProvisionerBareMetal && ci.Spec.Platform.Type != "" && ci.Spec.Platform.Type != v1alpha1.PlatformTypeBareMetal {
		errs = append(errs, fmt.Sprintf("%s.from.name %q is baremetal but ClusterInfra/%s spec.platform.type is %q",
			prefix, m.From.Name, ci.Metadata.Name, ci.Spec.Platform.Type))
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
			return []string{fmt.Sprintf("%s.from.profile %q declares multiple vSphere topology networks; set ClusterInfra/%s spec.platform.vsphere.nodeNetworking or InfraProvider profile nodeNetworking so machineNetwork mapping is explicit",
				prefix, profile.Name, ci.Metadata.Name)}
		}
	}
	return nil
}

func validateClusterMachineNetworkConfig(prefix string, m v1alpha1.ClusterMachineComponent, networkConfigs map[string]v1alpha1.NetworkConfig) []string {
	var errs []string
	if m.NetworkConfig.Ref.Name == "" {
		errs = append(errs, fmt.Sprintf("%s.networkConfig.ref.name is required", prefix))
		return errs
	}
	networkConfig, ok := networkConfigs[m.NetworkConfig.Ref.Name]
	if !ok {
		errs = append(errs, fmt.Sprintf("%s.networkConfig.ref %q does not match any NetworkConfig", prefix, m.NetworkConfig.Ref.Name))
		return errs
	}
	templateInterfaces := templateInterfaceNames(networkConfig)
	for i, addr := range m.NetworkConfig.Addresses {
		owner := fmt.Sprintf("%s.networkConfig.addresses[%d]", prefix, i)
		if addr.Interface == "" {
			errs = append(errs, fmt.Sprintf("%s.interface is required", owner))
		} else if len(templateInterfaces) > 0 && !templateInterfaces[addr.Interface] {
			errs = append(errs, fmt.Sprintf("%s.interface %q is not declared in NetworkConfig/%s spec.template.networkConfig.interfaces",
				owner, addr.Interface, networkConfig.Metadata.Name))
		}
		for _, ipaddr := range addr.IPv4 {
			errs = append(errs, validateOverlayAddress(owner+".ipv4", ipaddr, networkConfig)...)
		}
		for _, ipaddr := range addr.IPv6 {
			errs = append(errs, validateOverlayAddress(owner+".ipv6", ipaddr, networkConfig)...)
		}
	}
	return errs
}

func validateClusterMachineNetworkBinding(prefix string, ci v1alpha1.ClusterInfra, m v1alpha1.ClusterMachineComponent, providers map[string]v1alpha1.InfraProvider) []string {
	if m.From.Provider == "" || m.NetworkConfig.Ref.Name == "" {
		return nil
	}
	provider, ok := providers[m.From.Provider]
	if !ok {
		return nil
	}
	machineKind, ok := clusterMachineProvisionerKind(m, provider)
	if !ok {
		return nil
	}
	binding, ok := clusterNetworkBinding(ci, m.From.Provider, m.NetworkConfig.Ref.Name)
	if !ok {
		if machineKind == v1alpha1.ProvisionerBareMetal {
			return nil
		}
		return []string{fmt.Sprintf("%s.networkConfig.ref %q has no ClusterInfra/%s spec.networkBindings entry for InfraProvider/%s",
			prefix, m.NetworkConfig.Ref.Name, ci.Metadata.Name, m.From.Provider)}
	}
	attachment, ok := lookupNetworkAttachment(provider, binding.AttachmentRef.Name)
	if !ok {
		return nil
	}
	attachmentKind := v1alpha1.NetworkAttachmentKind(attachment)
	if attachmentKind != machineKind {
		return []string{fmt.Sprintf("%s.networkConfig.ref %q binds to InfraProvider/%s networkAttachment %q of kind %q, but machine substrate is %q",
			prefix, m.NetworkConfig.Ref.Name, provider.Metadata.Name, attachment.Name, attachmentKind, machineKind)}
	}
	return nil
}

func clusterMachineProvisionerKind(m v1alpha1.ClusterMachineComponent, provider v1alpha1.InfraProvider) (string, bool) {
	if m.From.Profile != "" {
		profile, ok := lookupMachineProfile(provider, m.From.Profile)
		if !ok {
			return "", false
		}
		return v1alpha1.ProfileProvisionerKind(profile), true
	}
	if m.From.Name != "" {
		machine, ok := lookupMachine(provider, m.From.Name)
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

func validateBareMetalMachineNetworkInterfaces(prefix string, m v1alpha1.ClusterMachineComponent, providers map[string]v1alpha1.InfraProvider, networkConfigs map[string]v1alpha1.NetworkConfig) []string {
	if m.From.Provider == "" || m.From.Name == "" || m.From.Profile != "" || m.NetworkConfig.Ref.Name == "" {
		return nil
	}
	provider, ok := providers[m.From.Provider]
	if !ok {
		return nil
	}
	machine, ok := lookupMachine(provider, m.From.Name)
	if !ok || machine.BareMetal == nil {
		return nil
	}
	networkConfig, ok := networkConfigs[m.NetworkConfig.Ref.Name]
	if !ok {
		return nil
	}
	required := templateBareMetalInterfaceNames(networkConfig)
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
	for _, name := range required {
		if declared[name] {
			continue
		}
		errs = append(errs, fmt.Sprintf("%s.networkConfig.ref %q requires baremetal interface %q but InfraProvider/%s spec.machines[%s].baremetal.interfaces does not declare it",
			prefix, networkConfig.Metadata.Name, name, provider.Metadata.Name, machine.Name))
	}
	return errs
}

func validateOverlayAddress(owner string, addr v1alpha1.NetworkIPAddress, n v1alpha1.NetworkConfig) []string {
	var errs []string
	ip := net.ParseIP(addr.IP)
	if addr.IP == "" {
		errs = append(errs, fmt.Sprintf("%s.ip is required", owner))
	} else if ip == nil {
		errs = append(errs, fmt.Sprintf("%s.ip %q is not a valid IP", owner, addr.IP))
	} else if !stateview.NetworkConfigContainsIP(n, ip) {
		errs = append(errs, fmt.Sprintf("%s.ip %q is outside NetworkConfig/%s spec.machineNetwork", owner, addr.IP, n.Metadata.Name))
	}
	if addr.PrefixLength < 0 || addr.PrefixLength > 128 {
		errs = append(errs, fmt.Sprintf("%s.prefix-length %d must be 0..128", owner, addr.PrefixLength))
	}
	if ip != nil && ip.To4() != nil && addr.PrefixLength > 32 {
		errs = append(errs, fmt.Sprintf("%s.prefix-length %d must be 0..32 for IPv4", owner, addr.PrefixLength))
	}
	return errs
}
