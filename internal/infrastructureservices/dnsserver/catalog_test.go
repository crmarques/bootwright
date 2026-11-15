package dnsserver

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
)

func gateways(records ...managedservice.Record) GatewayRecords {
	return func(api.Catalog) ([]managedservice.Record, error) { return records, nil }
}

func TestTheStorageDerivedGatewayRecordIsPublishedOnceBesideItsEqual(t *testing.T) {
	derived := managedservice.Record{Name: "s3.example.test", Addresses: []string{"192.0.2.80"}}
	records, err := exportRecords(api.NewCatalog(nil), nil, gateways(derived))
	if err != nil || !reflect.DeepEqual(records, []managedservice.Record{derived}) {
		t.Fatalf("records = %v (%v)", records, err)
	}
	records, err = exportRecords(api.NewCatalog(nil), []managedservice.Record{derived}, gateways(derived))
	if err != nil || len(records) != 1 {
		t.Fatalf("an equal record was published twice: %v (%v)", records, err)
	}
	if records, err = exportRecords(api.NewCatalog(nil), nil, nil); err != nil || len(records) != 0 {
		t.Fatalf("no derivation contributed %v (%v)", records, err)
	}
}

func TestTheStorageDerivedGatewayRecordRefusesAConflictingRecord(t *testing.T) {
	derived := gateways(managedservice.Record{Name: "s3.example.test", Addresses: []string{"192.0.2.80"}})
	for _, existing := range []managedservice.Record{{Name: "s3.example.test", Addresses: []string{"192.0.2.81"}}, {Name: "s3.example.test", Addresses: []string{"192.0.2.80"}, Subtree: true}} {
		if _, err := exportRecords(api.NewCatalog(nil), []managedservice.Record{existing}, derived); err == nil {
			t.Fatal("foreign record accepted")
		}
	}
	refused := errors.New("unresolved gateway")
	if _, err := exportRecords(api.NewCatalog(nil), nil, func(api.Catalog) ([]managedservice.Record, error) { return nil, refused }); !errors.Is(err, refused) {
		t.Fatalf("the derivation's refusal was lost: %v", err)
	}
}

func TestConditionalForwardersReachTheFrozenRequest(t *testing.T) {
	definition := Definition(nil)
	zone := api.MapValue(api.FieldValue{Name: "domain", Value: api.StringValue("sno.example.test")}, api.FieldValue{Name: "forwarders", Value: api.StringList("192.0.2.10")})
	spec := api.MapValue(api.FieldValue{Name: "forwarders", Value: api.StringList("198.51.100.53")}, api.FieldValue{Name: "forwardZones", Value: api.ListValue(zone)})
	var request managedservice.Request
	if err := definition.Extend(api.Catalog{}, spec, &request); err != nil {
		t.Fatal(err)
	}
	data, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded["forwardZones"]; !ok {
		t.Fatal("the DNS implementation ignored the declared forwarding zone")
	}
}

func TestClusterDNSZoneTerminatesLocally(t *testing.T) {
	env := api.NewObject(api.Environment, "lab", api.Value{}, api.MapValue(api.FieldValue{Name: "domains", Value: api.MapValue(api.FieldValue{Name: "containerClusters", Value: api.StringValue("clusters.example.test")})}))
	cluster := api.NewObject(api.OpenShiftCluster, "sno", api.Value{}, api.MapValue())
	var request managedservice.Request
	if err := Definition(nil).Extend(api.NewCatalog([]api.Object{env, cluster}), api.MapValue(), &request); err != nil {
		t.Fatal(err)
	}
	data, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got["localZones"], []any{"sno.clusters.example.test"}) {
		t.Fatalf("cluster zone is not frozen as local: %s", data)
	}
}
