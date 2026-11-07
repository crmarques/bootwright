package libvirt

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestPerInterfaceLibvirtSegmentsAndMTURequestGolden(t *testing.T) {
	p := provider(field("segments", api.ListValue(api.MapValue(text("name", "storage"), number("mtu", "9000"), field("libvirt", api.MapValue(text("bridge", "br-storage")))))))
	n := networkConfig()
	interfaces := n.Spec().Get("nmstate", "interfaces").Items()
	interfaces = append(interfaces, api.MapValue(text("name", "enp2s0"), text("type", "ethernet"), text("state", "up")))
	n = n.WithSpec(n.Spec().WithPath(api.ListValue(interfaces...), "nmstate", "interfaces"))
	g := guest("rhel-01")
	g = g.WithSpec(g.Spec().WithPath(api.ListValue(
		api.MapValue(text("nicRef", "enp1s0"), text("segmentRef", "lab-guests")),
		api.MapValue(text("nicRef", "enp2s0"), text("segmentRef", "storage")),
	), "substrate", "libvirt", "nics").WithPath(api.ListValue(api.MapValue(text("name", "enp1s0")), api.MapValue(text("name", "enp2s0"))), "hardware", "nics"))
	requests, err := MachineRequests(catalogOf(controller(), p, n, g), "controller", testContext)
	if err != nil || len(requests) != 1 {
		t.Fatalf("requests: %v %v", requests, err)
	}
	got := requests[0].Interfaces
	if len(got) != 2 || got[0].Network == "" || got[1].Network != "" || got[1].Bridge != "br-storage" || got[1].MTU != 9000 {
		t.Fatalf("interfaces: %+v", got)
	}
	canonical, err := requests[0].Canonical()
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	matchesGolden(t, "request-machine-attachments-mtu", canonical)
	if _, err := DecodeMachineRequest(canonical); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
}

func TestIPv6LibvirtNetworkMTURequestGolden(t *testing.T) {
	requests, err := HostRequests(catalogOf(controller(), provider(), managedNetwork("lab-guests", "virbr-lab", "2001:db8:42::1/64", number("mtu", "9000"))), "controller", testContext)
	if err != nil || len(requests) != 1 {
		t.Fatalf("requests: %v %v", requests, err)
	}
	canonical, err := requests[0].Canonical()
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	matchesGolden(t, "request-host-ipv6-mtu", canonical)
	if _, err := DecodeHostRequest(canonical); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
}

// A segment's capacity is emitted only where it is not the 1500 default, for an
// existing bridge as for a managed Network, so a materialized default moves no
// host or machine request.
func TestADefaultSegmentCapacityIsNotEmitted(t *testing.T) {
	p := provider(field("segments", api.ListValue(api.MapValue(text("name", "storage"), number("mtu", "1500"), field("libvirt", api.MapValue(text("bridge", "br-storage")))))))
	hosts, err := HostRequests(catalogOf(controller(), p), "controller", testContext)
	if err != nil || len(hosts) != 1 {
		t.Fatalf("hosts: %v %v", hosts, err)
	}
	found := false
	for _, network := range hosts[0].Networks {
		found = found || network.Bridge == "br-storage"
		if network.MTU != 0 {
			t.Fatalf("network %s emits the default MTU %d", network.Name, network.MTU)
		}
	}
	if !found {
		t.Fatalf("networks %+v hold no br-storage segment", hosts[0].Networks)
	}
}
