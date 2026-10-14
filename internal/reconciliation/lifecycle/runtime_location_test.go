package lifecycle

import (
	"context"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// launchRefused runs a fresh apply and a bounded run, each of which must
// refuse controller.state with message and run bootwright setup as its remedy
// before it registers anything or launches the runtime.
func launchRefused(t *testing.T, h *harness, message string) {
	t.Helper()
	_, err := apply(h)
	requireSetupRefusal(t, err, message)
	if _, found, _ := h.workspace.area.Read(context.Background(), "index.json", 1<<20); found {
		t.Fatal("a refused launch registered an operation")
	}
	err = h.service.WithRuntime(context.Background(), RuntimeRequest{ContextName: testContextName}, func(context.Context, Runtime) error {
		t.Fatal("a refused launch reached its call")
		return nil
	})
	requireSetupRefusal(t, err, message)
	if len(h.capability.applies) != 0 {
		t.Fatalf("a refused launch reached the host: %v", h.capability.applies)
	}
}

func requireSetupRefusal(t *testing.T, err error, message string) {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "controller.state" || reported[0].Message != message || reported[0].Remediation != "run bootwright setup" {
		t.Fatalf("the launch reported %+v (%v), want controller.state %q naming bootwright setup", reported, err, message)
	}
}

// A setup interrupted before it finished leaves its outcome unknown, so it
// blocks every launch until setup runs again, whatever the receipt it may
// still hold says.
func TestAPendingSetupBlocksALaunch(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	view := completedSetup("3.14", "3.14.8")
	view.State.Pending = &prerequisites.PendingSetup{ID: "setup-000042", Actions: []string{"runtime"}}
	h.setup.set(view)
	launchRefused(t, h, "a controller setup (setup-000042) was interrupted before it finished")
	if h.guard.entered() != 0 {
		t.Fatalf("a pending setup let %d launches through", h.guard.entered())
	}
}

// A setup record another build wrote, and a host no setup completed on, both
// refuse every launch with the setup that heals them.
func TestALaunchWithoutAReceiptRefusesWithSetup(t *testing.T) {
	for name, test := range map[string]struct {
		view    prerequisites.SetupView
		message string
	}{
		"no setup record": {prerequisites.SetupView{}, "the controller runtime for this build (lock 444444444444) is not prepared"},
		"another build's record": {prerequisites.SetupView{Exists: true, Foreign: "bootwright.setup-state-v9"},
			"the controller setup record was written by another Bootwright build"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "artifact-server-lab")
			h.setup.set(test.view)
			launchRefused(t, h, test.message)
		})
	}
}

// A build whose runtime this host never published, as a new lock leaves it
// until setup runs, refuses naming that lock and the receipt's ABI before it
// registers anything.
func TestAMissingRuntimeRefusesNamingTheBuildsLock(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	h.guard.unpublished = true
	launchRefused(t, h, "the controller runtime for this build (lock 444444444444, Python 3.14) is not prepared")
	if h.guard.entered() != 0 {
		t.Fatalf("an unpublished runtime was launched %d times", h.guard.entered())
	}
}

// A runtime retired between its location and its launch refuses that launch
// with the same remedy, before the bounded run's call.
func TestARuntimeGoneAtLaunchRefusesWithSetup(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	h.service.guard = vanishingGuard{}
	err := h.service.WithRuntime(context.Background(), RuntimeRequest{ContextName: testContextName}, func(context.Context, Runtime) error {
		t.Fatal("a launch of a retired runtime reached its call")
		return nil
	})
	requireSetupRefusal(t, err, "the controller runtime for this build (lock 444444444444, Python 3.14) is not prepared")
}

// vanishingGuard reports the runtime published and then finds it gone.
type vanishingGuard struct{}

func (vanishingGuard) Published(context.Context, prerequisites.Runtime) (bool, error) {
	return true, nil
}

func (vanishingGuard) WithPython(context.Context, prerequisites.Runtime, func(prerequisites.PythonLaunch) error) error {
	return prerequisites.ErrRuntimeUnavailable
}
