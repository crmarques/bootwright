package lifecycle

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

// Ownership answers what the current operation proved. Nothing is proved before
// one exists, and a plan that never ran owns nothing yet.
func TestOwnershipReportsOnlyWhatTheCurrentOperationProved(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	owned, err := h.service.Ownership(context.Background(), testContextName)
	if err != nil {
		t.Fatal(err)
	}
	if len(owned) != 0 {
		t.Fatalf("a context with no operation reported ownership: %+v", owned)
	}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	owned, err = h.service.Ownership(context.Background(), testContextName)
	if err != nil {
		t.Fatal(err)
	}
	state, ok := owned["ArtifactServer/artifact-server-lab"]
	if !ok {
		t.Fatalf("ownership = %+v", owned)
	}
	if state.Verb != string(reconciliation.Apply) || state.State != string(reconciliation.BlockDone) {
		t.Fatalf("state = %+v", state)
	}
}

// Several blocks may realize one object. The reported state is the one an
// operator must act on, never the most favorable of them.
func TestOwnershipReportsTheLeastSettledBlockOfOneObject(t *testing.T) {
	for _, test := range []struct {
		name   string
		states []string
		want   string
	}{
		{"one unknown among settled", []string{"done", "unknown"}, "unknown"},
		{"one failure among settled", []string{"done", "failed"}, "failed"},
		{"one pending among settled", []string{"done", "pending"}, "pending"},
		{"all settled", []string{"done", "done"}, "done"},
	} {
		t.Run(test.name, func(t *testing.T) {
			least := test.states[0]
			for _, state := range test.states {
				if settlement(state) < settlement(least) {
					least = state
				}
			}
			if least != test.want {
				t.Fatalf("least settled = %q, want %q", least, test.want)
			}
		})
	}
}

// A bounded operation borrows the approved bundle and the Secret material it
// names, and gives the binding back whether or not the call succeeded.
func TestWithRuntimeLendsTheApprovedBoundaryAndReleasesWhatItBound(t *testing.T) {
	h := newHarness(t)
	opened := 0
	err := h.service.WithRuntime(context.Background(), RuntimeRequest{ContextName: testContextName, Secrets: []string{"artifact-server-tls"}},
		func(_ context.Context, runtime Runtime) error {
			opened++
			if runtime.Context.Name != testContextName {
				t.Fatalf("runtime context = %+v", runtime.Context)
			}
			if _, ok := runtime.Material["artifact-server-tls"]; !ok {
				t.Fatalf("material = %+v", runtime.Material)
			}
			return nil
		})
	if err != nil || opened != 1 {
		t.Fatalf("runtime = %d (%v)", opened, err)
	}
	if h.guard.calls != 1 {
		t.Fatalf("execution guard calls = %d", h.guard.calls)
	}
	if len(h.binder.released) != 1 {
		t.Fatalf("released bindings = %+v", h.binder.released)
	}
}

func TestWithRuntimeReleasesItsBindingWhenTheOperationFails(t *testing.T) {
	h := newHarness(t)
	refusal := errors.New("the controller did not answer")
	err := h.service.WithRuntime(context.Background(), RuntimeRequest{ContextName: testContextName, Secrets: []string{"artifact-server-tls"}},
		func(context.Context, Runtime) error { return refusal })
	if !errors.Is(err, refusal) {
		t.Fatalf("error = %v", err)
	}
	if len(h.binder.released) != 1 {
		t.Fatalf("released bindings = %+v", h.binder.released)
	}
}

// reopenThen runs then once, right before the first reopen of a binding.
type reopenThen struct {
	*testBinder
	then func()
}

func (b *reopenThen) Reopen(ctx context.Context, request custody.BindingRequest) ([]secretstore.BoundMaterial, error) {
	if then := b.then; then != nil {
		b.then = nil
		then()
	}
	return b.testBinder.Reopen(ctx, request)
}

// A binding names no consumer, so a registration that lists a bounded run's
// binding between its bind and its reopen collects it with the bindings no
// operation names. The run finds its binding gone rather than unreadable and
// binds again, so it still runs, with material, and gives back what it bound;
// the operation keeps its own binding.
func TestABoundedRunWhoseBindingARegistrationCollectedBindsAgain(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "alpha")
	raced := &reopenThen{testBinder: h.binder}
	h.service.binder = raced
	raced.then = func() {
		if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
			t.Fatalf("the apply racing the run failed: %v", err)
		}
	}
	lent := 0
	err := h.service.WithRuntime(ctx, RuntimeRequest{ContextName: testContextName, Secrets: []string{"artifact-server-tls"}},
		func(_ context.Context, runtime Runtime) error {
			if _, ok := runtime.Material["artifact-server-tls"]; ok {
				lent++
			}
			return nil
		})
	if err != nil || lent != 1 || raced.then != nil {
		t.Fatalf("the run = %v with material %d times; the race ran %t", err, lent, raced.then == nil)
	}
	record, _ := durableOperation(t, h)
	if !slices.Equal(record.Bindings, []string{"bind-2"}) || h.binder.issued != 3 {
		t.Fatalf("the apply registered %v and the store issued %d bindings", record.Bindings, h.binder.issued)
	}
	if !slices.Contains(h.binder.released, "bind-1") || !slices.Contains(h.binder.released, "bind-3") || slices.Contains(h.binder.released, "bind-2") {
		t.Fatalf("released %v", h.binder.released)
	}
}

// A binding that is still held but cannot be reopened is no collection, so a
// bounded consumer reports the failure after binding once and releases it.
func TestABoundedConsumerWhoseHeldBindingCannotBeReopenedFails(t *testing.T) {
	h := newHarness(t)
	h.service.binder = unopenable{h.binder}
	err := h.service.WithMaterial(context.Background(), MaterialRequest{ContextName: testContextName, Secrets: []string{"artifact-server-tls"}},
		func(context.Context, map[string]secrets.Material) error {
			t.Fatal("the consumer ran without its material")
			return nil
		})
	if err == nil || h.binder.issued != 1 || !slices.Equal(h.binder.released, []string{"bind-1"}) {
		t.Fatalf("the consumer = %v after %d bindings, released %v", err, h.binder.issued, h.binder.released)
	}
}

// A bounded run has no attempt log for its output to sit beside, so it keeps
// one file of its own under an identity of its own, names it relative to the
// state root as a structured result does, and points an adapter failure at it.
// The file exists before the adapter runs, and what the adapter printed is on
// disk once the call returns.
func TestWithRuntimeRetainsWhatItsAdapterPrinted(t *testing.T) {
	h := newHarness(t)
	var identity string
	err := h.service.WithRuntime(context.Background(), RuntimeRequest{ContextName: testContextName, RetainOutput: true},
		func(ctx context.Context, runtime Runtime) error {
			const runs = "contexts/" + testContextName + "/state/runs/"
			if len(runtime.Logs) != 1 || !strings.HasPrefix(runtime.Logs[0], runs) || !strings.HasSuffix(runtime.Logs[0], "/"+runOutputName) {
				t.Fatalf("retained paths = %+v", runtime.Logs)
			}
			identity = strings.TrimSuffix(strings.TrimPrefix(runtime.Logs[0], runs), "/"+runOutputName)
			if !reconciliation.ValidRunID(identity) {
				t.Fatalf("run identity = %q", identity)
			}
			if !strings.HasSuffix(runtime.LogLocation, identity) {
				t.Fatalf("named location %q does not hold %q", runtime.LogLocation, identity)
			}
			if runtime.OutputRemediation != "read the adapter output retained in this run's "+runOutputName {
				t.Fatalf("an adapter failure is pointed at %q", runtime.OutputRemediation)
			}
			if _, found, err := h.workspace.runArea.Read(ctx, identity+"/"+runOutputName, 16); err != nil || !found {
				t.Fatalf("the retained file did not exist before the adapter ran: %t (%v)", found, err)
			}
			_, _ = runtime.Output.Write([]byte("what the adapter printed\n"))
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	data, found, err := h.workspace.runArea.Read(context.Background(), identity+"/"+runOutputName, 4096)
	if err != nil || !found || string(data) != "what the adapter printed\n" {
		t.Fatalf("retained output = %q %t (%v)", data, found, err)
	}
	if h.workspace.area.written(identity) {
		t.Fatal("a bounded run wrote into the operation records")
	}
}

// A run whose runtime is refused admission never reaches its adapter, and no
// result names a file for it, so it keeps none: the run area stays empty.
func TestARunRefusedAdmissionRetainsNothing(t *testing.T) {
	h := newHarness(t)
	refusal := errors.New("a native package transaction prevents coherent dependency execution")
	h.service.guard = refusingGuard{refusal}
	err := h.service.WithRuntime(context.Background(), RuntimeRequest{ContextName: testContextName, RetainOutput: true},
		func(context.Context, Runtime) error {
			t.Fatal("a run refused admission reached its call")
			return nil
		})
	if !errors.Is(err, refusal) {
		t.Fatalf("the refused run reported %v", err)
	}
	h.workspace.runArea.mutex.Lock()
	defer h.workspace.runArea.mutex.Unlock()
	if len(h.workspace.runArea.files) != 0 || len(h.workspace.runArea.directories) != 0 {
		t.Fatalf("a run refused admission left %v and %v", h.workspace.runArea.files, h.workspace.runArea.directories)
	}
}

// A reading names no retained output, so the run it asks for keeps none: its
// adapter is handed nothing to write into, nothing is named for it, and the
// run area stays empty.
func TestARunThatRetainsNoOutputLeavesNoRunFile(t *testing.T) {
	h := newHarness(t)
	called := false
	err := h.service.WithRuntime(context.Background(), RuntimeRequest{ContextName: testContextName},
		func(_ context.Context, runtime Runtime) error {
			called = true
			if runtime.Output != nil || runtime.LogLocation != "" || len(runtime.Logs) != 0 || runtime.OutputRemediation != "" {
				t.Fatalf("a run that retains nothing was given %v, %q, %v and %q",
					runtime.Output, runtime.LogLocation, runtime.Logs, runtime.OutputRemediation)
			}
			return nil
		})
	if err != nil || !called {
		t.Fatalf("the run = %v, called %t", err, called)
	}
	h.workspace.runArea.mutex.Lock()
	defer h.workspace.runArea.mutex.Unlock()
	if len(h.workspace.runArea.files) != 0 || len(h.workspace.runArea.directories) != 0 {
		t.Fatalf("a run that retains nothing left %v and %v", h.workspace.runArea.files, h.workspace.runArea.directories)
	}
}

// A run that cannot keep its file names none and never reaches its adapter,
// so the directory it made for that file goes too, whether its creation, the
// file's or an interrupt between them stopped it.
func TestARunWhoseOutputCannotBeKeptLeavesNoDirectory(t *testing.T) {
	identity := "run-" + strings.Repeat("2a", 16)
	injected := errors.New("the retained output could not be created")
	for _, test := range []struct {
		name string
		arm  func(*memoryArea, context.CancelFunc)
	}{
		{"its directory fails once it exists", func(area *memoryArea, _ context.CancelFunc) {
			area.failAfter["ensure "+identity] = injected
		}},
		{"its file cannot be created", func(area *memoryArea, _ context.CancelFunc) {
			area.fail["write "+identity+"/"+runOutputName] = injected
		}},
		{"an interrupt lands before its file", func(area *memoryArea, cancel context.CancelFunc) {
			area.landing = func(operation, _ string, _ map[string][]byte) error {
				if operation != "write" {
					return nil
				}
				cancel()
				return injected
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			test.arm(h.workspace.runArea, cancel)
			h.entropy[0] = 0x2a
			err := h.service.WithRuntime(ctx, RuntimeRequest{ContextName: testContextName, RetainOutput: true},
				func(context.Context, Runtime) error {
					t.Fatal("a run whose output could not be kept reached its adapter")
					return nil
				})
			if !errors.Is(err, injected) {
				t.Fatalf("the run reported %v, not the injected failure", err)
			}
			h.workspace.runArea.mutex.Lock()
			defer h.workspace.runArea.mutex.Unlock()
			if len(h.workspace.runArea.files) != 0 || len(h.workspace.runArea.directories) != 0 {
				t.Fatalf("a run whose output could not be kept left %v and %v", h.workspace.runArea.files, h.workspace.runArea.directories)
			}
		})
	}
}

// refusingGuard refuses to admit the private runtime, as a native package
// transaction holding its lock does.
type refusingGuard struct{ err error }

func (g refusingGuard) WithPython(context.Context, prerequisites.BundleArea, prerequisites.ExecutionRequirement, func(prerequisites.PythonLaunch, func() error) error) error {
	return g.err
}

// A bounded operation is not a lifecycle operation: it registers nothing, so it
// can never claim, continue or release ownership of anything.
func TestWithRuntimeRegistersNoOperation(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	if err := h.service.WithRuntime(context.Background(), RuntimeRequest{ContextName: testContextName},
		func(context.Context, Runtime) error { return nil }); err != nil {
		t.Fatal(err)
	}
	owned, err := h.service.Ownership(context.Background(), testContextName)
	if err != nil {
		t.Fatal(err)
	}
	if len(owned) != 0 {
		t.Fatalf("a bounded operation published ownership: %+v", owned)
	}
	result, err := h.service.Plan(context.Background(), PlanRequest{ContextName: testContextName})
	if err != nil {
		t.Fatal(err)
	}
	if result.Continuation || result.Receipt.Next != "apply" {
		t.Fatalf("a bounded operation changed the next legal step: %+v", result.Receipt)
	}
}

// A removal that completed keeps proving itself. Each superseding attempt plans
// only what is not yet gone, so an object an earlier attempt removed leaves the
// current plan, and without the apply this removal takes back it would read as
// an object nothing ever realized.
func TestOwnershipKeepsReportingWhatAnEarlierRemovalReleased(t *testing.T) {
	h := newHarness(t, "artifact-server-lab", "machine-rhel-01")
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeChanged}, {Outcome: reconciliation.OutcomeFailed}}
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
		t.Fatal("the seeded removal failure did not fire")
	}
	result, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
	if err != nil || result.Receipt.State != "done" {
		t.Fatalf("superseding removal = %+v (%v)", result, err)
	}
	if len(result.Blocks) != 1 {
		t.Fatalf("the superseding removal replanned %d blocks", len(result.Blocks))
	}
	owned, err := h.service.Ownership(context.Background(), testContextName)
	if err != nil {
		t.Fatal(err)
	}
	for _, identity := range []string{"ArtifactServer/artifact-server-lab", "ArtifactServer/machine-rhel-01"} {
		state, found := owned[identity]
		if !found {
			t.Fatalf("%s lost the record of its removal: %+v", identity, owned)
		}
		if state.Verb != string(reconciliation.Destroy) || state.State != string(reconciliation.BlockDone) {
			t.Fatalf("%s = %+v", identity, state)
		}
	}
}

// A removal already covered is not a removal already proved. The apply this
// removal takes back names every object it owns, so the block states of the
// removal itself must still decide what an operator has to act on.
func TestOwnershipReportsARemovalBlockThatStillNeedsAnOperator(t *testing.T) {
	h := newHarness(t, "artifact-server-lab", "machine-rhel-01")
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeChanged}, {Outcome: reconciliation.OutcomeFailed}}
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
		t.Fatal("the seeded removal failure did not fire")
	}
	owned, err := h.service.Ownership(context.Background(), testContextName)
	if err != nil {
		t.Fatal(err)
	}
	states := []string{}
	for _, identity := range []string{"ArtifactServer/artifact-server-lab", "ArtifactServer/machine-rhel-01"} {
		state, found := owned[identity]
		if !found || state.Verb != string(reconciliation.Destroy) {
			t.Fatalf("%s = %+v (%t)", identity, state, found)
		}
		states = append(states, state.State)
	}
	slices.Sort(states)
	if !slices.Equal(states, []string{string(reconciliation.BlockDone), string(reconciliation.BlockFailed)}) {
		t.Fatalf("removal states = %v", states)
	}
}
