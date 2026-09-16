package main

import (
	"context"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/machine"
	machineaccess "github.com/crmarques/bootwright/internal/machine/access"
	"github.com/crmarques/bootwright/internal/machine/inventory"
	"github.com/crmarques/bootwright/internal/machine/power"
	"github.com/crmarques/bootwright/internal/reconciliation/ansiblerunner"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// machineDependencies names what the Machine commands read: the selected
// context's own compiled graph, the durable evidence an operation published
// about it, and the bounded execution boundary a power operation crosses.
type machineDependencies struct {
	State     inventory.EffectiveState
	Lifecycle lifecycle.Service
	Confirmer power.Confirmer
	Selection contexts.SelectionStore
}

// wireMachine binds Machine inspection, explicit access and power. Inspection
// and access read state alone; power crosses the one Ansible boundary every
// managed-component effect crosses, through the Machine's own controller.
func wireMachine(deps machineDependencies) cli.Services {
	selection := currentSelection(deps.Selection)
	evidence := machineOwnership{reconciler: deps.Lifecycle}
	return cli.Services{
		MachineInventory: inventory.New(deps.State, evidence, selection),
		MachineAccess:    machineaccess.New(deps.State, selection),
		MachinePower: power.New(deps.State, evidence, deps.Lifecycle, ansiblerunner.New(),
			deps.Confirmer, selection),
	}
}

// machineOwnership presents reconciliation's durable evidence in the Machine
// domain's own vocabulary, so a Machine command depends on what evidence means
// rather than on the engine that published it.
type machineOwnership struct{ reconciler lifecycle.Service }

func (o machineOwnership) Ownership(ctx context.Context, name string) (map[string]machine.OwnershipState, error) {
	published, err := o.reconciler.Ownership(ctx, name)
	if err != nil {
		return nil, err
	}
	owned := make(map[string]machine.OwnershipState, len(published))
	for identity, state := range published {
		owned[identity] = machine.OwnershipState{Verb: state.Verb, State: state.State}
	}
	return owned, nil
}
