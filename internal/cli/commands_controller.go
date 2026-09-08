package cli

import (
	"context"
	"errors"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

func controllerPreflightCommand() commandSpec {
	return commandSpec{path: "preflight bastion", short: "Check controller prerequisites"}
}

func controllerSetupCommand() commandSpec {
	return commandSpec{path: "bastion setup", short: "Set up controller prerequisites", flags: []flagSpec{dryRunFlag(), confirmationFlag()}}
}

type ControllerService interface {
	Check(context.Context, prerequisites.CheckRequest) error
	Setup(context.Context, prerequisites.SetupRequest) error
}

func (s Services) invokeController(ctx context.Context, path string, values *requestValues, args []string) error {
	if s.Controller == nil {
		return errMissingService
	}
	switch path {
	case "preflight bastion":
		return invokeRequest(ctx, values, prerequisites.CheckRequest{}, s.Controller.Check)
	case "bastion setup":
		return invokeRequest(ctx, values, prerequisites.SetupRequest{
			DryRun:           values.boolean("dry-run"),
			SkipConfirmation: values.boolean("yes"),
		}, s.Controller.Setup)
	default:
		return errors.New("command has no application dispatch")
	}
}
