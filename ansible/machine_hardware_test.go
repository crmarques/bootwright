package ansible

import (
	"strings"
	"testing"
)

func TestEnrollmentUsesTheEmbeddedHardwareReader(t *testing.T) {
	const path = "collections/ansible_collections/bootwright/core/plugins/module_utils/machine_hardware.py"
	source := MachineHardwareSource()
	if !strings.Contains(source, "def read(") || source != string(Assets()[path]) {
		t.Fatal("enrollment must use the same read-only worker as the collection")
	}
}
