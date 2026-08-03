package desiredstate

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func cloneSeedMachine(config v1alpha1.MachineNetworkConfig) v1alpha1.Machine {
	return v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: "ceph-arbiter-0"},
		Spec: v1alpha1.MachineSpec{
			Network: v1alpha1.MachineNetwork{Config: config},
		},
	}
}

func cloneSeedRoutedNetworkConfig() map[string]any {
	return map[string]any{
		"interfaces": []any{
			map[string]any{
				"name": "ens192", "type": "ethernet",
				"ipv4": map[string]any{
					"enabled": true,
					"address": []any{map[string]any{"ip": "10.20.30.41", "prefix-length": 24}},
				},
			},
		},
		"routes": map[string]any{
			"config": []any{map[string]any{
				"destination":        "0.0.0.0/0",
				"next-hop-address":   "10.20.30.1",
				"next-hop-interface": "ens192",
			}},
		},
	}
}

func TestCloneSeedNetworkRefusesAMachineWithNoNetworkConfig(t *testing.T) {
	errs := validateMachineCloneSeedNetwork(cloneSeedMachine(v1alpha1.MachineNetworkConfig{}), nil)
	if len(errs) != 1 {
		t.Fatalf("errs = %v, want one refusal: a clone with no network config has nothing to seed", errs)
	}
	for _, want := range []string{"resolves no network config at all", "spec.network.config.networkConfigRef", "installer.anaconda"} {
		if !strings.Contains(errs[0], want) {
			t.Fatalf("missing %q in %q", want, errs[0])
		}
	}
}

func TestCloneSeedNetworkRefusesADanglingNetworkConfigRef(t *testing.T) {
	config := v1alpha1.MachineNetworkConfig{NetworkConfigRef: v1alpha1.LocalObjectReference{Name: "missing"}}
	if errs := validateMachineCloneSeedNetwork(cloneSeedMachine(config), map[string]v1alpha1.NetworkConfig{}); len(errs) != 1 {
		t.Fatalf("errs = %v, want one refusal: an unresolvable networkConfigRef seeds nothing either", errs)
	}
}

func TestCloneSeedNetworkAcceptsAnInlineStaticPrimary(t *testing.T) {
	config := v1alpha1.MachineNetworkConfig{
		Spec: &v1alpha1.NetworkConfigSpec{
			Template: v1alpha1.NetworkConfigTemplate{NetworkConfig: cloneSeedRoutedNetworkConfig()},
		},
	}
	if errs := validateMachineCloneSeedNetwork(cloneSeedMachine(config), nil); len(errs) != 0 {
		t.Fatalf("errs = %v, want an inline static IPv4 primary accepted", errs)
	}
}

func TestCloneSeedNetworkRefusesANetworkConfigWithoutAStaticPrimary(t *testing.T) {
	networkConfig := cloneSeedRoutedNetworkConfig()
	networkConfig["interfaces"].([]any)[0].(map[string]any)["ipv4"] = map[string]any{"enabled": true, "dhcp": true}
	config := v1alpha1.MachineNetworkConfig{NetworkConfigRef: v1alpha1.LocalObjectReference{Name: "ceph-public"}}
	networks := map[string]v1alpha1.NetworkConfig{"ceph-public": {
		Metadata: v1alpha1.Metadata{Name: "ceph-public"},
		Spec: v1alpha1.NetworkConfigSpec{
			Template: v1alpha1.NetworkConfigTemplate{NetworkConfig: networkConfig},
		},
	}}
	errs := validateMachineCloneSeedNetwork(cloneSeedMachine(config), networks)
	if len(errs) != 1 || !strings.Contains(errs[0], "NetworkConfig/ceph-public") {
		t.Fatalf("errs = %v, want one refusal naming the NetworkConfig that must declare the address", errs)
	}
}
