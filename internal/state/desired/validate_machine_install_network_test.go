package desiredstate

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func installNetworkIface(name, kind, family, ip string) map[string]any {
	entry := map[string]any{"name": name}
	if kind != "" {
		entry["type"] = kind
	}
	if ip != "" {
		entry[family] = map[string]any{
			"enabled": true,
			"address": []any{map[string]any{"ip": ip, "prefix-length": 24}},
		}
	}
	return entry
}

func installNetworkConfig(defaultRoute string, interfaces ...map[string]any) map[string]any {
	raw := make([]any, 0, len(interfaces))
	for _, entry := range interfaces {
		raw = append(raw, entry)
	}
	config := map[string]any{"interfaces": raw}
	if defaultRoute != "" {
		config["routes"] = map[string]any{"config": []any{map[string]any{
			"destination":        "0.0.0.0/0",
			"next-hop-interface": defaultRoute,
			"next-hop-address":   "192.168.140.1",
		}}}
	}
	return config
}

func installingMachine(sshAddress string) v1alpha1.Machine {
	provided := false
	return v1alpha1.Machine{
		Metadata: v1alpha1.Metadata{Name: "node-0"},
		Spec: v1alpha1.MachineSpec{
			OS: v1alpha1.MachineOSSpec{
				Provided:          &provided,
				InstallProfileRef: v1alpha1.LocalObjectReference{Name: "rhel-9"},
			},
			Addresses: []v1alpha1.MachineAddress{{Name: "ssh", Address: sshAddress}},
			Access: v1alpha1.MachineAccess{SSH: &v1alpha1.MachineSSHSpec{
				AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"},
			}},
		},
	}
}

func TestValidateMachineInstallNetworkRefusesInexpressibleInstallNetworks(t *testing.T) {
	const owner = "Machine/node-0 spec.network.config"
	cases := []struct {
		name    string
		machine v1alpha1.Machine
		config  map[string]any
		want    string
	}{
		{
			name:    "static ipv4 ethernet",
			machine: installingMachine("192.168.140.20"),
			config:  installNetworkConfig("eno1", installNetworkIface("eno1", "ethernet", "ipv4", "192.168.140.20")),
		},
		{
			name:    "vlan over bond",
			machine: installingMachine("192.168.140.20"),
			config: installNetworkConfig("bond0.140",
				installNetworkIface("eno1", "ethernet", "", ""),
				installNetworkIface("bond0", "bond", "", ""),
				installNetworkIface("bond0.140", "vlan", "ipv4", "192.168.140.20")),
		},
		{
			name:    "dhcp everywhere",
			machine: installingMachine("192.168.140.20"),
			config:  installNetworkConfig("", installNetworkIface("eno1", "ethernet", "", "")),
		},
		{
			name:    "ipv6 only",
			machine: installingMachine("2001:db8::20"),
			config:  installNetworkConfig("eno1", installNetworkIface("eno1", "ethernet", "ipv6", "2001:db8::20")),
			want:    owner + ` carries no IPv4 address; interface "eno1" is addressed 2001:db8::20 only, and an Anaconda network directive expresses IPv4 static addressing or DHCP and nothing else, so the install would run ` + "`network --device=link --bootproto=dhcp`" + ` and the machine would never answer at its authored address once its disk had been wiped. Give the install interface an IPv4 address, or set spec.os.provided: true to hand this machine to your own OS install`,
		},
		{
			name:    "bridge primary",
			machine: installingMachine("192.168.140.20"),
			config: installNetworkConfig("br0",
				installNetworkIface("eno1", "ethernet", "", ""),
				installNetworkIface("br0", "linux-bridge", "ipv4", "192.168.140.20")),
			want: owner + ` makes interface "br0" the install primary, but its type "linux-bridge" is not one an Anaconda network directive can express (ethernet, vlan, bond); the install would never create it and the machine would never answer at its authored address once its disk had been wiped. Carry the install address on an ethernet, vlan, or bond interface, or set spec.os.provided: true to hand this machine to your own OS install`,
		},
		{
			name:    "second addressed interface behind the reachable primary",
			machine: installingMachine("192.168.140.20"),
			config: installNetworkConfig("bond0.140",
				installNetworkIface("bond0.140", "vlan", "ipv4", "192.168.140.20"),
				installNetworkIface("bond0.141", "vlan", "ipv4", "192.168.141.20")),
		},
		{
			name:    "reachable address on a second interface the install never brings up",
			machine: installingMachine("192.168.141.20"),
			config: installNetworkConfig("bond0.140",
				installNetworkIface("bond0.140", "vlan", "ipv4", "192.168.140.20"),
				installNetworkIface("bond0.141", "vlan", "ipv4", "192.168.141.20")),
			want: owner + ` addresses 2 interfaces (bond0.140, bond0.141) but an Anaconda network directive brings up one, and the install primary "bond0.140" does not carry "192.168.141.20", the address Bootwright reaches this machine at after the install; the install would wipe the disk of a machine that then never answers. Move that address onto "bond0.140", or give its interface the default route so the install brings that interface up, or set spec.os.provided: true to hand this machine to your own OS install`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := validateMachineInstallNetwork(owner, tc.machine, tc.config)
			if tc.want == "" {
				if len(errs) != 0 {
					t.Fatalf("expressible install network rejected: %v", errs)
				}
				return
			}
			if len(errs) != 1 {
				t.Fatalf("errs = %v, want exactly one refusal", errs)
			}
			if errs[0] != tc.want {
				t.Errorf("refusal =\n%q\nwant\n%q", errs[0], tc.want)
			}
		})
	}
}

func TestValidateMachineInstallNetworkOnlyGuardsMachinesBootwrightInstalls(t *testing.T) {
	const owner = "Machine/node-0 spec.network.config"
	config := installNetworkConfig("br0", installNetworkIface("br0", "linux-bridge", "ipv4", "192.168.140.20"))

	provided := installingMachine("192.168.140.20")
	yes := true
	provided.Spec.OS.Provided = &yes
	provided.Spec.OS.InstallProfileRef = v1alpha1.LocalObjectReference{}
	if errs := validateMachineInstallNetwork(owner, provided, config); len(errs) != 0 {
		t.Fatalf("os.provided=true machine rejected: %v", errs)
	}

	coreOS := installingMachine("192.168.140.20")
	coreOS.Spec.OS.InstallProfileRef = v1alpha1.LocalObjectReference{}
	if errs := validateMachineInstallNetwork(owner, coreOS, config); len(errs) != 0 {
		t.Fatalf("machine without an install profile rejected: %v", errs)
	}
}
