package libvirt

import (
	"fmt"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/substrate"
)

// managedAttachments is count managed attachments of distinct names, bridges
// and prefixes.
func managedAttachments(count int) api.FieldValue {
	var found []api.Value
	for index := range count {
		found = append(found, api.MapValue(
			text("name", fmt.Sprintf("net-%02d", index)),
			field("libvirt", api.MapValue(
				text("bridge", fmt.Sprintf("br-%02d", index)), text("management", "managed"),
				text("address", fmt.Sprintf("10.%d.0.1/24", index)), text("forward", "nat"),
			)),
		))
	}
	return field("networkAttachments", api.ListValue(found...))
}

func TestTheManagedAttachmentBoundKeepsTheHostReservationWithinItsKeys(t *testing.T) {
	catalog := catalogOf(controller(), provider(managedAttachments(substrate.MaxManagedAttachments)))
	requests, err := HostRequests(catalog, "controller", testContext)
	if err != nil || len(requests) != 1 {
		t.Fatalf("requests = %+v (%v)", requests, err)
	}
	keys := requests[0].ReservationKeys()
	if len(keys) != 2+3*substrate.MaxManagedAttachments || len(keys) > prerequisites.MaxReservationKeys {
		t.Fatalf("%d managed attachments claim %d keys, of the %d one reservation holds", substrate.MaxManagedAttachments, len(keys), prerequisites.MaxReservationKeys)
	}
	if 2+3*(substrate.MaxManagedAttachments+1) <= prerequisites.MaxReservationKeys {
		t.Fatalf("%d managed attachments still fit the %d keys one reservation holds, so the bound is not the largest", substrate.MaxManagedAttachments+1, prerequisites.MaxReservationKeys)
	}
}
