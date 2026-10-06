package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

// moved is what a later build derives from the same input: a different content
// digest and a different request, which is exactly what used to refuse every
// removal of a context applied before it.
func moved(id string) reconciliation.BlockDefinition {
	block := definition(id)
	block.ContentDigest = strings.Repeat("d", 64)
	block.Request = json.RawMessage(`{"name":"` + id + `","tuned":true}`)
	return block
}

// A removal is planned from the plan its apply froze, so a build whose
// derivation has moved since still removes exactly the effects that exist.
func TestARemovalPlansTheBlocksItsApplyFrozeAndNotWhatThisBuildDerives(t *testing.T) {
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{definition("artifacts"), definition("network")})
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	frozen := slices.Clone(h.capability.executions)
	h.capability.definitions = []reconciliation.BlockDefinition{moved("artifacts"), moved("network")}
	h.capability.executions = nil
	result, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil {
		t.Fatalf("a moved derivation refused to remove what it created: %v", err)
	}
	if result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("receipt = %+v", result.Receipt)
	}
	if !slices.Equal(h.capability.removals, []string{"artifacts", "network"}) {
		t.Fatalf("the removal read %v", h.capability.removals)
	}
	// Every removed block carries the request and digest its apply froze, so
	// the effects removed are the effects that exist.
	applied := map[string]reconciliation.Block{}
	for _, execution := range frozen {
		applied[execution.Block.ID] = execution.Block
	}
	removed := make([]string, 0, len(h.capability.executions))
	for _, execution := range h.capability.executions {
		removed = append(removed, execution.Block.ID)
	}
	if !slices.Equal(removed, h.capability.destroys) || !slices.Equal(removed, []string{"artifacts", "network"}) {
		t.Fatalf("the removal performed %v through %v; want every block its apply froze", h.capability.destroys, removed)
	}
	for _, execution := range h.capability.executions {
		before, found := applied[execution.Block.ID]
		if !found {
			t.Fatalf("%s was removed but never applied", execution.Block.ID)
		}
		if execution.Block.RequestDigest != before.RequestDigest {
			t.Fatalf("%s was removed under digest %s, not the frozen %s",
				execution.Block.ID, execution.Block.RequestDigest, before.RequestDigest)
		}
		if !slices.Equal(execution.Block.Request, before.Request) {
			t.Fatalf("%s was removed with %s, not the frozen %s",
				execution.Block.ID, execution.Block.Request, before.Request)
		}
		if execution.Block.ContentDigest != before.ContentDigest {
			t.Fatalf("%s was removed under content digest %s", execution.Block.ID, execution.Block.ContentDigest)
		}
	}
}

// A block's content digest travels with the block and is never compared on
// continuation: a removal keeps the one its apply froze, so a removal left
// unknown by one build is resolved by a later build whose derivation moved,
// under the digest its apply froze, and the operation it continues is its own.
func TestAContinuedRemovalKeepsTheContentDigestItsApplyFroze(t *testing.T) {
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{definition("artifacts")})
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	h.capability.definitions = []reconciliation.BlockDefinition{moved("artifacts")}
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("the seeded unproved removal did not fire")
	}
	unknown, _ := durableOperation(t, h)
	if unknown.Verb != reconciliation.Destroy {
		t.Fatalf("the unproved operation is a %s", unknown.Verb)
	}
	h.capability.calls, h.capability.executions = nil, nil
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
	result, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil {
		t.Fatalf("a removal registered before the derivation moved was not continued: %v", err)
	}
	if result.Receipt.Operation != unknown.ID || result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("the continuation ended %s as %s; want %s done", result.Receipt.Operation, result.Receipt.State, unknown.ID)
	}
	if !slices.Equal(h.capability.calls, []string{"observe-removal:artifacts"}) {
		t.Fatalf("the continuation called %v", h.capability.calls)
	}
	if len(h.capability.executions) != 1 {
		t.Fatalf("the continuation recorded %d executions; want exactly the one resolution", len(h.capability.executions))
	}
	if got := h.capability.executions[0].Block.ContentDigest; got != strings.Repeat("c", 64) {
		t.Fatalf("the removal was resolved under content digest %s, not the one its apply froze", got)
	}
}

// The words and impacts of a removal come from its capability reading the
// frozen request, so a plan describes removing rather than creating.
func TestARemovalIsPlannedInTheWordsOfWhatItRemoves(t *testing.T) {
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{definition("artifacts")})
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	preview, err := h.service.Plan(context.Background(), PlanRequest{ContextName: "lab"})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if preview.Verb != string(reconciliation.Destroy) || len(preview.Steps) != 1 {
		t.Fatalf("preview = %+v", preview)
	}
	if preview.Steps[0].Description != "remove artifacts" {
		t.Fatalf("step = %+v", preview.Steps[0])
	}
	if !slices.Equal(preview.Steps[0].Impacts, []string{"remove-artifacts"}) {
		t.Fatalf("impacts = %v", preview.Steps[0].Impacts)
	}
}

// A block whose apply was destructive is not removed under that acknowledgement:
// what a removal consumes is what removing it costs, which for an installation
// that retains the installed system is nothing.
func TestARemovalConsumesWhatRemovingCostsRatherThanWhatItsApplyAcknowledged(t *testing.T) {
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{destructive("artifacts")})
	if _, err := h.service.Apply(context.Background(), ApplyRequest{
		ContextName: "lab", Authorizations: []string{"data-loss"}, SkipConfirmation: true,
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	result, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil {
		t.Fatalf("a retaining removal demanded an authorization: %v", err)
	}
	if result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("receipt = %+v", result.Receipt)
	}
}

// A frozen request older than the version its capability reads refuses before
// anything is registered, naming the block and the executable that wrote it.
func TestARemovalRefusesAFrozenRequestNoCapabilityCanRead(t *testing.T) {
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{definition("artifacts")})
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	registered := h.workspace.area.clone()
	h.capability.removalErr = failure("lifecycle.state", "the frozen request has an unsupported version", "")
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	if code := firstCode(err); code != "lifecycle.state" {
		t.Fatalf("destroy of an unreadable block = %q (%v)", code, err)
	}
	reported := diagnostics.Of(err)[0]
	if !strings.Contains(reported.Message, "artifacts") || !strings.Contains(reported.Message, "unsupported version") {
		t.Fatalf("the refusal did not say what could not be read: %q", reported.Message)
	}
	if !strings.Contains(reported.Remediation, "bootwright devel (abcdef1)") {
		t.Fatalf("the refusal did not name the executable that wrote it: %q", reported.Remediation)
	}
	if len(h.capability.destroys) != 0 {
		t.Fatalf("a refused removal destroyed %v", h.capability.destroys)
	}
	if !sameFiles(h.workspace.area, registered) {
		t.Fatal("a refused removal changed durable operation state")
	}
	if len(h.binder.released) != 0 {
		t.Fatalf("a refused removal released %v", h.binder.released)
	}
}

// An implementation this executable no longer provides refuses the same way: a
// removal it cannot run is never planned as though it could.
func TestARemovalRefusesAnImplementationThisExecutableNoLongerProvides(t *testing.T) {
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{definition("artifacts")})
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	h.service.capabilities = boundResolver{
		{Kind: "ArtifactServer", Implementation: "artifact-server-caddy-v1"}: h.capability,
	}
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	if code := firstCode(err); code != "lifecycle.state" {
		t.Fatalf("destroy of an unprovided implementation = %q (%v)", code, err)
	}
	reported := diagnostics.Of(err)[0]
	if !strings.Contains(reported.Message, "artifact-server-nginx-v1") {
		t.Fatalf("the refusal did not name the implementation: %q", reported.Message)
	}
	if !strings.Contains(reported.Remediation, "bootwright devel (abcdef1)") {
		t.Fatalf("the refusal did not name the executable that wrote it: %q", reported.Remediation)
	}
}

// A removal presents the material that created what it removes: it reopens the
// binding its apply froze rather than binding what the declarations name now.
func TestARemovalReopensTheBindingItsApplyFroze(t *testing.T) {
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{definition("artifacts")})
	h.capability.secrets = []string{"artifact-server-tls"}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	h.capability.secrets = []string{"rotated-elsewhere"}
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if slices.Contains(h.binder.bound, "rotated-elsewhere") {
		t.Fatalf("the removal bound current declarations: %v", h.binder.bound)
	}
	if !slices.Equal(h.binder.bound, []string{"artifact-server-tls"}) {
		t.Fatalf("bound %v", h.binder.bound)
	}
	if !slices.Equal(h.binder.released, []string{"bind-1"}) {
		t.Fatalf("released %v", h.binder.released)
	}
}

// An unreadable block is refused even when every other block reads, because a
// removal covers the whole owned set or none of it.
func TestOneUnreadableBlockRefusesTheWholeRemoval(t *testing.T) {
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{definition("artifacts"), definition("network")})
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	h.capability.removalErr = errors.New("undiagnosed")
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("an unreadable block was removed anyway")
	}
	if len(h.capability.destroys) != 0 {
		t.Fatalf("a refused removal destroyed %v", h.capability.destroys)
	}
}

func sameFiles(current, before *memoryArea) bool {
	if len(current.files) != len(before.files) {
		return false
	}
	for name, value := range current.files {
		other, found := before.files[name]
		if !found || !slices.Equal(value, other) {
			return false
		}
	}
	return true
}

// orderRecorder notes, in order, each step a destroy takes up to its
// registration: the plan it presents, the prompt, each lock-holding proof, and
// the registration its log location follows.
type orderRecorder struct {
	mutex   sync.Mutex
	steps   []string
	prompts int
}

func (r *orderRecorder) note(step string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if last := len(r.steps) - 1; last < 0 || r.steps[last] != step {
		r.steps = append(r.steps, step)
	}
}

func (r *orderRecorder) PresentLifecyclePlan(context.Context, PlanResult) error {
	r.note("present")
	return nil
}

func (r *orderRecorder) Confirm(context.Context, string, string) error {
	r.prompts++
	r.note("confirm")
	return nil
}

func (r *orderRecorder) ReportProgress(_ context.Context, event ProgressEvent) {
	if event.Phase == CheckPhase {
		r.note(event.Block)
	}
}

func (r *orderRecorder) ReportLogLocation(context.Context, string) { r.note("register") }

// A destroy confirms the plan it presents, and only then takes the proofs that
// need the exclusive lock or a remote observation: the resolution of every
// effect it takes back and its quiescence gate. Both still precede
// registration, so a Machine found running after the prompt refuses with
// nothing registered and no effect.
func TestDestroyConfirmsBeforeItsLockHoldingProofsAndRefusesBeforeRegistration(t *testing.T) {
	h := newHarness(t, "artifact-server-lab", "machine-rhel-01")
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeChanged}, {Outcome: reconciliation.OutcomeUnknown}}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("the seeded unknown outcome did not fire")
	}
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
	recorder := &orderRecorder{}
	h.service.options.Presenter, h.service.options.Confirmer, h.service.options.Progress = recorder, recorder, recorder
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab"}); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if want := []string{"present", "confirm", resolutionCheck, quiescenceCheck, "register"}; !slices.Equal(recorder.steps, want) {
		t.Fatalf("a destroy took %v, want %v", recorder.steps, want)
	}

	live := newHarness(t, "artifact-server-lab", "machine-rhel-01")
	if _, err := live.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	live.capability.quiescence = map[string]Quiescence{
		"machine-rhel-01": {State: Live, Reason: "its domain is running", Stop: "bootwright machine stop --context lab --name rhel-01"},
	}
	registered := live.workspace.area.clone()
	recorder = &orderRecorder{}
	live.service.options.Presenter, live.service.options.Confirmer, live.service.options.Progress = recorder, recorder, recorder
	_, err := live.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab"})
	if code := firstCode(err); code != "lifecycle.live" {
		t.Fatalf("destroy over a running machine = %q (%v)", code, err)
	}
	if want := []string{"present", "confirm", quiescenceCheck}; recorder.prompts != 1 || !slices.Equal(recorder.steps, want) {
		t.Fatalf("a refused destroy asked %d times and took %v, want one prompt and %v", recorder.prompts, recorder.steps, want)
	}
	if len(live.capability.destroys) != 0 || !sameFiles(live.workspace.area, registered) {
		t.Fatalf("a refused removal destroyed %v or changed durable operation state", live.capability.destroys)
	}
}

// probingCapability is a capability whose quiescence is observed on the host.
type probingCapability struct{ *testCapability }

func (probingCapability) ProbesQuiescence() bool { return true }

// A destroy's plan names the Machines its quiescence gate observes, so the
// operator reads what to stop before confirming rather than after a refusal.
// A capability whose quiescence is derived is not named, and an apply's plan
// names none.
func TestADestroyPlanNamesTheMachinesToStop(t *testing.T) {
	machine := definition("machine-rhel-01")
	machine.Kind, machine.Object, machine.Implementation = "Machine", "rhel-01", "machine-test-v1"
	h := newPlannedHarness(t, nil)
	artifacts := &testCapability{definitions: []reconciliation.BlockDefinition{definition("artifacts")}, secrets: []string{"artifact-server-tls"}}
	machines := &testCapability{definitions: []reconciliation.BlockDefinition{machine}}
	h.service.capabilities = boundResolver{
		{Kind: "ArtifactServer", Implementation: "artifact-server-nginx-v1"}: artifacts,
		{Kind: "Machine", Implementation: "machine-test-v1"}:                 probingCapability{machines},
	}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(h.presenter.presented) != 1 || len(h.presenter.presented[0].Stops) != 0 {
		t.Fatalf("an apply's plan named Machines to stop: %+v", h.presenter.presented)
	}
	preview, err := h.service.Plan(context.Background(), PlanRequest{ContextName: "lab"})
	if err != nil || preview.Verb != string(reconciliation.Destroy) || !slices.Equal(preview.Stops, []string{"rhel-01"}) {
		t.Fatalf("destroy preview = %+v (%v), want rhel-01 to stop first", preview, err)
	}
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if presented := h.presenter.presented[len(h.presenter.presented)-1]; presented.Verb != "destroy" || !slices.Equal(presented.Stops, []string{"rhel-01"}) {
		t.Fatalf("destroy presented %+v, want rhel-01 to stop first", presented)
	}
}
