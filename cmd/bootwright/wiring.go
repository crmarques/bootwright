package main

import (
	"context"
	"os"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/bundlelocal"
	"github.com/crmarques/bootwright/internal/controller/hostlinux"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/controller/privilege"
	machineaccess "github.com/crmarques/bootwright/internal/machine/access"
	"github.com/crmarques/bootwright/internal/machine/power"
	"github.com/crmarques/bootwright/internal/managedos/media"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets/material"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
	"github.com/crmarques/bootwright/internal/workspace/contextfs"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// processDependencies carries the capabilities only an interactive process can
// supply. Its zero value builds the services used by help, completion, version
// and explicit-input validation, which acquire no terminal, account or process
// capability.
type processDependencies struct {
	Confirmer          contexts.Confirmer
	SessionConfirmer   machineaccess.Confirmer
	Streams            machineaccess.Streams
	Terminal           func() (bool, error)
	SecretInput        material.InputReader
	SecretTerminal     material.TerminalInput
	Progress           prerequisites.ProgressReporter
	Presenter          prerequisites.PlanPresenter
	LifecycleProgress  lifecycle.ProgressReporter
	LifecyclePresenter lifecycle.PlanPresenter
	MediaPresenter     media.Presenter
	Executable         lifecycle.Executable
	// AmbientRoute is the acquisition route the invoking environment selected
	// for a context-free command. Its zero value selects nothing, so every
	// other entry point keeps the compiled direct baseline.
	AmbientRoute controller.Route
}

// operatorFiles opens the paths an operator names under the invoking
// account's credentials, so a root process reads only what that account can.
type operatorFiles interface {
	Begin(context.Context) (operatorFileSession, error)
}

type operatorFileSession interface {
	Root() (*os.File, error)
	OpenAt(*os.File, string, int) (*os.File, error)
	OpenFile(string) (*os.File, error)
	Close() error
}

// serviceDependencies names every replaceable implementation the application
// services consume. Each field is a consumer-owned contract, so a test binds
// the same graph through its own implementations.
type serviceDependencies struct {
	Repository       contexts.Repository
	Workspace        secretstore.Workspace
	Selection        contexts.SelectionStore
	Operator         material.Operator
	Confirmer        contexts.Confirmer
	SessionConfirmer machineaccess.Confirmer
	Streams          machineaccess.Streams
	Terminal         func() (bool, error)
	Home             func() (string, error)
	Owner            func() (int, error)
	Files            operatorFiles
	Trust            trustStore
	SecretInput      material.InputReader
	SecretTerminal   material.TerminalInput
	Resolver         secretstore.ImplementationResolver
	SessionMaterial  secretstore.SessionMaterialSource
	Controller       controllerDependencies
	Lifecycle        lifecycleDependencies
	Media            mediaDependencies
	Reporter         power.Reporter
	// AmbientRoute is the acquisition route the invoking environment selected
	// for a context-free command, and is nothing for every other entry point.
	AmbientRoute controller.Route
}

// wireServices also returns the release for every local resource the assembled
// services retain for the length of one invocation.
func wireServices(process processDependencies) (cli.Services, func()) {
	deps, release := localServiceDependencies(process)
	return assembleServices(deps), release
}

// localServiceDependencies binds every local adapter to its port, and also
// returns the release for every local resource those adapters retain.
func localServiceDependencies(process processDependencies) (serviceDependencies, func()) {
	repository := contextfs.New(contextfs.Options{})
	account := invokingAccount{resolver: privilege.Resolver{}}
	files := openerFiles{opener: account.files()}
	controllerPorts, release := localControllerDependencies(repository, process)
	return serviceDependencies{
		Repository:       repository,
		Workspace:        repository,
		Trust:            repository,
		SessionConfirmer: process.SessionConfirmer,
		Streams:          process.Streams,
		Terminal:         process.Terminal,
		Home:             account.home,
		Owner:            account.uid,
		Files:            files,
		Selection:        account,
		Operator:         account,
		Confirmer:        process.Confirmer,
		SecretInput:      process.SecretInput,
		SecretTerminal:   process.SecretTerminal,
		Reporter:         process.LifecycleProgress,
		Controller:       controllerPorts,
		AmbientRoute:     process.AmbientRoute,
		Media:            localMediaDependencies(repository, process.Confirmer, process.LifecycleProgress, process.MediaPresenter, process.AmbientRoute, files),
		Lifecycle: lifecycleDependencies{
			Workspace: repository, Inputs: contexts.Inputs{Repository: repository, Selection: account},
			Host: hostlinux.New(), Guard: bundlelocal.ExecutionGuard{}, Selection: account,
			Presenter: process.LifecyclePresenter, Progress: process.LifecycleProgress,
			Confirmer: process.Confirmer, Executable: process.Executable,
			Media: repository,
		},
	}, release
}

// assembleServices is the complete service graph. Secrets is assembled first
// because context initialization consumes its transaction-scoped hooks.
func assembleServices(deps serviceDependencies) cli.Services {
	compiler := wireCompiler()
	secrets := wireSecrets(deps, compiler)
	services := wireStubs()
	services.Contexts = wireContexts(deps, compiler, secrets.hooks)
	services.Secrets, services.Encryption = secrets.custody, secrets.encryption
	services.DesiredState = wireDesiredState(deps, compiler)
	services.Controller = wireController(deps.Controller, compiler, deps.Confirmer, deps.AmbientRoute)
	services.Media = wireMedia(deps.Media)
	reconciler := wireLifecycle(deps.Lifecycle, deps.Controller, compiler, secrets.binder)
	services.Lifecycle = reconciler
	machine := wireMachine(machineDependencies{
		State: services.DesiredState, Lifecycle: reconciler, Trust: deps.Trust,
		Confirmer: deps.Confirmer, Session: deps.SessionConfirmer, Reporter: deps.Reporter,
		Selection: deps.Selection, Streams: deps.Streams, Terminal: deps.Terminal,
		Home: deps.Home, Owner: deps.Owner, Files: deps.Files,
	})
	services.MachineInventory, services.MachineAccess = machine.MachineInventory, machine.MachineAccess
	services.MachinePower, services.MachineTrust = machine.MachinePower, machine.MachineTrust
	services.ClusterAccess = wireClusterAccess(services.DesiredState, secrets.binder, deps.Selection)
	return services
}

type secretInputFunc func(context.Context, []byte) (int, error)

func (f secretInputFunc) Read(ctx context.Context, buffer []byte) (int, error) { return f(ctx, buffer) }
