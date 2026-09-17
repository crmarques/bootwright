package main

import (
	"context"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/controller/bundlelocal"
	"github.com/crmarques/bootwright/internal/controller/hostlinux"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/controller/privilege"
	machineaccess "github.com/crmarques/bootwright/internal/machine/access"
	"github.com/crmarques/bootwright/internal/machine/power"
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
	Progress           prerequisites.ProgressReporter
	Presenter          prerequisites.PlanPresenter
	LifecycleProgress  lifecycle.ProgressReporter
	LifecyclePresenter lifecycle.PlanPresenter
	Executable         lifecycle.Executable
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
	Trust            machineaccess.HostKeyStore
	SecretInput      material.InputReader
	Resolver         secretstore.ImplementationResolver
	SessionMaterial  secretstore.SessionMaterialSource
	Controller       controllerDependencies
	Lifecycle        lifecycleDependencies
	Media            mediaDependencies
	Reporter         power.Reporter
}

// wireServices also returns the release for every local resource the assembled
// services retain for the length of one invocation.
func wireServices(process processDependencies) (cli.Services, func()) {
	repository := contextfs.New(contextfs.Options{})
	account := invokingAccount{resolver: privilege.Resolver{}}
	controller, release := localControllerDependencies(repository, process)
	services := assembleServices(serviceDependencies{
		Repository:       repository,
		Workspace:        repository,
		Trust:            repository,
		SessionConfirmer: process.SessionConfirmer,
		Streams:          process.Streams,
		Terminal:         process.Terminal,
		Home:             account.home,
		Selection:        account,
		Operator:         account,
		Confirmer:        process.Confirmer,
		SecretInput:      process.SecretInput,
		Reporter:         process.LifecycleProgress,
		Controller:       controller,
		Media:            localMediaDependencies(repository, process.Confirmer),
		Lifecycle: lifecycleDependencies{
			Workspace: repository, Inputs: contexts.Inputs{Repository: repository, Selection: account},
			Host: hostlinux.New(), Guard: bundlelocal.ExecutionGuard{}, Selection: account,
			Presenter: process.LifecyclePresenter, Progress: process.LifecycleProgress,
			Confirmer: process.Confirmer, Executable: process.Executable,
		},
	})
	return services, release
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
	services.Controller = wireController(deps.Controller, compiler, deps.Confirmer)
	services.Media = wireMedia(deps.Media)
	reconciler := wireLifecycle(deps.Lifecycle, deps.Controller, compiler, secrets.binder)
	services.Lifecycle = reconciler
	machine := wireMachine(machineDependencies{
		State: services.DesiredState, Lifecycle: reconciler, Trust: deps.Trust,
		Confirmer: deps.Confirmer, Session: deps.SessionConfirmer, Reporter: deps.Reporter,
		Selection: deps.Selection, Streams: deps.Streams, Terminal: deps.Terminal, Home: deps.Home,
	})
	services.MachineInventory, services.MachineAccess, services.MachinePower = machine.MachineInventory, machine.MachineAccess, machine.MachinePower
	return services
}

type secretInputFunc func(context.Context, []byte) (int, error)

func (f secretInputFunc) Read(ctx context.Context, buffer []byte) (int, error) { return f(ctx, buffer) }
