package dnsserver

import (
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
