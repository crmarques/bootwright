package libvirt

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestPerInterfaceLibvirtAttachmentsAndMTURequestGolden(t *testing.T) {
	p := provider(field("networkAttachments", api.ListValue(api.MapValue(text("name", "storage"), field("libvirt", api.MapValue(text("bridge", "br-storage"), number("mtu", "9000")))))))
	n := networkConfig()
	interfaces := n.Spec().Get("nmstate", "interfaces").Items()
	interfaces = append(interfaces, api.MapValue(text("name", "enp2s0"), text("type", "ethernet"), text("state", "up")))
	n = n.WithSpec(n.Spec().WithPath(api.ListValue(interfaces...), "nmstate", "interfaces"))
	g := guest("rhel-01")
	g = g.WithSpec(g.Spec().WithPath(api.ListValue(
		api.MapValue(text("nicRef", "enp1s0"), text("attachmentRef", "lab-guests")),
		api.MapValue(text("nicRef", "enp2s0"), text("attachmentRef", "storage")),
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
