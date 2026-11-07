package libvirt

import (
	"fmt"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/substrate"
)

// managedNetworks is count managed Networks of distinct names, bridges and
// prefixes beside the controller and the provider that defines them.
func managedNetworks(count int) []api.Object {
	found := []api.Object{controller(), provider()}
	for index := range count {
		found = append(found, managedNetwork(fmt.Sprintf("net-%02d", index), fmt.Sprintf("br-%02d", index), fmt.Sprintf("10.%d.0.1/24", index)))
	}
	return found
}

func TestTheManagedNetworkBoundKeepsTheHostReservationWithinItsKeys(t *testing.T) {
	catalog := catalogOf(managedNetworks(substrate.MaxManagedNetworks)...)
	requests, err := HostRequests(catalog, "controller", testContext)
	if err != nil || len(requests) != 1 {
		t.Fatalf("requests = %+v (%v)", requests, err)
	}
	keys := requests[0].ReservationKeys()
	if len(keys) != 3+3*substrate.MaxManagedNetworks || len(keys) > prerequisites.MaxReservationKeys {
		t.Fatalf("%d managed Networks claim %d keys, of the %d one reservation holds", substrate.MaxManagedNetworks, len(keys), prerequisites.MaxReservationKeys)
	}
	if 3+3*(substrate.MaxManagedNetworks+1) <= prerequisites.MaxReservationKeys {
		t.Fatalf("%d managed Networks still fit the %d keys one reservation holds, so the bound is not the largest", substrate.MaxManagedNetworks+1, prerequisites.MaxReservationKeys)
	}
}
