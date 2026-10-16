package preflight

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func reserveProvider(name, cpu, memory, disk string) api.Object {
	fields := []api.FieldValue{}
	for key, value := range map[string]string{"cpu": cpu, "memoryMiB": memory, "diskGiB": disk} {
		if value != "" {
			fields = append(fields, api.FieldValue{Name: key, Value: api.IntegerValue(value)})
		}
	}
	return api.NewObject(api.InfraProvider, name, api.Value{}, api.MapValue(api.FieldValue{Name: "libvirt", Value: api.MapValue(api.FieldValue{Name: "capacityReserve", Value: api.MapValue(fields...)})}))
}
func TestReserveRequiresExplicitCompleteNonnegativeSafeQuantities(t *testing.T) {
	target := Target{Host: "host", Pool: "/pool"}
	allocations := []Allocation{{Machine: "guest", Provider: "provider", Host: target.Host, Pool: target.Pool}}
	for _, row := range []struct {
		name, cpu, memory, disk string
		configured              bool
	}{{"zero", "0", "0", "0", true}, {"complete", "1", "2048", "10", true}, {"missing", "", "0", "0", false}, {"negative", "-1", "0", "0", false}, {"memory overflow", "0", "8796093022208", "0", false}, {"disk overflow", "0", "0", "8589934592", false}} {
		t.Run(row.name, func(t *testing.T) {
			reserve := ReservesFromCatalog(api.NewCatalog([]api.Object{reserveProvider("provider", row.cpu, row.memory, row.disk)}), allocations)(target)
			if reserve.Configured != row.configured {
				t.Fatalf("%+v", reserve)
			}
		})
	}
	if ReservesFromCatalog(api.Catalog{}, allocations)(target).Configured {
		t.Fatal("missing provider supplied reserve")
	}
}
func TestConflictingSharedReserveNeverPasses(t *testing.T) {
	target := Target{Host: "host", Pool: "/pool"}
	allocations := []Allocation{{Machine: "a", Provider: "a", Host: target.Host, Pool: target.Pool}, {Machine: "b", Provider: "b", Host: target.Host, Pool: target.Pool}}
	catalog := api.NewCatalog([]api.Object{reserveProvider("a", "1", "2048", "10"), reserveProvider("b", "2", "2048", "10")})
	if ReservesFromCatalog(catalog, allocations)(target).Configured {
		t.Fatal("conflicting reserve admitted")
	}
}
