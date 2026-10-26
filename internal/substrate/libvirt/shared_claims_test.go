package libvirt

import (
	"context"
	"fmt"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
)

func externalAttachments(bridges ...string) api.FieldValue {
	attachments := []api.Value{api.MapValue(
		text("name", "lab-guests"),
		field("libvirt", api.MapValue(text("bridge", "virbr-lab"), text("management", "managed"),
			text("address", "198.51.100.1/24"), text("forward", "nat"))),
	)}
	for index, bridge := range bridges {
		attachments = append(attachments, api.MapValue(text("name", fmt.Sprintf("edge-%02d", index)),
			field("libvirt", api.MapValue(text("bridge", bridge), text("management", "external")))))
	}
	return field("networkAttachments", api.ListValue(attachments...))
}

func planned(t *testing.T, capability interface {
	Plan(context.Context, lifecycle.PlanInput) (lifecycle.CapabilityPlan, error)
}, catalog api.Catalog) []prerequisites.HostReservation {
	t.Helper()
	plan, err := capability.Plan(context.Background(), lifecycle.PlanInput{
		Verb: reconciliation.Apply, State: compilation.NewState(catalog, catalog, nil),
		Controller: "controller", Context: lifecycle.ContextIdentity{Name: testContext},
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan.Reservations
}

// Each external attachment claims, shared, the bridge it uses, beside the
// provider's exclusive claim, so a removal of another context's managed bridge
// sees the use; a provider with no external attachment claims no such key
// (B803).
func TestAnExternalAttachmentClaimsItsBridgeShared(t *testing.T) {
	reservations := planned(t, NewHost(nil), catalogOf(controller(), provider(externalAttachments("br-edge", "br-uplink", "br-edge")), networkConfig()))
	if len(reservations) != 2 || reservations[0].Shared || reservations[1].Kind != "substrate-host-use" || !reservations[1].Shared ||
		reservations[1].Service != "lab-libvirt" || !slices.Equal(reservations[1].Keys, []string{"bridge-use:br-edge", "bridge-use:br-uplink"}) {
		t.Fatalf("reservations = %+v", reservations)
	}
	if slices.ContainsFunc(reservations[0].Keys, func(key string) bool { return key == "bridge:br-edge" }) {
		t.Fatal("an external attachment claimed its bridge exclusively")
	}
	if only := planned(t, NewHost(nil), labCatalog()); len(only) != 1 {
		t.Fatalf("a provider with no external attachment claimed %+v", only)
	}
}

// A Machine's emulated BMC claims its endpoint's bmc: key shared, normalized as
// a physical Machine addressing that endpoint claims it exclusively.
func TestAnEmulatedBMCClaimsItsControllerKeyShared(t *testing.T) {
	requests, err := MachineRequests(labCatalog(), "controller", testContext)
	if err != nil || len(requests) != 1 {
		t.Fatalf("requests = %+v (%v)", requests, err)
	}
	reservations := planned(t, NewMachine(nil), labCatalog())
	want := substrate.ControllerReservationKey(requests[0].Controller.Endpoint)
	if len(reservations) != 2 || reservations[1].Kind != "substrate-machine-bmc" || !reservations[1].Shared ||
		reservations[1].Service != "rhel-01" || !slices.Equal(reservations[1].Keys, []string{want}) || want != "bmc:192.0.2.1:8000/"+substrate.DomainUUID(testContext, "rhel-01") {
		t.Fatalf("reservations = %+v, want the shared %s", reservations, want)
	}
}

// The external bridges a provider uses travel in a reservation of their own,
// bounded by the keys one reservation holds.
func TestTheExternalBridgeBoundKeepsTheUseReservationWithinItsKeys(t *testing.T) {
	if substrate.MaxExternalBridges != prerequisites.MaxReservationKeys {
		t.Fatalf("%d external bridges against the %d keys one reservation holds", substrate.MaxExternalBridges, prerequisites.MaxReservationKeys)
	}
	var bridges []string
	for index := range substrate.MaxExternalBridges {
		bridges = append(bridges, fmt.Sprintf("br-%02d", index))
	}
	reservations := planned(t, NewHost(nil), catalogOf(controller(), provider(externalAttachments(bridges...)), networkConfig()))
	if len(reservations) != 2 || len(reservations[1].Keys) != prerequisites.MaxReservationKeys {
		t.Fatalf("%d external bridges claim %+v", len(bridges), reservations)
	}
}
