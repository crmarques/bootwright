package installation

import (
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/infrastructureservices"
)

func TestPhysicalMediaReservationsCoverOtherSystemsAndContexts(t *testing.T) {
	first := Request{Target: Target{Physical: true, Controller: Controller{Endpoint: "https://bmc.example.test/redfish/v1/Systems/1"}}}
	second := first
	second.Target.Controller.Endpoint = "https://bmc.example.test:443/redfish/v1/Systems/2"
	if !slices.Equal(first.ExclusiveKeys(), second.ExclusiveKeys()) {
		t.Fatalf("shared manager is not serialized: %v %v", first.ExclusiveKeys(), second.ExclusiveKeys())
	}
	held := []infrastructureservices.HostReservation{{Context: "first", Kind: "os-install", Service: "node-a", Keys: first.ReservationKeys()}}
	wanted := []infrastructureservices.HostReservation{{Context: "second", Kind: "os-install", Service: "node-b", Keys: second.ReservationKeys()}}
	if len(infrastructureservices.Conflicts(held, wanted)) != 1 {
		t.Fatal("different contexts could concurrently mutate one manager")
	}
	if got := (Request{}).ExclusiveKeys(); len(got) != 0 {
		t.Fatalf("virtual requests acquired a physical BMC claim: %v", got)
	}
}
