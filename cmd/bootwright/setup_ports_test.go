package main

import (
	"context"
	"errors"
	"strings"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

// setupPorts is an unprepared Fedora host as setup v2 reads it: every query
// answers, and every call that reaches dnf as root, the network or the store
// counts as an effect.
type setupPorts struct {
	identity controller.InstalledHostIdentity
	reads    int
	effects  int
}

func (p *setupPorts) Platform(context.Context) (prerequisites.Platform, error) {
	p.reads++
	return prerequisites.Platform{OS: "fedora", Release: "44", Architecture: "amd64"}, nil
}

func (p *setupPorts) Release(context.Context) (prerequisites.HostRelease, error) {
	p.reads++
	return prerequisites.HostRelease{ID: "fedora", VersionID: "44", Architecture: "amd64"}, nil
}

func (p *setupPorts) Identity(context.Context) (controller.InstalledHostIdentity, error) {
	if !p.identity.Valid() {
		p.effects++
		return controller.InstalledHostIdentity{}, errors.New("unexpected identity inspection")
	}
	return p.identity, nil
}

func (p *setupPorts) packages(controller.Route) (prerequisites.HostPackages, error) { return p, nil }

func (p *setupPorts) Qualify(context.Context) error { return nil }

func (p *setupPorts) Missing(_ context.Context, requirements []string) ([]string, error) {
	missing := []string{}
	for _, requirement := range requirements {
		if requirement == "podman" {
			missing = append(missing, requirement)
		}
	}
	return missing, nil
}

func (p *setupPorts) Installed(context.Context, []string) ([]prerequisites.HostPackageEvidence, error) {
	p.effects++
	return nil, errors.New("unexpected package evidence")
}

func (p *setupPorts) Present(context.Context, []string) ([]prerequisites.HostPackageEvidence, error) {
	p.effects++
	return nil, errors.New("unexpected package evidence")
}

func (p *setupPorts) Preview(context.Context, []string) (prerequisites.HostPackagePreview, error) {
	p.effects++
	return prerequisites.HostPackagePreview{}, errors.New("unexpected dnf preview")
}

func (p *setupPorts) Install(context.Context, []string, prerequisites.RunOutput) ([]prerequisites.HostPackageEvidence, error) {
	p.effects++
	return nil, errors.New("unexpected dnf install")
}

func (p *setupPorts) Select(context.Context, string) (prerequisites.PythonInterpreter, error) {
	return prerequisites.PythonInterpreter{Path: "/usr/bin/python3.14", ABI: "3.14", Version: "3.14.8", Glibc: "2.43"}, nil
}

func (p *setupPorts) Wheels(context.Context, string) ([]prerequisites.RuntimeWheel, error) {
	return []prerequisites.RuntimeWheel{{Filename: "ansible_core-2.21.5-py3-none-any.whl", Size: 2_400_000}}, nil
}

func (p *setupPorts) Fetch(context.Context, string, prerequisites.SetupEgress) error {
	p.effects++
	return errors.New("unexpected wheel fetch")
}

func (p *setupPorts) Inspect(context.Context, prerequisites.PythonInterpreter) (prerequisites.Runtime, bool, error) {
	return prerequisites.Runtime{}, false, nil
}

func (p *setupPorts) Build(context.Context, prerequisites.RuntimePublisher, prerequisites.PythonInterpreter) (prerequisites.Runtime, error) {
	p.effects++
	return prerequisites.Runtime{}, errors.New("unexpected runtime build")
}

func (p *setupPorts) WithPython(context.Context, prerequisites.Runtime, func(prerequisites.PythonLaunch) error) error {
	p.effects++
	return errors.New("unexpected launch")
}

var testRuntimeLock = &prerequisites.RuntimeLock{
	Digest: strings.Repeat("1", 64), AnsibleCore: "2.21.5", ABIs: []string{"3.14", "3.13", "3.12"}, GlibcFloor: "2.34",
	Tested: []prerequisites.TestedRelease{{ID: "fedora", Versions: []string{"43", "44"}}},
}

// setupDependencies composes setup over these ports and storage.
func (p *setupPorts) setupDependencies(storage prerequisites.Storage, setup prerequisites.SetupStore) controllerDependencies {
	return controllerDependencies{Storage: storage, Setup: setup, Host: p, Packages: p.packages, Python: p, Lock: testRuntimeLock}
}
