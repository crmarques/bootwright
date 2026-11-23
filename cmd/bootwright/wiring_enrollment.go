package main

import (
	"context"
	"crypto/rand"

	"github.com/crmarques/bootwright/ansible"
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/machine/account"
	"github.com/crmarques/bootwright/internal/machine/hardware"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func enrollmentRemote(deps serviceDependencies) account.Remote {
	remote := account.Remote{
		Launcher: sessionClient(deps.Home, deps.Owner, deps.Files, deps.CopyAccount),
		Caller:   invokingSSHUser(deps.CopyAccount), Hardware: ansible.MachineHardwareSource(),
		Entropy: rand.Read,
	}
	if deps.EnrollmentTerminal != nil {
		remote.Prompt = deps.EnrollmentTerminal.ReadHidden
	}
	return remote
}

func withEnrollmentInvocation(begin lifecycle.BeginInvocation) lifecycle.BeginInvocation {
	return func(ctx context.Context) (context.Context, func()) {
		scoped, end := begin(ctx)
		scoped, clear := account.BeginInvocation(scoped)
		return scoped, func() {
			clear()
			end()
		}
	}
}

func enrollmentPreparations(resolver lifecycle.CapabilityResolver) []lifecycle.PreparationDefinition {
	definition := lifecycle.PreparationDefinition{
		Kind: machine.EnrollmentFamily, Command: "machine enroll",
		Bearing:  func(api.Object) (lifecycle.Bearing, bool) { return lifecycle.Bearing{}, false },
		Validate: validateEnrollment,
		Advance: func(_ context.Context, input lifecycle.PreparationAdvance) error {
			if input.Before != nil {
				return enrollmentDefinitionError("the selected desired state changed after machine enrollment")
			}
			return nil
		},
	}
	for _, binding := range resolver.Bindings() {
		switch binding.Implementation {
		case hardware.Implementation, account.Implementation:
			definition.Bindings = append(definition.Bindings, binding)
		}
	}
	if len(definition.Bindings) != 2 {
		return nil
	}
	return []lifecycle.PreparationDefinition{definition}
}

func validateEnrollment(_ context.Context, input lifecycle.PlanInput, scope reconciliation.PreparationScope) error {
	if len(scope.Targets) != 1 {
		return enrollmentDefinitionError("select one Machine for first access")
	}
	object, ok := input.State.Effective().Find(api.Machine, scope.Targets[0])
	_, enabled := machine.BootstrapOf(object)
	if !ok || !enabled {
		return enrollmentDefinitionError("the selected Machine declares no managed lifecycle enrollment")
	}
	return nil
}

func enrollmentDefinitionError(reason string) error {
	return machine.EnrollmentRefused(reason, "check the selected Machine and its access.lifecycle.bootstrap declaration")
}
