package render

import (
	"net"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/stateview"
)

func findInfraComponent(state v1alpha1.State, name string) (v1alpha1.InfraComponent, bool) {
	return stateview.InfraComponent(state, name)
}

func selectedRegistryEntry(env *v1alpha1.Environment) (v1alpha1.EnvironmentRegistryComponent, bool) {
	if env == nil {
		return v1alpha1.EnvironmentRegistryComponent{}, false
	}
	for _, entry := range env.Spec.InfraComponents.Registries {
		if entry.Default {
			return entry, true
		}
	}
	if len(env.Spec.InfraComponents.Registries) == 1 {
		return env.Spec.InfraComponents.Registries[0], true
	}
	return v1alpha1.EnvironmentRegistryComponent{}, false
}

func nameResolutionEntry(env *v1alpha1.Environment, name string) (v1alpha1.EnvironmentNameResolutionComponent, bool) {
	if env == nil || name == "" {
		return v1alpha1.EnvironmentNameResolutionComponent{}, false
	}
	for _, entry := range env.Spec.InfraComponents.NameResolution {
		if entry.Name == name {
			return entry, true
		}
	}
	return v1alpha1.EnvironmentNameResolutionComponent{}, false
}

func managedServiceEndpointAddress(state v1alpha1.State, hostRef string, endpoints []v1alpha1.ServiceEndpoint, endpointName string) string {
	if endpointName != "" {
		for _, endpoint := range endpoints {
			if endpoint.Name == endpointName {
				if host, ok := stateview.NamedHostAddress(state, hostRef, endpoint.AddressName); ok {
					return host
				}
				return ""
			}
		}
		return ""
	}
	if len(endpoints) == 1 {
		if host, ok := stateview.NamedHostAddress(state, hostRef, endpoints[0].AddressName); ok {
			return host
		}
	}
	return ""
}

func resolvedNameResolutionIP(state v1alpha1.State, ci v1alpha1.ClusterInfra, network v1alpha1.NetworkConfig, entry v1alpha1.EnvironmentNameResolutionComponent) string {
	switch entry.Type {
	case v1alpha1.EnvironmentComponentExternal:
		return entry.IP
	case v1alpha1.EnvironmentComponentManaged:
		component, ok := findInfraComponent(state, entry.ComponentRef.Name)
		if !ok || component.Spec.NameResolution == nil {
			return ""
		}
		dns := component.Spec.NameResolution
		if ip := v1alpha1.DNSServiceIP(dns.BindAddress, network); ip != "" {
			return ip
		}
		address := managedServiceEndpointAddress(state, dns.HostRef.Name, dns.Endpoints, entry.Endpoint)
		if address == "" {
			return ""
		}
		ip := net.ParseIP(address)
		if ip == nil {
			return ""
		}
		if stateview.NetworkConfigContainsIP(network, ip) {
			return address
		}
	}
	return ""
}
