package render

import (
	"fmt"
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/artifacts"
	"github.com/crmarques/bootwright/internal/infra/support"
	"github.com/crmarques/bootwright/internal/state/graph"
)

const providerServiceKindBMC = "bmc"

func providerServicesVars(state v1alpha1.State) []any {
	builder := newProviderServiceBuilder()
	graph := stategraph.ResolveProviderServices(state)
	for _, service := range graph.Services {
		component, ok := providerServiceVarsFromGraph(state, service)
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
	for _, raw := range bmcProviderServiceVars(state) {
		if component, ok := raw.(map[string]any); ok {
			builder.Add(component, "")
		}
	}
	return builder.Services()
}

func providerServiceVarsFromGraph(state v1alpha1.State, service stategraph.ProviderService) (map[string]any, bool) {
	switch service.Identity.Kind {
	case v1alpha1.ComponentSlotLoadBalancer:
		return loadBalancerProviderServiceVars(state, service)
	case v1alpha1.ComponentSlotArtifacts:
		return artifactProviderServiceVars(state, service)
	case v1alpha1.ComponentSlotProxy:
		return proxyProviderServiceVars(state, service)
	case v1alpha1.ComponentSlotNameResolution:
		return nameResolutionProviderServiceVars(state, service)
	case v1alpha1.ComponentSlotRegistry:
		return registryProviderServiceVars(state, service)
	default:
		return nil, false
	}
}

func loadBalancerProviderServiceVars(state v1alpha1.State, service stategraph.ProviderService) (map[string]any, bool) {
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

func artifactProviderServiceVars(state v1alpha1.State, service stategraph.ProviderService) (map[string]any, bool) {
	server, ok := artifacts.Select(state)
	if !ok || server.Component.Metadata.Name != service.Identity.Name || server.Config == nil {
		return nil, false
	}
	return artifactServerComponentVars(state, server), true
}

func proxyProviderServiceVars(state v1alpha1.State, service stategraph.ProviderService) (map[string]any, bool) {
	component, ok := findInfraComponent(state, service.Identity.Name)
	if !ok || component.Spec.Proxy == nil {
		return nil, false
	}
	entry := v1alpha1.EnvironmentProxyComponent{Name: serviceEntryName(service)}
	return proxyComponentVars(state, entry, component), true
}

func nameResolutionProviderServiceVars(state v1alpha1.State, service stategraph.ProviderService) (map[string]any, bool) {
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

func registryProviderServiceVars(state v1alpha1.State, service stategraph.ProviderService) (map[string]any, bool) {
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

func serviceEntryName(service stategraph.ProviderService) string {
	for _, consumer := range service.Consumers {
		if entryName := consumer.Fields["entryName"]; entryName != "" {
			return entryName
		}
	}
	return ""
}

func serviceEntryNames(service stategraph.ProviderService) []string {
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

func nameResolutionRecordsForGraphService(state v1alpha1.State, service stategraph.ProviderService) ([]any, []any) {
	hostRecords := map[string]map[string]any{}
	domainRecords := map[string]map[string]any{}
	for _, entryName := range serviceEntryNames(service) {
		hosts, domains := nameResolutionRecordsVars(state, entryName, serviceEntryStringField(service, entryName, "additionalIngressHosts"))
		mergeRecordVars(hostRecords, hosts)
		mergeRecordVars(domainRecords, domains)
	}
	return sortedRecordVars(hostRecords), sortedRecordVars(domainRecords)
}

func serviceEntryStringField(service stategraph.ProviderService, entryName, field string) []string {
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

type bmcProviderServiceGroup struct {
	key              bmcProviderServiceKey
	component        map[string]any
	configKey        string
	configConsistent bool
}

type bmcProviderServiceKey struct {
	providerName string
	hostRef      string
	applyRole    string
}

func bmcProviderServiceVars(state v1alpha1.State) []any {
	groups := map[bmcProviderServiceKey]*bmcProviderServiceGroup{}
	var order []bmcProviderServiceKey
	for _, ocp := range state.ContainerClusters {
		ci, err := clusterInfraForOCP(state, ocp)
		if err != nil {
			continue
		}
		for _, machine := range ci.Spec.Components.Machines {
			driver := ProviderDriver(state, machine)
			if driver.Dispatch.BMCRole == "none" || driver.Roles.BMCApplyRole == "" {
				continue
			}
			hostRef := machineHostRef(state, machine)
			if hostRef == "" {
				continue
			}
			bmc := machineBMCServiceConfig(state, machine)
			if bmc == nil {
				continue
			}
			k := bmcProviderServiceKey{
				providerName: machine.From.Provider,
				hostRef:      hostRef,
				applyRole:    driver.Roles.BMCApplyRole,
			}
			g, ok := groups[k]
			if !ok {
				g = &bmcProviderServiceGroup{
					key:       k,
					configKey: bmcConfigKey(bmc),
					component: map[string]any{
						"kind":             providerServiceKindBMC,
						"providerName":     machine.From.Provider,
						"name":             driver.Dispatch.BMCRole,
						"hostRef":          hostRef,
						"hostAddress":      lookupHostAddress(state, hostRef),
						"realisation":      driver.Dispatch.BMCRole,
						"bmcRole":          driver.Dispatch.BMCRole,
						"applyRole":        driver.Roles.BMCApplyRole,
						"destroyRole":      driver.Roles.BMCDestroyRole,
						"bmcEmulated":      bmc,
						"machines":         []any{},
						"configConsistent": true,
					},
					configConsistent: true,
				}
				groups[k] = g
				order = append(order, k)
			}
			if g.configKey != bmcConfigKey(bmc) {
				g.configConsistent = false
				g.component["configConsistent"] = false
			}
			g.component["machines"] = append(g.component["machines"].([]any), map[string]any{
				"clusterName":  ocp.Metadata.Name,
				"name":         machine.Name,
				"bmcEmulated":  bmc,
				"providerName": machine.From.Provider,
			})
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		if order[i].hostRef != order[j].hostRef {
			return order[i].hostRef < order[j].hostRef
		}
		if order[i].providerName != order[j].providerName {
			return order[i].providerName < order[j].providerName
		}
		return order[i].applyRole < order[j].applyRole
	})
	out := make([]any, 0, len(order))
	for _, k := range order {
		out = append(out, groups[k].component)
	}
	return out
}

func machineBMCServiceConfig(state v1alpha1.State, machine v1alpha1.ClusterMachineComponent) map[string]any {
	if machine.From.Profile == "" {
		return nil
	}
	provider, ok := findProvider(state, machine.From.Provider)
	if !ok {
		return nil
	}
	profile, ok := findProfile(provider, machine.From.Profile)
	if !ok {
		return nil
	}
	return machineEmulatedBMCVars(state, profile)
}

func bmcConfigKey(m map[string]any) string {
	return fmt.Sprintf("%v|%v|%v|%v|%v|%v|%v",
		m["protocol"],
		m["libvirtURI"],
		m["bindAddress"],
		m["port"],
		m["vmediaPort"],
		m["credentialRef"],
		m["sushyToolsVersion"],
	)
}

func cloneComponentVars(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
