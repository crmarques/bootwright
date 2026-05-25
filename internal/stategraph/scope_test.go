package stategraph

import (
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestSharedDestroyConflictsDetectsProviderServiceConsumers(t *testing.T) {
	state := stateWithSharedDNS()

	conflicts := SharedDestroyConflicts(state, []string{"cluster-a"})
	if len(conflicts) != 1 {
		t.Fatalf("got %d conflicts, want 1: %#v", len(conflicts), conflicts)
	}
	got := conflicts[0]
	if got.Slot != "nameResolution" || got.Provider != "services" || got.Name != "default" {
		t.Fatalf("unexpected conflict identity: %#v", got)
	}
	if !reflect.DeepEqual(got.ScopedClusters, []string{"cluster-a"}) {
		t.Fatalf("scoped clusters = %#v", got.ScopedClusters)
	}
	if !reflect.DeepEqual(got.UnscopedClusters, []string{"cluster-b"}) {
		t.Fatalf("unscoped clusters = %#v", got.UnscopedClusters)
	}
}

func TestSharedDestroyConflictsUsesLoadBalancerComponentName(t *testing.T) {
	state := stateWithSharedDNS()
	state.ClusterInfras = []v1alpha1.ClusterInfra{
		clusterInfraWithLoadBalancer("infra-a", "machines-a", "control-plane"),
		clusterInfraWithLoadBalancer("infra-b", "machines-b", "apps"),
	}
	if conflicts := SharedDestroyConflicts(state, []string{"cluster-a"}); len(conflicts) != 0 {
		t.Fatalf("different local load balancer names must not conflict: %#v", conflicts)
	}

	state.ClusterInfras[1] = clusterInfraWithLoadBalancer("infra-b", "machines-b", "control-plane")
	conflicts := SharedDestroyConflicts(state, []string{"cluster-a"})
	if len(conflicts) != 1 {
		t.Fatalf("same local load balancer name should conflict, got %#v", conflicts)
	}
	if got := conflicts[0]; got.Slot != "loadBalancer" || got.Provider != "services" || got.Name != "control-plane" {
		t.Fatalf("unexpected conflict identity: %#v", got)
	}
}

func TestSharedDestroyConflictsDetectsAllSharedProviderServiceKinds(t *testing.T) {
	state := stateWithAllSharedProviderServices()

	conflicts := SharedDestroyConflicts(state, []string{"cluster-a"})
	got := map[string]DestroyScopeConflict{}
	for _, conflict := range conflicts {
		got[conflict.Slot] = conflict
	}
	for _, slot := range []string{
		v1alpha1.ComponentSlotArtifacts,
		v1alpha1.ComponentSlotLoadBalancer,
		v1alpha1.ComponentSlotNameResolution,
		v1alpha1.ComponentSlotProxy,
		v1alpha1.ComponentSlotRegistry,
	} {
		conflict, ok := got[slot]
		if !ok {
			t.Fatalf("missing shared %s conflict in %#v", slot, conflicts)
		}
		if conflict.Provider != "services" || conflict.Name != "default" {
			t.Fatalf("%s conflict identity = %#v, want services/default", slot, conflict)
		}
		if !reflect.DeepEqual(conflict.ScopedClusters, []string{"cluster-a"}) {
			t.Fatalf("%s scoped clusters = %#v", slot, conflict.ScopedClusters)
		}
		if !reflect.DeepEqual(conflict.UnscopedClusters, []string{"cluster-b"}) {
			t.Fatalf("%s unscoped clusters = %#v", slot, conflict.UnscopedClusters)
		}
	}
}

func TestFilterStateToClustersKeepsReferencedProviders(t *testing.T) {
	state := stateWithSharedDNS()

	filtered := FilterStateToClusters(state, []string{"cluster-a"})
	if got := namesOfClusters(filtered.ContainerClusters); !reflect.DeepEqual(got, []string{"cluster-a"}) {
		t.Fatalf("clusters = %#v", got)
	}
	if got := namesOfInfras(filtered.ClusterInfras); !reflect.DeepEqual(got, []string{"infra-a"}) {
		t.Fatalf("infras = %#v", got)
	}
	if got := namesOfProviders(filtered.InfraProviders); !reflect.DeepEqual(got, []string{"machines-a", "services"}) {
		t.Fatalf("providers = %#v", got)
	}
}

func stateWithSharedDNS() v1alpha1.State {
	return v1alpha1.State{
		Hosts: []v1alpha1.Host{{
			Metadata: v1alpha1.Metadata{Name: "service-host"},
			Spec: v1alpha1.HostSpec{
				Addresses:    []v1alpha1.HostAddress{{Name: "ssh", Address: "10.0.0.5"}},
				SSH:          &v1alpha1.HostSSHSpec{AddressName: "ssh"},
				Capabilities: []string{v1alpha1.HostCapabilityContainerRuntime},
			},
		}},
		InfraProviders: []v1alpha1.InfraProvider{
			{Metadata: v1alpha1.Metadata{Name: "machines-a"}},
			{Metadata: v1alpha1.Metadata{Name: "machines-b"}},
			{
				Metadata: v1alpha1.Metadata{Name: "services"},
				Spec: v1alpha1.InfraProviderSpec{
					DNS: []v1alpha1.DNSCapability{{
						Name:    "default",
						Dnsmasq: &v1alpha1.DnsmasqCapability{HostRef: v1alpha1.LocalObjectReference{Name: "service-host"}},
					}},
					LoadBalancers: []v1alpha1.LoadBalancerCapability{{
						Name:    "default",
						HAProxy: &v1alpha1.HAProxyCapability{HostRef: v1alpha1.LocalObjectReference{Name: "service-host"}},
					}},
				},
			},
		},
		ClusterInfras: []v1alpha1.ClusterInfra{
			clusterInfra("infra-a", "machines-a"),
			clusterInfra("infra-b", "machines-b"),
		},
		ContainerClusters: []v1alpha1.ContainerCluster{
			containerCluster("cluster-a", "infra-a"),
			containerCluster("cluster-b", "infra-b"),
		},
	}
}

func stateWithAllSharedProviderServices() v1alpha1.State {
	state := stateWithSharedDNS()
	for i := range state.InfraProviders {
		if state.InfraProviders[i].Metadata.Name != "services" {
			continue
		}
		state.InfraProviders[i].Spec.ArtifactPublishers = []v1alpha1.ArtifactPublisherCapability{{
			Name: "default",
			HTTP: &v1alpha1.ArtifactHTTPCapability{HostRef: v1alpha1.LocalObjectReference{
				Name: "service-host",
			}},
		}}
		state.InfraProviders[i].Spec.Proxies = []v1alpha1.ProxyCapability{{
			Name:  "default",
			Squid: &v1alpha1.SquidCapability{HostRef: v1alpha1.LocalObjectReference{Name: "service-host"}},
		}}
		state.InfraProviders[i].Spec.Registries = []v1alpha1.RegistryCapability{{
			Name:           "default",
			MirrorRegistry: &v1alpha1.MirrorRegistryCapability{HostRef: v1alpha1.LocalObjectReference{Name: "service-host"}},
		}}
	}
	for i := range state.ClusterInfras {
		state.ClusterInfras[i].Spec.Components.LoadBalancers = []v1alpha1.ClusterLoadBalancerComponent{{
			Name: "default",
			From: v1alpha1.From{Provider: "services", Name: "default"},
		}}
		state.ClusterInfras[i].Spec.Components.Proxy = &v1alpha1.ClusterComponentRef{
			From: v1alpha1.From{Provider: "services", Name: "default"},
		}
		state.ClusterInfras[i].Spec.Components.Registry = &v1alpha1.ClusterComponentRef{
			From: v1alpha1.From{Provider: "services", Name: "default"},
		}
	}
	for i := range state.ContainerClusters {
		state.ContainerClusters[i].Spec.Install.Mode = v1alpha1.InstallModeDisconnected
	}
	return state
}

func clusterInfra(name, machineProvider string) v1alpha1.ClusterInfra {
	return v1alpha1.ClusterInfra{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.ClusterInfraSpec{Components: v1alpha1.ClusterComponents{
			Machines: []v1alpha1.ClusterMachineComponent{{
				Name: "master-0",
				From: v1alpha1.From{Provider: machineProvider, Name: "node-0"},
			}},
			NameResolution: &v1alpha1.ClusterNameResolutionComponent{
				From: v1alpha1.From{Provider: "services", Name: "default"},
			},
		}},
	}
}

func clusterInfraWithLoadBalancer(name, machineProvider, loadBalancerName string) v1alpha1.ClusterInfra {
	infra := clusterInfra(name, machineProvider)
	infra.Spec.Components.NameResolution = nil
	infra.Spec.Components.LoadBalancers = []v1alpha1.ClusterLoadBalancerComponent{{
		Name: loadBalancerName,
		From: v1alpha1.From{Provider: "services", Name: "default"},
	}}
	return infra
}

func containerCluster(name, infraName string) v1alpha1.ContainerCluster {
	return v1alpha1.ContainerCluster{
		Metadata: v1alpha1.Metadata{Name: name},
		Spec: v1alpha1.ContainerClusterSpec{Nodes: []v1alpha1.OCPNodeSpec{{
			Hostname: "master-0",
			Role:     "master",
			MachineRef: v1alpha1.NodeMachineRef{
				ClusterInfra: infraName,
				Name:         "master-0",
			},
		}}},
	}
}

func namesOfClusters(clusters []v1alpha1.ContainerCluster) []string {
	out := make([]string, 0, len(clusters))
	for _, cluster := range clusters {
		out = append(out, cluster.Metadata.Name)
	}
	return out
}

func namesOfInfras(infras []v1alpha1.ClusterInfra) []string {
	out := make([]string, 0, len(infras))
	for _, infra := range infras {
		out = append(out, infra.Metadata.Name)
	}
	return out
}

func namesOfProviders(providers []v1alpha1.InfraProvider) []string {
	out := make([]string, 0, len(providers))
	for _, provider := range providers {
		out = append(out, provider.Metadata.Name)
	}
	return out
}
