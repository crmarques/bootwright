package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/clients"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate/baremetal"
)

type machineDiscovery struct{ lifecycle lifecycle.Service }

func discoveryNonce() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}

func (s machineDiscovery) Discover(ctx context.Context, r installation.DiscoverRequest) (*installation.DiscoverResult, error) {
	operation, err := s.lifecycle.Prepare(ctx, lifecycle.PrepareRequest{ContextName: r.ContextName, Kind: installation.DiscoveryPreparation, Targets: r.Names, PreparePrerequisites: r.PreparePrerequisites, SkipConfirmation: r.SkipConfirmation})
	if operation == nil {
		return nil, err
	}
	result := &installation.DiscoverResult{Operation: operation, Inventories: []installation.DiscoveryInventory{}}
	if err != nil || operation.Receipt.State != string(reconciliation.OperationDone) {
		return result, err
	}
	for _, name := range r.Names {
		records, readErr := s.lifecycle.AppliedEvidence(ctx, operation.Context.Name, installation.Kind, name)
		if readErr != nil {
			return result, readErr
		}
		inventory, found, readErr := installation.RecordedInventory(name, records)
		if readErr != nil {
			return result, readErr
		}
		if found {
			result.Inventories = append(result.Inventories, inventory)
		}
	}
	return result, nil
}

func discoveryPreparations(resolver lifecycle.CapabilityResolver) []lifecycle.PreparationDefinition {
	bound, found := resolver.Resolve(installation.Kind, installation.DiscoveryImplementation)
	owner, ok := bound.(installation.DiscoveryCapability)
	if !found || !ok {
		return nil
	}
	definition := lifecycle.PreparationDefinition{Automatic: owner.AutomaticPreparation, Kind: installation.DiscoveryPreparation, Command: "machine discover", Validate: owner.ValidatePreparation, Advance: owner.AdvancePreparation,
		Bearing: func(api.Object) (lifecycle.Bearing, bool) { return lifecycle.Bearing{}, false }}
	for _, binding := range resolver.Bindings() {
		switch binding.Implementation {
		case baremetal.Implementation, installation.DiscoveryImplementation:
			definition.Bindings = append(definition.Bindings, binding)
		case clients.Implementation, artifactserver.Implementation:
			definition.Prerequisites = append(definition.Prerequisites, binding)
		default:
			if binding.Family == managedservice.Family {
				definition.Prerequisites = append(definition.Prerequisites, binding)
			}
		}
	}
	return []lifecycle.PreparationDefinition{definition}
}

func (s machineDiscovery) PlanDiscovery(ctx context.Context, r installation.DiscoverRequest) (*lifecycle.PlanResult, error) {
	return s.lifecycle.PlanPreparation(ctx, lifecycle.PrepareRequest{ContextName: r.ContextName, Kind: installation.DiscoveryPreparation, Targets: r.Names, PreparePrerequisites: r.PreparePrerequisites})
}
