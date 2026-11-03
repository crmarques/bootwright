package v1alpha1

import (
	"slices"
	"testing"
)

func TestPhysicalEraseIsExplicitAndNamesEveryIdentity(t *testing.T) {
	hardware, _ := machineSchema().Field("hardware")
	erase, found := hardware.Shape.Field("eraseOnRemoval")
	if !found {
		t.Fatal("explicit physical erase policy missing")
	}
	if erase.Required || erase.Default.Present() {
		t.Fatal("omission must retain physical disks")
	}
	methods := []string{"logicalOverwrite", "metadataClear"}
	method, _ := erase.Shape.Field("method")
	if !method.Required || !slices.Equal(method.Shape.Enums, methods) {
		t.Fatal("erase claimed hardware sanitize or lost its explicit method")
	}
	targets, _ := erase.Shape.Field("targets")
	for _, key := range []string{"deviceName", "wwn", "serialNumber", "sizeBytes"} {
		field, ok := targets.Shape.Element.Field(key)
		if !ok || !field.Required {
			t.Fatalf("erase target does not require %s", key)
		}
	}
	override, ok := targets.Shape.Element.Field("method")
	if !ok || override.Required || override.Default.Present() || !slices.Equal(override.Shape.Enums, methods) {
		t.Fatal("a target's method must be an optional override of the policy's")
	}
}
