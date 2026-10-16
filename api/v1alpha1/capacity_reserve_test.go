package v1alpha1

import "testing"

func TestCapacityReserveIsOptionalButItsQuantitiesAreExplicit(t *testing.T) {
	libvirt, ok := machineProviderSchema().Field("libvirt")
	if !ok {
		t.Fatal("missing libvirt")
	}
	reserve, ok := libvirt.Shape.Field("capacityReserve")
	if !ok || reserve.Required || reserve.Default.Present() {
		t.Fatal("reserve must be optional without an implicit policy")
	}
	for _, name := range []string{"cpu", "memoryMiB", "diskGiB"} {
		field, ok := reserve.Shape.Field(name)
		if !ok || !field.Required || field.Shape.Type != Integer || field.Shape.Minimum != "0" {
			t.Fatalf("%s lacks an explicit nonnegative quantity", name)
		}
	}
}
