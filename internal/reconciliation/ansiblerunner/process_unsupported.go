//go:build !(linux && amd64)

package ansiblerunner

import (
	"context"
	"maps"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type Runner struct {
	playbooks map[string]string
}

func New(playbooks map[string]string) Runner { return Runner{playbooks: maps.Clone(playbooks)} }

func (Runner) Run(context.Context, lifecycle.RunRequest) (lifecycle.RunResult, error) {
	return lifecycle.RunResult{}, failure("lifecycle.state", "lifecycle adapter execution requires Linux on amd64", "")
}
