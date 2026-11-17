package cli

import (
	"context"

	"github.com/crmarques/bootwright/internal/managedos/diskdiscovery"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type MachineDiscoveryService interface {
	Discover(context.Context, diskdiscovery.DiscoverRequest) (*diskdiscovery.DiscoverResult, error)
	PlanDiscovery(context.Context, diskdiscovery.DiscoverRequest) (*lifecycle.PlanResult, error)
}

func (s Services) invokeMachineDiscovery(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.MachineDiscovery == nil {
		return commandResult{}, errMissingService
	}
	r := diskdiscovery.DiscoverRequest{ContextName: values.text("context"), Names: values.names("name"), PreparePrerequisites: values.boolean("prepare-prerequisites"), SkipConfirmation: values.boolean("yes")}
	if values.boolean("dry-run") {
		result, err := invokeResult(ctx, values, r, s.MachineDiscovery.PlanDiscovery)
		return commandResult{lifecyclePlan: result}, err
	}
	result, err := invokeResult(ctx, values, r, s.MachineDiscovery.Discover)
	if result == nil {
		return commandResult{}, err
	}
	return commandResult{machineDiscovery: result, lifecycleOperation: result.Operation}, err
}
