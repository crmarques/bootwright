package main

import (
	"context"
	"time"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/machine"
	machineaccess "github.com/crmarques/bootwright/internal/machine/access"
	"github.com/crmarques/bootwright/internal/machine/inventory"
	"github.com/crmarques/bootwright/internal/machine/power"
	"github.com/crmarques/bootwright/internal/machine/sshlocal"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation/ansiblerunner"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/trust"
	"github.com/crmarques/bootwright/internal/trust/enrollment"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// machineDependencies names what the Machine commands read: the selected
// context's own compiled graph, the durable evidence an operation published
// about it, the host keys it trusts, and the bounded execution boundaries a
// power operation and an SSH session each cross.
type machineDependencies struct {
	State     inventory.EffectiveState
	Lifecycle lifecycle.Service
	Trust     trustStore
	Confirmer power.Confirmer
	Session   machineaccess.Confirmer
	Reporter  power.Reporter
	Selection contexts.SelectionStore
	Streams   machineaccess.Streams
	Terminal  func() (bool, error)
	Home      func() (string, error)
}

// wireMachine binds Machine inspection, explicit access and power. Inspection
// reads local state and asks the power capability alone when an invocation
// wants a live reading; a session runs the one pinned SSH client after proving
// the host key its context already holds; power crosses the one Ansible
// boundary every managed-component effect crosses.
func wireMachine(deps machineDependencies) cli.Services {
	selection := currentSelection(deps.Selection)
	evidence := machineOwnership{reconciler: deps.Lifecycle}
	client := sshlocal.New(deps.Home)
	powered := power.New(deps.State, evidence, deps.Lifecycle, ansiblerunner.New(operationPlaybook()),
		deps.Confirmer, deps.Reporter, selection)
	return cli.Services{
		MachineInventory: inventory.New(deps.State, evidence, powered, selection),
		MachineAccess: machineaccess.New(deps.State, selection, machineaccess.Options{
			Lender: deps.Lifecycle, Ownership: evidence, Evidence: machineHostKeys{reconciler: deps.Lifecycle},
			Trust: deps.Trust, Observer: client, Confirmer: deps.Session, Launcher: client,
			Streams: deps.Streams, Terminal: deps.Terminal,
		}),
		MachinePower: powered,
		MachineTrust: enrollment.New(deps.State, deps.Trust, selection, enrollment.Options{
			Observer: client, Confirmer: deps.Confirmer, Clock: time.Now,
		}),
	}
}

// trustStore is the one host-key area both a session and enrollment reach. The
// two capabilities name the same contract, so composition binds one store.
type trustStore interface {
	machineaccess.HostKeyStore
	enrollment.HostKeyStore
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

// machineHostKeys reads the host key one context's own installation proved.
// Reconciliation never interprets a capability's evidence, so the capability
// that wrote it decodes it here, and a session asks only for the answer.
type machineHostKeys struct{ reconciler lifecycle.Service }

func (h machineHostKeys) HostKey(ctx context.Context, contextName, name string) (machine.HostKeyEvidence, bool, error) {
	published, err := h.reconciler.Evidence(ctx, contextName, "Machine", name)
	if err != nil {
		return machine.HostKeyEvidence{}, false, err
	}
	for _, block := range published {
		if block.Implementation != installation.Implementation || len(block.Evidence) == 0 {
			continue
		}
		address, key, err := installation.HostKeyEvidence(block.Evidence)
		if err != nil {
			return machine.HostKeyEvidence{}, false, err
		}
		parsed, err := trust.ParseAuthorizedKey(key)
		if err != nil {
			return machine.HostKeyEvidence{}, false, err
		}
		return machine.HostKeyEvidence{Address: address, HostKey: parsed}, true, nil
	}
	return machine.HostKeyEvidence{}, false, nil
}
