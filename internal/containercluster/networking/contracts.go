package networking

import (
	"context"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type Runner interface {
	Run(context.Context, lifecycle.RunRequest) (lifecycle.RunResult, error)
}
