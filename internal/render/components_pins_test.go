package render

import (
	"testing"

	"github.com/crmarques/bootwright/internal/infra/support"
)

func TestServicePinGatesCoverPinnableServices(t *testing.T) {
	want := support.PinnableServiceKeys()
	got := map[support.ServiceKey]bool{}
	for _, gate := range servicePinGates {
		if got[gate.key] {
			t.Fatalf("duplicate service pin gate for %v", gate.key)
		}
		got[gate.key] = true
	}
	if len(got) != len(want) {
		t.Fatalf("servicePinGates has %d entries, registry pins %d service images", len(got), len(want))
	}
	for _, key := range want {
		if !got[key] {
			t.Fatalf("registry pins an image for %v but ComponentPins has no gate for it", key)
		}
	}
}
