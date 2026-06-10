package inventory

import "github.com/crmarques/bootwright/api/v1alpha1"

func infraComponentsVars(state v1alpha1.State) []any {
	out := make([]any, 0, len(state.InfraComponents))
	for _, component := range state.InfraComponents {
		entry := map[string]any{"name": component.Metadata.Name}
		if server := component.Spec.ArtifactServer; server != nil {
			entry["artifactServer"] = map[string]any{
				"machineRef":  server.MachineRef.Name,
				"bindAddress": server.BindAddress,
				"listeners":   artifactServerListenersVars(server.Listeners),
				"endpoints":   artifactServerEndpointsVars(server.Endpoints),
			}
		}
		if lb := component.Spec.LoadBalancer; lb != nil {
			entry["loadBalancer"] = map[string]any{
				"implementation": lb.Implementation,
				"machineRef":     lb.MachineRef.Name,
				"bindAddresses":  loadBalancerBindAddressesVars(lb.BindAddresses),
			}
		}
		if proxy := component.Spec.Proxy; proxy != nil {
			entry["proxy"] = serviceComponentVars(proxy.Implementation, proxy.MachineRef.Name, proxy.BindAddress, proxy.Port, proxy.Endpoints)
		}
		if dns := component.Spec.NameResolution; dns != nil {
			v := serviceComponentVars(dns.Implementation, dns.MachineRef.Name, dns.BindAddress, dns.Port, dns.Endpoints)
			if len(dns.AdditionalIngressHosts) > 0 {
				v["additionalIngressHosts"] = stringSliceAny(dns.AdditionalIngressHosts)
			}
			if len(dns.Forwarders) > 0 {
				v["forwarders"] = stringSliceAny(dns.Forwarders)
			}
			entry["nameResolution"] = v
		}
		if ntp := component.Spec.NTP; ntp != nil {
			v := serviceComponentVars(ntp.Implementation, ntp.MachineRef.Name, ntp.BindAddress, ntp.Port, ntp.Endpoints)
			if len(ntp.UpstreamSources) > 0 {
				v["upstreamSources"] = stringSliceAny(ntp.UpstreamSources)
			}
			entry["ntp"] = v
		}
		if registry := component.Spec.Registry; registry != nil {
			entry["registry"] = serviceComponentVars(registry.Implementation, registry.MachineRef.Name, registry.BindAddress, registry.Port, registry.Endpoints)
		}
		out = append(out, entry)
	}
	return out
}

func environmentInfraComponentsVars(env *v1alpha1.Environment) map[string]any {
	out := map[string]any{}
	if len(env.Spec.InfraComponents.Proxies) > 0 {
		values := make([]any, 0, len(env.Spec.InfraComponents.Proxies))
		for _, entry := range env.Spec.InfraComponents.Proxies {
			values = append(values, environmentProxyComponentVars(entry))
		}
		out["proxies"] = values
	}
	if len(env.Spec.InfraComponents.NameResolution) > 0 {
		values := make([]any, 0, len(env.Spec.InfraComponents.NameResolution))
		for _, entry := range env.Spec.InfraComponents.NameResolution {
			values = append(values, environmentNameResolutionComponentVars(entry))
		}
		out["nameResolution"] = values
	}
	if len(env.Spec.InfraComponents.ArtifactServers) > 0 {
		values := make([]any, 0, len(env.Spec.InfraComponents.ArtifactServers))
		for _, entry := range env.Spec.InfraComponents.ArtifactServers {
			values = append(values, environmentArtifactServerComponentVars(entry))
		}
		out["artifactServers"] = values
	}
	if len(env.Spec.InfraComponents.Registries) > 0 {
		values := make([]any, 0, len(env.Spec.InfraComponents.Registries))
		for _, entry := range env.Spec.InfraComponents.Registries {
			values = append(values, environmentRegistryComponentVars(entry))
		}
		out["registries"] = values
	}
	if len(env.Spec.InfraComponents.NTP) > 0 {
		values := make([]any, 0, len(env.Spec.InfraComponents.NTP))
		for _, entry := range env.Spec.InfraComponents.NTP {
			values = append(values, environmentNTPComponentVars(entry))
		}
		out["ntp"] = values
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func environmentProxyComponentVars(entry v1alpha1.EnvironmentProxyComponent) map[string]any {
	out := environmentComponentBaseVars(entry.Name, entry.Management, false, entry.ComponentRef.Name, entry.EndpointRef)
	if entry.Connection != nil {
		connection := map[string]any{}
		if entry.Connection.HTTPProxy != "" {
			connection["httpProxy"] = entry.Connection.HTTPProxy
		}
		if entry.Connection.HTTPSProxy != "" {
			connection["httpsProxy"] = entry.Connection.HTTPSProxy
		}
		if len(entry.Connection.NoProxy) > 0 {
			connection["noProxy"] = stringSliceAny(entry.Connection.NoProxy)
		}
		if entry.Connection.Auth != nil && entry.Connection.Auth.ProxyAuthRef.Name != "" {
			connection["auth"] = map[string]any{"proxyAuthRef": entry.Connection.Auth.ProxyAuthRef.Name}
		}
		out["connection"] = connection
	}
	return out
}

func environmentNameResolutionComponentVars(entry v1alpha1.EnvironmentNameResolutionComponent) map[string]any {
	out := environmentComponentBaseVars(entry.Name, entry.Management, false, entry.ComponentRef.Name, entry.EndpointRef)
	if entry.Address != "" {
		out["address"] = entry.Address
	}
	if len(entry.AdditionalIngressHosts) > 0 {
		out["additionalIngressHosts"] = stringSliceAny(entry.AdditionalIngressHosts)
	}
	return out
}

func environmentNTPComponentVars(entry v1alpha1.EnvironmentNTPComponent) map[string]any {
	out := environmentComponentBaseVars(entry.Name, entry.Management, false, entry.ComponentRef.Name, entry.EndpointRef)
	if entry.Address != "" {
		out["address"] = entry.Address
	}
	return out
}

func environmentArtifactServerComponentVars(entry v1alpha1.EnvironmentArtifactServerComponent) map[string]any {
	out := environmentComponentBaseVars(entry.Name, entry.Management, false, entry.ComponentRef.Name, "")
	if len(entry.Endpoints) > 0 {
		endpoints := make([]any, 0, len(entry.Endpoints))
		for _, endpoint := range entry.Endpoints {
			endpoints = append(endpoints, map[string]any{
				"name": endpoint.Name,
				"url":  endpoint.URL,
			})
		}
		out["endpoints"] = endpoints
	}
	return out
}

func environmentRegistryComponentVars(entry v1alpha1.EnvironmentRegistryComponent) map[string]any {
	out := environmentComponentBaseVars(entry.Name, entry.Management, entry.Default, entry.ComponentRef.Name, entry.EndpointRef)
	if entry.URL != "" {
		out["url"] = entry.URL
	}
	return out
}

func environmentComponentBaseVars(name, management string, isDefault bool, componentRef, endpoint string) map[string]any {
	out := map[string]any{"name": name, "management": management}
	if isDefault {
		out["default"] = true
	}
	if componentRef != "" {
		out["componentRef"] = componentRef
	}
	if endpoint != "" {
		out["endpointRef"] = endpoint
	}
	return out
}

func serviceComponentVars(typ, machineRef, bindAddress string, port int, endpoints []v1alpha1.ServiceEndpoint) map[string]any {
	out := map[string]any{
		"implementation": typ,
		"machineRef":     machineRef,
		"bindAddress":    bindAddress,
		"port":           port,
	}
	if len(endpoints) > 0 {
		out["endpoints"] = serviceEndpointsVars(endpoints)
	}
	return out
}

func serviceEndpointsVars(endpoints []v1alpha1.ServiceEndpoint) []any {
	out := make([]any, 0, len(endpoints))
	for _, endpoint := range endpoints {
		out = append(out, map[string]any{
			"name":       endpoint.Name,
			"addressRef": endpoint.AddressRef,
		})
	}
	return out
}

func loadBalancerBindAddressesVars(binds []v1alpha1.LoadBalancerBindAddress) []any {
	out := make([]any, 0, len(binds))
	for _, bind := range binds {
		entry := map[string]any{"address": bind.Address}
		if bind.Name != "" {
			entry["name"] = bind.Name
		}
		out = append(out, entry)
	}
	return out
}
