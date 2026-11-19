package cephcluster

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

const (
	managementGatewayAt     = "$.spec.managementGateway"
	managementGatewayFQDNAt = "$.spec.managementGateway.fqdn"
)

func TestManagementGatewayFQDNDefaultsToTheStorageDomain(t *testing.T) {
	gateway := b479Service("mgmt-gateway", "port", 9443)
	for want, domains := range map[string]api.Value{
		"mgr.storage.storage.example.test":  m("base", "example.test", "clusters", "clusters.example.test", "storageClusters", "storage.example.test"),
		"mgr.storage.clusters.example.test": m("base", "example.test", "clusters", "clusters.example.test"),
		"mgr.storage.example.test":          m("base", "example.test"),
	} {
		cluster, catalog := withServices(t, gatewayDomainCatalog(domains), gateway)
		once, _ := Normalize(cluster, catalog)
		twice, _ := Normalize(once, catalog)
		if got := textOf(once.Spec(), "managementGateway", "fqdn"); got != want || !twice.Spec().Equal(once.Spec()) {
			t.Fatalf("derived management gateway FQDN %q, want %q (idempotent %v)", got, want, twice.Spec().Equal(once.Spec()))
		}
	}
	cluster, catalog := withServices(t, gatewayDomainCatalog(m("base", "example.test")), gateway)
	authored := cluster.WithSpec(cluster.Spec().With("managementGateway", m("fqdn", "ceph.example.test")))
	if normalized, _ := Normalize(authored, catalog); textOf(normalized.Spec(), "managementGateway", "fqdn") != "ceph.example.test" {
		t.Fatal("an authored management gateway FQDN was replaced")
	}
	if normalized, _ := Normalize(find(t, gatewayDomainCatalog(m("base", "example.test")), api.CephCluster, "storage"), catalog); normalized.Spec().Has("managementGateway") {
		t.Fatal("a cluster without a management gateway derived a management gateway FQDN")
	}
	cluster, catalog = withServices(t, gatewayDomainCatalog(api.Value{}), gateway)
	if normalized, _ := Normalize(cluster, catalog); normalized.Spec().Has("managementGateway") {
		t.Fatal("a cluster without a storage domain derived a management gateway FQDN")
	}
	external := object(api.CephCluster, "storage", m("management", "external", "services", api.ListValue(gateway)))
	if normalized, _ := Normalize(external, catalog); normalized.Spec().Has("managementGateway") {
		t.Fatal("an external cluster derived a management gateway FQDN")
	}
}

func TestManagementGatewayRefusesWithoutAGatewayService(t *testing.T) {
	catalog := fixtureCatalog()
	cluster := find(t, catalog, api.CephCluster, "storage")
	cluster = cluster.WithSpec(cluster.Spec().With("managementGateway", m("fqdn", "ceph.example.test")))
	exactlyOne(t, Validate(cluster, replace(catalog, cluster)), managementGatewayAt, "spec.managementGateway names the management gateway's DNS name, and this cluster declares no mgmt-gateway service",
		"declare a mgmt-gateway service in spec.services or remove spec.managementGateway")
	gated, gatedCatalog := withServices(t, catalog, b479Service("mgmt-gateway", "port", 9443))
	gated = gated.WithSpec(gated.Spec().With("managementGateway", m("fqdn", "ceph.example.test")))
	if found := issuesUnder(Validate(gated, replace(gatedCatalog, gated)), managementGatewayAt); len(found) != 0 {
		t.Fatalf("a management gateway FQDN beside its service refused: %#v", found)
	}
}

func TestManagementGatewayFQDNMustDifferFromNodeHostnames(t *testing.T) {
	cluster, catalog := withServices(t, fixtureCatalog(), b479Service("mgmt-gateway", "port", 9443))
	normalized, _ := Normalize(cluster, catalog)
	if textOf(normalized.Spec(), "managementGateway", "fqdn") != "mgr.storage.example.test" {
		t.Fatalf("derived management gateway FQDN %q", textOf(normalized.Spec(), "managementGateway", "fqdn"))
	}
	if found := issuesUnder(Validate(normalized, replace(catalog, normalized)), managementGatewayAt); len(found) != 0 {
		t.Fatalf("the derived management gateway FQDN refused: %#v", found)
	}
	host := normalized.WithSpec(normalized.Spec().WithPath(api.StringValue("node-2.storage.example.test"), "managementGateway", "fqdn"))
	issues := Validate(host, replace(catalog, host))
	exactlyOne(t, issues, managementGatewayFQDNAt, "the management gateway FQDN equals the hostname of storage node node-2",
		"set spec.managementGateway.fqdn to a name no storage node of this cluster uses")
	if strings.Contains(issues[0].Message+issues[0].Remediation, "node-2.storage.example.test") {
		t.Fatalf("the diagnostic echoed the management gateway FQDN: %#v", issues[0])
	}
}
