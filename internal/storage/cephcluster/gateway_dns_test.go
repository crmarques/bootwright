package cephcluster

import (
	"reflect"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestGatewayDNSUsesPublicPlacementAddressesOrIngressVIP(t *testing.T) {
	catalog := gatewayDNSCatalog(t)
	gateway := find(t, catalog, api.CephObjectGateway, "objects")
	addresses, err := GatewayDNSAddresses(catalog, gateway)
	if err != nil || !reflect.DeepEqual(addresses, []string{"192.0.2.1", "192.0.2.2", "192.0.2.3"}) {
		t.Fatalf("public addresses = %v (%v)", addresses, err)
	}
	cluster := find(t, catalog, api.CephCluster, "storage")
	ingress := m("service_type", "ingress", "spec", m("backend_service", "rgw.objects", "virtual_ip", "192.0.2.80/24"))
	services := append(cluster.Spec().Get("services").Items(), ingress)
	cluster = cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(services...), "services"))
	addresses, err = GatewayDNSAddresses(replace(catalog, cluster), gateway)
	if err != nil || !reflect.DeepEqual(addresses, []string{"192.0.2.80"}) {
		t.Fatalf("ingress addresses = %v (%v)", addresses, err)
	}
}

func TestGatewayDNSRefusesAnUnresolvedPublicPlacement(t *testing.T) {
	catalog := gatewayDNSCatalog(t)
	gateway := find(t, catalog, api.CephObjectGateway, "objects")
	host := find(t, catalog, api.Machine, "machine-1")
	host = host.WithSpec(host.Spec().WithPath(api.ListValue(m("name", "other", "address", "198.51.100.1/24")), "network", "addresses"))
	if addresses, err := GatewayDNSAddresses(replace(catalog, host), gateway); err == nil || addresses != nil {
		t.Fatalf("unresolved host = %v (%v)", addresses, err)
	}
}

func gatewayDNSCatalog(t *testing.T) api.Catalog {
	t.Helper()
	catalog := gatewayCatalog(t)
	cluster := find(t, catalog, api.CephCluster, "storage")
	services := cluster.Spec().Get("services").Items()
	for i, service := range services {
		if NativeServiceName(service) == "rgw.objects" {
			services[i] = service.WithPath(api.IntegerValue("3"), "placement", "count")
		}
	}
	return replace(catalog, cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(services...), "services")))
}
func TestGatewayDNSRefusesNondeterministicDaemonPlacement(t *testing.T) {
	catalog := gatewayCatalog(t)
	gateway := find(t, catalog, api.CephObjectGateway, "objects")
	if addresses, err := GatewayDNSAddresses(catalog, gateway); err == nil || addresses != nil {
		t.Fatalf("partial placement = %v (%v)", addresses, err)
	}
}

func attachedGatewayCatalog() api.Catalog {
	cluster := api.NewObject(api.CephCluster, "storage", api.Value{}, m("services", api.ListValue(m("service_type", "ingress", "spec", m("backend_service", "rgw.objects", "virtual_ip", "192.0.2.80/24")))))
	gateway := api.NewObject(api.CephObjectGateway, "objects", api.Value{}, m("clusterRef", "storage", "serviceRef", "rgw.objects", "endpoint", m("fqdn", "s3.example.test")))
	objects := []api.Object{cluster, gateway}
	for _, name := range []string{"one", "two"} {
		objects = append(objects, api.NewObject(api.CephConsumer, name, api.Value{}, m("dataFoundation", m("objectGatewayRef", "objects"))))
	}
	return api.NewCatalog(objects)
}

func TestAttachedGatewayRecordsAreSharedAndNoUnattachedGatewayContributesOne(t *testing.T) {
	records, err := AttachedGatewayRecords(attachedGatewayCatalog())
	if err != nil || !reflect.DeepEqual(records, []GatewayRecord{{Name: "s3.example.test", Addresses: []string{"192.0.2.80"}}}) {
		t.Fatalf("records = %v (%v)", records, err)
	}
	records, err = AttachedGatewayRecords(api.NewCatalog(attachedGatewayCatalog().Objects()[:2]))
	if err != nil || len(records) != 0 {
		t.Fatalf("unattached gateway records = %v (%v)", records, err)
	}
	missing := api.NewCatalog(append(attachedGatewayCatalog().Objects()[2:], attachedGatewayCatalog().Objects()[0]))
	if _, err := AttachedGatewayRecords(missing); err == nil {
		t.Fatal("an export attached to no retained gateway contributed a record")
	}
}
