package dnsserver

import (
	"reflect"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
)

func dnsMap(fields ...api.FieldValue) api.Value { return api.MapValue(fields...) }
func dnsField(name string, value api.Value) api.FieldValue {
	return api.FieldValue{Name: name, Value: value}
}
func dnsText(name, value string) api.FieldValue { return dnsField(name, api.StringValue(value)) }
func gatewayCatalog() api.Catalog {
	cluster := api.NewObject(api.StorageCluster, "storage", api.Value{}, dnsMap(dnsField("ceph", dnsMap(dnsField("services", api.ListValue(dnsMap(dnsText("service_type", "ingress"), dnsField("spec", dnsMap(dnsText("backend_service", "rgw.objects"), dnsText("virtual_ip", "192.0.2.80/24"))))))))))
	gateway := api.NewObject(api.StorageObjectGateway, "objects", api.Value{}, dnsMap(dnsText("clusterRef", "storage"), dnsText("serviceRef", "rgw.objects"), dnsField("endpoint", dnsMap(dnsText("fqdn", "s3.example.test")))))
	objects := []api.Object{cluster, gateway}
	for _, name := range []string{"one", "two"} {
		objects = append(objects, api.NewObject(api.StorageExport, name, api.Value{}, dnsMap(dnsField("dataFoundation", dnsMap(dnsText("objectGatewayRef", "objects"))))))
	}
	return api.NewCatalog(objects)
}
func TestAttachedGatewayDNSContributionIsSharedAndHasNoUnattachedRecord(t *testing.T) {
	records, err := exportRecords(gatewayCatalog(), nil)
	want := []managedservice.Record{{Name: "s3.example.test", Addresses: []string{"192.0.2.80"}}}
	if err != nil || !reflect.DeepEqual(records, want) {
		t.Fatalf("records = %v (%v)", records, err)
	}
	objects := gatewayCatalog().Objects()
	records, err = exportRecords(api.NewCatalog(objects[:2]), nil)
	if err != nil || len(records) != 0 {
		t.Fatalf("unattached gateway records = %v (%v)", records, err)
	}
}
func TestAttachedGatewayDNSRefusesAConflictingRecord(t *testing.T) {
	for _, existing := range []managedservice.Record{{Name: "s3.example.test", Addresses: []string{"192.0.2.81"}}, {Name: "s3.example.test", Addresses: []string{"192.0.2.80"}, Subtree: true}} {
		if _, err := exportRecords(gatewayCatalog(), []managedservice.Record{existing}); err == nil {
			t.Fatal("foreign record accepted")
		}
	}
}
