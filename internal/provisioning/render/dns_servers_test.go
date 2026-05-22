package render

import (
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// dnsResolveState returns a State with one NetworkConfig and an
// InfraProvider that exposes a dns_dnsmasq capability — the
// minimum shape resolveClusterDNSServers needs to act on.
func dnsResolveState(networkDNS []string) v1alpha1.State {
	networkConfig := map[string]any{}
	if len(networkDNS) > 0 {
		servers := make([]any, 0, len(networkDNS))
		for _, server := range networkDNS {
			servers = append(servers, server)
		}
		networkConfig["dns-resolver"] = map[string]any{"config": map[string]any{"server": servers}}
	}
	return v1alpha1.State{
		NetworkConfigs: []v1alpha1.NetworkConfig{{
			Metadata: v1alpha1.Metadata{Name: "lab-net"},
			Spec: v1alpha1.NetworkConfigSpec{
				MachineNetwork: []v1alpha1.MachineNetworkCIDR{{CIDR: "192.168.130.0/24"}},
				Template:       v1alpha1.NetworkConfigTemplate{NetworkConfig: networkConfig},
			},
		}},
		InfraProviders: []v1alpha1.InfraProvider{{
			Metadata: v1alpha1.Metadata{Name: "lab"},
			Spec: v1alpha1.InfraProviderSpec{
				DNS: []v1alpha1.DNSCapability{{
					Name:    "default",
					Dnsmasq: &v1alpha1.DnsmasqCapability{HostRef: v1alpha1.LocalObjectReference{Name: "lab-host"}},
				}},
			},
		}},
	}
}

func ciWithNameResolution(bindAddress string) v1alpha1.ClusterInfra {
	return v1alpha1.ClusterInfra{
		Metadata: v1alpha1.Metadata{Name: "c1"},
		Spec: v1alpha1.ClusterInfraSpec{
			Components: v1alpha1.ClusterComponents{
				NameResolution: &v1alpha1.ClusterNameResolutionComponent{
					From:        v1alpha1.From{Provider: "lab", Name: "default"},
					BindAddress: bindAddress,
				},
			},
		},
	}
}

func TestResolveClusterDNSServers_NoNameResolutionPassesThrough(t *testing.T) {
	state := dnsResolveState([]string{"10.0.0.1"})
	ci := v1alpha1.ClusterInfra{Metadata: v1alpha1.Metadata{Name: "c1"}}
	got := resolveClusterDNSServers(state, ci, state.NetworkConfigs[0])
	want := []string{"10.0.0.1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestResolveClusterDNSServers_EmptyAndNoNameResolution(t *testing.T) {
	state := dnsResolveState(nil)
	ci := v1alpha1.ClusterInfra{Metadata: v1alpha1.Metadata{Name: "c1"}}
	got := resolveClusterDNSServers(state, ci, state.NetworkConfigs[0])
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

func TestResolveClusterDNSServers_BindAddressInCIDRPrepended(t *testing.T) {
	state := dnsResolveState(nil)
	ci := ciWithNameResolution("192.168.130.1")
	got := resolveClusterDNSServers(state, ci, state.NetworkConfigs[0])
	want := []string{"192.168.130.1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestResolveClusterDNSServers_PrependsBeforeUserList(t *testing.T) {
	state := dnsResolveState([]string{"10.0.0.1"})
	ci := ciWithNameResolution("192.168.130.1")
	got := resolveClusterDNSServers(state, ci, state.NetworkConfigs[0])
	want := []string{"192.168.130.1", "10.0.0.1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestResolveClusterDNSServers_WildcardDoesNotInferServiceIP(t *testing.T) {
	state := dnsResolveState(nil)
	ci := ciWithNameResolution("0.0.0.0")
	got := resolveClusterDNSServers(state, ci, state.NetworkConfigs[0])
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

func TestResolveClusterDNSServers_BindOutsideCIDRNoop(t *testing.T) {
	state := dnsResolveState(nil)
	ci := ciWithNameResolution("10.99.0.1") // outside 192.168.130.0/24
	got := resolveClusterDNSServers(state, ci, state.NetworkConfigs[0])
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

func TestResolveClusterDNSServers_RequiresExplicitBind(t *testing.T) {
	state := dnsResolveState(nil)
	ci := ciWithNameResolution("0.0.0.0") // no usable derivation
	got := resolveClusterDNSServers(state, ci, state.NetworkConfigs[0])
	if len(got) != 0 {
		t.Errorf("got %v, want empty (validator catches this case)", got)
	}
}

func TestResolveClusterDNSServers_DeduplicatesExistingEntry(t *testing.T) {
	state := dnsResolveState([]string{"192.168.130.1", "10.0.0.1"})
	ci := ciWithNameResolution("192.168.130.1")
	got := resolveClusterDNSServers(state, ci, state.NetworkConfigs[0])
	want := []string{"192.168.130.1", "10.0.0.1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestResolveClusterDNSServers_NonDnsmasqCapabilityNoop(t *testing.T) {
	state := dnsResolveState([]string{"10.0.0.1"})
	state.InfraProviders[0].Spec.DNS[0].Dnsmasq = nil
	ci := ciWithNameResolution("192.168.130.1")
	got := resolveClusterDNSServers(state, ci, state.NetworkConfigs[0])
	want := []string{"10.0.0.1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v (no dnsmasq arm = nothing to inject)", got, want)
	}
}
