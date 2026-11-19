package main

import (
	"context"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine/power"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
)

func registeredMachineDriver(runner power.Runner) func(context.Context, lifecycle.Execution, lifecycle.BlockEvidence, bool) error {
	return func(ctx context.Context, e lifecycle.Execution, proof lifecycle.BlockEvidence, on bool) error {
		verb := power.Stop
		if on {
			verb = power.Start
		}
		request, pin, err := registeredMachineRequest(proof, verb)
		if err != nil {
			return err
		}
		// The native endpoint and identity stay frozen across context rename.
		request.Identity.Context = api.ContextName(e.Context)
		var host power.HostIdentity
		if substrate.PhysicalController(request.Arm) {
			host, err = (machineIdentities{}).ControllerHost(ctx)
			if err != nil {
				return err
			}
		}
		return power.DriveRegistered(ctx, runner, e, request, pin, host)
	}
}
