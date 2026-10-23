package main

import (
	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/ansiblelocal"
	"github.com/crmarques/bootwright/internal/controller/bundlelocal"
	"github.com/crmarques/bootwright/internal/controller/clients"
	"github.com/crmarques/bootwright/internal/controller/firewall"
	"github.com/crmarques/bootwright/internal/controller/firewalllocal"
	"github.com/crmarques/bootwright/internal/controller/hostdnf"
	"github.com/crmarques/bootwright/internal/controller/hostlinux"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/controller/runtimelock"
	"github.com/crmarques/bootwright/internal/controller/stagelocal"
	"github.com/crmarques/bootwright/internal/controller/venvlocal"
)

const (
	setupRuntimes = "/var/lib/bootwright/setup/runtimes"
	setupWheels   = "/var/lib/bootwright/setup/cache/wheels"
)

// controllerDependencies names every local capability controller setup and
// the controller stage consume. A nil field leaves its capability unavailable
// rather than substituting one.
type controllerDependencies struct {
	Firewall        firewall.Inspector
	Storage         prerequisites.Storage
	Setup           prerequisites.SetupStore
	Host            prerequisites.HostInspector
	Packages        func(controller.Route) (prerequisites.HostPackages, error)
	Python          prerequisites.PythonRuntime
	Lock            *prerequisites.RuntimeLock
	Stager          prerequisites.AutomationStager
	ClientInstaller clients.Installer
	Tools           prerequisites.TargetToolCatalog
	StagePackages   clients.HostPackages
	Presenter       prerequisites.PlanPresenter
	Progress        prerequisites.ProgressReporter
}

// localControllerDependencies also returns a release the invocation calls
// once it ends; nothing local holds anything across inspections any more.
func localControllerDependencies(storage prerequisites.Storage, process processDependencies) (controllerDependencies, func()) {
	stager := stagelocal.NewAutomationStager(stagelocal.StagingParent)
	deps := controllerDependencies{
		Firewall:        firewalllocal.Inspector{},
		Storage:         storage,
		Host:            hostlinux.New(),
		Packages:        hostPackages,
		Stager:          stager,
		ClientInstaller: ansiblelocal.New(stager),
		Tools:           bundlelocal.NewToolCatalog(),
		StagePackages:   stagePackages,
		Presenter:       process.Presenter,
		Progress:        process.Progress,
	}
	if setup, holds := storage.(prerequisites.SetupStore); holds {
		deps.Setup = setup
	}
	if lock, err := runtimelock.Embedded(); err == nil {
		deps.Lock = runtimeLock(lock)
		deps.Python = venvlocal.New(venvlocal.Config{Lock: lock, Runtimes: setupRuntimes, Wheels: setupWheels})
	}
	return deps, func() {}
}

func hostPackages(route controller.Route) (prerequisites.HostPackages, error) {
	return hostdnf.New(route)
}

func stagePackages(egress prerequisites.SetupEgress) (prerequisites.HostPackages, error) {
	return hostdnf.ForEgress(egress)
}

func runtimeLock(lock runtimelock.Lock) *prerequisites.RuntimeLock {
	value := prerequisites.RuntimeLock{Digest: lock.Digest(), AnsibleCore: lock.AnsibleCore(), ABIs: lock.PythonABIs(), GlibcFloor: lock.GlibcFloor()}
	for _, release := range lock.Tested() {
		value.Tested = append(value.Tested, prerequisites.TestedRelease{ID: release.ID, Versions: release.Versions})
	}
	for _, abi := range value.ABIs {
		files, _ := lock.Files(abi)
		for _, file := range files {
			value.Files = append(value.Files, prerequisites.LockedFile{ABI: abi, Filename: file.Filename, URL: file.URL, SHA256: file.SHA256})
		}
	}
	installer := lock.Installer()
	value.Files = append(value.Files, prerequisites.LockedFile{Filename: installer.Filename, URL: installer.URL, SHA256: installer.SHA256})
	return &value
}

func wireController(deps controllerDependencies, compiler prerequisites.Compiler, confirmer prerequisites.Confirmer, route controller.Route) cli.ControllerService {
	ports := prerequisites.Ports{Setup: deps.Setup, Python: deps.Python}
	if deps.Lock != nil {
		ports.Lock = *deps.Lock
	}
	if deps.Packages != nil {
		if packages, err := deps.Packages(route); err == nil {
			ports.Packages = packages
		}
	}
	return prerequisites.New(deps.Storage, compiler, deps.Host, ports, prerequisites.Options{
		ListenerChecks: firewallPreflight(deps.Firewall),
		AmbientRoute:   route,
		Confirmer:      confirmer,
		Presenter:      deps.Presenter,
		Progress:       deps.Progress,
		Tools:          deps.Tools,
	})
}
