package openshift

import (
	"net/netip"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestEndpointOwnershipAcrossCatalog(t *testing.T) {
	cluster, catalog := fixture("libvirt", 3, false)
	endpoint := m("address", "192.0.2.2", "source", m("type", "external"))
	cases := []struct {
		name   string
		holder api.Object
		field  string
	}{
		{"cluster", obj(api.OpenShiftCluster, "other", m("install", m("endpoints", m("api", endpoint)))), "spec.install.endpoints.api.address"},
		{"machine", obj(api.Machine, "other", m("network", m("addresses", list(m("address", "192.0.2.2/24"))))), "spec.network.addresses[0].address"},
		{"network host", obj(api.InfraProvider, "other", m("networkAttachments", list(m("libvirt", m("management", "managed", "address", "192.0.2.2/24"))))), "spec.networkAttachments[0].libvirt.address"},
		{"BMC", obj(api.InfraProvider, "other", m("libvirt", m("bmcEmulation", m("bindAddress", "192.0.2.2")))), "spec.libvirt.bmcEmulation.bindAddress"},
		{"load balancer", obj(api.LoadBalancer, "other", m("bindAddresses", list(m("address", "192.0.2.2")))), "spec.bindAddresses[0].address"},
	}
	for _, kind := range []api.Kind{api.DNSServer, api.NTPServer, api.Proxy, api.ArtifactServer, api.Registry} {
		cases = append(cases, struct {
			name   string
			holder api.Object
			field  string
		}{string(kind) + " bind", obj(kind, "other", m("management", "managed", "bindAddress", "192.0.2.2")), "spec.bindAddress"})
		cases = append(cases, struct {
			name   string
			holder api.Object
			field  string
		}{string(kind) + " endpoint", obj(kind, "other", m("management", "managed", "machineRef", "service-host", "endpoints", list(m("addressRef", "service")))), "spec.endpoints[0].addressRef"})
	}
	host := obj(api.Machine, "service-host", m("network", m("addresses", list(m("name", "service", "address", "192.0.2.2/24")))))
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := api.NewCatalog(append(catalog.Objects(), tc.holder, host))
			issues := endpointCollisions(cluster, c, "api", endpoint, netip.MustParseAddr("192.0.2.2"))
			found := false
			for _, issue := range issues {
				if strings.Contains(issue.Message, tc.holder.Identity()) && strings.Contains(issue.Message, tc.field) && issue.Field == "$.spec.install.endpoints.api.address" {
					found = true
				}
			}
			if !found {
				t.Fatalf("holder not named: %+v", issues)
			}
		})
	}
}

func TestEndpointOwnershipExceptions(t *testing.T) {
	cluster, catalog := fixture("libvirt", 1, false)
	cluster, _ = Normalize(cluster, catalog)
	catalog = api.NewCatalog(append(catalog.Objects(), cluster, obj(api.Proxy, "wildcard", m("management", "managed", "bindAddress", "0.0.0.0"))))
	if issues := validateEndpoints(cluster, catalog); len(issues) != 0 {
		t.Fatal(issues)
	}
	cluster, catalog = fixture("libvirt", 3, false)
	cluster, _ = Normalize(cluster, catalog)
	catalog = api.NewCatalog(append(catalog.Objects(), cluster))
	if issues := validateEndpoints(cluster, catalog); len(issues) != 0 {
		t.Fatal(issues)
	}
}

func TestInstallerEndpointPlatformOwnership(t *testing.T) {
	for _, source := range []string{"installer", "external", "loadBalancer"} {
		t.Run(source, func(t *testing.T) {
			cluster, catalog := fixture("libvirt", 3, false)
			for _, slot := range []string{"api", "ingress"} {
				cluster = cluster.WithSpec(cluster.Spec().WithPath(m("type", source), "install", "endpoints", slot, "source"))
			}
			cluster, _ = Normalize(cluster, catalog)
			want := "none"
			if source == "installer" {
				want = "baremetal"
			}
			if got := Platform(cluster.Spec()); got != want {
				t.Fatalf("platform %s, want %s", got, want)
			}
		})
	}
	cluster, catalog := fixture("libvirt", 3, false)
	cluster = cluster.WithSpec(cluster.Spec().WithPath(m("type", "installer"), "install", "endpoints", "api", "source"))
	cluster, _ = Normalize(cluster, catalog)
	if issues := validateEndpoints(cluster, catalog); !hasField(issues, "$.spec.install.endpoints") || !hasField(issues, "$.spec.install.endpoints.ingress.source.type") {
		t.Fatal(issues)
	}
	for _, slot := range []string{"api-int", "ingress"} {
		bad := cluster.WithSpec(cluster.Spec().WithPath(api.StringValue("192.0.2.4"), "install", "endpoints", "api-int", "address"))
		if slot == "ingress" {
			bad = cluster.WithSpec(cluster.Spec().WithPath(api.StringValue("192.0.2.2"), "install", "endpoints", "ingress", "address"))
		}
		if issues := validateEndpoints(bad, catalog); !hasField(issues, "$.spec.install.endpoints."+slot+".address") {
			t.Fatal(issues)
		}
	}
}

func TestASelectedLoadBalancerAddressBelongsToOneCluster(t *testing.T) {
	cluster, catalog := fixture("libvirt", 3, false)
	lb := obj(api.LoadBalancer, "lb", m("bindAddresses", list(m("name", "vip", "address", "192.0.2.2"))))
	for _, slot := range endpointSlots {
		cluster = cluster.WithSpec(cluster.Spec().WithPath(m("source", m("type", "loadBalancer", "loadBalancerRef", "lb", "bindAddressRef", "vip")), "install", "endpoints", slot))
	}
	catalog = api.NewCatalog(append(catalog.Objects(), lb))
	cluster, _ = Normalize(cluster, catalog)
	if issues := validateEndpoints(cluster, catalog); len(issues) != 0 {
		t.Fatal(issues)
	}
	other := obj(api.OpenShiftCluster, "other", cluster.Spec())
	catalog = api.NewCatalog(append(catalog.Objects(), other))
	issues := validateEndpoints(cluster, catalog)
	if !hasIssue(issues, "$.spec.install.endpoints.api.source", "OpenShiftCluster/other") {
		t.Fatal(issues)
	}
}

func TestAuthoredBaremetalRefusesExternalEndpoints(t *testing.T) {
	cluster, catalog := fixture("libvirt", 3, false)
	cluster = cluster.WithSpec(cluster.Spec().WithPath(m("baremetal", m()), "install", "platform"))
	cluster, _ = Normalize(cluster, catalog)
	for _, slot := range endpointSlots {
		if issues := validateEndpoints(cluster, catalog); !hasField(issues, "$.spec.install.endpoints."+slot+".source.type") {
			t.Fatal(issues)
		}
	}
}
