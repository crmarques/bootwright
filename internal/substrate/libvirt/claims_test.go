package libvirt

import (
	"context"
	"maps"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// A managed Network has no block of its own, so the host block of the provider
// its arm names is what diff reads for it.
func TestTheHostBlockCarriesTheManagedNetworksNamingItsProvider(t *testing.T) {
	provided := api.NewObject(api.Network, "uplink", api.Value{}, api.MapValue(field("provided", api.BoolValue(true))))
	catalog := catalogOf(controller(), provider(), managedNetwork("lab-guests", "br-guests", "192.0.2.1/24"),
		managedNetwork("lab-storage", "br-storage", "198.51.100.1/24"), provided)
	want := map[string][]string{"InfraProvider/lab-libvirt": {"Network/lab-guests", "Network/lab-storage"}}
	if got := (HostCapability{}).Carries(catalog); !maps.EqualFunc(got, want, slices.Equal[[]string]) {
		t.Fatalf("carried = %v, want %v", got, want)
	}
}

// A managed Network is in the request of the one provider its arm names, so
// within one plan no other host block defines or removes it: moving it to
// another provider of the same host changes the input, which an apply over a
// completed one refuses, and the destroy of the frozen plan removes it from
// the old provider's request before a fresh apply defines it from the new one.
func TestAManagedNetworkIsInTheRequestOfItsOwnProviderAlone(t *testing.T) {
	other := api.NewObject(api.InfraProvider, "lab-other", api.Value{}, provider().Spec())
	moved := managedNetwork("lab-guests", "virbr-lab", "198.51.100.1/24")
	moved = moved.WithSpec(moved.Spec().WithPath(api.StringValue("lab-other"), "libvirt", "providerRef"))
	requests, err := HostRequests(catalogOf(controller(), provider(), other, moved), "controller", testContext)
	if err != nil || len(requests) != 2 {
		t.Fatalf("requests = %+v (%v)", requests, err)
	}
	for _, request := range requests {
		managed := 0
		for _, network := range request.Networks {
			if network.Managed {
				managed++
			}
		}
		if want := map[string]int{"lab-libvirt": 0, "lab-other": 1}[request.Identity.Object]; managed != want {
			t.Fatalf("%s defines %d managed networks, want %d: %+v", request.Identity.Object, managed, want, request.Networks)
		}
	}
}

// A Machine plugs into a managed Network of its own provider, whose host block
// it requires, so a removal of both takes the domain back before the network.
func TestAMachineIsRemovedBeforeTheManagedNetworkItPlugsInto(t *testing.T) {
	input := planInput(reconciliation.Apply)
	host, err := NewHost(nil).Plan(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	machine, err := NewMachine(nil).Plan(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := reconciliation.NewPlan(reconciliation.Apply, append(host.Definitions, machine.Definitions...))
	if err != nil {
		t.Fatal(err)
	}
	removal, err := plan.Inverse()
	if err != nil {
		t.Fatal(err)
	}
	hosts := 0
	for _, block := range removal.Blocks {
		if block.Kind != HostKind {
			continue
		}
		hosts++
		if !slices.Contains(block.Dependencies, "machine-rhel-01") {
			t.Fatalf("the host block's removal waits for %v, not the machine's", block.Dependencies)
		}
	}
	if hosts != 1 {
		t.Fatalf("the removal holds %d host blocks: %+v", hosts, removal.Blocks)
	}
}
