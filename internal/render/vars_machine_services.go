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
	return machineServicesVars(state, func(service stategraph.MachineService) bool {
		return service.IsProviderService()
	})
}

func infraComponentServicesVars(state v1alpha1.State) []any {
	return machineServicesVars(state, func(service stategraph.MachineService) bool {
		return service.IsInfraComponentService()
	})
}

// FabricHostDesiredVars returns the deterministic rendered fabric vars for one
// host: the provider services, infra-component services, and provider machine
// setups whose placement machineRef is host. state-check hashes this instead of
// the whole desired state for fabric (provider/infra-component) tasks, so an
// edit elsewhere in the fleet that does not change this host's rendered vars no
// longer reports spurious infrastructure drift — while a change that does affect
// the host (a cluster its load balancer fronts, a storage node it prepares) still
// does, because that change flows into these same vars.
func FabricHostDesiredVars(state v1alpha1.State, host string) []any {
	out := []any{}
	for _, group := range [][]any{
		providerServicesVars(state),
		infraComponentServicesVars(state),
		providerMachineSetupsVars(state),
	} {
		for _, entry := range group {
			if m, ok := entry.(map[string]any); ok && stringMapValue(m, "machineRef") == host {
				out = append(out, entry)
			}
		}
	}
	return out
}

func machineServicesVars(state v1alpha1.State, include func(stategraph.MachineService) bool) []any {
	builder := newMachineServiceBuilder()
	graph := stategraph.ResolveMachineServices(state)
	for _, service := range graph.Services {
		if !include(service) {
			continue
		}
		component, ok := machineServiceVarsFromGraph(state, service)
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

// machineServiceVarBuilders maps each service-graph identity kind to its vars
// builder. Every kind the support registry can produce must have an entry here,
// or its rendered vars are silently dropped; TestMachineServiceVarBuildersCoverRegistry
// enforces that against support.ServiceEntries().
var machineServiceVarBuilders = map[string]func(v1alpha1.State, stategraph.MachineService) (map[string]any, bool){
	v1alpha1.ComponentSlotLoadBalancer:   loadBalancerMachineServiceVars,
	v1alpha1.ComponentSlotArtifacts:      artifactMachineServiceVars,
	v1alpha1.ComponentSlotProxy:          proxyMachineServiceVars,
	v1alpha1.ComponentSlotNameResolution: nameResolutionMachineServiceVars,
	v1alpha1.ComponentSlotNTP:            ntpMachineServiceVars,
	v1alpha1.ComponentSlotRegistry:       registryMachineServiceVars,
	v1alpha1.ProviderServiceKindBMC:      bmcMachineServiceVarsFromGraph,
}

func machineServiceVarsFromGraph(state v1alpha1.State, service stategraph.MachineService) (map[string]any, bool) {
	build, ok := machineServiceVarBuilders[service.Identity.Kind]
	if !ok {
		return nil, false
	}
	return build(state, service)
}

func loadBalancerMachineServiceVars(state v1alpha1.State, service stategraph.MachineService) (map[string]any, bool) {
	component, ok := findInfraComponent(state, service.Identity.Name)
	if !ok || component.Spec.LoadBalancer == nil {
		return nil, false
	}
	out := loadBalancerComponentVars(state, component)
	var frontends []any
	for _, consumer := range service.Consumers {
		ci, ok := clusterInstallByName(state, consumer.ClusterInstall)
		if !ok {
			continue
		}
		frontends = append(frontends, loadBalancerFrontends(state, ci, component.Metadata.Name, consumer.Cluster, ci.Machines, clusterNodesForCI(state, ci))...)
	}
	if len(frontends) > 0 {
		out["frontends"] = frontends
	}
	return out, true
}

func artifactMachineServiceVars(state v1alpha1.State, service stategraph.MachineService) (map[string]any, bool) {
	for _, consumer := range service.Consumers {
		ci, ok := clusterInstallByName(state, consumer.ClusterInstall)
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

func proxyMachineServiceVars(state v1alpha1.State, service stategraph.MachineService) (map[string]any, bool) {
	component, ok := findInfraComponent(state, service.Identity.Name)
	if !ok || component.Spec.Proxy == nil {
		return nil, false
	}
	entry := v1alpha1.EnvironmentProxyComponent{Name: serviceEntryName(service)}
	return proxyComponentVars(state, entry, component), true
}

func nameResolutionMachineServiceVars(state v1alpha1.State, service stategraph.MachineService) (map[string]any, bool) {
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
		"machineRef":             dns.MachineRef.Name,
		"machineAddress":         lookupMachineAddress(state, dns.MachineRef.Name),
		"realisation":            v1alpha1.InfraComponentTypeDnsmasq,
		"image":                  managedDnsmasqImage(state),
	}
	if len(hostRecords) > 0 {
		out["hostRecords"] = hostRecords
	}
	if len(domainRecords) > 0 {
		out["domainRecords"] = domainRecords
	}
	if len(dns.Forwarders) > 0 {
		out["forwarders"] = stringSliceAny(dns.Forwarders)
	}
	applyServiceRoleContract(out, v1alpha1.ComponentSlotNameResolution, v1alpha1.InfraComponentTypeDnsmasq)
	return out, true
}

func ntpMachineServiceVars(state v1alpha1.State, service stategraph.MachineService) (map[string]any, bool) {
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

func ntpAllowedNetworksForGraphService(state v1alpha1.State, service stategraph.MachineService) []string {
	seen := map[string]bool{}
	var out []string
	for _, consumer := range service.Consumers {
		ci, ok := clusterInstallByName(state, consumer.ClusterInstall)
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

func registryMachineServiceVars(state v1alpha1.State, service stategraph.MachineService) (map[string]any, bool) {
	component, ok := findInfraComponent(state, service.Identity.Name)
	if !ok || component.Spec.Registry == nil {
		return nil, false
	}
	entry := v1alpha1.EnvironmentRegistryComponent{Name: serviceEntryName(service)}
	return registryComponentVars(state, entry, component), true
}

func clusterInstallByName(state v1alpha1.State, name string) (v1alpha1.ClusterInstall, bool) {
	for _, cluster := range state.ContainerClusters {
		if cluster.Metadata.Name == name {
			return stateview.ClusterInstallForContainerCluster(state, cluster)
		}
	}
	for _, cluster := range managedStorageClusters(state) {
		if cluster.Metadata.Name == name {
			return storageClusterInstall(state, cluster)
		}
	}
	return v1alpha1.ClusterInstall{}, false
}

func serviceEntryName(service stategraph.MachineService) string {
	for _, consumer := range service.Consumers {
		if entryName := consumer.Fields["entryName"]; entryName != "" {
			return entryName
		}
	}
	return ""
}

func serviceEntryNames(service stategraph.MachineService) []string {
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

func nameResolutionRecordsForGraphService(state v1alpha1.State, service stategraph.MachineService) ([]any, []any) {
	hostRecords := map[string]map[string]any{}
	domainRecords := map[string]map[string]any{}
	for _, entryName := range serviceEntryNames(service) {
		hosts, domains := nameResolutionRecordsVars(state, entryName, serviceEntryStringField(service, entryName, "additionalIngressHosts"))
		mergeRecordVars(hostRecords, hosts)
		mergeRecordVars(domainRecords, domains)
	}
	return sortedRecordVars(hostRecords), sortedRecordVars(domainRecords)
}

func serviceEntryStringField(service stategraph.MachineService, entryName, field string) []string {
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

func providerMachineSetupsVars(state v1alpha1.State) []any {
	type key struct {
		machine string
		role    string
	}
	seen := map[key]bool{}
	var keys []key
	addMachine := func(machine v1alpha1.InstallMachine) {
		machineRef := machineHostRef(state, machine)
		if machineRef == "" {
			return
		}
		driver := ProviderDriver(state, machine)
		for _, role := range driver.Roles.MachineSetupRoles {
			k := key{machine: machineRef, role: role}
			if seen[k] {
				continue
			}
			seen[k] = true
			keys = append(keys, k)
		}
	}
	for _, cluster := range state.ContainerClusters {
		ci, ok := stateview.ClusterInstallForContainerCluster(state, cluster)
		if !ok {
			continue
		}
		for _, machine := range ci.Machines {
			addMachine(machine)
		}
	}
	for _, cluster := range managedStorageClusters(state) {
		ci, ok := storageClusterInstall(state, cluster)
		if !ok {
			continue
		}
		for _, machine := range ci.Machines {
			rawMachine, ok := stateview.Machine(state, machine.Name)
			if !ok || !v1alpha1.MachineInstallsOS(rawMachine) {
				continue
			}
			addMachine(machine)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].machine != keys[j].machine {
			return keys[i].machine < keys[j].machine
		}
		return keys[i].role < keys[j].role
	})
	out := make([]any, 0, len(keys))
	for _, k := range keys {
		out = append(out, map[string]any{
			"machineRef":     k.machine,
			"machineAddress": lookupMachineAddress(state, k.machine),
			"applyRole":      k.role,
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
