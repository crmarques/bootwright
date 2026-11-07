package machine

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestInterfaceMatchesResolveDefaultedAndAuthoredMatches(t *testing.T) {
	server, catalog := fixture()
	if got := InterfaceMatches(server, catalog); !slices.Equal(got, []InterfaceMatch{{NIC: "eth0", Interface: "eth0"}}) {
		t.Fatalf("defaulted match = %v", got)
	}
	relabelled := server.WithSpec(server.Spec().
		WithPath(list(m("nicRef", "nic0", "macAddress", "02:00:00:00:00:01")), "substrate", "baremetal", "nics").
		WithPath(list(m("name", "nic0")), "hardware", "nics").
		WithPath(list(m("interface", "eth0", "nicRef", "nic0")), "network", "interfaceMatches"))
	if got := InterfaceMatches(relabelled, catalog); !slices.Equal(got, []InterfaceMatch{{NIC: "nic0", Interface: "eth0"}}) {
		t.Fatalf("authored match = %v", got)
	}
	lab := libvirtLab(list(m("name", "enp1s0", "type", "ethernet")))
	virtual := guest(api.Value{}).WithSpec(guest(api.Value{}).Spec().With("network", m("configRef", "net")).WithPath(list(m("name", "vnic0")), "hardware", "nics").
		WithPath(list(m("interface", "enp1s0", "nicRef", "vnic0")), "network", "interfaceMatches"))
	if got := InterfaceMatches(virtual, api.NewCatalog(append(lab, virtual))); !slices.Equal(got, []InterfaceMatch{{NIC: "vnic0", Interface: "enp1s0"}}) {
		t.Fatalf("a libvirt Machine matches its interface to its vNIC, got %v", got)
	}
}
