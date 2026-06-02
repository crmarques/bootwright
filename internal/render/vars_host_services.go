package render

import (
	"fmt"
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/artifacts"
	"github.com/crmarques/bootwright/internal/infra/support"
	"github.com/crmarques/bootwright/internal/state/graph"
	"github.com/crmarques/bootwright/internal/state/view"
)

func providerServicesVars(state v1alpha1.State) []any {
	return hostServicesVars(state, func(service stategraph.HostService) bool {
		return service.IsProviderService()
	})
}

func infraComponentServicesVars(state v1alpha1.State) []any {
	return hostServicesVars(state, func(service stategraph.HostService) bool {
		return service.IsInfraComponentService()
	})
}

func hostServicesVars(state v1alpha1.State, include func(stategraph.HostService) bool) []any {
	builder := newHostServiceBuilder()
	graph := stategraph.ResolveHostServices(state)
	for _, service := range graph.Services {
		if !include(service) {
			continue
		}
		component, ok := hostServiceVarsFromGraph(state, service)
		if !ok {
			continue
		}
		clusters := service.ConsumerClusters()
		if len(clusters) == 0 {
			builder.Add(component, "")
			continue
		}
		for _, cluster := range clusters {
			builder.Add(component, cluster)
		}
	}
	return builder.Services()
}

func hostServiceVarsFromGraph(state v1alpha1.State, service stategraph.HostService) (map[string]any, bool) {
	switch service.Identity.Kind {
	case v1alpha1.ComponentSlotLoadBalancer:
		return loadBalancerHostServiceVars(state, service)
	case v1alpha1.ComponentSlotArtifacts:
		return artifactHostServiceVars(state, service)
	case v1alpha1.ComponentSlotProxy:
		return proxyHostServiceVars(state, service)
	case v1alpha1.ComponentSlotNameResolution:
		return nameResolutionHostServiceVars(state, service)
	case v1alpha1.ComponentSlotNTP:
		return ntpHostServiceVars(state, service)
	case v1alpha1.ComponentSlotRegistry:
		return registryHostServiceVars(state, service)
	case v1alpha1.ProviderServiceKindBMC:
		return bmcHostServiceVarsFromGraph(state, service)
	default:
		return nil, false
	}
}

func loadBalancerHostServiceVars(state v1alpha1.State, service stategraph.HostService) (map[string]any, bool) {
	component, ok := findInfraComponent(state, service.Identity.Name)
	if !ok || component.Spec.LoadBalancer == nil {
		return nil, false
	}
	out := loadBalancerComponentVars(state, component)
	var frontends []any
	for _, consumer := range service.Consumers {
		ci, ok := clusterInfraByName(state, consumer.ClusterInfra)
		if !ok {
			continue
		}
		frontends = append(frontends, loadBalancerFrontends(state, ci, component.Metadata.Name, consumer.Cluster, ci.Spec.Components.Machines, clusterNodesForCI(state, ci))...)
	}
	if len(frontends) > 0 {
		out["frontends"] = frontends
	}
	return out, true
}

func artifactHostServiceVars(state v1alpha1.State, service stategraph.HostService) (map[string]any, bool) {
	for _, consumer := range service.Consumers {
		ci, ok := clusterInfraByName(state, consumer.ClusterInfra)
		if !ok {
			continue
		}
		server, ok := artifacts.Select(state, ci)
		if !ok || server.Component.Metadata.Name != service.Identity.Name || server.Config == nil {
			continue
		}
		return artifactServerComponentVars(state, ci, server), true
	}
	return nil, false
}

func proxyHostServiceVars(state v1alpha1.State, service stategraph.HostService) (map[string]any, bool) {
	component, ok := findInfraComponent(state, service.Identity.Name)
	if !ok || component.Spec.Proxy == nil {
		return nil, false
	}
	entry := v1alpha1.EnvironmentProxyComponent{Name: serviceEntryName(service)}
	return proxyComponentVars(state, entry, component), true
}

func nameResolutionHostServiceVars(state v1alpha1.State, service stategraph.HostService) (map[string]any, bool) {
	component, ok := findInfraComponent(state, service.Identity.Name)
	if !ok || component.Spec.NameResolution == nil {
		return nil, false
	}
	dns := component.Spec.NameResolution
	port := dns.Port
	if port == 0 {
		port = support.LookupService(v1alpha1.ComponentSlotNameResolution, v1alpha1.InfraComponentTypeDnsmasq).DefaultPort
	}
	additionalHosts := append([]string(nil), service.MergedStringFields["additionalIngressHosts"]...)
	hostRecords, domainRecords := nameResolutionRecordsForGraphService(state, service)
	out := map[string]any{
		"kind":                   v1alpha1.ComponentSlotNameResolution,
		"providerName":           v1alpha1.KindInfraComponent,
		"name":                   component.Metadata.Name,
		"componentName":          component.Metadata.Name,
		"entryName":              serviceEntryName(service),
		"port":                   port,
		"bindAddress":            dns.BindAddress,
		"additionalIngressHosts": additionalHosts,
		"hostRef":                dns.HostRef.Name,
		"hostAddress":            lookupHostAddress(state, dns.HostRef.Name),
		"realisation":            v1alpha1.InfraComponentTypeDnsmasq,
		"image":                  managedDnsmasqImage(state),
	}
	if len(hostRecords) > 0 {
		out["hostRecords"] = hostRecords
	}
	if len(domainRecords) > 0 {
		out["domainRecords"] = domainRecords
	}
	applyServiceRoleContract(out, v1alpha1.ComponentSlotNameResolution, v1alpha1.InfraComponentTypeDnsmasq)
	return out, true
}

func ntpHostServiceVars(state v1alpha1.State, service stategraph.HostService) (map[string]any, bool) {
	component, ok := findInfraComponent(state, service.Identity.Name)
	if !ok || component.Spec.NTP == nil {
		return nil, false
	}
	entry := v1alpha1.EnvironmentNTPSourceComponent{Name: serviceEntryName(service)}
	out := ntpComponentVars(state, entry, component)
	if networks := ntpAllowedNetworksForGraphService(state, service); len(networks) > 0 {
		out["allowedNetworks"] = stringSliceAny(networks)
	}
	return out, true
}

func ntpAllowedNetworksForGraphService(state v1alpha1.State, service stategraph.HostService) []string {
	seen := map[string]bool{}
	var out []string
	for _, consumer := range service.Consumers {
		ci, ok := clusterInfraByName(state, consumer.ClusterInfra)
		if !ok {
			continue
		}
		for _, network := range stateview.ClusterNetworkConfigs(state, ci) {
			for _, item := range network.Spec.MachineNetwork {
				if item.CIDR == "" || seen[item.CIDR] {
					continue
				}
				seen[item.CIDR] = true
				out = append(out, item.CIDR)
			}
		}
	}
	sort.Strings(out)
	return out
}

func registryHostServiceVars(state v1alpha1.State, service stategraph.HostService) (map[string]any, bool) {
	component, ok := findInfraComponent(state, service.Identity.Name)
	if !ok || component.Spec.Registry == nil {
		return nil, false
	}
	entry := v1alpha1.EnvironmentRegistryComponent{Name: serviceEntryName(service)}
	return registryComponentVars(state, entry, component), true
}

func clusterInfraByName(state v1alpha1.State, name string) (v1alpha1.ClusterInfra, bool) {
	for _, infra := range state.ClusterInfras {
		if infra.Metadata.Name == name {
			return infra, true
		}
	}
	return v1alpha1.ClusterInfra{}, false
}

func serviceEntryName(service stategraph.HostService) string {
	for _, consumer := range service.Consumers {
		if entryName := consumer.Fields["entryName"]; entryName != "" {
			return entryName
		}
	}
	return ""
}

func serviceEntryNames(service stategraph.HostService) []string {
	seen := map[string]bool{}
	var out []string
	for _, consumer := range service.Consumers {
		entryName := consumer.Fields["entryName"]
		if entryName == "" || seen[entryName] {
			continue
		}
		seen[entryName] = true
		out = append(out, entryName)
	}
	sort.Strings(out)
	return out
}

func nameResolutionRecordsForGraphService(state v1alpha1.State, service stategraph.HostService) ([]any, []any) {
	hostRecords := map[string]map[string]any{}
	domainRecords := map[string]map[string]any{}
	for _, entryName := range serviceEntryNames(service) {
		hosts, domains := nameResolutionRecordsVars(state, entryName, serviceEntryStringField(service, entryName, "additionalIngressHosts"))
		mergeRecordVars(hostRecords, hosts)
		mergeRecordVars(domainRecords, domains)
	}
	return sortedRecordVars(hostRecords), sortedRecordVars(domainRecords)
}

func serviceEntryStringField(service stategraph.HostService, entryName, field string) []string {
	seen := map[string]bool{}
	var out []string
	for _, consumer := range service.Consumers {
		if consumer.Fields["entryName"] != entryName {
			continue
		}
		for _, value := range consumer.MergeStringFields[field] {
			if value == "" || seen[value] {
				continue
			}
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func mergeRecordVars(dst map[string]map[string]any, records []any) {
	for _, raw := range records {
		record, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		key := fmt.Sprintf("%v|%v", record["name"], record["address"])
		dst[key] = cloneComponentVars(record)
	}
}

func sortedRecordVars(records map[string]map[string]any) []any {
	if len(records) == 0 {
		return nil
	}
	keys := make([]string, 0, len(records))
	for key := range records {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]any, 0, len(keys))
	for _, key := range keys {
		out = append(out, records[key])
	}
	return out
}

func providerHostSetupsVars(state v1alpha1.State) []any {
	type key struct {
		host string
		role string
	}
	seen := map[key]bool{}
	var keys []key
	for _, ci := range state.ClusterInfras {
		for _, machine := range ci.Spec.Components.Machines {
			hostRef := machineHostRef(state, machine)
			if hostRef == "" {
				continue
			}
			driver := ProviderDriver(state, machine)
			for _, role := range driver.Roles.HostSetupRoles {
				k := key{host: hostRef, role: role}
				if seen[k] {
					continue
				}
				seen[k] = true
				keys = append(keys, k)
			}
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].host != keys[j].host {
			return keys[i].host < keys[j].host
		}
		return keys[i].role < keys[j].role
	})
	out := make([]any, 0, len(keys))
	for _, k := range keys {
		out = append(out, map[string]any{
			"hostRef":     k.host,
			"hostAddress": lookupHostAddress(state, k.host),
			"applyRole":   k.role,
		})
	}
	return out
}

func cloneComponentVars(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
