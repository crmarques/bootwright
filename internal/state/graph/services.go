package stategraph

import (
	"fmt"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/artifacts"
	"github.com/crmarques/bootwright/internal/infra/support"
	"github.com/crmarques/bootwright/internal/state/view"
)

type MachineServiceIdentity struct {
	Kind         string
	ProviderName string
	Name         string
}

type MachineServiceConsumer struct {
	Cluster           string
	ClusterInstall    string
	Owner             string
	Fields            map[string]string
	MergeStringFields map[string][]string
}

type MachineService struct {
	Identity           MachineServiceIdentity
	MachineRef         string
	Fields             map[string]string
	Consumers          []MachineServiceConsumer
	MergedStringFields map[string][]string
}

type MachineServiceGraph struct {
	Services []MachineService
}

func (s MachineService) ConsumerClusters() []string {
	return serviceConsumerClusters(s.Consumers)
}

func (s MachineService) IsProviderService() bool {
	return s.Identity.Kind == v1alpha1.ProviderServiceKindBMC
}

func (s MachineService) IsInfraComponentService() bool {
	return !s.IsProviderService()
}

type SharedServiceGroup struct {
	Kind              string   `yaml:"kind" json:"kind"`
	ProviderName      string   `yaml:"providerName" json:"providerName"`
	CapabilityName    string   `yaml:"capabilityName" json:"capabilityName"`
	MachineRef        string   `yaml:"machineRef,omitempty" json:"machineRef,omitempty"`
	ConsumingClusters []string `yaml:"consumingClusters" json:"consumingClusters"`
}

type DestroyScopeConflict struct {
	Slot             string
	Provider         string
	Name             string
	ScopedClusters   []string
	UnscopedClusters []string
}

func ResolveMachineServices(state v1alpha1.State) MachineServiceGraph {
	entries := machineServiceConsumers(state)
	grouped := map[machineServiceGroupKey]*MachineService{}
	var order []machineServiceGroupKey
	for _, entry := range entries {
		id := MachineServiceIdentity{
			Kind:         entry.Fields["kind"],
			ProviderName: entry.Fields["providerName"],
			Name:         entry.Fields["name"],
		}
		if id.Kind == "" || id.ProviderName == "" || id.Name == "" {
			continue
		}
		key := machineServiceGroupingKey(id, entry.Fields["machineRef"])
		service, ok := grouped[key]
		if !ok {
			service = &MachineService{
				Identity:           id,
				MachineRef:         entry.Fields["machineRef"],
				Fields:             cloneStringMap(entry.Fields),
				MergedStringFields: map[string][]string{},
			}
			grouped[key] = service
			order = append(order, key)
		}
		service.Consumers = append(service.Consumers, entry)
		mergeServiceStringFields(service.MergedStringFields, entry.MergeStringFields)
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := order[i].Identity, order[j].Identity
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.ProviderName != b.ProviderName {
			return a.ProviderName < b.ProviderName
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return order[i].MachineRef < order[j].MachineRef
	})
	out := make([]MachineService, 0, len(order))
	for _, key := range order {
		service := grouped[key]
		sort.SliceStable(service.Consumers, func(i, j int) bool {
			if service.Consumers[i].Cluster != service.Consumers[j].Cluster {
				return service.Consumers[i].Cluster < service.Consumers[j].Cluster
			}
			return service.Consumers[i].Owner < service.Consumers[j].Owner
		})
		out = append(out, *service)
	}
	return MachineServiceGraph{Services: out}
}

type machineServiceGroupKey struct {
	Identity   MachineServiceIdentity
	MachineRef string
}

func machineServiceGroupingKey(id MachineServiceIdentity, machineRef string) machineServiceGroupKey {
	key := machineServiceGroupKey{Identity: id}
	if id.Kind == v1alpha1.ProviderServiceKindBMC {
		key.MachineRef = machineRef
	}
	return key
}

func (g MachineServiceGraph) ValidateSharedServices() []string {
	var errs []string
	for _, service := range g.Services {
		if len(service.Consumers) < 2 {
			continue
		}
		first := service.Consumers[0]
		fields := support.ServiceConflictFields(service.Identity.Kind, first.Fields["realisation"])
		for _, other := range service.Consumers[1:] {
			for _, field := range fields {
				want := first.Fields[field]
				got := other.Fields[field]
				if want == got {
					continue
				}
				errs = append(errs, fmt.Sprintf(
					"shared machine service %s %s/%s has conflicting %s: %s uses %q, %s uses %q",
					service.Identity.Kind,
					service.Identity.ProviderName,
					service.Identity.Name,
					field,
					first.Owner,
					want,
					other.Owner,
					got,
				))
			}
		}
	}
	return errs
}

func (g MachineServiceGraph) SharedServices() []SharedServiceGroup {
	out := make([]SharedServiceGroup, 0)
	for _, service := range g.Services {
		clusters := serviceConsumerClusters(service.Consumers)
		if len(clusters) < 2 {
			continue
		}
		out = append(out, SharedServiceGroup{
			Kind:              service.Identity.Kind,
			ProviderName:      service.Identity.ProviderName,
			CapabilityName:    service.Identity.Name,
			MachineRef:        service.MachineRef,
			ConsumingClusters: clusters,
		})
	}
	return out
}

func (g MachineServiceGraph) ScopeConflicts(selected []string) []DestroyScopeConflict {
	selectedSet := map[string]bool{}
	for _, name := range selected {
		selectedSet[name] = true
	}
	var conflicts []DestroyScopeConflict
	for _, service := range g.Services {
		var scoped, unscoped []string
		for _, cluster := range serviceConsumerClusters(service.Consumers) {
			if selectedSet[cluster] {
				scoped = append(scoped, cluster)
			} else {
				unscoped = append(unscoped, cluster)
			}
		}
		if len(scoped) == 0 || len(unscoped) == 0 {
			continue
		}
		conflicts = append(conflicts, DestroyScopeConflict{
			Slot:             service.Identity.Kind,
			Provider:         service.Identity.ProviderName,
			Name:             service.Identity.Name,
			ScopedClusters:   scoped,
			UnscopedClusters: unscoped,
		})
	}
	sort.SliceStable(conflicts, func(i, j int) bool {
		if conflicts[i].Slot != conflicts[j].Slot {
			return conflicts[i].Slot < conflicts[j].Slot
		}
		if conflicts[i].Provider != conflicts[j].Provider {
			return conflicts[i].Provider < conflicts[j].Provider
		}
		return conflicts[i].Name < conflicts[j].Name
	})
	return conflicts
}

func (g MachineServiceGraph) MergedStringField(id MachineServiceIdentity, field string) []string {
	for _, service := range g.Services {
		if service.Identity != id {
			continue
		}
		return append([]string(nil), service.MergedStringFields[field]...)
	}
	return nil
}

func SharedDestroyConflicts(state v1alpha1.State, selected []string) []DestroyScopeConflict {
	return ResolveMachineServices(state).ScopeConflicts(selected)
}

func machineServiceConsumers(state v1alpha1.State) []MachineServiceConsumer {
	var out []MachineServiceConsumer
	for _, cluster := range state.ContainerClusters {
		infra, _ := stateview.ClusterInstallForContainerCluster(state, cluster)
		out = append(out, clusterMachineServiceConsumers(state, infra, cluster)...)
	}
	out = append(out, storageMachineServiceConsumers(state)...)
	return out
}

func clusterMachineServiceConsumers(state v1alpha1.State, infra v1alpha1.ClusterInstall, cluster v1alpha1.ContainerCluster) []MachineServiceConsumer {
	var out []MachineServiceConsumer
	out = append(out, loadBalancerConsumers(state, infra, cluster)...)
	out = append(out, bmcConsumers(state, infra, cluster)...)
	out = append(out, selectedManagedProxyConsumers(state, infra, cluster)...)
	out = append(out, networkNameResolutionConsumers(state, infra, cluster)...)
	out = append(out, selectedManagedNTPConsumers(state, infra, cluster)...)
	out = append(out, selectedManagedRegistryConsumers(state, infra, cluster)...)
	if artifacts.ClusterNeedsPublication(state, infra, cluster) {
		if server, ok := artifacts.Select(state, infra); ok && server.Config != nil {
			out = append(out, newServiceConsumer(
				cluster.Metadata.Name,
				infra.Metadata.Name,
				fmt.Sprintf("Environment artifactServer InfraComponent/%s", server.Component.Metadata.Name),
				v1alpha1.ComponentSlotArtifactServer,
				v1alpha1.KindInfraComponent,
				server.Component.Metadata.Name,
				v1alpha1.ArtifactServerProtocolHTTP,
				map[string]string{
					"machineRef":  server.Config.MachineRef.Name,
					"bindAddress": server.Config.BindAddress,
					"listeners":   artifactListenersKey(server.Config.Listeners),
					"endpoints":   artifactEndpointsKey(server.Config.Endpoints),
				},
				nil,
			))
		}
	}
	return out
}

func bmcConsumers(state v1alpha1.State, infra v1alpha1.ClusterInstall, cluster v1alpha1.ContainerCluster) []MachineServiceConsumer {
	var out []MachineServiceConsumer
	for _, machine := range infra.Machines {
		consumer, ok := bmcConsumerForMachine(
			state,
			cluster.Metadata.Name,
			infra.Metadata.Name,
			fmt.Sprintf("ClusterInstall/%s nodes[%s] provider %s", infra.Metadata.Name, machine.Name, machine.Source.ProviderRef.Name),
			machine,
		)
		if ok {
			out = append(out, consumer)
		}
	}
	return out
}

func storageMachineServiceConsumers(state v1alpha1.State) []MachineServiceConsumer {
	var out []MachineServiceConsumer
	for _, cluster := range state.StorageClusters {
		if cluster.Spec.Ceph == nil || (cluster.Spec.Management != "" && cluster.Spec.Management != v1alpha1.StorageClusterManagementManaged) {
			continue
		}
		out = append(out, nameResolutionConsumers(state, cluster.Metadata.Name, cluster.Metadata.Name, storageNetworkDNSRefs(state, cluster))...)
		for _, node := range cluster.Spec.Ceph.Topology.Nodes {
			machineObj, ok := stateview.Machine(state, node.MachineRef.Name)
			if !ok || !v1alpha1.MachineInstallsOS(machineObj) {
				continue
			}
			machine := stateview.InstallMachineFromMachine(machineObj)
			consumer, ok := bmcConsumerForMachine(
				state,
				cluster.Metadata.Name,
				cluster.Metadata.Name,
				fmt.Sprintf("StorageCluster/%s nodes[%s] provider %s", cluster.Metadata.Name, machine.Name, machine.Source.ProviderRef.Name),
				machine,
			)
			if ok {
				out = append(out, consumer)
			}
		}
	}
	return out
}

func bmcConsumerForMachine(state v1alpha1.State, clusterName, clusterInstallName, owner string, machine v1alpha1.InstallMachine) (MachineServiceConsumer, bool) {
	driver := machineDriver(state, machine)
	if driver.Dispatch.BMCRole == "none" || driver.Roles.BMCApplyRole == "" {
		return MachineServiceConsumer{}, false
	}
	machineRef := serviceMachineRef(state, machine)
	if machineRef == "" {
		return MachineServiceConsumer{}, false
	}
	configKey := machineBMCConfigKey(state, machine)
	if configKey == "" {
		return MachineServiceConsumer{}, false
	}
	return newServiceConsumer(
		clusterName,
		clusterInstallName,
		owner,
		v1alpha1.ProviderServiceKindBMC,
		machine.Source.ProviderRef.Name,
		driver.Dispatch.BMCRole,
		driver.Dispatch.BMCRole,
		map[string]string{
			"machineRef":  machineRef,
			"bmcRole":     driver.Dispatch.BMCRole,
			"configKey":   configKey,
			"machineName": machine.Name,
		},
		nil,
	), true
}

func loadBalancerConsumers(state v1alpha1.State, infra v1alpha1.ClusterInstall, cluster v1alpha1.ContainerCluster) []MachineServiceConsumer {
	seen := map[string]bool{}
	var out []MachineServiceConsumer
	for _, endpointName := range []string{v1alpha1.EndpointAPI, v1alpha1.EndpointAPIInt, v1alpha1.EndpointIngress} {
		endpoint, ok := infra.Endpoints[endpointName]
		if !ok || endpoint.Source.Type != v1alpha1.EndpointSourceInfraComponent || endpoint.Source.ComponentRef.Name == "" {
			continue
		}
		name := endpoint.Source.ComponentRef.Name
		if seen[name] {
			continue
		}
		seen[name] = true
		component, ok := stateview.InfraComponent(state, name)
		if !ok || component.Spec.LoadBalancer == nil {
			continue
		}
		lb := component.Spec.LoadBalancer
		out = append(out, newServiceConsumer(
			cluster.Metadata.Name,
			infra.Metadata.Name,
			fmt.Sprintf("ClusterInstall/%s endpoint source InfraComponent/%s", infra.Metadata.Name, component.Metadata.Name),
			v1alpha1.ComponentSlotLoadBalancer,
			v1alpha1.KindInfraComponent,
			component.Metadata.Name,
			v1alpha1.InfraComponentTypeHAProxy,
			map[string]string{"machineRef": lb.MachineRef.Name, "capabilityName": component.Metadata.Name},
			nil,
		))
	}
	return out
}

func machineBoundConsumerFields(arm v1alpha1.MachineBoundComponent, entryName, kind, realisation string) map[string]string {
	return map[string]string{
		"machineRef":  arm.MachineRefName(),
		"entryName":   entryName,
		"bindAddress": arm.ServiceBindAddress(),
		"port":        fmt.Sprint(servicePort(kind, realisation, arm.ServicePort())),
	}
}

func selectedManagedProxyConsumers(state v1alpha1.State, infra v1alpha1.ClusterInstall, cluster v1alpha1.ContainerCluster) []MachineServiceConsumer {
	env := stateview.Environment(state)
	if env == nil {
		return nil
	}
	entries := map[string]v1alpha1.EnvironmentProxyComponent{}
	for _, name := range []string{env.Spec.ProxyFor.Bootwright, env.Spec.ProxyFor.ContainerClusterInstall} {
		for _, entry := range env.Spec.InfraComponents.Proxies {
			if entry.Name == name && entry.Type == v1alpha1.EnvironmentComponentManaged {
				entries[entry.ComponentRef.Name] = entry
			}
		}
	}
	var out []MachineServiceConsumer
	for componentName, entry := range entries {
		component, ok := stateview.InfraComponent(state, componentName)
		if !ok || component.Spec.Proxy == nil {
			continue
		}
		proxy := component.Spec.Proxy
		out = append(out, newServiceConsumer(
			cluster.Metadata.Name,
			infra.Metadata.Name,
			fmt.Sprintf("Environment/%s infraComponents.proxies[%s]", env.Metadata.Name, entry.Name),
			v1alpha1.ComponentSlotProxy,
			v1alpha1.KindInfraComponent,
			component.Metadata.Name,
			v1alpha1.InfraComponentTypeSquid,
			machineBoundConsumerFields(proxy, entry.Name, v1alpha1.ComponentSlotProxy, v1alpha1.InfraComponentTypeSquid),
			nil,
		))
	}
	return out
}

func networkNameResolutionConsumers(state v1alpha1.State, infra v1alpha1.ClusterInstall, cluster v1alpha1.ContainerCluster) []MachineServiceConsumer {
	return nameResolutionConsumers(state, cluster.Metadata.Name, infra.Metadata.Name, networkDNSRefs(state, infra))
}

func nameResolutionConsumers(state v1alpha1.State, clusterName, clusterInstallName string, refs map[string]bool) []MachineServiceConsumer {
	env := stateview.Environment(state)
	if env == nil {
		return nil
	}
	var out []MachineServiceConsumer
	for _, entry := range env.Spec.InfraComponents.NameResolution {
		if !refs[entry.Name] || entry.Type != v1alpha1.EnvironmentComponentManaged {
			continue
		}
		component, ok := stateview.InfraComponent(state, entry.ComponentRef.Name)
		if !ok || component.Spec.NameResolution == nil {
			continue
		}
		dns := component.Spec.NameResolution
		mergedHosts := append([]string(nil), dns.AdditionalIngressHosts...)
		mergedHosts = append(mergedHosts, entry.AdditionalIngressHosts...)
		out = append(out, newServiceConsumer(
			clusterName,
			clusterInstallName,
			fmt.Sprintf("NetworkConfig dnsRefs[%s]", entry.Name),
			v1alpha1.ComponentSlotNameResolution,
			v1alpha1.KindInfraComponent,
			component.Metadata.Name,
			v1alpha1.InfraComponentTypeDnsmasq,
			machineBoundConsumerFields(dns, entry.Name, v1alpha1.ComponentSlotNameResolution, v1alpha1.InfraComponentTypeDnsmasq),
			map[string][]string{"additionalIngressHosts": mergedHosts},
		))
	}
	return out
}

func storageNetworkDNSRefs(state v1alpha1.State, cluster v1alpha1.StorageCluster) map[string]bool {
	out := map[string]bool{}
	if cluster.Spec.Ceph == nil {
		return out
	}
	for _, node := range cluster.Spec.Ceph.Topology.Nodes {
		machine, ok := stateview.Machine(state, node.MachineRef.Name)
		if !ok {
			continue
		}
		network, ok := stateview.NetworkConfig(state, machine.Spec.Network.Config.NetworkConfigRef.Name)
		if !ok {
			continue
		}
		for _, ref := range network.Spec.DNSRefs {
			out[ref] = true
		}
	}
	return out
}

func selectedManagedNTPConsumers(state v1alpha1.State, infra v1alpha1.ClusterInstall, cluster v1alpha1.ContainerCluster) []MachineServiceConsumer {
	env := stateview.Environment(state)
	if env == nil {
		return nil
	}
	entries := map[string]v1alpha1.EnvironmentNTPSourceComponent{}
	for _, entry := range env.Spec.InfraComponents.NTPSources {
		if entry.Type == v1alpha1.EnvironmentComponentManaged {
			entries[entry.ComponentRef.Name] = entry
		}
	}
	var out []MachineServiceConsumer
	for componentName, entry := range entries {
		component, ok := stateview.InfraComponent(state, componentName)
		if !ok || component.Spec.NTP == nil {
			continue
		}
		ntp := component.Spec.NTP
		out = append(out, newServiceConsumer(
			cluster.Metadata.Name,
			infra.Metadata.Name,
			fmt.Sprintf("Environment/%s infraComponents.ntpSources[%s]", env.Metadata.Name, entry.Name),
			v1alpha1.ComponentSlotNTP,
			v1alpha1.KindInfraComponent,
			component.Metadata.Name,
			v1alpha1.InfraComponentTypeChrony,
			machineBoundConsumerFields(ntp, entry.Name, v1alpha1.ComponentSlotNTP, v1alpha1.InfraComponentTypeChrony),
			nil,
		))
	}
	return out
}

func selectedManagedRegistryConsumers(state v1alpha1.State, infra v1alpha1.ClusterInstall, cluster v1alpha1.ContainerCluster) []MachineServiceConsumer {
	if v1alpha1.InstallMode(cluster) != v1alpha1.InstallModeDisconnected {
		return nil
	}
	env := stateview.Environment(state)
	if env == nil {
		return nil
	}
	entry, ok := selectedRegistry(env.Spec.InfraComponents.Registries)
	if !ok || entry.Type != v1alpha1.EnvironmentComponentManaged {
		return nil
	}
	component, ok := stateview.InfraComponent(state, entry.ComponentRef.Name)
	if !ok || component.Spec.Registry == nil {
		return nil
	}
	registry := component.Spec.Registry
	return []MachineServiceConsumer{newServiceConsumer(
		cluster.Metadata.Name,
		infra.Metadata.Name,
		fmt.Sprintf("Environment/%s infraComponents.registries[%s]", env.Metadata.Name, entry.Name),
		v1alpha1.ComponentSlotRegistry,
		v1alpha1.KindInfraComponent,
		component.Metadata.Name,
		v1alpha1.InfraComponentTypeMirrorRegistry,
		machineBoundConsumerFields(registry, entry.Name, v1alpha1.ComponentSlotRegistry, v1alpha1.InfraComponentTypeMirrorRegistry),
		nil,
	)}
}

func selectedRegistry(entries []v1alpha1.EnvironmentRegistryComponent) (v1alpha1.EnvironmentRegistryComponent, bool) {
	for _, entry := range entries {
		if entry.Default {
			return entry, true
		}
	}
	if len(entries) == 1 {
		return entries[0], true
	}
	return v1alpha1.EnvironmentRegistryComponent{}, false
}

func servicePort(kind, realisation string, configured int) int {
	if configured != 0 {
		return configured
	}
	return support.LookupService(kind, realisation).DefaultPort
}

func networkDNSRefs(state v1alpha1.State, infra v1alpha1.ClusterInstall) map[string]bool {
	out := map[string]bool{}
	for _, name := range stateview.ClusterConsumedNetworkConfigs(infra) {
		network, ok := stateview.NetworkConfig(state, name)
		if !ok {
			continue
		}
		for _, ref := range network.Spec.DNSRefs {
			out[ref] = true
		}
	}
	return out
}

func artifactListenersKey(listeners []v1alpha1.ArtifactServerListener) string {
	parts := make([]string, 0, len(listeners))
	for _, listener := range listeners {
		parts = append(parts, fmt.Sprintf("%s/%s/%d", listener.Name, listener.Protocol, listener.Port))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func artifactEndpointsKey(endpoints []v1alpha1.ArtifactServerEndpoint) string {
	parts := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		parts = append(parts, fmt.Sprintf("%s/%s/%s", endpoint.Name, endpoint.Listener, endpoint.MachineAddress))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func newServiceConsumer(cluster, infra, owner, kind, provider, name, realisation string, fields map[string]string, merge map[string][]string) MachineServiceConsumer {
	driver := support.LookupService(kind, realisation)
	out := cloneStringMap(fields)
	out["kind"] = kind
	out["providerName"] = provider
	out["name"] = name
	out["realisation"] = realisation
	out["applyRole"] = driver.ApplyRole
	out["destroyRole"] = driver.DestroyRole
	return MachineServiceConsumer{
		Cluster:           cluster,
		ClusterInstall:    infra,
		Owner:             owner,
		Fields:            out,
		MergeStringFields: supportedMergeStringFields(kind, realisation, merge),
	}
}

func supportedMergeStringFields(kind, realisation string, merge map[string][]string) map[string][]string {
	allowed := map[string]bool{}
	for _, field := range support.ServiceMergeStringFields(kind, realisation) {
		allowed[field] = true
	}
	out := map[string][]string{}
	for field, values := range merge {
		if allowed[field] {
			out[field] = append([]string(nil), values...)
		}
	}
	return out
}

func machineDriver(state v1alpha1.State, machine v1alpha1.InstallMachine) support.DispatchSupport {
	provider, ok := stateview.Provider(state, machine.Source.ProviderRef.Name)
	if !ok {
		return support.LookupDispatch("none", "none", "none")
	}
	if machine.Source.ProfileRef.Name != "" {
		if _, ok := stateview.MachineProfile(provider, machine.Source.ProfileRef.Name); !ok {
			return support.LookupDispatch("none", "none", "none")
		}
		return support.LookupProfileProvisioner(provider.Spec.Type)
	}
	if provider.Spec.Type == v1alpha1.ProvisionerBareMetal && machine.Source.MachineRef.Name != "" {
		if _, ok := stateview.Machine(state, machine.Source.MachineRef.Name); ok {
			return support.LookupMachineProvisioner(provider.Spec.Type)
		}
	}
	return support.LookupDispatch("none", "none", "none")
}

func serviceMachineRef(state v1alpha1.State, machine v1alpha1.InstallMachine) string {
	if machine.Source.ProfileRef.Name == "" {
		return ""
	}
	provider, ok := stateview.Provider(state, machine.Source.ProviderRef.Name)
	if !ok {
		return ""
	}
	if provider.Spec.Type != v1alpha1.ProvisionerLibvirt || provider.Spec.Libvirt == nil {
		return ""
	}
	return provider.Spec.Libvirt.MachineRef.Name
}

func machineBMCConfigKey(state v1alpha1.State, machine v1alpha1.InstallMachine) string {
	if machine.Source.ProfileRef.Name == "" {
		return ""
	}
	provider, ok := stateview.Provider(state, machine.Source.ProviderRef.Name)
	if !ok {
		return ""
	}
	if provider.Spec.Type != v1alpha1.ProvisionerLibvirt || provider.Spec.Libvirt == nil {
		return ""
	}
	libvirt := provider.Spec.Libvirt
	protocol := v1alpha1.DefaultBMCProtocol
	bindAddress := v1alpha1.DefaultBMCBindAddress
	port := v1alpha1.DefaultBMCEmulationStartPort
	vmediaPort := port + 1
	credentialsRef := ""
	if d := libvirt.BMCEmulationDefaults; d != nil {
		if d.Protocol != "" {
			protocol = d.Protocol
		}
		if d.BindAddress != "" {
			bindAddress = d.BindAddress
		}
		if d.Port != 0 {
			port = d.Port
		}
		if d.VMediaPort != 0 {
			vmediaPort = d.VMediaPort
		} else {
			vmediaPort = port + 1
		}
		if d.Auth != nil {
			credentialsRef = d.Auth.CredentialsRef.Name
		}
	}
	return fmt.Sprintf("%s|%s|%s|%d|%d|%s", protocol, libvirt.URI, bindAddress, port, vmediaPort, credentialsRef)
}

func serviceConsumerClusters(consumers []MachineServiceConsumer) []string {
	seen := map[string]bool{}
	var out []string
	for _, consumer := range consumers {
		if consumer.Cluster == "" || seen[consumer.Cluster] {
			continue
		}
		seen[consumer.Cluster] = true
		out = append(out, consumer.Cluster)
	}
	sort.Strings(out)
	return out
}

func mergeServiceStringFields(dst map[string][]string, src map[string][]string) {
	for field, values := range src {
		seen := map[string]bool{}
		out := append([]string(nil), dst[field]...)
		for _, value := range out {
			seen[value] = true
		}
		for _, value := range values {
			if value == "" || seen[value] {
				continue
			}
			seen[value] = true
			out = append(out, value)
		}
		sort.Strings(out)
		if len(out) > 0 {
			dst[field] = out
		}
	}
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
