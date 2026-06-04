package desiredstate

import (
	"fmt"
	"net"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/support"
)

func validateInfraComponents(state v1alpha1.State) []string {
	var errs []string
	machines := indexMachines(state.Machines)
	seen := map[string]bool{}
	for _, component := range state.InfraComponents {
		if e := validateName(v1alpha1.KindInfraComponent, component.Metadata.Name); e != "" {
			errs = append(errs, e)
			continue
		}
		if seen[component.Metadata.Name] {
			errs = append(errs, fmt.Sprintf("duplicate InfraComponent %q", component.Metadata.Name))
		}
		seen[component.Metadata.Name] = true
		errs = append(errs, validateInfraComponentSpec(component, machines)...)
	}
	return errs
}

func validateInfraComponentSpec(component v1alpha1.InfraComponent, machines map[string]v1alpha1.Machine) []string {
	prefix := fmt.Sprintf("InfraComponent/%s spec", component.Metadata.Name)
	set := 0
	if component.Spec.ArtifactServer != nil {
		set++
	}
	if component.Spec.LoadBalancer != nil {
		set++
	}
	if component.Spec.Proxy != nil {
		set++
	}
	if component.Spec.NameResolution != nil {
		set++
	}
	if component.Spec.NTP != nil {
		set++
	}
	if component.Spec.Registry != nil {
		set++
	}
	if set != 1 {
		return []string{fmt.Sprintf("%s must set exactly one of {artifactServer, loadBalancer, proxy, nameResolution, ntp, registry} (got %d)", prefix, set)}
	}
	var errs []string
	if component.Spec.ArtifactServer != nil {
		errs = append(errs, validateArtifactServerComponent(component, machines)...)
	}
	if component.Spec.LoadBalancer != nil {
		errs = append(errs, validateLoadBalancerComponent(component, machines)...)
	}
	if component.Spec.Proxy != nil {
		errs = append(errs, validateProxyComponent(component, machines)...)
	}
	if component.Spec.NameResolution != nil {
		errs = append(errs, validateNameResolutionComponent(component, machines)...)
	}
	if component.Spec.NTP != nil {
		errs = append(errs, validateNTPComponent(component, machines)...)
	}
	if component.Spec.Registry != nil {
		errs = append(errs, validateRegistryComponent(component, machines)...)
	}
	return errs
}

func validateArtifactServerComponent(component v1alpha1.InfraComponent, machines map[string]v1alpha1.Machine) []string {
	server := component.Spec.ArtifactServer
	prefix := fmt.Sprintf("InfraComponent/%s spec.artifactServer", component.Metadata.Name)
	var errs []string
	errs = append(errs, validateServiceMachineRef(prefix+".machineRef", server.MachineRef, machines, v1alpha1.ComponentSlotArtifacts, v1alpha1.ArtifactServerProtocolHTTP)...)
	errs = append(errs, validateServiceParams(prefix, server.BindAddress, 0)...)
	errs = append(errs, validateArtifactServerListeners(prefix, server.Listeners)...)
	if machine, ok := machines[server.MachineRef.Name]; ok {
		errs = append(errs, validateArtifactServerEndpoints(prefix, server.Listeners, server.Endpoints, machine)...)
	}
	return errs
}

func validateArtifactServerListeners(prefix string, listeners []v1alpha1.ArtifactServerListener) []string {
	if len(listeners) == 0 {
		return []string{prefix + ".listeners is required"}
	}
	var errs []string
	seenNames := map[string]bool{}
	seenPorts := map[int]bool{}
	for i, listener := range listeners {
		owner := fmt.Sprintf("%s.listeners[%d]", prefix, i)
		if listener.Name == "" {
			errs = append(errs, owner+".name is required")
		} else {
			if !IsDNSLabel(listener.Name) {
				errs = append(errs, fmt.Sprintf("%s.name %q is not a DNS label", owner, listener.Name))
			}
			if seenNames[listener.Name] {
				errs = append(errs, fmt.Sprintf("%s.name %q is duplicated", owner, listener.Name))
			}
			seenNames[listener.Name] = true
		}
		switch listener.Protocol {
		case v1alpha1.ArtifactServerProtocolHTTP, v1alpha1.ArtifactServerProtocolHTTPS:
		case "":
			errs = append(errs, owner+".protocol is required")
		default:
			errs = append(errs, fmt.Sprintf("%s.protocol %q must be one of {%s, %s}",
				owner, listener.Protocol, v1alpha1.ArtifactServerProtocolHTTP, v1alpha1.ArtifactServerProtocolHTTPS))
		}
		if listener.Port < 1 || listener.Port > 65535 {
			errs = append(errs, fmt.Sprintf("%s.port %d out of range", owner, listener.Port))
		} else {
			if seenPorts[listener.Port] {
				errs = append(errs, fmt.Sprintf("%s.port %d is duplicated", owner, listener.Port))
			}
			seenPorts[listener.Port] = true
		}
	}
	return errs
}

func validateArtifactServerEndpoints(prefix string, listeners []v1alpha1.ArtifactServerListener, endpoints []v1alpha1.ArtifactServerEndpoint, machine v1alpha1.Machine) []string {
	var errs []string
	listenerNames := map[string]bool{}
	for _, listener := range listeners {
		listenerNames[listener.Name] = true
	}
	seen := map[string]bool{}
	for i, endpoint := range endpoints {
		owner := fmt.Sprintf("%s.endpoints[%d]", prefix, i)
		if endpoint.Name == "" {
			errs = append(errs, owner+".name is required")
		} else {
			if seen[endpoint.Name] {
				errs = append(errs, fmt.Sprintf("%s.name %q is duplicated", owner, endpoint.Name))
			}
			seen[endpoint.Name] = true
		}
		if endpoint.Listener == "" {
			errs = append(errs, owner+".listener is required")
		} else if !listenerNames[endpoint.Listener] {
			errs = append(errs, fmt.Sprintf("%s.listener %q does not match any %s.listeners[].name", owner, endpoint.Listener, prefix))
		}
		if endpoint.MachineAddress == "" {
			errs = append(errs, owner+".machineAddress is required")
		} else if _, ok := v1alpha1.MachineAddressByName(machine, endpoint.MachineAddress); !ok {
			errs = append(errs, fmt.Sprintf("%s.machineAddress %q does not resolve to Machine/%s spec.os.addresses[].name", owner, endpoint.MachineAddress, machine.Metadata.Name))
		}
	}
	return errs
}

func validateLoadBalancerComponent(component v1alpha1.InfraComponent, machines map[string]v1alpha1.Machine) []string {
	lb := component.Spec.LoadBalancer
	prefix := fmt.Sprintf("InfraComponent/%s spec.loadBalancer", component.Metadata.Name)
	var errs []string
	if lb.Type != v1alpha1.InfraComponentTypeHAProxy {
		errs = append(errs, fmt.Sprintf("%s.type %q must be %q", prefix, lb.Type, v1alpha1.InfraComponentTypeHAProxy))
	}
	errs = append(errs, validateServiceMachineRef(prefix+".machineRef", lb.MachineRef, machines, v1alpha1.ComponentSlotLoadBalancer, v1alpha1.InfraComponentTypeHAProxy)...)
	errs = append(errs, validateLoadBalancerBindAddresses(prefix, lb.BindAddresses, nil)...)
	return errs
}

func validateProxyComponent(component v1alpha1.InfraComponent, machines map[string]v1alpha1.Machine) []string {
	proxy := component.Spec.Proxy
	prefix := fmt.Sprintf("InfraComponent/%s spec.proxy", component.Metadata.Name)
	var errs []string
	if proxy.Type != v1alpha1.InfraComponentTypeSquid {
		errs = append(errs, fmt.Sprintf("%s.type %q must be %q", prefix, proxy.Type, v1alpha1.InfraComponentTypeSquid))
	}
	errs = append(errs, validateServiceMachineRef(prefix+".machineRef", proxy.MachineRef, machines, v1alpha1.ComponentSlotProxy, v1alpha1.InfraComponentTypeSquid)...)
	errs = append(errs, validateServiceParams(prefix, proxy.BindAddress, proxy.Port)...)
	if machine, ok := machines[proxy.MachineRef.Name]; ok {
		errs = append(errs, validateServiceEndpoints(prefix, proxy.Endpoints, machine)...)
	}
	return errs
}

func validateNameResolutionComponent(component v1alpha1.InfraComponent, machines map[string]v1alpha1.Machine) []string {
	dns := component.Spec.NameResolution
	prefix := fmt.Sprintf("InfraComponent/%s spec.nameResolution", component.Metadata.Name)
	var errs []string
	if dns.Type != v1alpha1.InfraComponentTypeDnsmasq {
		errs = append(errs, fmt.Sprintf("%s.type %q must be %q", prefix, dns.Type, v1alpha1.InfraComponentTypeDnsmasq))
	}
	errs = append(errs, validateServiceMachineRef(prefix+".machineRef", dns.MachineRef, machines, v1alpha1.ComponentSlotNameResolution, v1alpha1.InfraComponentTypeDnsmasq)...)
	errs = append(errs, validateServiceParams(prefix, dns.BindAddress, dns.Port)...)
	if machine, ok := machines[dns.MachineRef.Name]; ok {
		errs = append(errs, validateServiceEndpoints(prefix, dns.Endpoints, machine)...)
	}
	return errs
}

func validateNTPComponent(component v1alpha1.InfraComponent, machines map[string]v1alpha1.Machine) []string {
	ntp := component.Spec.NTP
	prefix := fmt.Sprintf("InfraComponent/%s spec.ntp", component.Metadata.Name)
	var errs []string
	if ntp.Type != v1alpha1.InfraComponentTypeChrony {
		errs = append(errs, fmt.Sprintf("%s.type %q must be %q", prefix, ntp.Type, v1alpha1.InfraComponentTypeChrony))
	}
	errs = append(errs, validateServiceMachineRef(prefix+".machineRef", ntp.MachineRef, machines, v1alpha1.ComponentSlotNTP, v1alpha1.InfraComponentTypeChrony)...)
	errs = append(errs, validateServiceParams(prefix, ntp.BindAddress, ntp.Port)...)
	if machine, ok := machines[ntp.MachineRef.Name]; ok {
		errs = append(errs, validateServiceEndpoints(prefix, ntp.Endpoints, machine)...)
	}
	for i, source := range ntp.UpstreamSources {
		errs = append(errs, validateNTPAddress(fmt.Sprintf("%s.upstreamSources[%d]", prefix, i), source)...)
	}
	return errs
}

func validateRegistryComponent(component v1alpha1.InfraComponent, machines map[string]v1alpha1.Machine) []string {
	registry := component.Spec.Registry
	prefix := fmt.Sprintf("InfraComponent/%s spec.registry", component.Metadata.Name)
	var errs []string
	if registry.Type != v1alpha1.InfraComponentTypeMirrorRegistry {
		errs = append(errs, fmt.Sprintf("%s.type %q must be %q", prefix, registry.Type, v1alpha1.InfraComponentTypeMirrorRegistry))
	}
	errs = append(errs, validateServiceMachineRef(prefix+".machineRef", registry.MachineRef, machines, v1alpha1.ComponentSlotRegistry, v1alpha1.InfraComponentTypeMirrorRegistry)...)
	errs = append(errs, validateServiceParams(prefix, registry.BindAddress, registry.Port)...)
	if machine, ok := machines[registry.MachineRef.Name]; ok {
		errs = append(errs, validateServiceEndpoints(prefix, registry.Endpoints, machine)...)
	}
	return errs
}

func validateServiceMachineRef(owner string, ref v1alpha1.LocalObjectReference, machines map[string]v1alpha1.Machine, kind, realisation string) []string {
	var errs []string
	capabilities := support.ServiceHostCapabilities(kind, realisation)
	if len(capabilities) == 0 {
		return validateMachineRefCapability(owner, ref, machines, "")
	}
	for _, capability := range capabilities {
		errs = append(errs, validateMachineRefCapability(owner, ref, machines, capability)...)
	}
	return errs
}

func validateMachineRefCapability(owner string, ref v1alpha1.LocalObjectReference, machines map[string]v1alpha1.Machine, want string) []string {
	if ref.Name == "" {
		return []string{owner + ".name is required"}
	}
	machine, ok := machines[ref.Name]
	if !ok {
		return []string{fmt.Sprintf("%s %q does not match any Machine", owner, ref.Name)}
	}
	if want != "" && !machineHasCapability(machine, want) {
		return []string{fmt.Sprintf("%s %q lacks capability %q (Machine.spec.capabilities or spec.os.capabilities)", owner, ref.Name, want)}
	}
	return nil
}

func validateServiceEndpoints(prefix string, endpoints []v1alpha1.ServiceEndpoint, machine v1alpha1.Machine) []string {
	var errs []string
	seen := map[string]bool{}
	for i, endpoint := range endpoints {
		owner := fmt.Sprintf("%s.endpoints[%d]", prefix, i)
		if endpoint.Name == "" {
			errs = append(errs, owner+".name is required")
		} else {
			if seen[endpoint.Name] {
				errs = append(errs, fmt.Sprintf("%s.name %q is duplicated", owner, endpoint.Name))
			}
			seen[endpoint.Name] = true
		}
		if endpoint.MachineAddress == "" {
			errs = append(errs, owner+".machineAddress is required")
		} else if _, ok := v1alpha1.MachineAddressByName(machine, endpoint.MachineAddress); !ok {
			errs = append(errs, fmt.Sprintf("%s.machineAddress %q does not resolve to Machine/%s spec.os.addresses[].name", owner, endpoint.MachineAddress, machine.Metadata.Name))
		}
	}
	return errs
}

func validateServiceParams(prefix, bindAddress string, port int) []string {
	var errs []string
	if bindAddress != "" && net.ParseIP(bindAddress) == nil {
		errs = append(errs, fmt.Sprintf("%s.bindAddress %q is not a valid IP address", prefix, bindAddress))
	}
	if port < 0 || port > 65535 {
		errs = append(errs, fmt.Sprintf("%s.port %d out of range", prefix, port))
	}
	return errs
}
