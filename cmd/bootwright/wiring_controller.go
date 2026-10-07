package main

import (
	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/controller"
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
	Foundation      prerequisites.FoundationInspector
	Presenter       prerequisites.PlanPresenter
	Progress        prerequisites.ProgressReporter
}

// localControllerDependencies also returns the release its native resolver
// needs: that resolver retains one copy of the installed package database
// between inspections, and the invocation that took it owns removing it. Both
// resolvers stage beneath the one Bootwright-owned staging parent.
func localControllerDependencies(storage prerequisites.Storage, process processDependencies) (controllerDependencies, func()) {
	staging := nativelocal.NewStaging(nativelocal.StagingParent)
	native := nativelocal.New(bundlelocal.FetchMetadata, staging)
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
		Bootstrap:       bundlelocal.NewBootstrapResolver(staging),
		Native:          native,
		NativeInspector: native,
		Foundation:      guard,
		Presenter:       process.Presenter,
		Progress:        process.Progress,
	}, native.Close
}

func wireController(deps controllerDependencies, compiler prerequisites.Compiler, confirmer prerequisites.Confirmer, route controller.Route) cli.ControllerService {
	return prerequisites.New(deps.Storage, compiler, deps.Host, deps.Catalog, deps.Bundle, deps.Runtime, prerequisites.Options{
		AmbientRoute:    route,
		Confirmer:       confirmer,
		Presenter:       deps.Presenter,
		Progress:        deps.Progress,
		Tools:           deps.Tools,
		Bootstrap:       deps.Bootstrap,
		Native:          deps.Native,
		NativeInspector: deps.NativeInspector,
		Foundation:      deps.Foundation,
	})
}
