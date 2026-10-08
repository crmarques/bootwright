package managedservice

import (
	"fmt"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
)

// TestTheReservationSocketBoundKeepsAClaimWithinItsKeys holds the admission
// bound to the real claim: the largest wildcard Proxy the bound admits claims
// exactly the keys one reservation holds.
func TestTheReservationSocketBoundKeepsAClaimWithinItsKeys(t *testing.T) {
	const addresses = infrastructureservices.MaxReservationSockets - 1
	var named, endpoints []api.Value
	for i := 0; i < addresses; i++ {
		name := fmt.Sprintf("a%02d", i)
		named = append(named, api.MapValue(text("name", name), text("address", fmt.Sprintf("198.51.100.%d", i+1))))
		endpoints = append(endpoints, api.MapValue(text("name", fmt.Sprintf("e%02d", i)), text("addressRef", name)))
	}
	machine := api.NewObject(api.Machine, "controller", api.Value{}, controller().Spec().With("network", api.MapValue(field("addresses", api.ListValue(named...)))))
	object := api.NewObject(api.Proxy, "lab-proxy", api.Value{}, service(api.Proxy, "lab-proxy").Spec().With("bindAddress", api.StringValue("0.0.0.0")).With("endpoints", api.ListValue(endpoints...)))
	requests, err := NewCapability(testDefinition(), nil).Requests(catalogOf(machine, object), "controller", testContext)
	if err != nil || len(requests) != 1 {
		t.Fatalf("requests = %d (%v)", len(requests), err)
	}
	keys := requests[0].ReservationKeys()
	if len(keys) != prerequisites.MaxReservationKeys {
		t.Fatalf("%d distinct endpoint addresses claim %d keys, want the %d one reservation holds", addresses, len(keys), prerequisites.MaxReservationKeys)
	}
	if infrastructureservices.ReservationSockets(object, machine)+2 != len(keys) {
		t.Fatalf("the bound counts %d sockets, the claim holds %d keys", infrastructureservices.ReservationSockets(object, machine), len(keys))
	}
	if infrastructureservices.MaxReservationSockets+2 != prerequisites.MaxReservationKeys {
		t.Fatalf("%d sockets beside the unit and the content root are not the %d keys one reservation holds", infrastructureservices.MaxReservationSockets, prerequisites.MaxReservationKeys)
	}
}
