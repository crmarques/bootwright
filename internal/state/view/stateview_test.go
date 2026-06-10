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
			Type: v1alpha1.ProvisionerLibvirt,
			Libvirt: &v1alpha1.InfraProviderLibvirt{
				MachineRef:      v1alpha1.LocalObjectReference{Name: "server"},
				URI:             "qemu:///system",
				MachineProfiles: []v1alpha1.MachineProfile{{Name: "profile"}},
			},
		},
	}
	machine := v1alpha1.Machine{Metadata: v1alpha1.Metadata{Name: "server"}}
	component := v1alpha1.InfraComponent{
		Metadata: v1alpha1.Metadata{Name: "lb"},
		Spec: v1alpha1.InfraComponentSpec{
			Type:         v1alpha1.ComponentSlotLoadBalancer,
			LoadBalancer: &v1alpha1.LoadBalancerComponent{Implementation: v1alpha1.InfraComponentTypeHAProxy},
		},
	}
	state := v1alpha1.State{Machines: []v1alpha1.Machine{machine}, InfraProviders: []v1alpha1.InfraProvider{provider}, InfraComponents: []v1alpha1.InfraComponent{component}}

	gotProvider, ok := Provider(state, "provider")
	if !ok || gotProvider.Metadata.Name != "provider" {
		t.Fatalf("Provider lookup failed: %v %+v", ok, gotProvider)
	}
	if profile, ok := MachineProfile(gotProvider, "profile"); !ok || profile.Name != "profile" {
		t.Fatalf("MachineProfile lookup failed: %v %+v", ok, profile)
	}
	if machine, ok := Machine(state, "server"); !ok || machine.Metadata.Name != "server" {
		t.Fatalf("Machine lookup failed: %v %+v", ok, machine)
	}
	if gotComponent, ok := InfraComponent(state, "lb"); !ok || gotComponent.Metadata.Name != "lb" {
		t.Fatalf("InfraComponent lookup failed: %v %+v", ok, gotComponent)
	}
}

func TestClusterInstallRelationships(t *testing.T) {
	cluster := v1alpha1.ContainerCluster{
		Metadata: v1alpha1.Metadata{Name: "cluster"},
		Spec: v1alpha1.ContainerClusterSpec{Hosts: []v1alpha1.OCPHostSpec{
			{Hostname: "master-0", MachineRef: v1alpha1.LocalObjectReference{Name: "master-0"}},
			{Hostname: "master-1", MachineRef: v1alpha1.LocalObjectReference{Name: "master-1"}},
		}},
	}
	state := v1alpha1.State{
		Machines: []v1alpha1.Machine{
			{Metadata: v1alpha1.Metadata{Name: "master-0"}},
			{Metadata: v1alpha1.Metadata{Name: "master-1"}},
		},
		ContainerClusters: []v1alpha1.ContainerCluster{cluster},
	}
	infra, ok := ClusterInstallForContainerCluster(state, cluster)
	if !ok {
		t.Fatal("ClusterInstallForContainerCluster returned false")
	}

	if got, ok := ClusterForInstall(state, infra); !ok || got.Metadata.Name != "cluster" {
		t.Fatalf("ClusterForInstall failed: %v %+v", ok, got)
	}
	names := ClusterInstallNames(cluster)
	if !reflect.DeepEqual(names, []string{"cluster"}) {
		t.Fatalf("ClusterInstallNames = %v", names)
	}
	nodes := ClusterNodesForInstall(state, infra)
	if len(nodes) != 2 || nodes["master-0"].Hostname != "master-0" || nodes["master-1"].Hostname != "master-1" {
		t.Fatalf("ClusterNodesForInstall = %+v", nodes)
	}
}

func TestEndpointAddressAndNetworkMatching(t *testing.T) {
	infra := v1alpha1.ClusterInstall{
		Endpoints: map[string]v1alpha1.Endpoint{
			v1alpha1.EndpointAPI: {
				Source: v1alpha1.EndpointSource{
					Type:         v1alpha1.EndpointSourceInfraComponent,
					ComponentRef: v1alpha1.LocalObjectReference{Name: "lb"},
					BindAddress:  "api",
				},
			},
		},
		Machines: []v1alpha1.InstallMachine{{
			Network: v1alpha1.MachineNetworkConfig{
				NetworkConfigRef: v1alpha1.LocalObjectReference{Name: "network"},
			},
		}},
	}
	network := v1alpha1.NetworkConfig{
		Metadata: v1alpha1.Metadata{Name: "network"},
		Spec: v1alpha1.NetworkConfigSpec{
			MachineNetwork: []v1alpha1.MachineNetworkCIDR{{CIDR: "192.168.133.0/24"}},
		},
	}
	state := v1alpha1.State{
		NetworkConfigs: []v1alpha1.NetworkConfig{network},
		InfraComponents: []v1alpha1.InfraComponent{{
			Metadata: v1alpha1.Metadata{Name: "lb"},
			Spec: v1alpha1.InfraComponentSpec{
				Type: v1alpha1.ComponentSlotLoadBalancer,
				LoadBalancer: &v1alpha1.LoadBalancerComponent{
					Implementation: v1alpha1.InfraComponentTypeHAProxy,
					BindAddresses: []v1alpha1.LoadBalancerBindAddress{{
						Name:    "api",
						Address: "192.168.133.10",
					}},
				},
			},
		}},
	}

	if got := EndpointAddress(state, infra, v1alpha1.EndpointAPI); got != "192.168.133.10" {
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
		Machines: []v1alpha1.Machine{{
			Metadata: v1alpha1.Metadata{Name: "provider"},
			Spec: v1alpha1.MachineSpec{
				OS: v1alpha1.MachineOSSpec{
					Provided: v1alpha1.BoolPtr(true),
				},
				Addresses: []v1alpha1.MachineAddress{
					{Name: "ssh", Address: "127.0.0.1"},
					{Name: "lan", Address: "192.168.133.2"},
				},
				Access: v1alpha1.MachineAccess{
					SSH: &v1alpha1.MachineSSHSpec{AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"}},
				},
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
	infra := v1alpha1.ClusterInstall{
		Endpoints: map[string]v1alpha1.Endpoint{
			v1alpha1.EndpointAPI: {Address: "192.168.133.10"},
		},
		Machines: []v1alpha1.InstallMachine{{
			Network: v1alpha1.MachineNetworkConfig{
				NetworkConfigRef: v1alpha1.LocalObjectReference{Name: "network"},
			},
		}},
	}

	if got := MachineRouteAddress(state, "provider", "lan", infra); got != "192.168.133.2" {
		t.Fatalf("named MachineRouteAddress = %q", got)
	}
	if got := MachineRouteAddress(state, "provider", "", infra); got != "192.168.133.1" {
		t.Fatalf("fallback MachineRouteAddress = %q", got)
	}
}

func TestIsLoopbackAlias(t *testing.T) {
	for _, value := range []string{"localhost", "LOCALHOST", "127.0.0.1", "::1", "0.0.0.0"} {
		if !IsLoopbackAlias(value) {
			t.Fatalf("IsLoopbackAlias(%q) = false, want true", value)
		}
	}
	for _, value := range []string{"provider.example.test", "192.168.133.2"} {
		if IsLoopbackAlias(value) {
			t.Fatalf("IsLoopbackAlias(%q) = true, want false", value)
		}
	}
}
