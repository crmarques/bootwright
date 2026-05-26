package desiredstate

import (
	"fmt"
	"net"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func validateInfraComponents(state v1alpha1.State) []string {
	var errs []string
	hosts := indexHosts(state.Hosts)
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
		errs = append(errs, validateInfraComponentSpec(component, hosts)...)
	}
	return errs
}

func validateInfraComponentSpec(component v1alpha1.InfraComponent, hosts map[string]v1alpha1.Host) []string {
	prefix := fmt.Sprintf("InfraComponent/%s spec", component.Metadata.Name)
	set := 0
	if component.Spec.ArtifactServer != nil {
		set++
	}
	if set != 1 {
		return []string{fmt.Sprintf("%s must set exactly one of {artifactServer} (got %d)", prefix, set)}
	}
	return validateArtifactServerComponent(component, hosts)
}

func validateArtifactServerComponent(component v1alpha1.InfraComponent, hosts map[string]v1alpha1.Host) []string {
	server := component.Spec.ArtifactServer
	prefix := fmt.Sprintf("InfraComponent/%s spec.artifactServer", component.Metadata.Name)
	var errs []string
	errs = append(errs, validateServiceHostRef(prefix+".hostRef", server.HostRef, hosts, v1alpha1.ComponentSlotArtifacts, "http")...)
	if server.BindAddress != "" && net.ParseIP(server.BindAddress) == nil {
		errs = append(errs, fmt.Sprintf("%s.bindAddress %q is not a valid IP address", prefix, server.BindAddress))
	}
	errs = append(errs, validateArtifactServerListeners(prefix, server.Listeners)...)
	if host, ok := hosts[server.HostRef.Name]; ok {
		errs = append(errs, validateArtifactServerEndpoints(prefix, server.Listeners, server.Endpoints, host)...)
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

func validateArtifactServerEndpoints(prefix string, listeners []v1alpha1.ArtifactServerListener, endpoints []v1alpha1.ArtifactServerEndpoint, host v1alpha1.Host) []string {
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
			if !IsDNSLabel(endpoint.Name) {
				errs = append(errs, fmt.Sprintf("%s.name %q is not a DNS label", owner, endpoint.Name))
			}
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
		if endpoint.AddressName == "" {
			errs = append(errs, owner+".addressName is required")
		} else if _, ok := v1alpha1.HostAddressByName(host, endpoint.AddressName); !ok {
			errs = append(errs, fmt.Sprintf("%s.addressName %q does not resolve on Host/%s spec.addresses", owner, endpoint.AddressName, host.Metadata.Name))
		}
	}
	return errs
}
