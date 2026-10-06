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
	"github.com/crmarques/bootwright/internal/substrate/baremetal"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
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
	Owner     func() (int, error)
	Files     operatorFiles
}

// wireMachine binds Machine inspection, explicit access and power. Inspection
// reads local state and asks the power capability alone when an invocation
// wants a live reading; a session runs the one pinned SSH client after proving
// the host key its context already holds; power crosses the one Ansible
// boundary every managed-component effect crosses.
func wireMachine(deps machineDependencies) cli.Services {
	selection := currentSelection(deps.Selection)
	evidence := machineOwnership{reconciler: deps.Lifecycle}
	client := sshlocal.New(deps.Home, deps.Owner, sessionFiles(deps.Files))
	powered := power.New(deps.State, machineRealization{reconciler: deps.Lifecycle}, machineIdentities{reconciler: deps.Lifecycle},
		deps.Lifecycle, ansiblerunner.New(operationPlaybook()), deps.Confirmer, deps.Reporter, selection)
	return cli.Services{
		MachineInventory: inventory.New(deps.State, evidence, powered, selection),
		MachineAccess: machineaccess.New(deps.State, selection, machineaccess.Options{
			Lender: deps.Lifecycle, Ownership: evidence, Evidence: machineHostKeys{reconciler: deps.Lifecycle},
			Trust: deps.Trust, Observer: client, Confirmer: deps.Session, Launcher: client,
			Streams: deps.Streams, Terminal: deps.Terminal,
		}),
		MachinePower: powered,
		MachineTrust: enrollment.New(deps.State, deps.Trust, selection, trustOptions(deps, client)),
	}
}

// sessionFiles opens an offered key through the invoking account's opener, and
// binds no opener at all when files is nil, so an offered key refuses rather
// than reaching a wrapper around nothing.
func sessionFiles(files operatorFiles) sshlocal.Files {
	if files == nil {
		return nil
	}
	return sshFiles{files: files}
}

type sshFiles struct{ files operatorFiles }

func (f sshFiles) Begin(ctx context.Context) (sshlocal.FileSession, error) {
	session, err := f.files.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return session, nil
}

// trustOptions shows the trust plan on the process's standard output, ahead of
// the prompt on standard error. A process with no output binds no presenter,
// so enrollment refuses to ask rather than ask blind.
func trustOptions(deps machineDependencies, observer enrollment.Observer) enrollment.Options {
	options := enrollment.Options{Observer: observer, Confirmer: deps.Confirmer, Clock: time.Now}
	if deps.Streams.Out != nil {
		options.Presenter = cli.NewTrustPlanPresenter(deps.Streams.Out)
	}
	return options
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

// machineRealization reads the block that realizes one Machine on its
// provider. Its emulated controller is that block's effect, so power asks it
// alone: the installation that follows it on the same Machine, and the
// bare-metal claim of a physical one, create no controller.
type machineRealization struct{ reconciler lifecycle.Service }

func (r machineRealization) Realization(ctx context.Context, contextName, name string) (machine.OwnershipState, bool, error) {
	published, err := r.reconciler.Evidence(ctx, contextName, "Machine", name)
	if err != nil {
		return machine.OwnershipState{}, false, err
	}
	state, found := realizationOf(published)
	return state, found, nil
}

// realizationOf picks the libvirt machine block out of everything the current
// plan froze for one Machine, and reports the verb and state it reached.
func realizationOf(published []lifecycle.BlockEvidence) (machine.OwnershipState, bool) {
	for _, block := range published {
		if block.Implementation == libvirt.MachineImplementation {
			return machine.OwnershipState{Verb: string(block.Verb), State: string(block.State)}, true
		}
	}
	return machine.OwnershipState{}, false
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

// machineIdentities reads the hardware identity one context's current apply
// proved for a physical Machine. The bare-metal capability wrote that
// evidence, so it decodes it here and decides what pins the Machine; a power
// operation asks only for the answer.
type machineIdentities struct{ reconciler lifecycle.Service }

func (i machineIdentities) ProvedIdentity(ctx context.Context, contextName, name string) (machine.HardwareIdentity, bool, error) {
	published, err := i.reconciler.Evidence(ctx, contextName, baremetal.Kind, name)
	if err != nil {
		return machine.HardwareIdentity{}, false, err
	}
	return baremetal.PinnedIdentity(name, published)
}

// provedIdentities reads the identity a physical Machine's own block proved
// earlier in the operation an installation runs in, from the evidence the
// engine handed that installation's attempt. An attempt receives every
// dependency's evidence, another Machine's among them, so only the entries of
// this exact Machine are the bare-metal capability's to decode.
type provedIdentities struct{}

func (provedIdentities) PinnedIdentity(name string, proved []lifecycle.BlockEvidence) (machine.HardwareIdentity, bool, error) {
	var own []lifecycle.BlockEvidence
	for _, block := range proved {
		if block.Kind == baremetal.Kind && block.Object == name {
			own = append(own, block)
		}
	}
	return baremetal.PinnedIdentity(name, own)
}
