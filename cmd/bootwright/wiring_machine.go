package main

import (
	"context"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/containercluster/agentinstall"
	"github.com/crmarques/bootwright/internal/containercluster/openshift"
	"github.com/crmarques/bootwright/internal/controller/hostlinux"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	environmentaccess "github.com/crmarques/bootwright/internal/environment/access"
	"github.com/crmarques/bootwright/internal/machine"
	machineaccess "github.com/crmarques/bootwright/internal/machine/access"
	"github.com/crmarques/bootwright/internal/machine/inventory"
	"github.com/crmarques/bootwright/internal/machine/power"
	"github.com/crmarques/bootwright/internal/machine/sshlocal"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/nativeexec"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/ansiblerunner"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
	"github.com/crmarques/bootwright/internal/substrate/baremetal"
	"github.com/crmarques/bootwright/internal/substrate/kubevirt"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
	"github.com/crmarques/bootwright/internal/substrate/vsphere"
	"github.com/crmarques/bootwright/internal/trust"
	"github.com/crmarques/bootwright/internal/trust/enrollment"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// machineDependencies names what the Machine commands read: the selected
// context's own compiled graph, the durable evidence an operation published
// about it, the host keys it trusts, and the bounded execution boundaries a
// power operation and an SSH session each cross.
type machineDependencies struct {
	State       inventory.EffectiveState
	Lifecycle   lifecycle.Service
	Trust       trustStore
	Confirmer   power.Confirmer
	Session     machineaccess.Confirmer
	Reporter    power.Reporter
	Selection   contexts.SelectionStore
	Streams     machineaccess.Streams
	Terminal    func() (bool, error)
	CopyAccount func(context.Context) (machineaccess.CopyAccount, error)
	Home        func() (string, error)
	Owner       func() (int, error)
	Files       operatorFiles
	Stager      prerequisites.AutomationStager
	Custody     hostCustody
	Pins        enrollment.ReadPin
	PublicKey   func(context.Context, string) ([]byte, error)
	Admit       func(context.Context) (func(), error)
	Authorize   func(context.Context, string, api.Catalog, []machine.SessionTarget, []string) error
}

// wireMachine binds Machine inspection, explicit access and power. Inspection
// reads local state and asks the power capability alone when an invocation
// wants a live reading; a session runs the one pinned SSH client after proving
// the host key its context already holds; power crosses the one Ansible
// boundary every managed-component effect crosses.
func wireMachine(deps machineDependencies) cli.Services {
	selection := currentSelection(deps.Selection)
	evidence := machineOwnership{reconciler: deps.Lifecycle}
	client := sessionClient(deps.Home, deps.Owner, deps.Files, deps.CopyAccount)
	runner := ansiblerunner.New(operationPlaybook(), deps.Stager).AdmitReadOnly(readOnlyOperations()...)
	powered := power.New(deps.State, power.NewEvidenceRealization(deps.Lifecycle, realizesMachine), machineIdentities{reconciler: deps.Lifecycle},
		deps.Lifecycle, runner, runner.BeginInvocation, deps.Confirmer, deps.Reporter, selection).
		WithHostAccess(hostedAccess{lender: kubevirt.NewLender(runner), custody: deps.Custody})
	accessState := nodeAccessState{state: deps.State}
	sessions := machineaccess.New(accessState, selection, machineaccess.Options{
		InvokingUser: invokingSSHUser(deps.CopyAccount),
		Copier:       sshlocal.Copier{SSH: client, Account: deps.CopyAccount},
		Lender:       deps.Lifecycle, Ownership: evidence, Evidence: machineHostKeys{reconciler: deps.Lifecycle},
		Trust: deps.Trust, Observer: client, Confirmer: deps.Session, Launcher: client,
		Streams: deps.Streams, Terminal: deps.Terminal, Authorize: deps.Authorize, Prepared: sessionPrepared,
	})
	var direct cli.MachineAccessService = sessions
	var cluster cli.EnvironmentAccessService = clusterSessions(deps.State, sessions, selection)
	if deps.Admit != nil {
		bound := delegatedSessions{hold: deps.Admit, selection: selection, direct: direct, cluster: cluster}
		direct, cluster = bound, bound
	}
	return cli.Services{
		MachineInventory: inventory.New(accessState, evidence, powered, selection).WithAccount(func(ctx context.Context) string {
			if deps.CopyAccount == nil {
				return ""
			}
			account, err := deps.CopyAccount(ctx)
			if err != nil {
				return ""
			}
			return account.Name
		}),
		MachineAccess:     direct,
		MachineCopy:       sessions,
		EnvironmentAccess: cluster,
		MachinePower:      powered,
		MachineTrust:      enrollment.New(accessState, deps.Trust, selection, trustOptions(deps, client)),
	}
}

func invokingSSHUser(resolve func(context.Context) (machineaccess.CopyAccount, error)) func(context.Context) (string, error) {
	if resolve == nil {
		return nil
	}
	return func(ctx context.Context) (string, error) {
		account, err := resolve(ctx)
		return account.Name, err
	}
}

func sessionClient(home func() (string, error), owner func() (int, error), files operatorFiles, resolve func(context.Context) (machineaccess.CopyAccount, error)) sshlocal.Launcher {
	client := sshlocal.New(home, owner, sessionFiles(files))
	if resolve != nil {
		client.Account = func(ctx context.Context) (nativeexec.Account, error) {
			account, err := resolve(ctx)
			return nativeexec.Account{UID: uint32(account.UID), GID: uint32(account.GID), Groups: account.Groups}, err
		}
	}
	return client
}

func clusterSessions(state inventory.EffectiveState, sessions machineaccess.Service, selection func(context.Context) (string, error)) environmentaccess.Service {
	return environmentaccess.New(effectiveGraph{state: state}, machineSessions{access: sessions}, selection).WithMembers(clusterMembers()).WithAdministrator(clusterAdministrator)
}

type effectiveGraph struct{ state inventory.EffectiveState }

func (g effectiveGraph) Effective(ctx context.Context, contextName string) (api.Catalog, error) {
	result, err := g.state.RenderEffective(ctx, compilation.EffectiveRequest{ContextName: contextName})
	if err != nil {
		return api.Catalog{}, err
	}
	return result.Effective, nil
}

type machineSessions struct{ access machineaccess.Service }

func (m machineSessions) Rsh(ctx context.Context, contextName, name string, catalog api.Catalog, ssh machine.SSHOptions) (*machine.SessionResult, error) {
	return m.access.WithCatalog(openshift.AccessCatalog(catalog)).Rsh(ctx, machineaccess.RshRequest{ContextName: contextName, Name: name, SSH: ssh})
}

func (m machineSessions) Exec(ctx context.Context, contextName string, names []string, catalog api.Catalog, ssh machine.SSHOptions, command []string) (*machine.SessionResult, error) {
	return m.access.WithCatalog(openshift.AccessCatalog(catalog)).Exec(ctx, machineaccess.ExecRequest{ContextName: contextName, Names: names, SSH: ssh, Command: command})
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
	options := enrollment.Options{Observer: observer, Pins: deps.Pins, PublicKey: deps.PublicKey, Confirmer: deps.Confirmer, Clock: time.Now}
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

func (o machineOwnership) Completed(ctx context.Context, contextName, kind, implementation, object string) (bool, error) {
	published, err := o.reconciler.Evidence(ctx, contextName, kind, object)
	if err != nil {
		return false, err
	}
	completed := false
	for _, block := range published {
		if block.Implementation != implementation {
			continue
		}
		if block.Verb != reconciliation.Apply || block.State != reconciliation.BlockDone {
			return false, nil
		}
		completed = true
	}
	return completed, nil
}

func realizesMachine(implementation string) bool {
	return implementation == libvirt.MachineImplementation || implementation == kubevirt.MachineImplementation || implementation == vsphere.Implementation
}

// machineHostKeys reads the host key one context's own installation proved.
// Reconciliation never interprets a capability's evidence, so the capability
// that wrote it decodes it here, and a session asks only for the answer.
type machineHostKeys struct{ reconciler evidenceReader }

func (h machineHostKeys) HostKey(ctx context.Context, contextName, name string) (machine.HostKeyEvidence, bool, error) {
	published, err := h.reconciler.Evidence(ctx, contextName, "Machine", name)
	if err != nil {
		return machine.HostKeyEvidence{}, false, err
	}
	for _, block := range published {
		if block.Implementation != installation.Implementation || block.Verb != reconciliation.Apply || block.State != reconciliation.BlockDone || len(block.Evidence) == 0 {
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

// machineIdentities reads the identity a Machine's apply proved, the current one
// or the one a current destroy takes back. The capability that wrote that
// evidence decodes it here and decides what pins the Machine; a power
// operation asks only for the answer.
type machineIdentities struct{ reconciler appliedEvidence }

type appliedEvidence interface {
	AppliedEvidence(ctx context.Context, contextName, kind, object string) ([]lifecycle.BlockEvidence, error)
}

func (machineIdentities) ControllerHost(ctx context.Context) (power.HostIdentity, error) {
	inspector := hostlinux.New()
	identity, err := inspector.Identity(ctx)
	if err != nil {
		return power.HostIdentity{}, err
	}
	addresses, err := inspector.HardwareAddresses(ctx)
	if err != nil {
		return power.HostIdentity{}, err
	}
	return power.HostIdentity{UUID: identity.ProductUUID(), MACs: addresses}, nil
}

func (i machineIdentities) ProvedIdentity(ctx context.Context, contextName, name string) (machine.HardwareIdentity, bool, error) {
	published, err := i.reconciler.AppliedEvidence(ctx, contextName, baremetal.Kind, name)
	if err != nil {
		return machine.HardwareIdentity{}, false, err
	}
	return pinnedIdentity(name, published)
}

func pinnedIdentity(name string, published []lifecycle.BlockEvidence) (machine.HardwareIdentity, bool, error) {
	if pin, found, err := baremetal.PinnedIdentity(name, published); err != nil || found {
		return pin, found, err
	}
	if pin, found, err := kubevirt.PinnedIdentity(name, published); err != nil || found {
		return pin, found, err
	}
	return vsphere.PinnedIdentity(name, published)
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
	return pinnedIdentity(name, own)
}

type hostCustody interface {
	ReadProduced(context.Context, custody.ReadProducedRequest) (secretstore.ProducedMaterial, bool, error)
}

type hostedAccess struct {
	lender  kubevirt.Lender
	custody hostCustody
}

func (a hostedAccess) LendHostAccess(ctx context.Context, lend power.HostLend) (secrets.Material, func(), error) {
	execution := kubevirt.UtilityExecution(lifecycle.Execution{
		Context: lend.Runtime.Context.Name, Launch: lend.Runtime.Launch, Output: lend.Runtime.Output,
		Block: reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{Kind: string(api.Machine), Object: lend.Machine}, RequestDigest: lend.Digest},
	}, lend.Cluster, a.administrator)
	return a.lender.LendHostAccess(ctx, execution, lend.Cluster, lend.Namespace, lend.Placement, lend.Deadline)
}

func (a hostedAccess) administrator(ctx context.Context, contextName, cluster string) (secrets.Material, bool, bool, error) {
	read, found, err := a.custody.ReadProduced(ctx, custody.ReadProducedRequest{ContextName: contextName, Block: agentinstall.InstallBlockID(cluster), Name: agentinstall.KubeconfigOutput})
	return read.Material, found, read.Unproved, err
}
