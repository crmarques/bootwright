package cli

import (
	"context"
	"errors"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

func controllerPreflightCommand() commandSpec {
	return available(commandSpec{path: "preflight controller", short: "Check controller prerequisites", long: "Verify local controller prerequisites without installing or changing state. Omit --context for the host baseline; only an explicit nonempty --context adds that context's own target tools, the native closures its controller stage selects (the libvirt client, the hypervisor and the installer-media tooling) and its host binding."})
}

func controllerSetupCommand() commandSpec {
	return available(commandSpec{path: "setup", short: "Set up context-independent controller prerequisites", flags: []flagSpec{dryRunFlag(), confirmationFlag(), boolFlag("purge-old-bundles", "Retire the execution bundles this host no longer needs")}, long: "Prepare this host's context-independent controller prerequisites: the private execution bundle and the baseline native packages. Setup selects no context; the prerequisites a context adds are installed by the controller stage of apply. --dry-run previews the plan with unverified checks and makes no changes. --purge-old-bundles retires the superseded execution bundles this host still holds after the setup completes, and first, before it publishes, when a new bundle would not fit within the host's bound of 16; at that bound it first cancels a setup an earlier build left pending there that never took effect and that this executable cannot resume, then sets the host up afresh. It removes no client closure and nothing the current receipt names. Review the plan before confirming; --yes skips ordinary confirmation."})
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
			PurgeOldBundles:  values.boolean("purge-old-bundles"),
		}, s.Controller.Setup)
	default:
		return commandResult{}, errors.New("command has no application dispatch")
	}
	return result, err
}
