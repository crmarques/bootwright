package installer

import (
	"reflect"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestMachineNetworkConfigUsesMachineNetworkRefsOnly(t *testing.T) {
	state := v1alpha1.State{
		NetworkConfigs: []v1alpha1.NetworkConfig{
			{
				Metadata: v1alpha1.Metadata{Name: "machine-net"},
				Spec: v1alpha1.NetworkConfigSpec{
					MachineNetwork: []v1alpha1.MachineNetworkCIDR{{CIDR: "192.168.132.0/24"}},
				},
			},
			{
				Metadata: v1alpha1.Metadata{Name: "vip-net"},
				Spec: v1alpha1.NetworkConfigSpec{
					MachineNetwork: []v1alpha1.MachineNetworkCIDR{{CIDR: "192.168.140.0/24"}},
				},
			},
		},
	}
	ci := v1alpha1.ClusterInstall{
		Endpoints: map[string]v1alpha1.Endpoint{
			v1alpha1.EndpointAPI: {Address: "192.168.140.10"},
			"apps":               {Address: "192.168.140.11"},
		},
		Machines: []v1alpha1.InstallMachine{
			{
				Name: "master-0",
				Network: v1alpha1.MachineNetworkConfig{
					NetworkConfigRef: v1alpha1.LocalObjectReference{Name: "machine-net"},
				},
			},
		},
	}

	got := machineNetworkConfig(state, ci)
	want := []any{
		map[string]any{"cidr": "192.168.132.0/24"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("machineNetworkConfig = %#v, want %#v", got, want)
	}
}

// TestNetworkConfigInterfaceNamesSkipsVirtualInterfaces pins that only
// physical (ethernet/untyped) interfaces get a name that later drives MAC
// generation and VM-NIC fabrication; bond/vlan (and other virtual) interfaces
// are created inside the guest by NMState and must not become substrate NICs.
func TestNetworkConfigInterfaceNamesSkipsVirtualInterfaces(t *testing.T) {
	config := map[string]any{
		"interfaces": []any{
			map[string]any{"name": "eno1", "type": "ethernet"},
			map[string]any{"name": "eno2", "type": "ethernet"},
			map[string]any{"name": "bond0", "type": "bond"},
			map[string]any{"name": "bond0.151", "type": "vlan"},
			map[string]any{"name": "untyped"}, // treated as physical
		},
	}
	got := networkConfigInterfaceNames(config)
	want := []string{"eno1", "eno2", "untyped"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("networkConfigInterfaceNames = %#v, want %#v", got, want)
	}
}

func TestMachineNetworkConfigUsesInlineMachineSpecs(t *testing.T) {
	state := v1alpha1.State{}
	ci := v1alpha1.ClusterInstall{
		Machines: []v1alpha1.InstallMachine{{
			Name: "master-0",
			Network: v1alpha1.MachineNetworkConfig{
				Spec: &v1alpha1.NetworkConfigSpec{
					MachineNetwork: []v1alpha1.MachineNetworkCIDR{{CIDR: "192.168.132.0/24"}},
				},
			},
		}},
	}

	got := machineNetworkConfig(state, ci)
	want := []any{
		map[string]any{"cidr": "192.168.132.0/24"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("machineNetworkConfig = %#v, want %#v", got, want)
	}
}
