package inventory

import (
	"net"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	stategraph "github.com/crmarques/bootwright/internal/state/graph"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

type ControllerNameResolutionTarget struct {
	MachineRef       string
	ProviderName     string
	Name             string
	EntryNames       []string
	ConsumerClusters []string
}

func ControllerNameResolutionTargets(state v1alpha1.State) []ControllerNameResolutionTarget {
	var out []ControllerNameResolutionTarget
	for _, service := range stategraph.ResolveMachineServices(state).Services {
		if service.Identity.Kind != v1alpha1.ComponentSlotNameResolution || service.MachineRef == "" {
			continue
		}
		out = append(out, ControllerNameResolutionTarget{
			MachineRef:       service.MachineRef,
			ProviderName:     service.Identity.ProviderName,
			Name:             service.Identity.Name,
			EntryNames:       serviceEntryNames(service),
			ConsumerClusters: service.ConsumerClusters(),
		})
	}
	return out
}

func ControllerNameResolutionTargetDesiredVars(state v1alpha1.State, target ControllerNameResolutionTarget) []any {
	for _, service := range stategraph.ResolveMachineServices(state).Services {
		if service.Identity.Kind != v1alpha1.ComponentSlotNameResolution ||
			service.MachineRef != target.MachineRef ||
			service.Identity.ProviderName != target.ProviderName ||
			service.Identity.Name != target.Name {
			continue
		}
		entry, ok := nameResolutionMachineServiceVars(state, service)
		if ok {
			return []any{controllerNameResolutionDesiredVars(entry)}
		}
	}
	return nil
}

func controllerNameResolutionServicesVars(state v1alpha1.State) []any {
	var out []any
	for _, service := range stategraph.ResolveMachineServices(state).Services {
		if service.Identity.Kind != v1alpha1.ComponentSlotNameResolution || service.MachineRef == "" {
			continue
		}
		entry, ok := nameResolutionMachineServiceVars(state, service)
		if ok {
			out = append(out, entry)
		}
	}
	return out
}

func controllerNameResolutionDesiredVars(entry map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range []string{
		"kind",
		"providerName",
		"name",
		"machineRef",
		"realisation",
		"applyRole",
		"controllerAddresses",
		"controllerDomains",
		"controllerProbes",
	} {
		if value, ok := entry[key]; ok {
			out[key] = value
		}
	}
	return out
}

func nameResolutionMachineServiceVars(state v1alpha1.State, service stategraph.MachineService) (map[string]any, bool) {
	component, ok := stateview.InfraComponent(state, service.Identity.Name)
	if !ok || component.Spec.NameResolution == nil {
		return nil, false
	}
	entry := v1alpha1.EnvironmentNameResolutionComponent{Name: serviceEntryName(service)}
	out := nameResolutionComponentVars(state, entry, component)
	out["additionalIngressHosts"] = append([]string(nil), service.MergedStringFields["additionalIngressHosts"]...)
	hostRecords, domainRecords, cnameRecords := nameResolutionRecordsForGraphService(state, service)
	if len(hostRecords) > 0 {
		out["hostRecords"] = hostRecords
	} else {
		delete(out, "hostRecords")
	}
	if len(domainRecords) > 0 {
		out["domainRecords"] = domainRecords
	} else {
		delete(out, "domainRecords")
	}
	if len(cnameRecords) > 0 {
		out["cnameRecords"] = cnameRecords
	} else {
		delete(out, "cnameRecords")
	}
	out["controllerAddresses"] = stringSliceAny(nameResolutionControllerAddresses(state, service))
	out["controllerDomains"] = stringSliceAny(nameResolutionControllerDomains(state, service, hostRecords, domainRecords, cnameRecords))
	out["controllerProbes"] = nameResolutionControllerProbes(hostRecords, domainRecords, cnameRecords)
	return out, true
}

func nameResolutionControllerAddresses(state v1alpha1.State, service stategraph.MachineService) []string {
	component, ok := stateview.InfraComponent(state, service.Identity.Name)
	if !ok || component.Spec.NameResolution == nil {
		return nil
	}
	dns := component.Spec.NameResolution
	seen := map[string]bool{}
	var out []string
	add := func(address string) bool {
		address = strings.TrimSpace(address)
		ip := net.ParseIP(address)
		if ip == nil || ip.IsUnspecified() || ip.IsLoopback() || ip.IsMulticast() || ip.IsLinkLocalUnicast() {
			return false
		}
		address = ip.String()
		if seen[address] {
			return true
		}
		seen[address] = true
		out = append(out, address)
		return true
	}
	env := stateview.Environment(state)
	if env != nil {
		for _, entryName := range serviceEntryNames(service) {
			entry, ok := stateview.NameResolutionEntry(env, entryName)
			if !ok || entry.Management != v1alpha1.EnvironmentComponentManaged || entry.ComponentRef.Name != service.Identity.Name {
				continue
			}
			endpointName := entry.EndpointRef.Name
			selectedEndpoint := endpointName != "" || len(dns.Endpoints) == 1
			for _, endpoint := range dns.Endpoints {
				if endpointName != "" && endpoint.Name != endpointName {
					continue
				}
				if endpointName == "" && len(dns.Endpoints) != 1 {
					continue
				}
				if address, found := stateview.NamedMachineAddress(state, dns.MachineRef.Name, endpoint.AddressRef.Name); found {
					add(address)
				}
			}
			if !selectedEndpoint {
				add(dns.BindAddress)
			}
		}
	}
	sort.Strings(out)
	return out
}

func nameResolutionControllerDomains(state v1alpha1.State, service stategraph.MachineService, recordGroups ...[]any) []string {
	seen := map[string]bool{}
	var out []string
	add := func(domain string) {
		domain = strings.TrimSuffix(strings.TrimSpace(domain), ".")
		if domain == "" || seen[domain] {
			return
		}
		seen[domain] = true
		out = append(out, domain)
	}
	env := stateview.Environment(state)
	if env != nil {
		add(env.Spec.Domains.MachinesDomain())
		for _, consumer := range service.Consumers {
			if stateHasContainerCluster(state, consumer.Cluster) {
				add(consumer.Cluster + "." + env.Spec.Domains.ContainerClustersDomain())
				continue
			}
			if stateHasStorageCluster(state, consumer.Cluster) {
				add(consumer.Cluster + "." + env.Spec.Domains.StorageClustersDomain())
			}
		}
	}
	mandatory := append([]string(nil), out...)
	for _, records := range recordGroups {
		for _, raw := range records {
			record, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			name, _ := record["name"].(string)
			if !nameResolutionDomainCovered(name, mandatory) {
				add(name)
			}
		}
	}
	sort.Strings(out)
	return out
}

func nameResolutionDomainCovered(name string, domains []string) bool {
	name = strings.TrimSuffix(strings.TrimSpace(name), ".")
	for _, domain := range domains {
		if name == domain || strings.HasSuffix(name, "."+domain) {
			return true
		}
	}
	return false
}

func stateHasContainerCluster(state v1alpha1.State, name string) bool {
	for _, cluster := range state.ContainerClusters {
		if cluster.Metadata.Name == name {
			return true
		}
	}
	return false
}

func stateHasStorageCluster(state v1alpha1.State, name string) bool {
	for _, cluster := range state.StorageClusters {
		if cluster.Metadata.Name == name {
			return true
		}
	}
	return false
}
