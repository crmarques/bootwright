package containercluster

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestVirtualIPsOnlyProbeManagedIPv4NetworksOnController(t *testing.T) {
	for _, tc := range []struct {
		name, controller, management, address string
		ipv6                                  bool
		want                                  string
	}{
		{"local managed", "host", "managed", "192.0.2.1/24", false, "br-test"},
		{"remote managed", "other", "managed", "192.0.2.1/24", false, ""},
		{"external", "host", "external", "192.0.2.1/24", false, ""},
		{"outside prefix", "host", "managed", "198.51.100.1/24", false, ""},
		{"IPv6", "host", "managed", "2001:db8::1/64", true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cluster, catalog := fixture("libvirt", 3, tc.ipv6)
			for _, slot := range []string{"api", "ingress"} {
				cluster = cluster.WithSpec(cluster.Spec().WithPath(m("type", "openshift"), "install", "endpoints", slot, "source"))
			}
			cluster, _ = Normalize(cluster, catalog)
			provider := obj(api.InfraProvider, "network", m("libvirt", m("machineRef", tc.controller), "networkAttachments", list(m("libvirt", m("management", tc.management, "address", tc.address, "bridge", "br-test")))))
			catalog = api.NewCatalog(append(catalog.Objects(), provider))
			ips := VirtualIPs(catalog, cluster, "host")
			if len(ips) != 3 {
				t.Fatalf("VIPs = %+v", ips)
			}
			for i, ip := range ips {
				if ip.Slot != endpointSlots[i] || ip.Bridge != tc.want {
					t.Fatalf("VIP = %+v", ip)
				}
			}
			cluster = cluster.WithSpec(cluster.Spec().WithPath(api.StringValue("none"), "install", "platform", "type"))
			if ips := VirtualIPs(catalog, cluster, "host"); len(ips) != 0 {
				t.Fatalf("platform none VIPs = %+v", ips)
			}
		})
	}
}
