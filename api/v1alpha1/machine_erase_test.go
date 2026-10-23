package v1alpha1

import "testing"

func TestPhysicalEraseIsExplicitAndNamesEveryIdentity(t *testing.T) {
	hardware, _ := machineSchema().Field("hardware")
	erase, found := hardware.Shape.Field("eraseOnRemoval")
	if !found {
		t.Fatal("explicit physical erase policy missing")
	}
	if erase.Required || erase.Default.Present() {
		t.Fatal("omission must retain physical disks")
	}
	method, _ := erase.Shape.Field("method")
	if len(method.Shape.Enums) != 1 || method.Shape.Enums[0] != "logicalOverwrite" {
		t.Fatal("erase claimed hardware sanitize")
	}
	targets, _ := erase.Shape.Field("targets")
	for _, key := range []string{"deviceName", "wwn", "serialNumber", "sizeBytes"} {
		field, ok := targets.Shape.Element.Field(key)
		if !ok || !field.Required {
			t.Fatalf("erase target does not require %s", key)
		}
	}
}
