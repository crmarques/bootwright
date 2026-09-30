package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"path"
	"sync"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

// handedProof is what one attempt or observation was handed as its
// dependencies' evidence.
type handedProof struct {
	call   string
	proved []BlockEvidence
}

// provedRecorder notes what every call the engine makes was handed, destroys
// included, and answers from the fixture it wraps.
type provedRecorder struct {
	*testCapability
	mutex  sync.Mutex
	handed []handedProof
}

func (r *provedRecorder) note(call string, execution Execution) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.handed = append(r.handed, handedProof{call: call + execution.Block.ID, proved: execution.Proved})
}

func (r *provedRecorder) Apply(ctx context.Context, execution Execution) (Result, error) {
	r.note("apply:", execution)
	return r.testCapability.Apply(ctx, execution)
}

func (r *provedRecorder) Destroy(ctx context.Context, execution Execution) (Result, error) {
	r.note("destroy:", execution)
	return r.testCapability.Destroy(ctx, execution)
}

func (r *provedRecorder) Observe(ctx context.Context, execution Execution) (Observation, error) {
	r.note("observe:", execution)
	return r.testCapability.Observe(ctx, execution)
}

func (r *provedRecorder) ObserveRemoval(ctx context.Context, execution Execution) (Observation, error) {
	r.note("observe-removal:", execution)
	return r.testCapability.ObserveRemoval(ctx, execution)
}

func (r *provedRecorder) calls() []handedProof {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return append([]handedProof(nil), r.handed...)
}

func recordingProof(h *harness) *provedRecorder {
	recorder := &provedRecorder{testCapability: h.capability}
	h.service.capabilities = testResolver{capability: recorder}
	return recorder
}

// interruptCompletion fails the durable completion of one block's first
// attempt, so that attempt stays running on disk, as an executor that died
// after its effect leaves it, and the block is proved only by a resolution.
func interruptCompletion(h *harness, block string) string {
	name, _ := reconciliation.FormatNumber(1)
	recorded := "replace " + path.Join(firstOperation, "blocks", block, "attempt-"+name+".json")
	h.workspace.area.mutex.Lock()
	defer h.workspace.area.mutex.Unlock()
	h.workspace.area.fail[recorded] = errors.New("interrupted")
	return recorded
}

func clearFault(h *harness, recorded string) {
	h.workspace.area.mutex.Lock()
	defer h.workspace.area.mutex.Unlock()
	delete(h.workspace.area.fail, recorded)
}

func applyOnce(h *harness) error {
	_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	return err
}

// An apply attempt is handed what each block it depends on durably proved in
// this operation, in frozen plan order and unread, and nothing of a block it
// does not depend on. A dependency a resolution completed after its attempt
// never recorded an outcome is handed what that resolution observed, because
// the resolution, not the attempt, is what proved it.
func TestAnApplyAttemptReceivesWhatItsDependenciesProved(t *testing.T) {
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{
		definition("alpha"), definition("bravo"), definition("delta"), dependent("charlie", "alpha", "delta"),
	})
	recorder := recordingProof(h)
	h.capability.outcomeFor = map[string]Result{
		"alpha": {Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(`{"proved":"alpha"}`)},
		"bravo": {Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(`{"proved":"bravo"}`)},
		"delta": {Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(`{"proved":"delta"}`)},
	}
	recorded := interruptCompletion(h, "delta")
	if err := applyOnce(h); err == nil {
		t.Fatal("an apply whose block could not record its completion succeeded")
	}
	clearFault(h, recorded)
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted, Evidence: json.RawMessage(`{"observed":"delta"}`)}}
	if err := applyOnce(h); err != nil {
		t.Fatalf("continuing the apply: %v", err)
	}
	var charlie []BlockEvidence
	applied := 0
	for _, call := range recorder.calls() {
		switch call.call {
		case "apply:charlie":
			charlie, applied = call.proved, applied+1
		case "apply:alpha", "apply:bravo", "apply:delta", "observe:delta":
			if call.proved != nil {
				t.Fatalf("%s, which depends on nothing, was handed %+v", call.call, call.proved)
			}
		default:
			t.Fatalf("unexpected call %s", call.call)
		}
	}
	if applied != 1 || len(charlie) != 2 {
		t.Fatalf("charlie was applied %d times and handed %+v", applied, charlie)
	}
	for index, want := range []struct {
		object   string
		evidence string
	}{{"alpha", `{"proved":"alpha"}`}, {"delta", `{"observed":"delta"}`}} {
		got := charlie[index]
		if got.Kind != "ArtifactServer" || got.Object != want.object || got.Implementation != "artifact-server-nginx-v1" ||
			got.Verb != reconciliation.Apply || got.State != reconciliation.BlockDone || string(got.Evidence) != want.evidence {
			t.Fatalf("charlie was handed %+v for %s, want its done apply with %q", got, want.object, want.evidence)
		}
	}
}

// Only an apply attempt is handed its dependencies' evidence. A resolution
// observes what its own frozen request did, and a removal's dependencies are
// the blocks it removes before, which prove their absence rather than anything
// its effect relies on, so neither is handed any.
func TestOnlyAnApplyAttemptReceivesProvedEvidence(t *testing.T) {
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{definition("alpha"), dependent("charlie", "alpha")})
	recorder := recordingProof(h)
	recorded := interruptCompletion(h, "charlie")
	if err := applyOnce(h); err == nil {
		t.Fatal("an apply whose block could not record its completion succeeded")
	}
	clearFault(h, recorded)
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
	if err := applyOnce(h); err != nil {
		t.Fatalf("continuing the apply: %v", err)
	}
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	seen := map[string]bool{}
	for _, call := range recorder.calls() {
		seen[call.call] = true
		if call.call == "apply:charlie" {
			if len(call.proved) != 1 || call.proved[0].Object != "alpha" {
				t.Fatalf("charlie's apply was handed %+v", call.proved)
			}
			continue
		}
		if call.proved != nil {
			t.Fatalf("%s was handed %+v", call.call, call.proved)
		}
	}
	for _, want := range []string{"apply:charlie", "observe:charlie", "destroy:charlie", "destroy:alpha"} {
		if !seen[want] {
			t.Fatalf("the journey never made %s: %+v", want, recorder.calls())
		}
	}
}

// What an attempt relies on is read before it starts. When a dependency's
// attempt record cannot be read, the dependent never runs without that proof
// and never starts: it records no attempt and keeps the pending state it had.
func TestAnUnreadableDependencyProofStartsNoAttempt(t *testing.T) {
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{definition("alpha"), dependent("charlie", "alpha")})
	recorder := recordingProof(h)
	h.capability.outcomeFor = map[string]Result{
		"alpha": {Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(`{"proved":"alpha"}`)},
	}
	refuseStart(h, "charlie", 1)
	if err := applyOnce(h); err == nil {
		t.Fatal("an apply whose block could not start succeeded")
	}
	allowStart(h)
	name, _ := reconciliation.FormatNumber(1)
	unreadable := "read " + path.Join(firstOperation, "blocks", "alpha", "attempt-"+name+".json")
	h.workspace.area.mutex.Lock()
	h.workspace.area.fail[unreadable] = errors.New("unreadable")
	h.workspace.area.mutex.Unlock()
	if err := applyOnce(h); err == nil {
		t.Fatal("an apply whose dependency's proof could not be read succeeded")
	}
	clearFault(h, unreadable)
	for _, call := range recorder.calls() {
		if call.call == "apply:charlie" {
			t.Fatalf("charlie applied with %+v although alpha's proof could not be read", call.proved)
		}
	}
	if records := attemptRecords(h, "charlie"); len(records) != 0 {
		t.Fatalf("charlie holds %v although its attempt never started", records)
	}
	if _, states := durableOperation(t, h); states["alpha"] != reconciliation.BlockDone || states["charlie"] != reconciliation.BlockPending {
		t.Fatalf("durable states = %v", states)
	}
}
