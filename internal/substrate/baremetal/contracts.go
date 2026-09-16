package baremetal

import (
	"context"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// Runner performs one authorized operation through the fixed Ansible
// entrypoint. It chooses no target, implementation or workflow, and returns
// bounded structured evidence the capability validates strictly.
type Runner interface {
	Run(context.Context, lifecycle.RunRequest) (lifecycle.RunResult, error)
}
