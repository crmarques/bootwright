package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

type originalProofRecorder struct{ *provedRecorder }

func (r originalProofRecorder) Quiescent(ctx context.Context, probe Probe) (Quiescence, error) {
	r.note("probe:", probe.Execution())
	return r.testCapability.Quiescent(ctx, probe)
}

func TestOriginalApplyDependenciesSurviveResolutionAndRemoval(t *testing.T) {
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{
		definition("alpha"), dependent("bravo", "alpha"), dependent("charlie", "bravo"), definition("unrelated"),
	})
	h.service.options.Concurrency = 1
	recorder := recordingProof(h)
	h.service.capabilities = testResolver{capability: originalProofRecorder{recorder}}
	h.capability.outcomeFor = map[string]Result{
		"alpha": {Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(`{"host":"original"}`)},
		"bravo": {Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(`{"disk":"original"}`)},
	}
	fault := interruptCompletion(h, "charlie")
	if err := applyOnce(h); err == nil {
		t.Fatal("the seeded interruption did not fire")
	}
	clearFault(h, fault)
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
	if err := applyOnce(h); err != nil {
		t.Fatal(err)
	}
	created, _ := durableOperation(t, h)
	renamed(h, "renamed")
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	request := DestroyRequest{ContextName: "renamed", SkipConfirmation: true}
	if _, err := h.service.Destroy(context.Background(), request); err == nil {
		t.Fatal("the seeded unknown removal did not fire")
	}
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
	if _, err := h.service.Destroy(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, call := range recorder.calls() {
		if !strings.HasSuffix(call.call, ":charlie") {
			continue
		}
		seen[call.call] = true
		assertOriginalDependencies(t, call, created.ID)
	}
	for _, name := range []string{"apply:charlie", "observe:charlie", "probe:charlie", "destroy:charlie", "observe-removal:charlie"} {
		if !seen[name] {
			t.Fatalf("the journey did not make %s: %v", name, seen)
		}
	}
}

func assertOriginalDependencies(t *testing.T, call handedProof, operation string) {
	t.Helper()
	if call.applyOperation != operation || len(call.applied) != 2 {
		t.Fatalf("%s has wrong original dependency set: %+v", call.call, call)
	}
	for index, name := range []string{"alpha", "bravo"} {
		fact := call.applied[index]
		if fact.Object != name || fact.Verb != reconciliation.Apply || fact.State != reconciliation.BlockDone ||
			fact.Block.ID != name || fact.Block.RequestDigest == "" || !strings.Contains(string(fact.Evidence), "original") {
			t.Fatalf("%s lost original proof of %s: %+v", call.call, name, fact)
		}
	}
}

func TestOriginalApplyFactsCarryDetachedFrozenRequests(t *testing.T) {
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{definition("alpha"), dependent("bravo", "alpha")})
	recorder := recordingProof(h)
	if err := applyOnce(h); err != nil {
		t.Fatal(err)
	}
	calls := recorder.calls()
	index := slices.IndexFunc(calls, func(call handedProof) bool { return call.call == "apply:bravo" })
	proof := calls[index].applied[0]
	request := slices.Clone(proof.Block.Request)
	proof.Block.Request[0] = '!'
	proof.Evidence[0] = '!'
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	for _, call := range recorder.calls() {
		if call.call == "destroy:bravo" && (!slices.Equal(call.applied[0].Block.Request, request) || !json.Valid(call.applied[0].Evidence)) {
			t.Fatalf("a consumer changed the original durable proof: %+v", call)
		}
	}
}

func TestUnreadableOriginalTransitiveProofStartsNoAttempt(t *testing.T) {
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{
		definition("alpha"), dependent("bravo", "alpha"), dependent("charlie", "bravo"),
	})
	recorder := recordingProof(h)
	refuseStart(h, "charlie", 1)
	if err := applyOnce(h); err == nil {
		t.Fatal("the seeded interruption did not fire")
	}
	allowStart(h)
	number, _ := reconciliation.FormatNumber(1)
	unreadable := "read " + path.Join(firstOperation, "blocks", "alpha", "attempt-"+number+".json")
	h.workspace.area.mutex.Lock()
	h.workspace.area.fail[unreadable] = errors.New("unreadable original proof")
	h.workspace.area.mutex.Unlock()
	if err := applyOnce(h); err == nil {
		t.Fatal("an unreadable transitive identity allowed the dependent to run")
	}
	for _, call := range recorder.calls() {
		if call.call == "apply:charlie" {
			t.Fatal("the dependent ran without its original identity proof")
		}
	}
	if records := attemptRecords(h, "charlie"); len(records) != 0 {
		t.Fatalf("the unreadable original proof started an attempt: %v", records)
	}
}
