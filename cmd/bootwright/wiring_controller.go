package main

import (
	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/controller/ansiblelocal"
	"github.com/crmarques/bootwright/internal/controller/bundlelocal"
	"github.com/crmarques/bootwright/internal/controller/clients"
	"github.com/crmarques/bootwright/internal/controller/hostlinux"
	"github.com/crmarques/bootwright/internal/controller/nativelocal"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

// controllerDependencies names every local capability controller setup consumes.
// A nil field leaves its capability unavailable rather than substituting one.
type controllerDependencies struct {
	Storage         prerequisites.Storage
	Host            prerequisites.HostInspector
	Catalog         prerequisites.DependencyCatalog
	Bundle          prerequisites.BundleManager
	Runtime         prerequisites.RuntimeInstaller
	ClientInstaller clients.Installer
	Tools           prerequisites.TargetToolCatalog
	Bootstrap       prerequisites.BootstrapResolver
	Native          prerequisites.NativeResolver
	NativeInspector prerequisites.NativeInspector
	Presenter       prerequisites.PlanPresenter
	Progress        prerequisites.ProgressReporter
}

func localControllerDependencies(storage prerequisites.Storage, process processDependencies) controllerDependencies {
	native := nativelocal.New(bundlelocal.FetchMetadata)
	guard := bundlelocal.ExecutionGuard{}
	installer := ansiblelocal.New(guard)
	return controllerDependencies{
		Storage:         storage,
		Host:            hostlinux.New(),
		Catalog:         bundlelocal.Catalog{},
		Bundle:          bundlelocal.New(guard),
		Runtime:         installer,
		ClientInstaller: installer,
		Tools:           bundlelocal.NewToolCatalog(),
		Bootstrap:       bundlelocal.NewBootstrapResolver(),
		Native:          native,
		NativeInspector: native,
		Presenter:       process.Presenter,
		Progress:        process.Progress,
	}
}

func wireController(deps controllerDependencies, compiler prerequisites.Compiler, confirmer prerequisites.Confirmer) cli.ControllerService {
	return prerequisites.New(deps.Storage, compiler, deps.Host, deps.Catalog, deps.Bundle, deps.Runtime, prerequisites.Options{
		Confirmer:       confirmer,
		Presenter:       deps.Presenter,
		Progress:        deps.Progress,
		Tools:           deps.Tools,
		Bootstrap:       deps.Bootstrap,
		Native:          deps.Native,
		NativeInspector: deps.NativeInspector,
	})
}
