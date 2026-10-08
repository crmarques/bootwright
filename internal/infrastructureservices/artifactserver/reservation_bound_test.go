package artifactserver

import (
	"fmt"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
)

func crowdedController(n int) api.Object {
	addresses := make([]api.Value, n)
	for i := range addresses {
		addresses[i] = api.MapValue(text("name", fmt.Sprintf("a%02d", i)), text("address", fmt.Sprintf("198.51.100.%d", i+1)))
	}
	return api.NewObject(api.Machine, "controller", api.Value{}, controller().Spec().With("network", api.MapValue(field("addresses", api.ListValue(addresses...)))))
}

func addressedEndpoints(listener string, n int) api.Value {
	endpoints := make([]api.Value, n)
	for i := range endpoints {
		fields := []api.FieldValue{text("name", fmt.Sprintf("e%02d", i)), text("addressRef", fmt.Sprintf("a%02d", i))}
		if listener != "" {
			fields = append(fields, text("listenerRef", listener))
		}
		endpoints[i] = api.MapValue(fields...)
	}
	return api.ListValue(endpoints...)
}

// boundRowGraph is the selected graph that one reservation-bound row of the
// refusal table refuses, and the placeholder values that row writes for it.
// A network service has the endpoint-address row and an ArtifactServer the
// socket row.
func boundRowGraph(row string, kind api.Kind) (api.Catalog, map[string]string, bool) {
	switch {
	case row == "Endpoint addresses beyond the host reservation" && kind != api.ArtifactServer:
		service := api.NewObject(kind, "lab-service", api.Value{}, api.MapValue(text("management", "managed"), text("machineRef", "controller"), text("bindAddress", "0.0.0.0"), number("port", "3128"), field("endpoints", addressedEndpoints("", 62))))
		return catalogOf(crowdedController(62), service), map[string]string{"<server>": service.Identity()}, true
	case row == "Artifact sockets beyond the host reservation" && kind == api.ArtifactServer:
		server := artifactServer(text("bindAddress", "0.0.0.0"),
			field("listeners", api.ListValue(api.MapValue(text("name", "https"), text("protocol", "https"), number("port", "8443")))),
			field("endpoints", addressedEndpoints("https", 62)))
		return catalogOf(crowdedController(62), server), map[string]string{"<server>": server.Identity()}, true
	}
	return api.Catalog{}, nil, false
}

// TestTheArtifactSocketBoundKeepsAClaimWithinItsKeys holds the admission bound
// to the real claim: the largest wildcard artifact server the bound admits
// claims exactly the keys one reservation holds.
func TestTheArtifactSocketBoundKeepsAClaimWithinItsKeys(t *testing.T) {
	catalog, _, found := boundRowGraph("Artifact sockets beyond the host reservation", api.ArtifactServer)
	if !found {
		t.Fatal("the artifact bound has no graph")
	}
	server := catalog.OfKind(api.ArtifactServer)[0]
	spec := server.Spec().With("endpoints", addressedEndpoints("https", infrastructureservices.MaxReservationSockets-1))
	server = api.NewObject(api.ArtifactServer, server.Name(), api.Value{}, spec)
	machine := crowdedController(infrastructureservices.MaxReservationSockets - 1)
	requests, err := Requests(catalogOf(machine, server), "controller", testContext)
	if err != nil || len(requests) != 1 {
		t.Fatalf("requests = %d (%v)", len(requests), err)
	}
	keys := requests[0].reservationKeys()
	if len(keys) != prerequisites.MaxReservationKeys {
		t.Fatalf("the largest admitted claim holds %d keys, want the %d one reservation holds", len(keys), prerequisites.MaxReservationKeys)
	}
	if infrastructureservices.ReservationSockets(server, machine)+2 != len(keys) {
		t.Fatalf("the bound counts %d sockets, the claim holds %d keys", infrastructureservices.ReservationSockets(server, machine), len(keys))
	}
}
