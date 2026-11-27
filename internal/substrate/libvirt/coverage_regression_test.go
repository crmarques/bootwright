package libvirt

import (
	"context"
	"errors"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate/capacity"
)

func TestLibvirtStartPowerBorrowsOnlyProviderAccess(t *testing.T) {
	_, block := remoteMachine(t)
	capability := NewMachine(nil)
	want := []string{"hypervisor-key", "host-key"}
	if got, err := capability.StartPowerSecrets(block); err != nil || !slices.Equal(got, want) {
		t.Fatalf("start power secrets = %v (%v), want provider access %v", got, err, want)
	}
	block.Request = []byte("{}")
	if got, err := capability.StartPowerSecrets(block); err == nil || len(got) != 0 {
		t.Fatalf("unreadable request borrowed secrets %v (%v)", got, err)
	}
}

func TestLibvirtCapacityAdmissionPropagatesUnprovedWriteBudget(t *testing.T) {
	environment := api.NewObject(api.Environment, "lab", api.Value{}, api.MapValue(field("controller", api.MapValue(text("machineRef", "controller")))))
	catalog := catalogOf(controller(), environment, provider(), networkConfig(), guest("rhel-01"))
	block := reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{
		Kind: MachineKind, Implementation: MachineImplementation, Object: "rhel-01",
	}}
	unavailable := errors.New("write budget unavailable")
	err := AdmitCapacity(context.Background(), testContext, catalog, map[string]lifecycle.ObjectOwnership{}, []reconciliation.Block{block}, nil,
		func(_ context.Context, _ api.Catalog, _ string, _ []capacity.Allocation, _ capacity.Unfinished) ([]capacity.Write, error) {
			return nil, unavailable
		})
	if !errors.Is(err, unavailable) {
		t.Fatalf("capacity admission hid the unproved budget: %v", err)
	}
}
