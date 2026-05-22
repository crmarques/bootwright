package render

import (
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func dnsProviderState() v1alpha1.State {
	return v1alpha1.State{
		Hosts: []v1alpha1.Host{{
			Metadata: v1alpha1.Metadata{Name: "lab-host"},
			Spec: v1alpha1.HostSpec{
				Addresses:    []v1alpha1.HostAddress{{Name: "ssh", Address: "10.0.0.5"}},
				SSH:          &v1alpha1.HostSSHSpec{AddressName: "ssh"},
				Capabilities: []string{"container-runtime"},
			},
		}},
		InfraProviders: []v1alpha1.InfraProvider{{
			Metadata: v1alpha1.Metadata{Name: "lab"},
			Spec: v1alpha1.InfraProviderSpec{
				DNS: []v1alpha1.DNSCapability{{
					Name:    "default",
					Dnsmasq: &v1alpha1.DnsmasqCapability{HostRef: v1alpha1.LocalObjectReference{Name: "lab-host"}},
				}},
				LoadBalancers: []v1alpha1.LoadBalancerCapability{{
					Name:    "default",
					HAProxy: &v1alpha1.HAProxyCapability{HostRef: v1alpha1.LocalObjectReference{Name: "lab-host"}},
				}},
			},
		}},
	}
}

// TestApplySharedOverlaysMergesAdditionalIngressHosts is the regression
// test for the critical sharing finding in the architecture review:
// two clusters that consume the same dnsmasq capability must both end
// up with the union of additionalIngressHosts so the Ansible converge
// produces the same dnsmasq.conf regardless of apply order.
func TestApplySharedOverlaysMergesAdditionalIngressHosts(t *testing.T) {
	state := dnsProviderState()
	state.ClusterInfras = []v1alpha1.ClusterInfra{
		{
			Metadata: v1alpha1.Metadata{Name: "cluster-a"},
			Spec: v1alpha1.ClusterInfraSpec{
				Components: v1alpha1.ClusterComponents{
					NameResolution: &v1alpha1.ClusterNameResolutionComponent{
						From:                   v1alpha1.From{Provider: "lab", Name: "default"},
						AdditionalIngressHosts: []string{"app-a.example.test", "shared.example.test"},
					},
				},
			},
		},
		{
			Metadata: v1alpha1.Metadata{Name: "cluster-b"},
			Spec: v1alpha1.ClusterInfraSpec{
				Components: v1alpha1.ClusterComponents{
					NameResolution: &v1alpha1.ClusterNameResolutionComponent{
						From:                   v1alpha1.From{Provider: "lab", Name: "default"},
						AdditionalIngressHosts: []string{"app-b.example.test", "shared.example.test"},
					},
				},
			},
		},
	}

	ApplySharedOverlays(&state)

	want := []string{"app-a.example.test", "app-b.example.test", "shared.example.test"}
	for i, ci := range state.ClusterInfras {
		got := ci.Spec.Components.NameResolution.AdditionalIngressHosts
		if !reflect.DeepEqual(got, want) {
			t.Errorf("cluster %d (%s) additionalIngressHosts = %v, want %v", i, ci.Metadata.Name, got, want)
		}
	}
}

// TestApplySharedOverlaysIsIdempotent runs the merge twice and asserts
// the result of the second run equals the first. Validators rely on
// the union pass being a fixed point so they can run after it without
// re-introducing duplicates.
func TestApplySharedOverlaysIsIdempotent(t *testing.T) {
	state := dnsProviderState()
	state.ClusterInfras = []v1alpha1.ClusterInfra{{
		Metadata: v1alpha1.Metadata{Name: "c1"},
		Spec: v1alpha1.ClusterInfraSpec{
			Components: v1alpha1.ClusterComponents{
				NameResolution: &v1alpha1.ClusterNameResolutionComponent{
					From:                   v1alpha1.From{Provider: "lab", Name: "default"},
					AdditionalIngressHosts: []string{"host.example.test"},
				},
			},
		},
	}}

	ApplySharedOverlays(&state)
	first := append([]string(nil), state.ClusterInfras[0].Spec.Components.NameResolution.AdditionalIngressHosts...)
	ApplySharedOverlays(&state)
	second := state.ClusterInfras[0].Spec.Components.NameResolution.AdditionalIngressHosts

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("not idempotent: first=%v second=%v", first, second)
	}
}

// TestApplySharedOverlaysLeavesUnsharedAlone confirms the merge does
// nothing surprising when only one cluster consumes a capability.
func TestApplySharedOverlaysLeavesUnsharedAlone(t *testing.T) {
	state := dnsProviderState()
	state.ClusterInfras = []v1alpha1.ClusterInfra{{
		Metadata: v1alpha1.Metadata{Name: "solo"},
		Spec: v1alpha1.ClusterInfraSpec{
			Components: v1alpha1.ClusterComponents{
				NameResolution: &v1alpha1.ClusterNameResolutionComponent{
					From:                   v1alpha1.From{Provider: "lab", Name: "default"},
					AdditionalIngressHosts: []string{"only.example.test"},
				},
			},
		},
	}}

	ApplySharedOverlays(&state)

	got := state.ClusterInfras[0].Spec.Components.NameResolution.AdditionalIngressHosts
	if !reflect.DeepEqual(got, []string{"only.example.test"}) {
		t.Fatalf("solo cluster overlay was mutated unexpectedly: got %v", got)
	}
}

// TestSharedComponentsReportsTwoOrMoreConsumers verifies that
// SharedComponents only surfaces (provider, kind, name) tuples that
// are consumed by two or more clusters. Single-consumer entries are
// noise for the status report and the union pass.
func TestSharedComponentsReportsTwoOrMoreConsumers(t *testing.T) {
	state := dnsProviderState()
	state.ClusterInfras = []v1alpha1.ClusterInfra{
		{
			Metadata: v1alpha1.Metadata{Name: "cluster-a"},
			Spec: v1alpha1.ClusterInfraSpec{
				Components: v1alpha1.ClusterComponents{
					NameResolution: &v1alpha1.ClusterNameResolutionComponent{
						From: v1alpha1.From{Provider: "lab", Name: "default"},
					},
					LoadBalancers: []v1alpha1.ClusterLoadBalancerComponent{{
						Name: "default",
						From: v1alpha1.From{Provider: "lab", Name: "default"},
					}},
				},
			},
		},
		{
			Metadata: v1alpha1.Metadata{Name: "cluster-b"},
			Spec: v1alpha1.ClusterInfraSpec{
				Components: v1alpha1.ClusterComponents{
					NameResolution: &v1alpha1.ClusterNameResolutionComponent{
						From: v1alpha1.From{Provider: "lab", Name: "default"},
					},
					// Cluster B does NOT use the load balancer — only DNS should appear.
				},
			},
		},
	}

	groups := SharedComponents(state)
	if len(groups) != 1 {
		t.Fatalf("want exactly 1 shared group (nameResolution); got %d: %+v", len(groups), groups)
	}
	g := groups[0]
	if g.Kind != v1alpha1.ComponentSlotNameResolution {
		t.Errorf("group.Kind = %q, want %q", g.Kind, v1alpha1.ComponentSlotNameResolution)
	}
	if g.HostRef != "lab-host" {
		t.Errorf("group.HostRef = %q, want %q", g.HostRef, "lab-host")
	}
	want := []string{"cluster-a", "cluster-b"}
	if !reflect.DeepEqual(g.ConsumingClusters, want) {
		t.Errorf("ConsumingClusters = %v, want %v", g.ConsumingClusters, want)
	}
}

func TestSharedComponentsReportsContainerClusterConsumers(t *testing.T) {
	state := dnsProviderState()
	state.ClusterInfras = []v1alpha1.ClusterInfra{
		{
			Metadata: v1alpha1.Metadata{Name: "infra-a"},
			Spec: v1alpha1.ClusterInfraSpec{
				Components: v1alpha1.ClusterComponents{
					NameResolution: &v1alpha1.ClusterNameResolutionComponent{
						From: v1alpha1.From{Provider: "lab", Name: "default"},
					},
				},
			},
		},
		{
			Metadata: v1alpha1.Metadata{Name: "infra-b"},
			Spec: v1alpha1.ClusterInfraSpec{
				Components: v1alpha1.ClusterComponents{
					NameResolution: &v1alpha1.ClusterNameResolutionComponent{
						From: v1alpha1.From{Provider: "lab", Name: "default"},
					},
				},
			},
		},
	}
	state.ContainerClusters = []v1alpha1.ContainerCluster{
		{
			Metadata: v1alpha1.Metadata{Name: "cluster-a"},
			Spec: v1alpha1.ContainerClusterSpec{
				Nodes: []v1alpha1.OCPNodeSpec{{
					Hostname: "master-a",
					Role:     v1alpha1.NodeRoleMaster,
					MachineRef: v1alpha1.NodeMachineRef{
						ClusterInfra: "infra-a",
						Name:         "master-a",
					},
				}},
			},
		},
		{
			Metadata: v1alpha1.Metadata{Name: "cluster-b"},
			Spec: v1alpha1.ContainerClusterSpec{
				Nodes: []v1alpha1.OCPNodeSpec{{
					Hostname: "master-b",
					Role:     v1alpha1.NodeRoleMaster,
					MachineRef: v1alpha1.NodeMachineRef{
						ClusterInfra: "infra-b",
						Name:         "master-b",
					},
				}},
			},
		},
	}

	groups := SharedComponents(state)
	if len(groups) != 1 {
		t.Fatalf("want exactly 1 shared group; got %d: %+v", len(groups), groups)
	}
	want := []string{"cluster-a", "cluster-b"}
	if !reflect.DeepEqual(groups[0].ConsumingClusters, want) {
		t.Errorf("ConsumingClusters = %v, want %v", groups[0].ConsumingClusters, want)
	}
}
