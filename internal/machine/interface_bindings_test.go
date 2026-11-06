package machine

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestInterfaceBindingsResolveDerivedAuthoredAndAbsentBindings(t *testing.T) {
	server, catalog := fixture()
	if got := InterfaceBindings(server, catalog); !slices.Equal(got, []InterfaceBinding{{NIC: "eth0", Interface: "eth0"}}) {
		t.Fatalf("derived identity binding = %v", got)
	}
	relabelled := server.WithSpec(server.Spec().
		WithPath(list(m("nicRef", "nic0", "macAddress", "02:00:00:00:00:01")), "substrate", "baremetal", "nics").
		WithPath(list(m("name", "nic0")), "hardware", "nics").
		WithPath(list(m("nicRef", "nic0", "interfaceName", "eth0")), "network", "interfaceBinding"))
	if got := InterfaceBindings(relabelled, catalog); !slices.Equal(got, []InterfaceBinding{{NIC: "nic0", Interface: "eth0"}}) {
		t.Fatalf("authored binding = %v", got)
	}
	lab := libvirtLab(list(m("name", "enp1s0", "type", "ethernet")))
	virtual := guest(api.Value{})
	if got := InterfaceBindings(virtual, api.NewCatalog(append(lab, virtual))); len(got) != 0 {
		t.Fatalf("a libvirt Machine has no NIC to bind, got %v", got)
	}
}
