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
	for key, required := range map[string]bool{"deviceName": false, "wwn": false, "serialNumber": true, "sizeBytes": true} {
		field, ok := targets.Shape.Element.Field(key)
		if !ok || field.Required != required {
			t.Fatalf("erase target %s: present %t, required %t, want required %t", key, ok, field.Required, required)
		}
	}
	if targets.Shape.NameKey != "serialNumber" {
		t.Fatalf("erase targets are keyed by %q, want serialNumber, which every target declares", targets.Shape.NameKey)
	}
	override, ok := targets.Shape.Element.Field("method")
	if !ok || override.Required || override.Default.Present() || !slices.Equal(override.Shape.Enums, methods) {
		t.Fatal("a target's method must be an optional override of the policy's")
	}
}

// A clear target, like an erase target, names its disk by deviceName with its
// wwn or by serialNumber and sizeBytes alone, so only those two are required
// and the targets are keyed by the serial number every one declares.
func TestAClearTargetMayOmitItsPathAndWWN(t *testing.T) {
	hardware, _ := machineSchema().Field("hardware")
	clear, _ := hardware.Shape.Field("clearOnInstall")
	targets, _ := clear.Shape.Field("targets")
	for key, required := range map[string]bool{"deviceName": false, "wwn": false, "serialNumber": true, "sizeBytes": true} {
		field, ok := targets.Shape.Element.Field(key)
		if !ok || field.Required != required {
			t.Fatalf("clear target %s: present %t, required %t, want required %t", key, ok, field.Required, required)
		}
	}
	if targets.Shape.NameKey != "serialNumber" {
		t.Fatalf("clear targets are keyed by %q, want serialNumber", targets.Shape.NameKey)
	}
	substrate, _ := machineSchema().Field("substrate")
	baremetal, _ := substrate.Shape.Field("baremetal")
	root, _ := baremetal.Shape.Field("rootDevice")
	size, ok := root.Shape.Field("sizeBytes")
	if !ok || size.Required || size.Shape.Minimum != "4096" || size.Shape.Maximum != "70368744177664" {
		t.Fatalf("substrate.baremetal.rootDevice.sizeBytes = %+v, want an optional integer from 4096 to 64 TiB", size)
	}
}
