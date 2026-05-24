package stategraph

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

func TestProviderServiceGraphMergesAdditionalIngressHosts(t *testing.T) {
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
	state.ContainerClusters = []v1alpha1.ContainerCluster{
		containerCluster("cluster-a", "cluster-a"),
		containerCluster("cluster-b", "cluster-b"),
	}
	graph := ResolveProviderServices(state)
	got := graph.MergedStringField(ProviderServiceIdentity{
		Kind:         v1alpha1.ComponentSlotNameResolution,
		ProviderName: "lab",
		Name:         "default",
	}, "additionalIngressHosts")
	want := []string{"app-a.example.test", "app-b.example.test", "shared.example.test"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("additionalIngressHosts = %v, want %v", got, want)
	}
}

func TestSharedServicesReportsTwoOrMoreConsumers(t *testing.T) {
	state := dnsProviderState()
	state.ClusterInfras = []v1alpha1.ClusterInfra{
		{
			Metadata: v1alpha1.Metadata{Name: "cluster-a"},
			Spec: v1alpha1.ClusterInfraSpec{
				Components: v1alpha1.ClusterComponents{
					NameResolution: &v1alpha1.ClusterNameResolutionComponent{From: v1alpha1.From{Provider: "lab", Name: "default"}},
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
					NameResolution: &v1alpha1.ClusterNameResolutionComponent{From: v1alpha1.From{Provider: "lab", Name: "default"}},
				},
			},
		},
	}
	state.ContainerClusters = []v1alpha1.ContainerCluster{
		containerCluster("cluster-a", "cluster-a"),
		containerCluster("cluster-b", "cluster-b"),
	}

	groups := ResolveProviderServices(state).SharedServices()
	if len(groups) != 1 {
		t.Fatalf("want exactly 1 shared group; got %d: %+v", len(groups), groups)
	}
	group := groups[0]
	if group.Kind != v1alpha1.ComponentSlotNameResolution {
		t.Fatalf("group.Kind = %q, want %q", group.Kind, v1alpha1.ComponentSlotNameResolution)
	}
	if group.HostRef != "lab-host" {
		t.Fatalf("group.HostRef = %q, want lab-host", group.HostRef)
	}
	want := []string{"cluster-a", "cluster-b"}
	if !reflect.DeepEqual(group.ConsumingClusters, want) {
		t.Fatalf("ConsumingClusters = %v, want %v", group.ConsumingClusters, want)
	}
}

func TestSharedServicesReportsContainerClusterConsumers(t *testing.T) {
	state := dnsProviderState()
	state.ClusterInfras = []v1alpha1.ClusterInfra{
		{
			Metadata: v1alpha1.Metadata{Name: "infra-a"},
			Spec: v1alpha1.ClusterInfraSpec{
				Components: v1alpha1.ClusterComponents{
					NameResolution: &v1alpha1.ClusterNameResolutionComponent{From: v1alpha1.From{Provider: "lab", Name: "default"}},
				},
			},
		},
		{
			Metadata: v1alpha1.Metadata{Name: "infra-b"},
			Spec: v1alpha1.ClusterInfraSpec{
				Components: v1alpha1.ClusterComponents{
					NameResolution: &v1alpha1.ClusterNameResolutionComponent{From: v1alpha1.From{Provider: "lab", Name: "default"}},
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

	groups := ResolveProviderServices(state).SharedServices()
	if len(groups) != 1 {
		t.Fatalf("want exactly 1 shared group; got %d: %+v", len(groups), groups)
	}
	want := []string{"cluster-a", "cluster-b"}
	if !reflect.DeepEqual(groups[0].ConsumingClusters, want) {
		t.Fatalf("ConsumingClusters = %v, want %v", groups[0].ConsumingClusters, want)
	}
}
