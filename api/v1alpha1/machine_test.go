package v1alpha1

import "testing"

func TestMachineNetworkConfigIsZero(t *testing.T) {
	cases := []struct {
		name string
		cfg  MachineNetworkConfig
		want bool
	}{
		{"empty", MachineNetworkConfig{}, true},
		{"networkConfigRef", MachineNetworkConfig{NetworkConfigRef: LocalObjectReference{Name: "n"}}, false},
		{"attachmentRef", MachineNetworkConfig{AttachmentRef: LocalObjectReference{Name: "a"}}, false},
		{"overrides", MachineNetworkConfig{Overrides: map[string]any{"k": "v"}}, false},
		{"spec", MachineNetworkConfig{Spec: &NetworkConfigSpec{}}, false},
		{"interfaceAddresses", MachineNetworkConfig{InterfaceAddresses: []MachineInterfaceAddress{{Interface: "eth0"}}}, false},
	}
	for _, tc := range cases {
		if got := tc.cfg.IsZero(); got != tc.want {
			t.Errorf("%s: IsZero() = %v, want %v", tc.name, got, tc.want)
		}
	}
}
