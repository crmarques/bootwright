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
