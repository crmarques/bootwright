//go:build !(linux && amd64)

package ansiblerunner

import (
	"context"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type Runner struct{}

func New() Runner { return Runner{} }

func (Runner) Run(context.Context, lifecycle.RunRequest) (lifecycle.RunResult, error) {
	return lifecycle.RunResult{}, failure("lifecycle.state", "lifecycle adapter execution requires Linux on amd64", "")
}
