package lifecycle

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
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

// A bounded run has no attempt log for its output to sit beside, so it keeps
// one file of its own under an identity of its own. The file exists before the
// adapter runs, and what the adapter printed is on disk once the call returns.
func TestWithRuntimeRetainsWhatItsAdapterPrinted(t *testing.T) {
	h := newHarness(t)
	var identity string
	err := h.service.WithRuntime(context.Background(), RuntimeRequest{ContextName: testContextName},
		func(ctx context.Context, runtime Runtime) error {
			if len(runtime.Logs) != 1 || !strings.HasSuffix(runtime.Logs[0], "/"+runOutputName) {
				t.Fatalf("retained paths = %+v", runtime.Logs)
			}
			identity = strings.TrimSuffix(runtime.Logs[0], "/"+runOutputName)
			if !reconciliation.ValidRunID(identity) {
				t.Fatalf("run identity = %q", identity)
			}
			if !strings.HasSuffix(runtime.LogLocation, identity) {
				t.Fatalf("named location %q does not hold %q", runtime.LogLocation, identity)
			}
			if _, found, err := h.workspace.runArea.Read(ctx, runtime.Logs[0], 16); err != nil || !found {
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
