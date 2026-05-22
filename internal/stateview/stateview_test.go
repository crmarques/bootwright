package stateview

import (
	"net"
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestProviderCapabilityLookups(t *testing.T) {
	provider := v1alpha1.InfraProvider{
		Metadata: v1alpha1.Metadata{Name: "provider"},
		Spec: v1alpha1.InfraProviderSpec{
			MachineProfiles: []v1alpha1.MachineProfileCapability{{Name: "profile"}},
			Machines:        []v1alpha1.MachineCapability{{Name: "server"}},
			LoadBalancers:   []v1alpha1.LoadBalancerCapability{{Name: "lb"}},
			Proxies:         []v1alpha1.ProxyCapability{{Name: "proxy"}},
			DNS:             []v1alpha1.DNSCapability{{Name: "dns"}},
			Registries:      []v1alpha1.RegistryCapability{{Name: "registry"}},
		},
	}
	state := v1alpha1.State{InfraProviders: []v1alpha1.InfraProvider{provider}}

	gotProvider, ok := Provider(state, "provider")
	if !ok || gotProvider.Metadata.Name != "provider" {
		t.Fatalf("Provider lookup failed: %v %+v", ok, gotProvider)
	}
	if profile, ok := MachineProfile(gotProvider, "profile"); !ok || profile.Name != "profile" {
		t.Fatalf("MachineProfile lookup failed: %v %+v", ok, profile)
	}
	if machine, ok := Machine(gotProvider, "server"); !ok || machine.Name != "server" {
		t.Fatalf("Machine lookup failed: %v %+v", ok, machine)
	}
	if lb, ok := LoadBalancer(gotProvider, "lb"); !ok || lb.Name != "lb" {
		t.Fatalf("LoadBalancer lookup failed: %v %+v", ok, lb)
	}
	if proxy, ok := Proxy(gotProvider, "proxy"); !ok || proxy.Name != "proxy" {
		t.Fatalf("Proxy lookup failed: %v %+v", ok, proxy)
	}
	if dns, ok := DNS(gotProvider, "dns"); !ok || dns.Name != "dns" {
		t.Fatalf("DNS lookup failed: %v %+v", ok, dns)
	}
	if registry, ok := Registry(gotProvider, "registry"); !ok || registry.Name != "registry" {
		t.Fatalf("Registry lookup failed: %v %+v", ok, registry)
	}
}

func TestClusterInfraRelationships(t *testing.T) {
	infra := v1alpha1.ClusterInfra{Metadata: v1alpha1.Metadata{Name: "infra"}}
	cluster := v1alpha1.ContainerCluster{
		Metadata: v1alpha1.Metadata{Name: "cluster"},
		Spec: v1alpha1.ContainerClusterSpec{Nodes: []v1alpha1.OCPNodeSpec{
			{Hostname: "master-0", MachineRef: v1alpha1.NodeMachineRef{ClusterInfra: "infra"}},
			{Hostname: "master-1", MachineRef: v1alpha1.NodeMachineRef{ClusterInfra: "infra"}},
			{Hostname: "other", MachineRef: v1alpha1.NodeMachineRef{ClusterInfra: "other"}},
		}},
	}
	state := v1alpha1.State{
		ClusterInfras:     []v1alpha1.ClusterInfra{infra},
		ContainerClusters: []v1alpha1.ContainerCluster{cluster},
	}

	if got, ok := ClusterForInfra(state, infra); !ok || got.Metadata.Name != "cluster" {
		t.Fatalf("ClusterForInfra failed: %v %+v", ok, got)
	}
	names := ClusterInfraNames(cluster)
	if !reflect.DeepEqual(names, []string{"infra", "other"}) {
		t.Fatalf("ClusterInfraNames = %v", names)
	}
	nodes := ClusterNodesForInfra(state, infra)
	if len(nodes) != 2 || nodes["master-0"].Hostname != "master-0" || nodes["master-1"].Hostname != "master-1" {
		t.Fatalf("ClusterNodesForInfra = %+v", nodes)
	}
}

func TestEndpointAddressAndNetworkMatching(t *testing.T) {
	infra := v1alpha1.ClusterInfra{
		Spec: v1alpha1.ClusterInfraSpec{
			Endpoints: map[string]v1alpha1.Endpoint{
				v1alpha1.EndpointAPI: {
					ProvidedBy: &v1alpha1.EndpointProvidedBy{LoadBalancer: "lb", Address: "api"},
				},
			},
			Components: v1alpha1.ClusterComponents{
				LoadBalancers: []v1alpha1.ClusterLoadBalancerComponent{{
					Name: "lb",
					BindAddresses: []v1alpha1.LoadBalancerBindAddress{{
						Name: "api",
						IP:   "192.168.133.10",
					}},
				}},
				Machines: []v1alpha1.ClusterMachineComponent{{
					NetworkConfig: v1alpha1.ClusterMachineNetworkConfig{
						Ref: v1alpha1.LocalObjectReference{Name: "network"},
					},
				}},
			},
		},
	}
	network := v1alpha1.NetworkConfig{
		Metadata: v1alpha1.Metadata{Name: "network"},
		Spec: v1alpha1.NetworkConfigSpec{
			MachineNetwork: []v1alpha1.MachineNetworkCIDR{{CIDR: "192.168.133.0/24"}},
		},
	}
	state := v1alpha1.State{NetworkConfigs: []v1alpha1.NetworkConfig{network}}

	if got := EndpointAddress(infra, v1alpha1.EndpointAPI); got != "192.168.133.10" {
		t.Fatalf("EndpointAddress = %q", got)
	}
	if !NetworkConfigContainsIP(network, net.ParseIP("192.168.133.10")) {
		t.Fatal("NetworkConfigContainsIP returned false for in-range IP")
	}
	if got, ok := EndpointNetworkConfig(state, infra, "192.168.133.10"); !ok || got.Metadata.Name != "network" {
		t.Fatalf("EndpointNetworkConfig failed: %v %+v", ok, got)
	}
}

func TestHostRouteAddressFallback(t *testing.T) {
	state := v1alpha1.State{
		Hosts: []v1alpha1.Host{{
			Metadata: v1alpha1.Metadata{Name: "provider"},
			Spec: v1alpha1.HostSpec{
				Addresses: []v1alpha1.HostAddress{
					{Name: "ssh", Address: "127.0.0.1"},
					{Name: "lan", Address: "192.168.133.2"},
				},
				SSH: &v1alpha1.HostSSHSpec{AddressName: "ssh"},
			},
		}},
		NetworkConfigs: []v1alpha1.NetworkConfig{{
			Metadata: v1alpha1.Metadata{Name: "network"},
			Spec: v1alpha1.NetworkConfigSpec{
				MachineNetwork: []v1alpha1.MachineNetworkCIDR{{CIDR: "192.168.133.0/24"}},
				Template: v1alpha1.NetworkConfigTemplate{NetworkConfig: map[string]any{
					"routes": map[string]any{
						"config": []any{map[string]any{
							"destination":      "0.0.0.0/0",
							"next-hop-address": "192.168.133.1",
						}},
					},
				}},
			},
		}},
	}
	infra := v1alpha1.ClusterInfra{
		Spec: v1alpha1.ClusterInfraSpec{
			Endpoints: map[string]v1alpha1.Endpoint{
				v1alpha1.EndpointAPI: {VIP: "192.168.133.10"},
			},
			Components: v1alpha1.ClusterComponents{
				Machines: []v1alpha1.ClusterMachineComponent{{
					NetworkConfig: v1alpha1.ClusterMachineNetworkConfig{
						Ref: v1alpha1.LocalObjectReference{Name: "network"},
					},
				}},
			},
		},
	}

	if got := HostRouteAddress(state, "provider", "lan", infra); got != "192.168.133.2" {
		t.Fatalf("named HostRouteAddress = %q", got)
	}
	if got := HostRouteAddress(state, "provider", "", infra); got != "192.168.133.1" {
		t.Fatalf("fallback HostRouteAddress = %q", got)
	}
}
