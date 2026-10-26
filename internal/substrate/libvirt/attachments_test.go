package libvirt

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestPerInterfaceLibvirtAttachmentsAndMTURequestGolden(t *testing.T) {
	p := provider()
	attachments := p.Spec().Get("networkAttachments").Items()
	attachments = append(attachments, api.MapValue(text("name", "storage"), field("libvirt", api.MapValue(text("bridge", "br-storage"), text("management", "external"), number("mtu", "9000")))))
	p = p.WithSpec(p.Spec().With("networkAttachments", api.ListValue(attachments...)))
	n := networkConfig()
	interfaces := n.Spec().Get("nmstate", "interfaces").Items()
	interfaces = append(interfaces, api.MapValue(text("name", "enp2s0"), text("type", "ethernet"), text("state", "up")))
	n = n.WithSpec(n.Spec().WithPath(api.ListValue(interfaces...), "nmstate", "interfaces"))
	g := guest("rhel-01")
	g = g.WithSpec(g.Spec().With("network", g.Spec().Get("network").Without("attachmentRef").With("interfaceAttachments", api.ListValue(
		api.MapValue(text("interface", "enp1s0"), text("attachmentRef", "lab-guests")),
		api.MapValue(text("interface", "enp2s0"), text("attachmentRef", "storage")),
	))))
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
	p := provider()
	attachment := p.Spec().Get("networkAttachments").Items()[0]
	attachment = attachment.With("libvirt", attachment.Get("libvirt").With("address", api.StringValue("2001:db8:42::1/64")).With("mtu", api.IntegerValue("9000")))
	p = p.WithSpec(p.Spec().With("networkAttachments", api.ListValue(attachment)))
	requests, err := HostRequests(catalogOf(controller(), p), "controller", testContext)
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
