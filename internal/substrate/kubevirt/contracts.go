package kubevirt

import (
	"context"
	"time"

	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

// HostAccess mints the access one run on a host cluster of this context is
// lent, which the caller clears.
type HostAccess interface {
	LendHostAccess(ctx context.Context, execution lifecycle.Execution, cluster, namespace string, placement machineref.Placement, deadline time.Duration) (secrets.Material, error)
}

// Runner performs one authorized operation through the fixed Ansible
// entrypoint. It chooses no target, implementation or workflow, and returns
// bounded structured evidence the capability validates strictly.
type Runner interface {
	Run(context.Context, lifecycle.RunRequest) (lifecycle.RunResult, error)
}
