package diskdiscovery

import (
	"context"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type Runner interface {
	Run(context.Context, lifecycle.RunRequest) (lifecycle.RunResult, error)
}

type DiscoveryOperations interface {
	Prepare(context.Context, lifecycle.PrepareRequest) (*lifecycle.OperationResult, error)
	AppliedEvidence(ctx context.Context, contextName, kind, object string) ([]lifecycle.BlockEvidence, error)
	PlanPreparation(context.Context, lifecycle.PrepareRequest) (*lifecycle.PlanResult, error)
}
