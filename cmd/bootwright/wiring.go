package main

import (
	"context"
	"os"

	addoncatalog "github.com/crmarques/bootwright/internal/addons/catalog"
	addonpackages "github.com/crmarques/bootwright/internal/addons/packages"
	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/hostlinux"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/controller/privilege"
	"github.com/crmarques/bootwright/internal/desiredstate/encoding"
	"github.com/crmarques/bootwright/internal/environment/clusterpreflight"
	environmentpreflight "github.com/crmarques/bootwright/internal/environment/preflight"
	machineaccess "github.com/crmarques/bootwright/internal/machine/access"
	"github.com/crmarques/bootwright/internal/machine/power"
	"github.com/crmarques/bootwright/internal/managedos/media"
	"github.com/crmarques/bootwright/internal/nativeartifacts/comparisonlocal"
	artifactrendering "github.com/crmarques/bootwright/internal/nativeartifacts/rendering"
	"github.com/crmarques/bootwright/internal/nativeartifacts/renderlocal"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets/material"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
	"github.com/crmarques/bootwright/internal/substrate"
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
	CopyAccount      func(context.Context) (machineaccess.CopyAccount, error)
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
	Comparison       comparisonDependencies
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
	repository := contextfs.New(contextfs.Options{LibvirtImages: substrate.ImagePrefix})
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
		CopyAccount:      account.copyAccount,
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
		Comparison:       comparisonDependencies{Account: account.comparison, Terminal: os.Getenv("TERM"), Language: os.Getenv("LANG")},
		Media:            localMediaDependencies(repository, process.Confirmer, process.LifecycleProgress, process.MediaPresenter, process.AmbientRoute, files),
		Lifecycle: lifecycleDependencies{
			Workspace: repository, Inputs: contexts.Inputs{Repository: repository, Selection: account},
			Host: hostlinux.New(), HostKeys: hostlinux.New(), Guard: runtimeGuard(controllerPorts.Python), Setup: controllerPorts.Setup, Selection: account,
			Presenter: process.LifecyclePresenter, Progress: process.LifecycleProgress,
			Confirmer: process.Confirmer, Executable: process.Executable,
			Media: repository, Paths: repository, Logs: repository,
		},
	}, release
}

// assembleServices is the complete service graph. Secrets is assembled first
// because context initialization consumes its transaction-scoped hooks.
func assembleServices(deps serviceDependencies) cli.Services {
	compiler := wireCompiler()
	secrets := wireSecrets(deps, compiler)
	services := wireStubs()
	services.AddOnCatalog = addoncatalog.New(addonpackages.Builtin())
	services.Contexts = wireContexts(deps, compiler, secrets.hooks)
	services.Secrets, services.Encryption = secrets.custody, secrets.encryption
	services.DesiredState = wireDesiredState(deps, compiler)
	services.Controller = wireController(deps.Controller, compiler, deps.Confirmer, deps.AmbientRoute)
	services.Media = wireMedia(deps.Media)
	var reconciler lifecycle.Service
	if deps.Lifecycle.Capacity == nil {
		deps.Lifecycle.Capacity = remoteCapacity(services.DesiredState, &reconciler, deps)
	}
	reconciler = wireLifecycle(deps.Lifecycle, deps.Controller, compiler, secrets.binder)
	services.Lifecycle = reconciler
	services.Secrets = secrets.binder.WithHolds(reconciler)
	services.Logs = wireLogs(deps.Lifecycle)
	machine := wireMachine(machineDependencies{
		State: services.DesiredState, Lifecycle: reconciler, Trust: deps.Trust,
		Confirmer: deps.Confirmer, Session: deps.SessionConfirmer, Reporter: deps.Reporter,
		Selection: deps.Selection, Streams: deps.Streams, Terminal: deps.Terminal,
		CopyAccount: deps.CopyAccount, Home: deps.Home, Owner: deps.Owner, Files: deps.Files, Stager: deps.Controller.Stager,
		Custody: secrets.binder,
	})
	services.MachineInventory, services.MachineAccess = machine.MachineInventory, machine.MachineAccess
	services.MachineCopy = machine.MachineCopy
	services.EnvironmentAccess = machine.EnvironmentAccess
	services.MachinePower, services.MachineTrust = machine.MachinePower, machine.MachineTrust
	services.ClusterAccess = wireClusterAccess(services.DesiredState, secrets.binder, deps.Selection)
	services.EnvironmentInspection = wireInspection(services.DesiredState, reconciler, secrets.binder, deps.Selection, deps.Trust)
	storagePreflight := wireStoragePreflight(services.DesiredState, reconciler, deps)
	clusters := clusterpreflight.Service{Graph: effectiveGraph{state: services.DesiredState}, Selection: currentSelection(deps.Selection), Container: wireContainerPreflight(services.DesiredState, reconciler, deps), Storage: storagePreflight}
	comparison := comparisonlocal.Service{
		Trees: renderlocal.Files{}, Account: deps.Comparison.Account, In: deps.Streams.In, Out: deps.Streams.Out, Err: deps.Streams.Err,
		Terminal: deps.Comparison.Terminal, Language: deps.Comparison.Language,
	}
	services.Artifacts = artifactrendering.Service{Compiler: services.DesiredState, Encode: encoding.YAML, Files: comparison}
	services.Comparison = comparison
	services.EnvironmentPreflight = environmentpreflight.Service{Graph: effectiveGraph{state: services.DesiredState}, Selection: currentSelection(deps.Selection), Controller: services.Controller, Capacity: storagePreflight, Clusters: clusters}
	return services
}

type secretInputFunc func(context.Context, []byte) (int, error)

func (f secretInputFunc) Read(ctx context.Context, buffer []byte) (int, error) { return f(ctx, buffer) }
