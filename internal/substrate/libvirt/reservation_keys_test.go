package libvirt

import (
	"slices"
	"testing"
)

// TestEveryLibvirtReservationKeyKeepsItsText pins each key the host and
// Machine requests claim, byte for byte, with every branch that adds one.
func TestEveryLibvirtReservationKeyKeepsItsText(t *testing.T) {
	host := HostRequest{
		PoolPath: "/var/lib/bootwright/pool", PoolName: "lab-pool",
		FirewallReadyUnit: "bootwright-lab-firewall.service", VLANRefusalUnit: "bootwright-lab-vlan.service",
		Networks: []Network{
			{Name: "lab-net", Bridge: "virbr-lab", Managed: true, Address: "192.0.2.1/24"},
			{Name: "shared", Bridge: "br-shared"},
		},
		Routing: &Routing{Unit: "bootwright-lab-routing.service"},
		Trunks:  []Trunk{{Name: "trunk-160", Bridge: "br-trunk"}},
		Legs:    []Leg{{Name: "leg-160", Device: "br-leg", CIDR: "203.0.113.193/26"}},
	}
	want := []string{
		"bridge:br-leg", "bridge:br-trunk", "bridge:virbr-lab", "libvirt-network:lab-net", "libvirt-pool:lab-pool",
		"nm-connection:leg-160", "nm-connection:trunk-160", "path:/var/lib/bootwright/pool", "prefix:192.0.2.0/24",
		"prefix:203.0.113.192/26", "unit:bootwright-lab-firewall", "unit:bootwright-lab-routing", "unit:bootwright-lab-vlan",
	}
	if got := host.ReservationKeys(); !slices.Equal(got, want) {
		t.Errorf("host keys = %q, want %q", got, want)
	}
	if got := host.SharedReservationKeys(); !slices.Equal(got, []string{"bridge-use:br-shared"}) {
		t.Errorf("shared host keys = %q", got)
	}
	machine := MachineRequest{Domain: "lab-node-01", Directory: "/var/lib/bootwright/lab-node-01",
		Controller: Controller{Unit: "bootwright-lab-bmc", Address: "2001:db8::1", Port: 8443}}
	want = []string{"libvirt-domain:lab-node-01", "path:/var/lib/bootwright/lab-node-01", "socket:2001:db8::1:8443", "unit:bootwright-lab-bmc"}
	if got := machine.ReservationKeys(); !slices.Equal(got, want) {
		t.Errorf("machine keys = %q, want %q", got, want)
	}
}
