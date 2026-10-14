//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
)

const healingInterpreter = "/usr/bin/python3.14"

var healingLock = prerequisites.RuntimeLock{
	Digest: setupDigest(1), AnsibleCore: "2.21.5", ABIs: []string{"3.14"}, GlibcFloor: "2.34",
	Tested: []prerequisites.TestedRelease{{ID: "fedora", Versions: []string{"44"}}},
	Files: []prerequisites.LockedFile{
		{ABI: "3.14", Filename: "ansible_core-2.21.5-py3-none-any.whl", URL: "https://files.pythonhosted.org/a/ansible_core-2.21.5-py3-none-any.whl", SHA256: setupDigest(10)},
		{Filename: "pip-26.0.1-py3-none-any.whl", URL: "https://files.pythonhosted.org/p/pip-26.0.1-py3-none-any.whl", SHA256: setupDigest(11)},
	},
}

// healingPython reads the store's own runtimes and wheel cache the way the
// venv adapter does.
type healingPython struct {
	setup  string
	builds int
}

func (p *healingPython) interpreter() prerequisites.PythonInterpreter {
	return prerequisites.PythonInterpreter{Path: healingInterpreter, ABI: "3.14", Version: "3.14.8", Glibc: "2.43"}
}

func (p *healingPython) runtime() prerequisites.Runtime {
	return prerequisites.Runtime{Digest: prerequisites.RuntimeDigest(healingLock.Digest, "3.14", healingInterpreter), LockDigest: healingLock.Digest,
		Interpreter: p.interpreter(), AnsibleCore: healingLock.AnsibleCore}
}

func (p *healingPython) wheel(file prerequisites.LockedFile) string {
	return filepath.Join(p.setup, setupCacheName, setupWheelsName, file.SHA256, file.Filename)
}

func (p *healingPython) Select(context.Context, string) (prerequisites.PythonInterpreter, error) {
	return p.interpreter(), nil
}

func (p *healingPython) Wheels(context.Context, string) ([]prerequisites.RuntimeWheel, error) {
	wheels := []prerequisites.RuntimeWheel{}
	for _, file := range healingLock.Files {
		_, err := os.Stat(p.wheel(file))
		wheels = append(wheels, prerequisites.RuntimeWheel{Filename: file.Filename, Size: 1_000_000, Cached: err == nil})
	}
	return wheels, nil
}

func (p *healingPython) Fetch(context.Context, string, prerequisites.SetupEgress, prerequisites.WheelMirror) error {
	for _, file := range healingLock.Files {
		if err := os.MkdirAll(filepath.Dir(p.wheel(file)), 0700); err != nil {
			return err
		}
		if err := os.WriteFile(p.wheel(file), []byte(file.Filename), 0600); err != nil {
			return err
		}
	}
	return nil
}

func (p *healingPython) Export(context.Context, string, prerequisites.SetupEgress, prerequisites.WheelMirror) (prerequisites.WheelExport, error) {
	return prerequisites.WheelExport{}, errors.New("no export in a healing test")
}

func (p *healingPython) Inspect(context.Context, prerequisites.PythonInterpreter) (prerequisites.Runtime, bool, error) {
	_, err := os.Stat(filepath.Join(p.setup, setupRuntimesName, p.runtime().Digest, "runtime.json"))
	return p.runtime(), err == nil, nil
}

func (p *healingPython) Build(ctx context.Context, publisher prerequisites.RuntimePublisher, _ prerequisites.PythonInterpreter) (prerequisites.Runtime, error) {
	p.builds++
	stage, err := buildRuntime(publisher.(prerequisites.SetupTransaction), "healing", p.runtime().Digest)
	if err != nil {
		return prerequisites.Runtime{}, err
	}
	if _, err := publisher.PublishRuntime(ctx, stage, p.runtime().Digest); err != nil {
		return prerequisites.Runtime{}, err
	}
	return p.runtime(), nil
}

func (p *healingPython) RuntimeWheels(context.Context, string) ([]string, error) {
	return []string{setupDigest(10), setupDigest(11)}, nil
}

func (p *healingPython) WithPython(context.Context, prerequisites.Runtime, func(prerequisites.PythonLaunch) error) error {
	return errors.New("no launch in a healing test")
}

type healingHost struct {
	identity controller.InstalledHostIdentity
}

func (h healingHost) Platform(context.Context) (prerequisites.Platform, error) {
	return prerequisites.Platform{OS: "fedora", Release: "44", Architecture: "amd64"}, nil
}

func (h healingHost) Identity(context.Context) (controller.InstalledHostIdentity, error) {
	return h.identity, nil
}

func (h healingHost) Release(context.Context) (prerequisites.HostRelease, error) {
	return prerequisites.HostRelease{ID: "fedora", VersionID: "44", Architecture: "amd64"}, nil
}

type healingPackages struct{}

func (healingPackages) Qualify(context.Context) error { return nil }
func (healingPackages) Missing(context.Context, []string) ([]string, error) {
	return []string{}, nil
}
func (p healingPackages) Installed(ctx context.Context, requirements []string) ([]prerequisites.HostPackageEvidence, error) {
	return p.Present(ctx, requirements)
}
func (healingPackages) Present(_ context.Context, requirements []string) ([]prerequisites.HostPackageEvidence, error) {
	evidence := []prerequisites.HostPackageEvidence{}
	for _, requirement := range requirements {
		evidence = append(evidence, prerequisites.HostPackageEvidence{Requirement: requirement, Package: requirement + "-1.0-1.fc44.x86_64", Repository: "fedora"})
	}
	return evidence, nil
}
func (healingPackages) Preview(context.Context, []string) (prerequisites.HostPackagePreview, error) {
	return prerequisites.HostPackagePreview{}, errors.New("nothing to preview")
}
func (healingPackages) Install(context.Context, []string, prerequisites.RunOutput) ([]prerequisites.HostPackageEvidence, error) {
	return nil, errors.New("nothing to install")
}

type healingController struct{}

func (healingController) ReadController(_ context.Context, _ string, read func(prerequisites.StorageView) error) error {
	return read(prerequisites.StorageView{})
}

func (healingController) Compile(context.Context, desiredstate.Sources) (*compilation.State, *compilation.Report, error) {
	return nil, nil, errors.New("setup selects no context")
}

type healingPlans struct{ actions [][]string }

func (p *healingPlans) PresentControllerScope(context.Context, string, prerequisites.Report) error {
	return nil
}

func (p *healingPlans) PresentControllerPlan(_ context.Context, report prerequisites.Report) error {
	p.actions = append(p.actions, slices.Clone(report.Actions))
	return nil
}

func TestHealingASubtreeOverAHealthyRuntimeLeavesItLaunchable(t *testing.T) {
	store := New(testOptions(filepath.Join(t.TempDir(), "state")))
	identity, err := controller.NewInstalledHostIdentity(controller.LinuxInstalledIdentityV1, strings.Repeat("1", 32), "11111111-2222-4333-8444-555555555555", "22222222-3333-4444-8555-666666666666")
	if err != nil {
		t.Fatal(err)
	}
	python, plans := &healingPython{setup: filepath.Join(store.options.Root, setupSubtree)}, &healingPlans{}
	service := prerequisites.New(healingController{}, healingController{}, healingHost{identity}, prerequisites.Ports{
		Setup: store, Packages: healingPackages{}, Python: python, Lock: healingLock,
	}, prerequisites.Options{Presenter: plans})
	setup := func() *prerequisites.Report {
		t.Helper()
		report, err := service.Setup(context.Background(), prerequisites.SetupRequest{SkipConfirmation: true})
		if err != nil || report.Outcome != "changed" {
			t.Fatalf("Setup = %+v, %v", report, err)
		}
		return report
	}
	setup()
	digest := python.runtime().Digest
	if hold, err := store.HoldRuntime(context.Background(), digest); err != nil {
		t.Fatalf("the first setup left no runtime: %v", err)
	} else {
		hold.Release()
	}
	if err := os.WriteFile(filepath.Join(python.setup, setupStateName+".bak"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	setup()
	healed := plans.actions[len(plans.actions)-1]
	if len(healed) != 2 || !strings.HasPrefix(healed[1], "Prepare the controller runtime") || !strings.Contains(healed[1], "2 of 2 wheels to download") {
		t.Fatalf("the healing plan is %q", healed)
	}
	if python.builds != 2 {
		t.Fatalf("the healing setup built %d runtimes in all", python.builds)
	}
	hold, err := store.HoldRuntime(context.Background(), digest)
	if err != nil {
		t.Fatalf("the healed host cannot launch its runtime: %v", err)
	}
	hold.Release()
	view, err := store.ReadSetup(context.Background())
	if err != nil || view.Foreign != "" || view.State.Receipt == nil || view.State.Receipt.Runtime.Digest != digest {
		t.Fatalf("after healing %+v, %v", view, err)
	}
}
