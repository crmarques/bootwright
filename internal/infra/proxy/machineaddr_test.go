package proxy

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func clusterFacingState(hostSSH, hostClusterAddr, networkGateway string) v1alpha1.State {
	state := v1alpha1.State{
		Machines: []v1alpha1.Machine{{
			Metadata: v1alpha1.Metadata{Name: "lab-host"},
			Spec: v1alpha1.MachineSpec{
				OS: v1alpha1.MachineOSSpec{
					Provided: v1alpha1.BoolPtr(true),
				},
				Addresses: []v1alpha1.MachineAddress{
					{Name: "ssh", Address: hostSSH},
					{Name: "cluster", Address: hostClusterAddr},
				},
				Access: v1alpha1.MachineAccess{
					SSH: &v1alpha1.MachineSSHSpec{AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"}},
				},
			},
		}},
		NetworkConfigs: []v1alpha1.NetworkConfig{{
			Metadata: v1alpha1.Metadata{Name: "bridge-net"},
			Spec:     networkConfigSpec("192.168.132.0/24", networkGateway),
		}},
	}
	return state
}

func clusterFacingInfra() v1alpha1.ClusterInstall {
	return v1alpha1.ClusterInstall{
		Metadata: v1alpha1.Metadata{Name: "c1"},
		Endpoints: map[string]v1alpha1.Endpoint{
			v1alpha1.EndpointAPI: {Address: "192.168.132.10"},
		},
		Machines: []v1alpha1.InstallMachine{{
			Name: "master-0",
			Network: v1alpha1.MachineNetworkConfig{
				NetworkConfigRef: v1alpha1.LocalObjectReference{Name: "bridge-net"},
			},
		}},
	}
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
			got := ClusterFacingMachineAddress(state, "lab-host", clusterFacingInfra())
			if got != tc.want {
				t.Fatalf("ClusterFacingMachineAddress = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClusterFacingHostAddressUnknownHost(t *testing.T) {
	state := clusterFacingState("10.0.0.5", "", "192.168.132.1")
	if got := ClusterFacingMachineAddress(state, "no-such-host", clusterFacingInfra()); got != "" {
		t.Fatalf("unknown host: got %q, want empty", got)
	}
}

func TestClusterFacingHostAddressFallsBackThroughMachineInterface(t *testing.T) {
	// No endpoints declared — the helper must still find a primary
	// network via the first machine's first interface.
	state := clusterFacingState("localhost", "", "192.168.132.1")
	infra := clusterFacingInfra()
	infra.Endpoints = nil
	infra.Machines = []v1alpha1.InstallMachine{{
		Name: "master-0",
		Network: v1alpha1.MachineNetworkConfig{
			NetworkConfigRef: v1alpha1.LocalObjectReference{Name: "bridge-net"},
		},
	}}
	got := ClusterFacingMachineAddress(state, "lab-host", infra)
	if got != "192.168.132.1" {
		t.Fatalf("got %q, want %q (gateway via machine interface fallback)", got, "192.168.132.1")
	}
}

func TestHostRouteAddressUsesNamedAddress(t *testing.T) {
	state := clusterFacingState("localhost", "10.42.0.7", "192.168.132.1")
	got := MachineRouteAddress(state, "lab-host", "cluster", clusterFacingInfra())
	if got != "10.42.0.7" {
		t.Fatalf("MachineRouteAddress = %q, want %q", got, "10.42.0.7")
	}
}

func TestManagedProxyURLAutoSubstitutesLoopback(t *testing.T) {
	state := stateWithManagedProxy()
	state.Machines[0].Spec.Addresses = []v1alpha1.MachineAddress{{Name: "ssh", Address: "localhost"}}
	state.Machines[0].Spec.Access.SSH = &v1alpha1.MachineSSHSpec{AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"}}
	state.NetworkConfigs = []v1alpha1.NetworkConfig{{
		Metadata: v1alpha1.Metadata{Name: "bridge-net"},
		Spec:     networkConfigSpec("192.168.132.0/24", "192.168.132.1"),
	}}
	infra := clusterFacingInfra()
	infra.Endpoints = map[string]v1alpha1.Endpoint{
		v1alpha1.EndpointAPI: {Address: "192.168.132.10"},
	}
	infra.Machines = []v1alpha1.InstallMachine{{
		Name: "master-0",
		Network: v1alpha1.MachineNetworkConfig{
			NetworkConfigRef: v1alpha1.LocalObjectReference{Name: "bridge-net"},
		},
	}}
	got, err := ManagedProxyURL(state, infra)
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

func TestMachineRouteAddressMissingNamedAddress(t *testing.T) {
	state := clusterFacingState("localhost", "10.42.0.7", "192.168.132.1")
	got := MachineRouteAddress(state, "lab-host", "missing", clusterFacingInfra())
	if got != "" {
		t.Fatalf("MachineRouteAddress = %q, want empty", got)
	}
}
