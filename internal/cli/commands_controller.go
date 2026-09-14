package cli

import (
	"context"
	"errors"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

func controllerPreflightCommand() commandSpec {
	return available(commandSpec{path: "preflight controller", short: "Check controller prerequisites", long: "Verify local controller prerequisites without installing or changing state. Omit --context for the host baseline; only an explicit nonempty --context adds that context's own target tools and host binding."})
}

func controllerSetupCommand() commandSpec {
	return available(commandSpec{path: "setup", short: "Set up context-independent controller prerequisites", flags: []flagSpec{dryRunFlag(), confirmationFlag()}, long: "Prepare this host's context-independent controller prerequisites: the private execution bundle and the baseline native packages. Setup selects no context; the prerequisites a context adds are installed by the controller stage of apply. --dry-run previews the plan with unverified checks and makes no changes. Review the plan before confirming; --yes skips ordinary confirmation."})
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
	case "setup":
		result.controller, err = invokeResult(ctx, values, prerequisites.SetupRequest{
			DryRun:           values.boolean("dry-run"),
			SkipConfirmation: values.boolean("yes"),
		}, s.Controller.Setup)
	default:
		return commandResult{}, errors.New("command has no application dispatch")
	}
	return result, err
}
