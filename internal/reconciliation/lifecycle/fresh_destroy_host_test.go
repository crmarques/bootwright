package lifecycle

import (
	"context"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// anotherHost moves the harness onto a host its controller state does not
// record, as a context store copied to another machine reads.
func anotherHost(t *testing.T, h *harness) {
	t.Helper()
	h.service.host = testHost{identity: anotherHostIdentity(t)}
}

func anotherHostIdentity(t *testing.T) controller.InstalledHostIdentity {
	t.Helper()
	host, err := controller.NewInstalledHostIdentity(controller.LinuxInstalledIdentityV1,
		"fedcba9876543210fedcba9876543210", "87654321-4321-8765-cba9-876543210fed", "01234567-89ab-cdef-0123-456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	return host
}

// requireRemovalRefusedUntouched runs a fresh destroy confirmed and then with
// --yes. Both refuse with the host proof's own diagnostic before any plan is
// presented or confirmed, and nothing is resolved, probed, registered,
// removed or released.
func requireRemovalRefusedUntouched(t *testing.T, h *harness, code, message, remediation string) {
	t.Helper()
	before := untouchedOf(h)
	presented, asked := len(h.presenter.presented), h.confirmer.asked
	for _, skip := range []bool{false, true} {
		_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: skip})
		reported := diagnostics.Of(err)
		if len(reported) != 1 || reported[0].Code != code || reported[0].Message != message || reported[0].Remediation != remediation {
			t.Fatalf("the destroy (--yes %t) reported %+v (%v), want %s %q with %q", skip, reported, err, code, message, remediation)
		}
	}
	if len(h.presenter.presented) != presented || h.confirmer.asked != asked {
		t.Fatalf("a refused removal presented %d plans and asked %d times", len(h.presenter.presented)-presented, h.confirmer.asked-asked)
	}
	before.require(t, h)
}

const anotherHostRemedy = "restore the original host, or create a context on this one"

// A fresh removal of a completed apply proves the host before anything else:
// on a host the controller state does not record it refuses, presenting no
// plan, asking nothing and touching nothing.
func TestAFreshDestroyOnAnotherHostRefusesBeforeAnyEffect(t *testing.T) {
	h := newHarness(t, "alpha")
	completeApply(t, h)
	anotherHost(t, h)
	requireRemovalRefusedUntouched(t, h, "controller.identity", "this host is not the host the context is bound to", anotherHostRemedy)
}

// A fresh removal that supersedes an incomplete apply is held to the same
// proof, before it resolves any of that apply's blocks.
func TestAFreshDestroySupersedingAnIncompleteApplyOnAnotherHostRefuses(t *testing.T) {
	h := newHarness(t, "alpha")
	failedApply(t, h)
	anotherHost(t, h)
	requireRemovalRefusedUntouched(t, h, "controller.identity", "this host is not the host the context is bound to", anotherHostRemedy)
}

// A binding the controller state records for the context that names another
// host refuses the removal too.
func TestAFreshDestroyWhoseBindingNamesAnotherHostRefuses(t *testing.T) {
	h := newHarness(t, "alpha")
	completeApply(t, h)
	h.workspace.controller.State.Bindings[0].HostDigest = strings.Repeat("0", 64)
	requireRemovalRefusedUntouched(t, h, "controller.identity", "the recorded controller binding does not match this host", anotherHostRemedy)
}

// Setup that has not completed on this host refuses the removal, naming the
// setup that resolves it.
func TestAFreshDestroyOverIncompleteSetupRefuses(t *testing.T) {
	h := newHarness(t, "alpha")
	failedApply(t, h)
	h.workspace.controller.State.Receipt.Status = "running"
	requireRemovalRefusedUntouched(t, h, "controller.state", "the retained controller setup is incomplete", "run bootwright setup to resolve it")
}

// A context the controller state records no binding for is admitted once its
// host is proved: no apply can bind it while it holds an incomplete operation,
// and the removal is its exit.
func TestAFreshDestroyOverAnUnboundContextVerifiesTheHostAndRemoves(t *testing.T) {
	h := newHarness(t, "alpha")
	failedApply(t, h)
	h.workspace.controller.State.Bindings = nil
	result, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
	if err != nil || result.Receipt.Verb != string(reconciliation.Destroy) || result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("the removal of an unbound context = %+v, %+v (%v)", result, diagnostics.Of(err), err)
	}
}

// The remedy an unbound failed apply names is a removal that runs, and the
// apply after it binds the context again.
func TestTheUnboundContinuationsRemedyNamesADestroyThatRuns(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "alpha")
	failedApply(t, h)
	h.workspace.controller.State.Bindings = nil
	_, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
	reported := diagnostics.Of(err)
	want := "take back what this operation owns with bootwright destroy --context lab, after which bootwright apply --context lab binds the context again"
	if len(reported) != 1 || reported[0].Message != "this context is not bound to a controller host" || !strings.HasPrefix(reported[0].Remediation, want) {
		t.Fatalf("the unbound continuation reported %+v (%v)", reported, err)
	}
	if result, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil || result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("the named removal = %+v (%v)", result, err)
	}
	if result, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil || result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("the apply after it = %+v (%v)", result, err)
	}
	if bindings := h.workspace.controller.State.Bindings; len(bindings) != 1 || bindings[0].Context != testContextName {
		t.Fatalf("the apply after the removal left bindings %+v", bindings)
	}
}

// The host proof under the exclusive lock is the authoritative one: a host,
// receipt or binding that moves after the shared decision, as the exclusive
// lock is taken, refuses the removal with that proof's own diagnostic before
// anything is resolved, probed, registered or removed.
func TestAFreshDestroyReprovesItsHostUnderTheExclusiveLock(t *testing.T) {
	moves := []struct {
		name, code, message, remediation string
		move                             func(*testing.T, *harness)
	}{
		{"another host", "controller.identity", "this host is not the host the context is bound to", anotherHostRemedy,
			func(t *testing.T, h *harness) {
				// The running destroy holds its own copy of the service, so
				// the host it reads moves through the shared pointer.
				h.service.host.(*movableHost).identity = anotherHostIdentity(t)
			}},
		{"a binding naming another host", "controller.identity", "the recorded controller binding does not match this host", anotherHostRemedy,
			func(_ *testing.T, h *harness) {
				h.workspace.controller.State.Bindings[0].HostDigest = strings.Repeat("0", 64)
			}},
		{"incomplete setup", "controller.state", "the retained controller setup is incomplete", "run bootwright setup to resolve it",
			func(_ *testing.T, h *harness) { h.workspace.controller.State.Receipt.Status = "running" }},
	}
	operations := []struct {
		name  string
		start func(*testing.T, *harness)
	}{
		{"a completed apply", func(t *testing.T, h *harness) { completeApply(t, h) }},
		{"a failed apply", failedApply},
	}
	for _, operation := range operations {
		for _, moved := range moves {
			t.Run(operation.name+", "+moved.name, func(t *testing.T) {
				h := newHarness(t, "alpha")
				operation.start(t, h)
				h.service.host = &movableHost{testHost: h.service.host.(testHost)}
				calls := len(h.capability.calls)
				before := beforeItsTransaction(h, func() { moved.move(t, h) })
				_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
				reported := diagnostics.Of(err)
				if len(reported) != 1 || reported[0].Code != moved.code || reported[0].Message != moved.message || reported[0].Remediation != moved.remediation {
					t.Fatalf("the destroy reported %+v (%v), want %s %q with %q", reported, err, moved.code, moved.message, moved.remediation)
				}
				if len(h.capability.calls) != calls {
					t.Fatalf("a refused removal called %v", h.capability.calls[calls:])
				}
				before.require(t, h)
			})
		}
	}
}

// movableHost is the harness host behind a pointer, so a host that moves as
// the exclusive lock is taken reaches the destroy already running.
type movableHost struct {
	testHost
}
