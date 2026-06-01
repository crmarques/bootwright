package render

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
	ci := v1alpha1.ClusterInfra{
		Spec: v1alpha1.ClusterInfraSpec{
			Endpoints: map[string]v1alpha1.Endpoint{
				v1alpha1.EndpointAPI: {Address: "192.168.140.10"},
				"apps":               {Address: "192.168.140.11"},
			},
			Components: v1alpha1.ClusterComponents{
				Machines: []v1alpha1.ClusterMachineComponent{
					{
						Name: "master-0",
						NetworkConfig: v1alpha1.ClusterMachineNetworkConfig{
							Ref: v1alpha1.LocalObjectReference{Name: "machine-net"},
						},
					},
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
