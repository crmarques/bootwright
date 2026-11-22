package diskdiscovery

import (
	"context"
	"slices"

	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type DiscoveryService struct{ operations DiscoveryOperations }

func NewDiscoveryService(operations DiscoveryOperations) DiscoveryService {
	return DiscoveryService{operations: operations}
}

func (s DiscoveryService) Discover(ctx context.Context, r DiscoverRequest) (*DiscoverResult, error) {
	operation, err := s.operations.Prepare(ctx, lifecycle.PrepareRequest{ContextName: r.ContextName, Kind: DiscoveryPreparation, Targets: r.Names, PreparePrerequisites: r.PreparePrerequisites, SkipConfirmation: r.SkipConfirmation})
	if operation == nil {
		return nil, err
	}
	result := &DiscoverResult{Operation: operation, Inventories: []DiscoveryInventory{}}
	if err != nil || operation.Receipt.State != string(reconciliation.OperationDone) {
		return result, err
	}
	recorded, err := s.Inventory(ctx, InventoryRequest{ContextName: operation.Context.Name, Names: r.Names})
	if err != nil {
		return result, err
	}
	result.Inventories = slices.DeleteFunc(recorded.Inventories, func(inventory DiscoveryInventory) bool { return inventory.Source == "" })
	return result, nil
}

func (s DiscoveryService) PlanDiscovery(ctx context.Context, r DiscoverRequest) (*lifecycle.PlanResult, error) {
	return s.operations.PlanPreparation(ctx, lifecycle.PrepareRequest{ContextName: r.ContextName, Kind: DiscoveryPreparation, Targets: r.Names, PreparePrerequisites: r.PreparePrerequisites})
}

func (s DiscoveryService) Inventory(ctx context.Context, r InventoryRequest) (*InventoryResult, error) {
	records, err := s.operations.EvidenceForObjects(ctx, r.ContextName, installation.Kind, r.Names)
	if err != nil {
		return nil, err
	}
	result := &InventoryResult{Inventories: make([]DiscoveryInventory, 0, len(r.Names))}
	for _, name := range r.Names {
		inventory, _, err := RecordedInventory(name, records[name])
		if err != nil {
			return nil, err
		}
		result.Inventories = append(result.Inventories, inventory)
	}
	return result, nil
}
