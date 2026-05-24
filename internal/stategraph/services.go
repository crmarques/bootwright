package stategraph

import (
	"fmt"
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/artifactpub"
	"github.com/crmarques/bootwright/internal/stateview"
	"github.com/crmarques/bootwright/internal/support"
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
	grouped := map[ProviderServiceIdentity]*ProviderService{}
	var order []ProviderServiceIdentity
	for _, entry := range entries {
		id := ProviderServiceIdentity{
			Kind:         entry.Fields["kind"],
			ProviderName: entry.Fields["providerName"],
			Name:         entry.Fields["name"],
		}
		if id.Kind == "" || id.ProviderName == "" || id.Name == "" {
			continue
		}
		service, ok := grouped[id]
		if !ok {
			service = &ProviderService{
				Identity:           id,
				HostRef:            entry.Fields["hostRef"],
				Fields:             cloneStringMap(entry.Fields),
				MergedStringFields: map[string][]string{},
			}
			grouped[id] = service
			order = append(order, id)
		}
		service.Consumers = append(service.Consumers, entry)
		mergeServiceStringFields(service.MergedStringFields, entry.MergeStringFields)
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := order[i], order[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.ProviderName != b.ProviderName {
			return a.ProviderName < b.ProviderName
		}
		return a.Name < b.Name
	})
	out := make([]ProviderService, 0, len(order))
	for _, id := range order {
		service := grouped[id]
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
					"shared provider service %s %s/%s has conflicting %s: %s uses %q, %s uses %q",
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
	c := infra.Spec.Components
	var out []ProviderServiceConsumer
	for _, lb := range c.LoadBalancers {
		if cap, ok := loadBalancerCapability(state, lb.From); ok && cap.HAProxy != nil {
			out = append(out, newServiceConsumer(
				cluster.Metadata.Name,
				infra.Metadata.Name,
				fmt.Sprintf("ClusterInfra/%s spec.components.loadBalancers[%s]", infra.Metadata.Name, lb.Name),
				v1alpha1.ComponentSlotLoadBalancer,
				lb.From.Provider,
				lb.Name,
				"haProxy",
				map[string]string{"hostRef": cap.HAProxy.HostRef.Name, "capabilityName": lb.From.Name},
				nil,
			))
		}
	}
	if c.Proxy != nil {
		if cap, ok := proxyCapability(state, c.Proxy.From); ok && cap.Squid != nil {
			out = append(out, newServiceConsumer(
				cluster.Metadata.Name,
				infra.Metadata.Name,
				fmt.Sprintf("ClusterInfra/%s spec.components.proxy", infra.Metadata.Name),
				v1alpha1.ComponentSlotProxy,
				c.Proxy.From.Provider,
				c.Proxy.From.Name,
				"squid",
				map[string]string{"hostRef": cap.Squid.HostRef.Name, "bindAddress": c.Proxy.BindAddress, "port": fmt.Sprint(c.Proxy.Port)},
				nil,
			))
		}
	}
	if c.NameResolution != nil {
		if cap, ok := dnsCapability(state, c.NameResolution.From); ok && cap.Dnsmasq != nil {
			out = append(out, newServiceConsumer(
				cluster.Metadata.Name,
				infra.Metadata.Name,
				fmt.Sprintf("ClusterInfra/%s spec.components.nameResolution", infra.Metadata.Name),
				v1alpha1.ComponentSlotNameResolution,
				c.NameResolution.From.Provider,
				c.NameResolution.From.Name,
				"dnsmasq",
				map[string]string{"hostRef": cap.Dnsmasq.HostRef.Name, "bindAddress": c.NameResolution.BindAddress, "port": fmt.Sprint(c.NameResolution.Port)},
				map[string][]string{"additionalIngressHosts": c.NameResolution.AdditionalIngressHosts},
			))
		}
	}
	if c.Registry != nil {
		if cap, ok := registryCapability(state, c.Registry.From); ok && cap.MirrorRegistry != nil {
			out = append(out, newServiceConsumer(
				cluster.Metadata.Name,
				infra.Metadata.Name,
				fmt.Sprintf("ClusterInfra/%s spec.components.registry", infra.Metadata.Name),
				v1alpha1.ComponentSlotRegistry,
				c.Registry.From.Provider,
				c.Registry.From.Name,
				"mirrorRegistry",
				map[string]string{"hostRef": cap.MirrorRegistry.HostRef.Name, "bindAddress": c.Registry.BindAddress, "port": fmt.Sprint(c.Registry.Port)},
				nil,
			))
		}
	}
	if artifactpub.ClusterNeedsPublication(state, infra, cluster) {
		if publisher, ok := artifactpub.Select(state); ok && publisher.Capability.HTTP != nil {
			out = append(out, newServiceConsumer(
				cluster.Metadata.Name,
				infra.Metadata.Name,
				fmt.Sprintf("ClusterInfra/%s generated artifact publisher", infra.Metadata.Name),
				v1alpha1.ComponentSlotArtifacts,
				publisher.ProviderName,
				publisher.Capability.Name,
				"http",
				map[string]string{
					"hostRef":     publisher.Capability.HTTP.HostRef.Name,
					"bindAddress": v1alpha1.DefaultServiceBindAddress,
					"port":        fmt.Sprint(publisher.Capability.HTTP.Port),
				},
				nil,
			))
		}
	}
	return out
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

func loadBalancerCapability(state v1alpha1.State, from v1alpha1.From) (v1alpha1.LoadBalancerCapability, bool) {
	provider, ok := stateview.Provider(state, from.Provider)
	if !ok {
		return v1alpha1.LoadBalancerCapability{}, false
	}
	return stateview.LoadBalancer(provider, from.Name)
}

func proxyCapability(state v1alpha1.State, from v1alpha1.From) (v1alpha1.ProxyCapability, bool) {
	provider, ok := stateview.Provider(state, from.Provider)
	if !ok {
		return v1alpha1.ProxyCapability{}, false
	}
	return stateview.Proxy(provider, from.Name)
}

func dnsCapability(state v1alpha1.State, from v1alpha1.From) (v1alpha1.DNSCapability, bool) {
	provider, ok := stateview.Provider(state, from.Provider)
	if !ok {
		return v1alpha1.DNSCapability{}, false
	}
	return stateview.DNS(provider, from.Name)
}

func registryCapability(state v1alpha1.State, from v1alpha1.From) (v1alpha1.RegistryCapability, bool) {
	provider, ok := stateview.Provider(state, from.Provider)
	if !ok {
		return v1alpha1.RegistryCapability{}, false
	}
	return stateview.Registry(provider, from.Name)
}
