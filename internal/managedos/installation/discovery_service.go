package installation

import (
	"context"

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
	for _, name := range r.Names {
		records, readErr := s.operations.AppliedEvidence(ctx, operation.Context.Name, Kind, name)
		if readErr != nil {
			return result, readErr
		}
		inventory, found, readErr := RecordedInventory(name, records)
		if readErr != nil {
			return result, readErr
		}
		if found {
			result.Inventories = append(result.Inventories, inventory)
		}
	}
	return result, nil
}

func (s DiscoveryService) PlanDiscovery(ctx context.Context, r DiscoverRequest) (*lifecycle.PlanResult, error) {
	return s.operations.PlanPreparation(ctx, lifecycle.PrepareRequest{ContextName: r.ContextName, Kind: DiscoveryPreparation, Targets: r.Names, PreparePrerequisites: r.PreparePrerequisites})
}
