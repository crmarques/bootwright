package desiredstate

import (
	"fmt"
	"net"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func validateClusterPlatform(owner string, platform v1alpha1.InstallPlatform, required bool) []string {
	var errs []string
	switch platform.Type {
	case "":
		if required || platform.BareMetal != nil || platform.VSphere != nil || platform.External != nil {
			errs = append(errs, owner+".type is required")
		}
	case v1alpha1.PlatformTypeBareMetal:
		if platform.VSphere != nil || platform.External != nil {
			errs = append(errs, owner+".type=bareMetal must not set vsphere or external")
		}
	case v1alpha1.PlatformTypeVSphere:
		if platform.BareMetal != nil || platform.External != nil {
			errs = append(errs, owner+".type=vsphere must not set bareMetal or external")
		}
	case v1alpha1.PlatformTypeNone:
		if platform.BareMetal != nil || platform.VSphere != nil || platform.External != nil {
			errs = append(errs, owner+".type=none must not set platform-specific config")
		}
	case v1alpha1.PlatformTypeExternal:
		if platform.BareMetal != nil || platform.VSphere != nil {
			errs = append(errs, owner+".type=external must not set bareMetal or vsphere")
		}
	default:
		errs = append(errs, fmt.Sprintf("%s.type %q must be one of {%s, %s, %s, %s}",
			owner, platform.Type, v1alpha1.PlatformTypeBareMetal, v1alpha1.PlatformTypeVSphere, v1alpha1.PlatformTypeNone, v1alpha1.PlatformTypeExternal))
	}
	if bm := platform.BareMetal; bm != nil && bm.ProvisioningNetwork != "" {
		switch bm.ProvisioningNetwork {
		case v1alpha1.ProvisioningNetworkDisabled, v1alpha1.ProvisioningNetworkManaged, v1alpha1.ProvisioningNetworkUnmanaged:
		default:
			errs = append(errs, fmt.Sprintf("%s.bareMetal.provisioningNetwork %q must be one of {%s, %s, %s}",
				owner, bm.ProvisioningNetwork, v1alpha1.ProvisioningNetworkDisabled, v1alpha1.ProvisioningNetworkManaged, v1alpha1.ProvisioningNetworkUnmanaged))
		}
	}
	return errs
}

func validateClusterEndpoints(owner string, ci v1alpha1.ClusterInstall, components map[string]v1alpha1.InfraComponent, networkConfigs map[string]v1alpha1.NetworkConfig, requireOpenShiftEndpoints bool) []string {
	var errs []string
	if len(ci.Endpoints) == 0 {
		if requireOpenShiftEndpoints {
			errs = append(errs, owner+".endpoints is required")
		}
		return errs
	}
	loadBalancerRefs := map[string]map[string]bool{}
	for name, endpoint := range ci.Endpoints {
		switch name {
		case v1alpha1.EndpointAPI, v1alpha1.EndpointAPIInt, v1alpha1.EndpointIngress:
		default:
			errs = append(errs, fmt.Sprintf("%s.endpoints.%s is not a consumed endpoint; accepted keys are {%s, %s, %s}",
				owner, name, v1alpha1.EndpointAPI, v1alpha1.EndpointAPIInt, v1alpha1.EndpointIngress))
		}
		errs = append(errs, validateClusterEndpoint(owner+".endpoints."+name, ci, components, endpoint, networkConfigs)...)
		if endpoint.Source.Type == v1alpha1.EndpointSourceInfraComponent && endpoint.Source.ComponentRef.Name != "" && endpoint.Source.BindAddressRef.Name != "" {
			if loadBalancerRefs[endpoint.Source.ComponentRef.Name] == nil {
				loadBalancerRefs[endpoint.Source.ComponentRef.Name] = map[string]bool{}
			}
			loadBalancerRefs[endpoint.Source.ComponentRef.Name][endpoint.Source.BindAddressRef.Name] = true
		}
	}
	for componentName, refs := range loadBalancerRefs {
		component, ok := components[componentName]
		if !ok || component.Spec.LoadBalancer == nil {
			continue
		}
		errs = append(errs, validateLoadBalancerBindAddressUse(fmt.Sprintf("InfraComponent/%s spec.loadBalancer", component.Metadata.Name), component.Spec.LoadBalancer.BindAddresses, refs)...)
	}
	return errs
}

func validateClusterEndpoint(prefix string, ci v1alpha1.ClusterInstall, components map[string]v1alpha1.InfraComponent, endpoint v1alpha1.Endpoint, networkConfigs map[string]v1alpha1.NetworkConfig) []string {
	var errs []string
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
	case "", v1alpha1.EndpointSourceOpenShift, v1alpha1.EndpointSourceExternal:
		if endpoint.Source.ComponentRef.Name != "" || endpoint.Source.BindAddressRef.Name != "" {
			errs = append(errs, prefix+".source component fields are only valid when source.type=infraComponent")
		}
	case v1alpha1.EndpointSourceInfraComponent:
		if endpoint.Address != "" {
			errs = append(errs, prefix+".address must be empty when source.type=infraComponent; use source.bindAddressRef")
		}
		errs = append(errs, validateEndpointProvider(prefix, endpoint.Source, components)...)
		errs = append(errs, validateEndpointBindAddressNetwork(prefix, ci, components, endpoint.Source, networkConfigs)...)
	default:
		errs = append(errs, fmt.Sprintf("%s.source.type %q must be one of {%s, %s, %s}",
			prefix, endpoint.Source.Type, v1alpha1.EndpointSourceOpenShift, v1alpha1.EndpointSourceExternal, v1alpha1.EndpointSourceInfraComponent))
	}
	if endpoint.Address == "" && endpoint.DNSName == "" && endpoint.Source.Type != v1alpha1.EndpointSourceInfraComponent {
		errs = append(errs, prefix+" must set address, dnsName, or source.type=infraComponent")
	}
	return errs
}

func validateEndpointAddressNetwork(prefix string, ci v1alpha1.ClusterInstall, networkConfigs map[string]v1alpha1.NetworkConfig, address string, ip net.IP) []string {
	var errs []string
	if !clusterInstallHasSelectedInstallNetwork(ci) {
		return errs
	}
	matches := endpointNetworkMatches(ci, networkConfigs, ip)
	switch len(matches) {
	case 0:
		errs = append(errs, fmt.Sprintf("%s %q is outside selected NetworkConfig machine networks", prefix, address))
	case 1:
	default:
		errs = append(errs, fmt.Sprintf("%s %q matches multiple selected NetworkConfigs (%s)", prefix, address, joinSortedNames(matches)))
	}
	if len(ci.Machines) > 1 {
		for _, node := range clusterInstallNodeInstallIPs(ci) {
			if node.ip.Equal(ip) {
				errs = append(errs, fmt.Sprintf("%s %q collides with the static install IP of %s; an API/ingress VIP must not equal a node address or keepalived and the node contend for it (ARP conflict, flapping API) mid-install", prefix, address, node.owner))
				break
			}
		}
	}
	return errs
}

type nodeInstallIP struct {
	ip    net.IP
	owner string
}

func clusterInstallNodeInstallIPs(ci v1alpha1.ClusterInstall) []nodeInstallIP {
	var out []nodeInstallIP
	for _, machine := range ci.Machines {
		addrByName := map[string]string{}
		for _, addr := range machine.Addresses {
			if addr.Name != "" {
				addrByName[addr.Name] = addr.Address
			}
		}
		for _, ia := range machine.Network.InterfaceAddresses {
			raw, ok := addrByName[ia.AddressRef.Name]
			if !ok {
				continue
			}
			if ip := net.ParseIP(raw); ip != nil {
				out = append(out, nodeInstallIP{ip: ip, owner: fmt.Sprintf("node %q", machine.Name)})
			}
		}
	}
	return out
}

func validateEndpointBindAddressNetwork(prefix string, ci v1alpha1.ClusterInstall, components map[string]v1alpha1.InfraComponent, source v1alpha1.EndpointSource, networkConfigs map[string]v1alpha1.NetworkConfig) []string {
	if !clusterInstallHasSelectedInstallNetwork(ci) {
		return nil
	}
	component, ok := components[source.ComponentRef.Name]
	if !ok || component.Spec.LoadBalancer == nil {
		return nil
	}
	address := resolveLoadBalancerBindAddress(component, source.BindAddressRef.Name)
	if address == "" {
		return nil
	}
	ip := net.ParseIP(address)
	if ip == nil {
		return nil
	}
	if len(endpointNetworkMatches(ci, networkConfigs, ip)) == 0 {
		return []string{fmt.Sprintf("%s.source.bindAddressRef resolves to %s, which is outside selected NetworkConfig machine networks", prefix, address)}
	}
	return nil
}

func resolveLoadBalancerBindAddress(component v1alpha1.InfraComponent, ref string) string {
	if component.Spec.LoadBalancer == nil {
		return ""
	}
	binds := component.Spec.LoadBalancer.BindAddresses
	if ref == "" {
		if len(binds) == 1 {
			return binds[0].Address
		}
		return ""
	}
	for _, bind := range binds {
		if bind.Name == ref {
			return bind.Address
		}
	}
	return ""
}

func validateEndpointPrefixLength(prefix string, prefixLength int, ip net.IP) []string {
	if prefixLength == 0 {
		return nil
	}
	if ip.To4() != nil {
		if prefixLength < 1 || prefixLength > 32 {
			return []string{fmt.Sprintf("%s %d out of IPv4 range", prefix, prefixLength)}
		}
		return nil
	}
	if prefixLength < 1 || prefixLength > 128 {
		return []string{fmt.Sprintf("%s %d out of IPv6 range", prefix, prefixLength)}
	}
	return nil
}

func validateEndpointProvider(prefix string, source v1alpha1.EndpointSource, components map[string]v1alpha1.InfraComponent) []string {
	var errs []string
	if source.ComponentRef.Name == "" {
		return []string{prefix + ".source.componentRef is required when source.type=infraComponent"}
	}
	component, ok := components[source.ComponentRef.Name]
	if !ok {
		return []string{fmt.Sprintf("%s.source.componentRef %q does not match any InfraComponent", prefix, source.ComponentRef.Name)}
	}
	if component.Spec.LoadBalancer == nil {
		return []string{fmt.Sprintf("%s.source.componentRef %q must reference a loadBalancer InfraComponent", prefix, source.ComponentRef.Name)}
	}
	if source.BindAddressRef.Name == "" && len(component.Spec.LoadBalancer.BindAddresses) != 1 {
		errs = append(errs, prefix+".source.bindAddressRef is required unless the referenced loadBalancer declares exactly one bindAddress")
	}
	referenced := map[string]bool{}
	if source.BindAddressRef.Name != "" {
		referenced[source.BindAddressRef.Name] = true
	}
	errs = append(errs, validateLoadBalancerBindAddresses(fmt.Sprintf("InfraComponent/%s spec.loadBalancer", component.Metadata.Name), component.Spec.LoadBalancer.BindAddresses, referenced)...)
	return errs
}

type artifactServerEndpointValidation struct {
	Required       bool
	RequireManaged bool
	RequireHTTP    bool
}

func validateArtifactServerEndpointRef(owner string, ref v1alpha1.ArtifactServerEndpointRef, env *v1alpha1.Environment, components map[string]v1alpha1.InfraComponent, opts artifactServerEndpointValidation) []string {
	if ref.IsZero() && !opts.Required {
		return nil
	}
	var errs []string
	if ref.EndpointRef.Name == "" {
		errs = append(errs, owner+".endpointRef is required")
	}
	if env == nil {
		errs = append(errs, owner+".serverRef requires an Environment with spec.infraComponents.artifactServers")
		return errs
	}
	serverRef := ref.ServerRef.Name
	if serverRef == "" {
		serverRef = env.Spec.DefaultArtifactServerName()
	}
	if serverRef == "" {
		errs = append(errs, owner+".serverRef is required, or one Environment.spec.infraComponents.artifactServers[] entry must be marked default")
		return errs
	}
	entry, ok := environmentArtifactServerByName(env, serverRef)
	if !ok {
		errs = append(errs, fmt.Sprintf("%s.serverRef %q does not resolve to Environment/%s spec.infraComponents.artifactServers[].name", owner, serverRef, env.Metadata.Name))
		return errs
	}
	if opts.RequireManaged && entry.Management != v1alpha1.EnvironmentComponentManaged {
		errs = append(errs, fmt.Sprintf("%s.serverRef %q must select a managed artifact server", owner, serverRef))
		return errs
	}
	endpoints := artifactServerEndpointNames(entry, components)
	if entry.Management == v1alpha1.EnvironmentComponentManaged && len(endpoints) == 0 {
		errs = append(errs, fmt.Sprintf("%s.serverRef %q does not resolve to managed artifact server endpoints", owner, serverRef))
	}
	if ref.EndpointRef.Name != "" && !endpoints[ref.EndpointRef.Name] {
		errs = append(errs, fmt.Sprintf("%s.endpointRef %q does not resolve to the selected artifact server endpoints", owner, ref.EndpointRef.Name))
		return errs
	}
	if opts.RequireHTTP {
		protocol := artifactServerEndpointProtocol(entry, components, ref.EndpointRef.Name)
		if protocol != "" && protocol != v1alpha1.ArtifactServerProtocolHTTP {
			errs = append(errs, fmt.Sprintf("%s.endpointRef %q must select an http artifact server endpoint", owner, ref.EndpointRef.Name))
		}
	}
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
	switch entry.Management {
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

func artifactServerEndpointProtocol(entry v1alpha1.EnvironmentArtifactServerComponent, components map[string]v1alpha1.InfraComponent, name string) string {
	if entry.Management != v1alpha1.EnvironmentComponentManaged {
		return ""
	}
	component, ok := components[entry.ComponentRef.Name]
	if !ok || component.Spec.ArtifactServer == nil {
		return ""
	}
	listeners := map[string]string{}
	for _, listener := range component.Spec.ArtifactServer.Listeners {
		listeners[listener.Name] = listener.Protocol
	}
	for _, endpoint := range component.Spec.ArtifactServer.Endpoints {
		if endpoint.Name == name {
			return listeners[endpoint.ListenerRef.Name]
		}
	}
	return ""
}

func validateMachineNetworkBindings(ci v1alpha1.ClusterInstall, providers map[string]v1alpha1.InfraProvider, networkConfigs map[string]v1alpha1.NetworkConfig) []string {
	var errs []string
	for _, binding := range ci.NetworkBindings {
		mp := fmt.Sprintf("Machine/%s", binding.MachineName)
		if binding.MachineName == "" {
			mp = fmt.Sprintf("ContainerCluster/%s node Machine", ci.Metadata.Name)
		}
		if binding.NetworkConfigRef.Name == "" {
			errs = append(errs, mp+" spec.network.config.networkConfigRef is required")
		} else if _, ok := networkConfigs[binding.NetworkConfigRef.Name]; !ok {
			errs = append(errs, fmt.Sprintf("%s spec.network.config.networkConfigRef %q does not match any NetworkConfig", mp, binding.NetworkConfigRef.Name))
		}
		provider, ok := providers[binding.ProviderRef.Name]
		if binding.ProviderRef.Name == "" {
			errs = append(errs, mp+" spec.substrate.providerRef is required")
		} else if !ok {
			errs = append(errs, fmt.Sprintf("%s spec.substrate.providerRef %q does not match any InfraProvider", mp, binding.ProviderRef.Name))
		}
		if binding.AttachmentRef.Name == "" {
			errs = append(errs, mp+" spec.network.config.attachmentRef is required")
		} else if ok {
			if attachment, found := lookupNetworkAttachment(provider, binding.AttachmentRef.Name); !found {
				errs = append(errs, fmt.Sprintf("%s spec.network.config.attachmentRef %q does not match any networkAttachments[] on InfraProvider/%s", mp, binding.AttachmentRef.Name, provider.Metadata.Name))
			} else if kind := v1alpha1.NetworkAttachmentKind(attachment); kind != "" && kind != provider.Spec.Type {
				errs = append(errs, fmt.Sprintf("%s spec.network.config.attachmentRef %q binds to InfraProvider/%s networkAttachment of kind %q, but provider type is %q",
					mp, binding.AttachmentRef.Name, provider.Metadata.Name, kind, provider.Spec.Type))
			}
		}
	}
	return errs
}
