package render

import (
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/artifactpub"
)

// ApplySharedOverlays merges cluster-side overlay fields on shared
// service capabilities so the renderer emits the union, not whichever
// cluster's vars apply last. The Ansible roles converge ONE instance
// per (host, capability) — if two ClusterInfras consume the same
// (provider, kind, name) and each carry a different overlay, the last
// converge wins and the other cluster's overlay silently disappears on
// re-apply. The fix is to compute the union once at render time and
// patch every consuming cluster to see the same merged value; the
// per-cluster Ansible iteration then writes the union no matter which
// cluster's apply runs first.
//
// Today the only overlay that requires merging is
// nameResolution.additionalIngressHosts. Add new shared overlay fields
// here as the schema introduces them.
//
// Idempotent: calling twice with the same input yields the same state.
func ApplySharedOverlays(state *v1alpha1.State) {
	if state == nil {
		return
	}
	mergeNameResolutionAdditionalIngressHosts(state)
}

type sharedCapKey struct {
	provider string
	name     string
}

func mergeNameResolutionAdditionalIngressHosts(state *v1alpha1.State) {
	unions := map[sharedCapKey]map[string]struct{}{}
	for i := range state.ClusterInfras {
		nr := state.ClusterInfras[i].Spec.Components.NameResolution
		if nr == nil {
			continue
		}
		key := sharedCapKey{provider: nr.From.Provider, name: nr.From.Name}
		set, ok := unions[key]
		if !ok {
			set = map[string]struct{}{}
			unions[key] = set
		}
		for _, host := range nr.AdditionalIngressHosts {
			if host == "" {
				continue
			}
			set[host] = struct{}{}
		}
	}
	for i := range state.ClusterInfras {
		nr := state.ClusterInfras[i].Spec.Components.NameResolution
		if nr == nil {
			continue
		}
		key := sharedCapKey{provider: nr.From.Provider, name: nr.From.Name}
		set := unions[key]
		if len(set) == 0 {
			nr.AdditionalIngressHosts = nil
			continue
		}
		merged := make([]string, 0, len(set))
		for host := range set {
			merged = append(merged, host)
		}
		sort.Strings(merged)
		nr.AdditionalIngressHosts = merged
	}
}

// SharedServiceGroup is one shared instance, the host that runs it, and the
// consuming clusters. Load balancers use the ClusterInfra component name as
// CapabilityName because that local name identifies the HAProxy instance.
type SharedServiceGroup struct {
	Kind              string   `yaml:"kind" json:"kind"`
	ProviderName      string   `yaml:"providerName" json:"providerName"`
	CapabilityName    string   `yaml:"capabilityName" json:"capabilityName"`
	HostRef           string   `yaml:"hostRef,omitempty" json:"hostRef,omitempty"`
	ConsumingClusters []string `yaml:"consumingClusters" json:"consumingClusters"`
}

// SharedComponents returns one entry per service instance consumed by two or
// more clusters. Single-consumer instances are omitted.
//
// Kind values match the ComponentSlot* constants in api/v1alpha1.
func SharedComponents(state v1alpha1.State) []SharedServiceGroup {
	type entry struct {
		kind     string
		from     v1alpha1.From
		hostRef  string
		consumer string
	}
	var entries []entry
	for _, ci := range state.ClusterInfras {
		consumer := clusterNameForCI(state, ci)
		c := ci.Spec.Components
		for _, component := range c.LoadBalancers {
			lb, _ := resolveLoadBalancer(state, component.From)
			host := ""
			if lb.HAProxy != nil {
				host = lb.HAProxy.HostRef.Name
			}
			entries = append(entries, entry{
				kind: v1alpha1.ComponentSlotLoadBalancer,
				from: v1alpha1.From{
					Provider: component.From.Provider,
					Name:     component.Name,
				},
				hostRef:  host,
				consumer: consumer,
			})
		}
		if ocp, ok := clusterForCI(state, ci); ok && artifactpub.ClusterNeedsPublication(state, ci, ocp) {
			if publisher, ok := artifactpub.Select(state); ok {
				host := ""
				if publisher.Capability.HTTP != nil {
					host = publisher.Capability.HTTP.HostRef.Name
				}
				entries = append(entries, entry{
					kind: v1alpha1.ComponentSlotArtifacts,
					from: v1alpha1.From{
						Provider: publisher.ProviderName,
						Name:     publisher.Capability.Name,
					},
					hostRef:  host,
					consumer: consumer,
				})
			}
		}
		if c.Proxy != nil {
			pr, _ := resolveProxy(state, c.Proxy.From)
			host := ""
			if pr.Squid != nil {
				host = pr.Squid.HostRef.Name
			}
			entries = append(entries, entry{
				kind: v1alpha1.ComponentSlotProxy, from: c.Proxy.From, hostRef: host, consumer: consumer,
			})
		}
		if c.NameResolution != nil {
			d, _ := resolveDNS(state, c.NameResolution.From)
			host := ""
			if d.Dnsmasq != nil {
				host = d.Dnsmasq.HostRef.Name
			}
			entries = append(entries, entry{
				kind: v1alpha1.ComponentSlotNameResolution, from: c.NameResolution.From, hostRef: host, consumer: consumer,
			})
		}
		if c.Registry != nil {
			r, _ := resolveRegistry(state, c.Registry.From)
			host := ""
			if r.MirrorRegistry != nil {
				host = r.MirrorRegistry.HostRef.Name
			}
			entries = append(entries, entry{
				kind: v1alpha1.ComponentSlotRegistry, from: c.Registry.From, hostRef: host, consumer: consumer,
			})
		}
	}

	type key struct {
		kind     string
		provider string
		name     string
	}
	grouped := map[key]*SharedServiceGroup{}
	var order []key
	for _, e := range entries {
		k := key{kind: e.kind, provider: e.from.Provider, name: e.from.Name}
		g, ok := grouped[k]
		if !ok {
			g = &SharedServiceGroup{
				Kind:           e.kind,
				ProviderName:   e.from.Provider,
				CapabilityName: e.from.Name,
				HostRef:        e.hostRef,
			}
			grouped[k] = g
			order = append(order, k)
		}
		g.ConsumingClusters = append(g.ConsumingClusters, e.consumer)
	}

	out := make([]SharedServiceGroup, 0)
	for _, k := range order {
		g := grouped[k]
		if len(g.ConsumingClusters) < 2 {
			continue
		}
		sort.Strings(g.ConsumingClusters)
		out = append(out, *g)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].ProviderName != out[j].ProviderName {
			return out[i].ProviderName < out[j].ProviderName
		}
		return out[i].CapabilityName < out[j].CapabilityName
	})
	return out
}
