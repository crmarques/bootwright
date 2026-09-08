package cli

import (
	"context"
	"errors"

	"github.com/crmarques/bootwright/internal/controller"
)

func (s Services) invokeController(ctx context.Context, path string, values *requestValues, args []string) error {
	if s.Controller == nil {
		return errMissingService
	}
	switch path {
	case "preflight bastion":
		return invokeRequest(ctx, values, controller.CheckRequest{}, s.Controller.Check)
	case "bastion setup":
		return invokeRequest(ctx, values, controller.SetupRequest{
			DryRun:           values.boolean("dry-run"),
			SkipConfirmation: values.boolean("yes"),
		}, s.Controller.Setup)
	default:
		return errors.New("command has no application dispatch")
	}
}
