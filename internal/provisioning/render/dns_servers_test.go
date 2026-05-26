package render

import (
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestResolveClusterDNSServersAppendsDNSRefs(t *testing.T) {
	state := dnsRefState(v1alpha1.EnvironmentNameResolutionComponent{
		Name: "default",
		Type: v1alpha1.EnvironmentComponentExternal,
		IP:   "192.168.130.53",
	})
	got := resolveClusterDNSServers(state, state.ClusterInfras[0], state.NetworkConfigs[0])
	want := []string{"10.0.0.1", "192.168.130.53"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestResolveClusterDNSServersResolvesManagedBindAddress(t *testing.T) {
	state := dnsRefState(v1alpha1.EnvironmentNameResolutionComponent{
		Name:         "default",
		Type:         v1alpha1.EnvironmentComponentManaged,
		ComponentRef: v1alpha1.LocalObjectReference{Name: "dns"},
	})
	state.InfraComponents = []v1alpha1.InfraComponent{{
		Metadata: v1alpha1.Metadata{Name: "dns"},
		Spec: v1alpha1.InfraComponentSpec{NameResolution: &v1alpha1.NameResolutionComponent{
			Type:        v1alpha1.InfraComponentTypeDnsmasq,
			HostRef:     v1alpha1.LocalObjectReference{Name: "host"},
			BindAddress: "192.168.130.53",
			Port:        53,
		}},
	}}
	got := resolveClusterDNSServers(state, state.ClusterInfras[0], state.NetworkConfigs[0])
	want := []string{"10.0.0.1", "192.168.130.53"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestResolveClusterDNSServersDeduplicates(t *testing.T) {
	state := dnsRefState(v1alpha1.EnvironmentNameResolutionComponent{
		Name: "default",
		Type: v1alpha1.EnvironmentComponentExternal,
		IP:   "10.0.0.1",
	})
	state.NetworkConfigs[0].Spec.Template.NetworkConfig["dns-resolver"].(map[string]any)["config"].(map[string]any)["server"] = []any{"10.0.0.1", "10.0.0.1"}
	got := resolveClusterDNSServers(state, state.ClusterInfras[0], state.NetworkConfigs[0])
	want := []string{"10.0.0.1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestAgentNetworkConfigAppendsDNSRefs(t *testing.T) {
	state := dnsRefState(v1alpha1.EnvironmentNameResolutionComponent{
		Name: "default",
		Type: v1alpha1.EnvironmentComponentExternal,
		IP:   "192.168.130.53",
	})
	got := agentNetworkConfig(state, state.ClusterInfras[0], state.ClusterInfras[0].Spec.Components.Machines[0], "")
	want := []string{"10.0.0.1", "192.168.130.53"}
	if servers := networkConfigDNSServers(got); !reflect.DeepEqual(servers, want) {
		t.Fatalf("got %v, want %v", servers, want)
	}
}

func TestAgentNetworkConfigCreatesDNSResolverForDNSRefs(t *testing.T) {
	state := dnsRefState(v1alpha1.EnvironmentNameResolutionComponent{
		Name: "default",
		Type: v1alpha1.EnvironmentComponentExternal,
		IP:   "192.168.130.53",
	})
	delete(state.NetworkConfigs[0].Spec.Template.NetworkConfig, "dns-resolver")
	got := agentNetworkConfig(state, state.ClusterInfras[0], state.ClusterInfras[0].Spec.Components.Machines[0], "")
	want := []string{"192.168.130.53"}
	if servers := networkConfigDNSServers(got); !reflect.DeepEqual(servers, want) {
		t.Fatalf("got %v, want %v", servers, want)
	}
}

func TestAgentNetworkConfigUsesMachineOverrideDNSServers(t *testing.T) {
	state := dnsRefState(v1alpha1.EnvironmentNameResolutionComponent{
		Name: "default",
		Type: v1alpha1.EnvironmentComponentExternal,
		IP:   "192.168.130.53",
	})
	machine := &state.ClusterInfras[0].Spec.Components.Machines[0]
	machine.NetworkConfig.NetworkConfig = map[string]any{
		"dns-resolver": map[string]any{"config": map[string]any{"server": []any{"10.0.0.2"}}},
	}
	got := agentNetworkConfig(state, state.ClusterInfras[0], *machine, "")
	want := []string{"10.0.0.2", "192.168.130.53"}
	if servers := networkConfigDNSServers(got); !reflect.DeepEqual(servers, want) {
		t.Fatalf("got %v, want %v", servers, want)
	}
}

func dnsRefState(entry v1alpha1.EnvironmentNameResolutionComponent) v1alpha1.State {
	return v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Spec: v1alpha1.EnvironmentSpec{
				InfraComponents: v1alpha1.EnvironmentInfraComponentsSpec{
					NameResolution: []v1alpha1.EnvironmentNameResolutionComponent{entry},
				},
			},
		}},
		NetworkConfigs: []v1alpha1.NetworkConfig{{
			Metadata: v1alpha1.Metadata{Name: "lab-net"},
			Spec: v1alpha1.NetworkConfigSpec{
				MachineNetwork: []v1alpha1.MachineNetworkCIDR{{CIDR: "192.168.130.0/24"}},
				DNSRefs:        []string{"default"},
				Template: v1alpha1.NetworkConfigTemplate{
					NetworkConfig: map[string]any{
						"dns-resolver": map[string]any{"config": map[string]any{"server": []any{"10.0.0.1"}}},
					},
				},
			},
		}},
		ClusterInfras: []v1alpha1.ClusterInfra{{
			Metadata: v1alpha1.Metadata{Name: "c1"},
			Spec: v1alpha1.ClusterInfraSpec{Components: v1alpha1.ClusterComponents{Machines: []v1alpha1.ClusterMachineComponent{{
				Name: "master-0",
				NetworkConfig: v1alpha1.ClusterMachineNetworkConfig{
					Ref: v1alpha1.LocalObjectReference{Name: "lab-net"},
				},
			}}}},
		}},
	}
}
