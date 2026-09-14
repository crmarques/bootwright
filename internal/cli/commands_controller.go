package cli

import (
	"context"
	"errors"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

func controllerPreflightCommand() commandSpec {
	return available(commandSpec{path: "preflight controller", short: "Check controller prerequisites", long: "Verify local controller prerequisites without installing or changing state. Omit --context for baseline checks; only an explicit nonempty --context selects Environment requirements."})
}

func controllerSetupCommand() commandSpec {
	return available(commandSpec{path: "controller setup", short: "Set up controller prerequisites", flags: []flagSpec{dryRunFlag(), confirmationFlag()}, long: "Prepare qualified local controller prerequisites. Omit --context for baseline setup; only an explicit nonempty --context selects Environment requirements. --dry-run previews the plan with unverified checks and makes no changes. Review the plan before confirming; --yes skips ordinary confirmation."})
}

type ControllerService interface {
	Check(context.Context, prerequisites.CheckRequest) (*prerequisites.Report, error)
	Setup(context.Context, prerequisites.SetupRequest) (*prerequisites.Report, error)
}

func (s Services) invokeController(ctx context.Context, path string, values *requestValues, args []string) (commandResult, error) {
	if s.Controller == nil {
		return commandResult{}, errMissingService
	}
	var result commandResult
	var err error
	switch path {
	case "preflight controller":
		result.controller, err = invokeResult(ctx, values, prerequisites.CheckRequest{ContextName: values.text("context")}, s.Controller.Check)
	case "controller setup":
		result.controller, err = invokeResult(ctx, values, prerequisites.SetupRequest{
			ContextName:      values.text("context"),
			DryRun:           values.boolean("dry-run"),
			SkipConfirmation: values.boolean("yes"),
		}, s.Controller.Setup)
	default:
		return commandResult{}, errors.New("command has no application dispatch")
	}
	return result, err
}
