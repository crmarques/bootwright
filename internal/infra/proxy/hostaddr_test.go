package proxy

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func clusterFacingState(hostSSH, hostClusterAddr, networkGateway string) v1alpha1.State {
	state := v1alpha1.State{
		Hosts: []v1alpha1.Host{{
			Metadata: v1alpha1.Metadata{Name: "lab-host"},
			Spec: v1alpha1.HostSpec{
				Addresses: []v1alpha1.HostAddress{
					{Name: "ssh", Address: hostSSH},
					{Name: "cluster", Address: hostClusterAddr},
				},
				SSH: &v1alpha1.HostSSHSpec{AddressName: "ssh"},
			},
		}},
		NetworkConfigs: []v1alpha1.NetworkConfig{{
			Metadata: v1alpha1.Metadata{Name: "bridge-net"},
			Spec:     networkConfigSpec("192.168.132.0/24", networkGateway),
		}},
		ClusterInfras: []v1alpha1.ClusterInfra{{
			Metadata: v1alpha1.Metadata{Name: "c1"},
			Spec: v1alpha1.ClusterInfraSpec{
				Endpoints: map[string]v1alpha1.Endpoint{
					v1alpha1.EndpointAPI: {ExternalVIP: "192.168.132.10"},
				},
				Components: v1alpha1.ClusterComponents{
					Machines: []v1alpha1.ClusterMachineComponent{{
						Name: "master-0",
						NetworkConfig: v1alpha1.ClusterMachineNetworkConfig{
							Ref: v1alpha1.LocalObjectReference{Name: "bridge-net"},
						},
					}},
				},
			},
		}},
	}
	return state
}

func TestClusterFacingHostAddress(t *testing.T) {
	cases := []struct {
		name           string
		sshAddress     string
		networkGateway string
		want           string
	}{
		{
			name:           "non-loopback ssh address used directly",
			sshAddress:     "10.0.0.5",
			networkGateway: "192.168.132.1",
			want:           "10.0.0.5",
		},
		{
			name:           "loopback ssh address falls back to gateway",
			sshAddress:     "localhost",
			networkGateway: "192.168.132.1",
			want:           "192.168.132.1",
		},
		{
			name:           "127.0.0.1 ssh address falls back to gateway",
			sshAddress:     "127.0.0.1",
			networkGateway: "192.168.132.1",
			want:           "192.168.132.1",
		},
		{
			name:           "IPv6 loopback ssh address falls back to gateway",
			sshAddress:     "::1",
			networkGateway: "192.168.132.1",
			want:           "192.168.132.1",
		},
		{
			name:           "unspecified ssh address falls back to gateway",
			sshAddress:     "0.0.0.0",
			networkGateway: "192.168.132.1",
			want:           "192.168.132.1",
		},
		{
			name:           "loopback with no gateway yields empty",
			sshAddress:     "localhost",
			networkGateway: "",
			want:           "",
		},
		{
			name:           "empty ssh address with gateway falls back",
			sshAddress:     "",
			networkGateway: "192.168.132.1",
			want:           "192.168.132.1",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := clusterFacingState(tc.sshAddress, "", tc.networkGateway)
			got := ClusterFacingHostAddress(state, "lab-host", state.ClusterInfras[0])
			if got != tc.want {
				t.Fatalf("ClusterFacingHostAddress = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClusterFacingHostAddressUnknownHost(t *testing.T) {
	state := clusterFacingState("10.0.0.5", "", "192.168.132.1")
	if got := ClusterFacingHostAddress(state, "no-such-host", state.ClusterInfras[0]); got != "" {
		t.Fatalf("unknown host: got %q, want empty", got)
	}
}

func TestClusterFacingHostAddressFallsBackThroughMachineInterface(t *testing.T) {
	// No endpoints declared — the helper must still find a primary
	// network via the first machine's first interface.
	state := clusterFacingState("localhost", "", "192.168.132.1")
	state.ClusterInfras[0].Spec.Endpoints = nil
	state.ClusterInfras[0].Spec.Components.Machines = []v1alpha1.ClusterMachineComponent{{
		Name: "master-0",
		NetworkConfig: v1alpha1.ClusterMachineNetworkConfig{
			Ref: v1alpha1.LocalObjectReference{Name: "bridge-net"},
		},
	}}
	got := ClusterFacingHostAddress(state, "lab-host", state.ClusterInfras[0])
	if got != "192.168.132.1" {
		t.Fatalf("got %q, want %q (gateway via machine interface fallback)", got, "192.168.132.1")
	}
}

func TestHostRouteAddressUsesNamedAddress(t *testing.T) {
	state := clusterFacingState("localhost", "10.42.0.7", "192.168.132.1")
	got := HostRouteAddress(state, "lab-host", "cluster", state.ClusterInfras[0])
	if got != "10.42.0.7" {
		t.Fatalf("HostRouteAddress = %q, want %q", got, "10.42.0.7")
	}
}

func TestManagedProxyURLAutoSubstitutesLoopback(t *testing.T) {
	state := stateWithManagedProxy()
	state.Hosts[0].Spec.Addresses = []v1alpha1.HostAddress{{Name: "ssh", Address: "localhost"}}
	state.Hosts[0].Spec.SSH = &v1alpha1.HostSSHSpec{AddressName: "ssh"}
	state.NetworkConfigs = []v1alpha1.NetworkConfig{{
		Metadata: v1alpha1.Metadata{Name: "bridge-net"},
		Spec:     networkConfigSpec("192.168.132.0/24", "192.168.132.1"),
	}}
	state.ClusterInfras[0].Spec.Endpoints = map[string]v1alpha1.Endpoint{
		v1alpha1.EndpointAPI: {ExternalVIP: "192.168.132.10"},
	}
	state.ClusterInfras[0].Spec.Components.Machines = []v1alpha1.ClusterMachineComponent{{
		Name: "master-0",
		NetworkConfig: v1alpha1.ClusterMachineNetworkConfig{
			Ref: v1alpha1.LocalObjectReference{Name: "bridge-net"},
		},
	}}
	got, err := ManagedProxyURL(state, state.ClusterInfras[0])
	if err != nil {
		t.Fatalf("ManagedProxyURL: %v", err)
	}
	want := "http://192.168.132.1:3128"
	if got != want {
		t.Errorf("ManagedProxyURL = %q, want %q", got, want)
	}
}

func networkConfigSpec(cidr, gateway string) v1alpha1.NetworkConfigSpec {
	config := map[string]any{}
	if gateway != "" {
		config["routes"] = map[string]any{"config": []any{
			map[string]any{
				"destination":        "0.0.0.0/0",
				"next-hop-address":   gateway,
				"next-hop-interface": "bond0",
				"table-id":           254,
			},
		}}
	}
	return v1alpha1.NetworkConfigSpec{
		MachineNetwork: []v1alpha1.MachineNetworkCIDR{{CIDR: cidr}},
		Template:       v1alpha1.NetworkConfigTemplate{NetworkConfig: config},
	}
}

func TestHostRouteAddressMissingNamedAddress(t *testing.T) {
	state := clusterFacingState("localhost", "10.42.0.7", "192.168.132.1")
	got := HostRouteAddress(state, "lab-host", "missing", state.ClusterInfras[0])
	if got != "" {
		t.Fatalf("HostRouteAddress = %q, want empty", got)
	}
}
