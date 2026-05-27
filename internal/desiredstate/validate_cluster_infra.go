package desiredstate

import (
	"fmt"
	"net"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/stateview"
)

func validateClusterInfras(state v1alpha1.State) []string {
	var errs []string
	providers := indexProviders(state.InfraProviders)
	networkConfigs := indexNetworkConfigs(state.NetworkConfigs)
	components := indexInfraComponents(state.InfraComponents)
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
		errs = append(errs, validateClusterEndpoints(ci, components, networkConfigs)...)
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

func validateClusterEndpoints(ci v1alpha1.ClusterInfra, components map[string]v1alpha1.InfraComponent, networkConfigs map[string]v1alpha1.NetworkConfig) []string {
	var errs []string
	if len(ci.Spec.Endpoints) == 0 {
		errs = append(errs, fmt.Sprintf("ClusterInfra/%s spec.endpoints is required", ci.Metadata.Name))
		return errs
	}
	allowed := map[string]bool{
		v1alpha1.EndpointAPI:     true,
		v1alpha1.EndpointAPIInt:  true,
		v1alpha1.EndpointIngress: true,
	}
	for name := range ci.Spec.Endpoints {
		if !allowed[name] {
			errs = append(errs, fmt.Sprintf("ClusterInfra/%s spec.endpoints.%s is not supported; expected only {%s, %s, %s}",
				ci.Metadata.Name, name, v1alpha1.EndpointAPI, v1alpha1.EndpointAPIInt, v1alpha1.EndpointIngress))
		}
	}
	for _, name := range []string{v1alpha1.EndpointAPI, v1alpha1.EndpointAPIInt, v1alpha1.EndpointIngress} {
		endpoint, ok := ci.Spec.Endpoints[name]
		if !ok {
			errs = append(errs, fmt.Sprintf("ClusterInfra/%s spec.endpoints.%s is required", ci.Metadata.Name, name))
			continue
		}
		errs = append(errs, validateClusterEndpoint(ci, components, name, endpoint, networkConfigs)...)
	}
	return errs
}

func validateClusterEndpoint(ci v1alpha1.ClusterInfra, components map[string]v1alpha1.InfraComponent, name string, endpoint v1alpha1.Endpoint, networkConfigs map[string]v1alpha1.NetworkConfig) []string {
	var errs []string
	prefix := fmt.Sprintf("ClusterInfra/%s spec.endpoints.%s", ci.Metadata.Name, name)
	set := 0
	if endpoint.VIP != "" {
		set++
	}
	if endpoint.ExternalVIP != "" {
		set++
	}
	if endpoint.ProvidedBy != nil {
		set++
	}
	if set != 1 {
		errs = append(errs, fmt.Sprintf("%s must set exactly one of {vip, externalVip, providedBy} (got %d)", prefix, set))
		return errs
	}
	if endpoint.ProvidedBy != nil {
		errs = append(errs, validateEndpointProvider(prefix, *endpoint.ProvidedBy, components)...)
	}
	vip, ok := endpointVIP(components, endpoint)
	if !ok {
		return errs
	}
	ip := net.ParseIP(vip)
	if ip == nil {
		errs = append(errs, fmt.Sprintf("%s effective VIP %q is not a valid IP", prefix, vip))
		return errs
	}
	matches := endpointNetworkMatches(ci, networkConfigs, ip)
	switch len(matches) {
	case 0:
		errs = append(errs, fmt.Sprintf("%s effective VIP %q is outside selected NetworkConfig machine networks", prefix, vip))
	case 1:
	default:
		errs = append(errs, fmt.Sprintf("%s effective VIP %q matches multiple selected NetworkConfigs (%s)",
			prefix, vip, joinSortedNames(matches)))
	}
	return errs
}

func validateEndpointProvider(prefix string, ref v1alpha1.EndpointProvidedBy, components map[string]v1alpha1.InfraComponent) []string {
	var errs []string
	if ref.ComponentRef.Name == "" {
		return []string{fmt.Sprintf("%s.providedBy.componentRef.name is required", prefix)}
	}
	component, ok := components[ref.ComponentRef.Name]
	if !ok || component.Spec.LoadBalancer == nil {
		return []string{fmt.Sprintf("%s.providedBy.componentRef.name %q does not resolve to an InfraComponent loadBalancer", prefix, ref.ComponentRef.Name)}
	}
	lb := component.Spec.LoadBalancer
	if len(lb.BindAddresses) == 0 {
		return []string{fmt.Sprintf("InfraComponent/%s spec.loadBalancer.bindAddresses is required for provided endpoints", component.Metadata.Name)}
	}
	if len(lb.BindAddresses) > 1 && ref.Address == "" {
		errs = append(errs, fmt.Sprintf("%s.providedBy.address is required because InfraComponent/%s spec.loadBalancer has multiple bindAddresses",
			prefix, component.Metadata.Name))
	}
	if ref.Address != "" && !loadBalancerHasBindAddress(lb, ref.Address) {
		errs = append(errs, fmt.Sprintf("%s.providedBy.address %q does not match any InfraComponent/%s spec.loadBalancer.bindAddresses[].name",
			prefix, ref.Address, component.Metadata.Name))
	}
	return errs
}

func endpointVIP(components map[string]v1alpha1.InfraComponent, endpoint v1alpha1.Endpoint) (string, bool) {
	switch {
	case endpoint.VIP != "":
		return endpoint.VIP, true
	case endpoint.ExternalVIP != "":
		return endpoint.ExternalVIP, true
	case endpoint.ProvidedBy != nil:
		component, ok := components[endpoint.ProvidedBy.ComponentRef.Name]
		if !ok || component.Spec.LoadBalancer == nil || len(component.Spec.LoadBalancer.BindAddresses) == 0 {
			return "", false
		}
		lb := component.Spec.LoadBalancer
		if endpoint.ProvidedBy.Address == "" {
			if len(lb.BindAddresses) == 1 {
				return lb.BindAddresses[0].IP, true
			}
			return "", false
		}
		for _, bind := range lb.BindAddresses {
			if bind.Name == endpoint.ProvidedBy.Address {
				return bind.IP, true
			}
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
