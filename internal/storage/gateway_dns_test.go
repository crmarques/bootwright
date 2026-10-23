package storage

import (
	"reflect"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestGatewayDNSUsesPublicPlacementAddressesOrIngressVIP(t *testing.T) {
	catalog := gatewayDNSCatalog(t)
	gateway := find(t, catalog, api.StorageObjectGateway, "objects")
	addresses, err := GatewayDNSAddresses(catalog, gateway)
	if err != nil || !reflect.DeepEqual(addresses, []string{"192.0.2.1", "192.0.2.2", "192.0.2.3"}) {
		t.Fatalf("public addresses = %v (%v)", addresses, err)
	}
	cluster := find(t, catalog, api.StorageCluster, "storage")
	ingress := m("service_type", "ingress", "spec", m("backend_service", "rgw.objects", "virtual_ip", "192.0.2.80/24"))
	services := append(cluster.Spec().Get("ceph", "services").Items(), ingress)
	cluster = cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(services...), "ceph", "services"))
	addresses, err = GatewayDNSAddresses(replace(catalog, cluster), gateway)
	if err != nil || !reflect.DeepEqual(addresses, []string{"192.0.2.80"}) {
		t.Fatalf("ingress addresses = %v (%v)", addresses, err)
	}
}

func TestGatewayDNSRefusesAnUnresolvedPublicPlacement(t *testing.T) {
	catalog := gatewayDNSCatalog(t)
	gateway := find(t, catalog, api.StorageObjectGateway, "objects")
	host := find(t, catalog, api.Machine, "machine-1")
	host = host.WithSpec(host.Spec().WithPath(api.ListValue(m("name", "other", "address", "198.51.100.1/24")), "network", "addresses"))
	if addresses, err := GatewayDNSAddresses(replace(catalog, host), gateway); err == nil || addresses != nil {
		t.Fatalf("unresolved host = %v (%v)", addresses, err)
	}
}

func gatewayDNSCatalog(t *testing.T) api.Catalog {
	t.Helper()
	catalog := gatewayCatalog(t)
	cluster := find(t, catalog, api.StorageCluster, "storage")
	services := cluster.Spec().Get("ceph", "services").Items()
	for i, service := range services {
		if NativeServiceName(service) == "rgw.objects" {
			services[i] = service.WithPath(api.IntegerValue("3"), "placement", "count")
		}
	}
	return replace(catalog, cluster.WithSpec(cluster.Spec().WithPath(api.ListValue(services...), "ceph", "services")))
}
func TestGatewayDNSRefusesNondeterministicDaemonPlacement(t *testing.T) {
	catalog := gatewayCatalog(t)
	gateway := find(t, catalog, api.StorageObjectGateway, "objects")
	if addresses, err := GatewayDNSAddresses(catalog, gateway); err == nil || addresses != nil {
		t.Fatalf("partial placement = %v (%v)", addresses, err)
	}
}
