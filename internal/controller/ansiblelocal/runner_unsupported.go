//go:build !linux || !amd64

package ansiblelocal

import (
	"context"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

func run(ctx context.Context, _ prerequisites.PythonLaunch, _ capabilityRequest, _ func() error, _ func(context.Context, prerequisites.NativePreparation) error) (prerequisites.ActionResult, error) {
	if err := ctx.Err(); err != nil {
		return actionResult("failed", false), err
	}
	return actionResult("failed", false), failure("controller.unsupported", "the Ansible controller adapter requires Linux amd64")
}
