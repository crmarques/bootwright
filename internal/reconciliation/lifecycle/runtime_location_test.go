package lifecycle

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
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

// cacheRebuilder stands in for the rebuild from the wheel cache: with every
// wheel cached it publishes the runtime the guard then launches, and
// otherwise it reports the wheel it lacks.
type cacheRebuilder struct {
	guard    *testGuard
	refusal  error
	rebuilds []prerequisites.Runtime
}

func (r *cacheRebuilder) Rebuild(_ context.Context, runtime prerequisites.Runtime) error {
	r.rebuilds = append(r.rebuilds, runtime)
	if r.refusal != nil {
		return r.refusal
	}
	r.guard.mutex.Lock()
	defer r.guard.mutex.Unlock()
	r.guard.unpublished = false
	return nil
}

func withRebuilder(h *harness, refusal error) *cacheRebuilder {
	rebuilder := &cacheRebuilder{guard: h.guard, refusal: refusal}
	h.service.options.Rebuild = rebuilder
	h.guard.unpublished = true
	return rebuilder
}

// A launch whose runtime directory is gone rebuilds it from the wheel cache,
// once and without asking, and then runs as if setup had never lost it.
func TestAMissingRuntimeIsRebuiltFromCachedWheels(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	rebuilder := withRebuilder(h, nil)
	if _, err := apply(h); err != nil {
		t.Fatalf("the apply did not run on the rebuilt runtime: %v", err)
	}
	if len(rebuilder.rebuilds) != 1 || rebuilder.rebuilds[0].Interpreter.Path != "/usr/bin/python3.14" || h.guard.entered() == 0 {
		t.Fatalf("rebuilt %+v, launched %d times", rebuilder.rebuilds, h.guard.entered())
	}
	h.guard.unpublished = true
	err := h.service.WithRuntime(context.Background(), RuntimeRequest{ContextName: testContextName}, func(context.Context, Runtime) error { return nil })
	if err != nil || len(rebuilder.rebuilds) != 2 {
		t.Fatalf("a bounded run did not rebuild its runtime: %v after %d rebuilds", err, len(rebuilder.rebuilds))
	}
}

// Only a wheel the cache lacks needs the network, which is setup's to use:
// the launch refuses naming setup before it registers anything.
func TestAMissingWheelRefusesWithRunSetup(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	withRebuilder(h, prerequisites.ErrRuntimeWheelMissing)
	launchRefused(t, h, "the controller runtime for this build (lock 444444444444, Python 3.14) is not prepared")
	if h.guard.entered() != 0 {
		t.Fatalf("a runtime missing a wheel was launched %d times", h.guard.entered())
	}
}

// A rebuild that fails once every wheel is cached names that failure and
// still points at setup, which repeats the build with its own diagnostics.
func TestAFailedRebuildRefusesWithRunSetup(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	withRebuilder(h, errors.Join(prerequisites.ErrRuntimeRebuildFailed, &prerequisites.ScopedFailure{
		Message: "pip could not install the locked wheels from the cache", Correction: "Restore the host's Python interpreter and the locked controller wheels",
	}))
	_, err := apply(h)
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "controller.state" || reported[0].Remediation != "run bootwright setup" ||
		!strings.Contains(reported[0].Message, "could not be rebuilt from the wheel cache: pip could not install") {
		t.Fatalf("a failed rebuild reported %+v (%v)", reported, err)
	}
}

// Status changes nothing, so it never rebuilds; it offers setup instead.
func TestStatusNeverRebuildsAMissingRuntime(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	rebuilder := withRebuilder(h, nil)
	if _, err := h.service.Status(context.Background(), StatusRequest{ContextName: testContextName}); err != nil {
		t.Fatal(err)
	}
	if len(rebuilder.rebuilds) != 0 {
		t.Fatalf("status rebuilt the runtime %d times", len(rebuilder.rebuilds))
	}
}

// A continuation is a launch too: with only the runtime directory gone it
// rebuilds it from the cache once, before it presents its plan, and then
// continues the operation it registered.
func TestAContinuationRebuildsAMissingRuntime(t *testing.T) {
	for _, test := range []struct {
		verb  reconciliation.Verb
		start func(*testing.T, *harness)
	}{
		{reconciliation.Apply, failedApply},
		{reconciliation.Destroy, unknownRemoval},
	} {
		t.Run(string(test.verb), func(t *testing.T) {
			h := newHarness(t, "artifact-server-lab")
			test.start(t, h)
			registered, _ := durableOperation(t, h)
			rebuilder := withRebuilder(h, nil)
			status, err := h.service.Status(context.Background(), StatusRequest{ContextName: testContextName})
			if err != nil || len(rebuilder.rebuilds) != 0 {
				t.Fatalf("status over the incomplete operation = %v, rebuilt %d times", err, len(rebuilder.rebuilds))
			}
			if len(status.NextSteps) != 1 || status.NextSteps[0] != setupStep {
				t.Fatalf("status offered %v over a missing runtime", status.NextSteps)
			}
			h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
			result, err := continued(h, test.verb)
			if err != nil || result.Receipt.Operation != registered.ID || result.Receipt.State != string(reconciliation.OperationDone) {
				t.Fatalf("the continuation over a missing runtime = %+v (%v)", result, err)
			}
			if len(rebuilder.rebuilds) != 1 {
				t.Fatalf("the continuation rebuilt its runtime %d times", len(rebuilder.rebuilds))
			}
		})
	}
}

// An apply over an incomplete removal whose runtime the cache can rebuild
// names that removal's continuation, not setup.
func TestAnApplyOverARemovalWithAMissingRuntimeNamesItsContinuation(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	unknownRemoval(t, h)
	withRebuilder(h, nil)
	_, err := apply(h)
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Remediation != "continue it with bootwright destroy --context lab" {
		t.Fatalf("the apply over the removal reported %+v (%v)", reported, err)
	}
}

// A continuation whose missing runtime the cache cannot rebuild refuses with
// setup before it presents anything.
func TestAContinuationMissingAWheelRefusesWithRunSetup(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	failedApply(t, h)
	withRebuilder(h, prerequisites.ErrRuntimeWheelMissing)
	_, err := continued(h, reconciliation.Apply)
	requireSetupRefusal(t, err, "the controller runtime for this build (lock 444444444444, Python 3.14) is not prepared")
}
