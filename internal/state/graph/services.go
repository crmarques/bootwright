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

type ProviderServiceIdentity struct {
	Kind         string
	ProviderName string
	Name         string
}

type ProviderServiceConsumer struct {
	Cluster           string
	ClusterInfra      string
	Owner             string
	Fields            map[string]string
	MergeStringFields map[string][]string
}

type ProviderService struct {
	Identity           ProviderServiceIdentity
	HostRef            string
	Fields             map[string]string
	Consumers          []ProviderServiceConsumer
	MergedStringFields map[string][]string
}

type ProviderServiceGraph struct {
	Services []ProviderService
}

func (s ProviderService) ConsumerClusters() []string {
	return serviceConsumerClusters(s.Consumers)
}

type SharedServiceGroup struct {
	Kind              string   `yaml:"kind" json:"kind"`
	ProviderName      string   `yaml:"providerName" json:"providerName"`
	CapabilityName    string   `yaml:"capabilityName" json:"capabilityName"`
	HostRef           string   `yaml:"hostRef,omitempty" json:"hostRef,omitempty"`
	ConsumingClusters []string `yaml:"consumingClusters" json:"consumingClusters"`
}

type DestroyScopeConflict struct {
	Slot             string
	Provider         string
	Name             string
	ScopedClusters   []string
	UnscopedClusters []string
}

func ResolveProviderServices(state v1alpha1.State) ProviderServiceGraph {
	entries := providerServiceConsumers(state)
	grouped := map[providerServiceGroupKey]*ProviderService{}
	var order []providerServiceGroupKey
	for _, entry := range entries {
		id := ProviderServiceIdentity{
			Kind:         entry.Fields["kind"],
			ProviderName: entry.Fields["providerName"],
			Name:         entry.Fields["name"],
		}
		if id.Kind == "" || id.ProviderName == "" || id.Name == "" {
			continue
		}
		key := providerServiceGroupingKey(id, entry.Fields["hostRef"])
		service, ok := grouped[key]
		if !ok {
			service = &ProviderService{
				Identity:           id,
				HostRef:            entry.Fields["hostRef"],
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
		return order[i].HostRef < order[j].HostRef
	})
	out := make([]ProviderService, 0, len(order))
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
	return ProviderServiceGraph{Services: out}
}

type providerServiceGroupKey struct {
	Identity ProviderServiceIdentity
	HostRef  string
}

func providerServiceGroupingKey(id ProviderServiceIdentity, hostRef string) providerServiceGroupKey {
	key := providerServiceGroupKey{Identity: id}
	if id.Kind == v1alpha1.ProviderServiceKindBMC {
		key.HostRef = hostRef
	}
	return key
}

func (g ProviderServiceGraph) ValidateSharedServices() []string {
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
					"shared infra component service %s %s/%s has conflicting %s: %s uses %q, %s uses %q",
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

func (g ProviderServiceGraph) SharedServices() []SharedServiceGroup {
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
			HostRef:           service.HostRef,
			ConsumingClusters: clusters,
		})
	}
	return out
}

func (g ProviderServiceGraph) ScopeConflicts(selected []string) []DestroyScopeConflict {
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

func (g ProviderServiceGraph) MergedStringField(id ProviderServiceIdentity, field string) []string {
	for _, service := range g.Services {
		if service.Identity != id {
			continue
		}
		return append([]string(nil), service.MergedStringFields[field]...)
	}
	return nil
}

func SharedDestroyConflicts(state v1alpha1.State, selected []string) []DestroyScopeConflict {
	return ResolveProviderServices(state).ScopeConflicts(selected)
}

func providerServiceConsumers(state v1alpha1.State) []ProviderServiceConsumer {
	infraToClusters := clustersByInfra(state)
	var out []ProviderServiceConsumer
	for _, infra := range state.ClusterInfras {
		for _, cluster := range infraToClusters[infra.Metadata.Name] {
			out = append(out, infraProviderServiceConsumers(state, infra, cluster)...)
		}
	}
	return out
}

func infraProviderServiceConsumers(state v1alpha1.State, infra v1alpha1.ClusterInfra, cluster v1alpha1.ContainerCluster) []ProviderServiceConsumer {
	var out []ProviderServiceConsumer
	out = append(out, loadBalancerConsumers(state, infra, cluster)...)
	out = append(out, bmcConsumers(state, infra, cluster)...)
	out = append(out, selectedManagedProxyConsumers(state, infra, cluster)...)
	out = append(out, networkNameResolutionConsumers(state, infra, cluster)...)
	out = append(out, selectedManagedRegistryConsumers(state, infra, cluster)...)
	if artifacts.ClusterNeedsPublication(state, infra, cluster) {
		if server, ok := artifacts.Select(state); ok && server.Config != nil {
			out = append(out, newServiceConsumer(
				cluster.Metadata.Name,
				infra.Metadata.Name,
				fmt.Sprintf("Environment artifactServer InfraComponent/%s", server.Component.Metadata.Name),
				v1alpha1.ComponentSlotArtifacts,
				v1alpha1.KindInfraComponent,
				server.Component.Metadata.Name,
				v1alpha1.ArtifactServerProtocolHTTP,
				map[string]string{
					"hostRef":     server.Config.HostRef.Name,
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

func bmcConsumers(state v1alpha1.State, infra v1alpha1.ClusterInfra, cluster v1alpha1.ContainerCluster) []ProviderServiceConsumer {
	var out []ProviderServiceConsumer
	for _, machine := range infra.Spec.Components.Machines {
		driver := machineDriver(state, machine)
		if driver.Dispatch.BMCRole == "none" || driver.Roles.BMCApplyRole == "" {
			continue
		}
		hostRef := machineHostRef(state, machine)
		if hostRef == "" {
			continue
		}
		configKey := machineBMCConfigKey(state, machine)
		if configKey == "" {
			continue
		}
		out = append(out, newServiceConsumer(
			cluster.Metadata.Name,
			infra.Metadata.Name,
			fmt.Sprintf("ClusterInfra/%s machines[%s] provider %s", infra.Metadata.Name, machine.Name, machine.From.Provider),
			v1alpha1.ProviderServiceKindBMC,
			machine.From.Provider,
			driver.Dispatch.BMCRole,
			driver.Dispatch.BMCRole,
			map[string]string{
				"hostRef":     hostRef,
				"bmcRole":     driver.Dispatch.BMCRole,
				"configKey":   configKey,
				"machineName": machine.Name,
			},
			nil,
		))
	}
	return out
}

func loadBalancerConsumers(state v1alpha1.State, infra v1alpha1.ClusterInfra, cluster v1alpha1.ContainerCluster) []ProviderServiceConsumer {
	seen := map[string]bool{}
	var out []ProviderServiceConsumer
	for _, ref := range []v1alpha1.EndpointRef{
		cluster.Spec.Install.EndpointRefs.API,
		cluster.Spec.Install.EndpointRefs.APIInt,
		cluster.Spec.Install.EndpointRefs.Ingress,
	} {
		endpoint, ok := infra.Spec.Endpoints[ref.Name]
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
			fmt.Sprintf("ClusterInfra/%s endpoint source InfraComponent/%s", infra.Metadata.Name, component.Metadata.Name),
			v1alpha1.ComponentSlotLoadBalancer,
			v1alpha1.KindInfraComponent,
			component.Metadata.Name,
			v1alpha1.InfraComponentTypeHAProxy,
			map[string]string{"hostRef": lb.HostRef.Name, "capabilityName": component.Metadata.Name},
			nil,
		))
	}
	return out
}

func selectedManagedProxyConsumers(state v1alpha1.State, infra v1alpha1.ClusterInfra, cluster v1alpha1.ContainerCluster) []ProviderServiceConsumer {
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
	var out []ProviderServiceConsumer
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
			map[string]string{"hostRef": proxy.HostRef.Name, "entryName": entry.Name, "bindAddress": proxy.BindAddress, "port": fmt.Sprint(servicePort(v1alpha1.ComponentSlotProxy, v1alpha1.InfraComponentTypeSquid, proxy.Port))},
			nil,
		))
	}
	return out
}

func networkNameResolutionConsumers(state v1alpha1.State, infra v1alpha1.ClusterInfra, cluster v1alpha1.ContainerCluster) []ProviderServiceConsumer {
	env := stateview.Environment(state)
	if env == nil {
		return nil
	}
	refs := networkDNSRefs(state, infra)
	var out []ProviderServiceConsumer
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
			cluster.Metadata.Name,
			infra.Metadata.Name,
			fmt.Sprintf("NetworkConfig dnsRefs[%s]", entry.Name),
			v1alpha1.ComponentSlotNameResolution,
			v1alpha1.KindInfraComponent,
			component.Metadata.Name,
			v1alpha1.InfraComponentTypeDnsmasq,
			map[string]string{"hostRef": dns.HostRef.Name, "entryName": entry.Name, "bindAddress": dns.BindAddress, "port": fmt.Sprint(servicePort(v1alpha1.ComponentSlotNameResolution, v1alpha1.InfraComponentTypeDnsmasq, dns.Port))},
			map[string][]string{"additionalIngressHosts": mergedHosts},
		))
	}
	return out
}

func selectedManagedRegistryConsumers(state v1alpha1.State, infra v1alpha1.ClusterInfra, cluster v1alpha1.ContainerCluster) []ProviderServiceConsumer {
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
	return []ProviderServiceConsumer{newServiceConsumer(
		cluster.Metadata.Name,
		infra.Metadata.Name,
		fmt.Sprintf("Environment/%s infraComponents.registries[%s]", env.Metadata.Name, entry.Name),
		v1alpha1.ComponentSlotRegistry,
		v1alpha1.KindInfraComponent,
		component.Metadata.Name,
		v1alpha1.InfraComponentTypeMirrorRegistry,
		map[string]string{"hostRef": registry.HostRef.Name, "entryName": entry.Name, "bindAddress": registry.BindAddress, "port": fmt.Sprint(servicePort(v1alpha1.ComponentSlotRegistry, v1alpha1.InfraComponentTypeMirrorRegistry, registry.Port))},
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

func networkDNSRefs(state v1alpha1.State, infra v1alpha1.ClusterInfra) map[string]bool {
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
		parts = append(parts, fmt.Sprintf("%s/%s/%s", endpoint.Name, endpoint.Listener, endpoint.HostAddress))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func newServiceConsumer(cluster, infra, owner, kind, provider, name, realisation string, fields map[string]string, merge map[string][]string) ProviderServiceConsumer {
	driver := support.LookupService(kind, realisation)
	out := cloneStringMap(fields)
	out["kind"] = kind
	out["providerName"] = provider
	out["name"] = name
	out["realisation"] = realisation
	out["applyRole"] = driver.ApplyRole
	out["destroyRole"] = driver.DestroyRole
	return ProviderServiceConsumer{
		Cluster:           cluster,
		ClusterInfra:      infra,
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

func clustersByInfra(state v1alpha1.State) map[string][]v1alpha1.ContainerCluster {
	out := map[string][]v1alpha1.ContainerCluster{}
	for _, cluster := range state.ContainerClusters {
		for _, name := range stateview.ClusterInfraNames(cluster) {
			out[name] = append(out[name], cluster)
		}
	}
	return out
}

func machineDriver(state v1alpha1.State, machine v1alpha1.ClusterMachineComponent) support.DispatchSupport {
	provider, ok := stateview.Provider(state, machine.From.Provider)
	if !ok {
		return support.LookupDispatch("none", "none", "none")
	}
	if machine.From.Profile != "" {
		profile, ok := stateview.MachineProfile(provider, machine.From.Profile)
		if !ok {
			return support.LookupDispatch("none", "none", "none")
		}
		return support.LookupProfileProvisioner(v1alpha1.ProfileProvisionerKind(profile))
	}
	if machine.From.Name != "" {
		server, ok := stateview.Machine(provider, machine.From.Name)
		if !ok {
			return support.LookupDispatch("none", "none", "none")
		}
		return support.LookupMachineProvisioner(v1alpha1.MachineProvisionerKind(server))
	}
	return support.LookupDispatch("none", "none", "none")
}

func machineHostRef(state v1alpha1.State, machine v1alpha1.ClusterMachineComponent) string {
	if machine.From.Profile == "" {
		return ""
	}
	provider, ok := stateview.Provider(state, machine.From.Provider)
	if !ok {
		return ""
	}
	profile, ok := stateview.MachineProfile(provider, machine.From.Profile)
	if !ok || profile.Libvirt == nil {
		return ""
	}
	return profile.Libvirt.HostRef.Name
}

func machineBMCConfigKey(state v1alpha1.State, machine v1alpha1.ClusterMachineComponent) string {
	if machine.From.Profile == "" {
		return ""
	}
	provider, ok := stateview.Provider(state, machine.From.Provider)
	if !ok {
		return ""
	}
	profile, ok := stateview.MachineProfile(provider, machine.From.Profile)
	if !ok || profile.Libvirt == nil {
		return ""
	}
	libvirt := profile.Libvirt
	protocol := v1alpha1.DefaultBMCProtocol
	bindAddress := v1alpha1.DefaultBMCBindAddress
	port := v1alpha1.DefaultBMCEmulationStartPort
	vmediaPort := port + 1
	credentialRef := ""
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
			credentialRef = d.Auth.CredentialRef.Name
		}
	}
	return fmt.Sprintf("%s|%s|%s|%d|%d|%s", protocol, libvirt.URI, bindAddress, port, vmediaPort, credentialRef)
}

func serviceConsumerClusters(consumers []ProviderServiceConsumer) []string {
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
