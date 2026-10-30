package lifecycle

import (
	"context"
	"path"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

func completedLineage(t *testing.T) (*harness, *lineageFixture) {
	t.Helper()
	h := newHarness(t, "producer-a")
	f := newLineageFixture(t, h, consumerDefinition())
	f.finish(t, "consumer", `{"head":true}`)
	f.complete(t)
	return h, f
}

func currentRemoval(t *testing.T, h *harness) (operationstore.Operation, reconciliation.Plan) {
	t.Helper()
	operation, _ := durableOperation(t, h)
	plan, err := lineageReader(h).ReadPlan(context.Background(), operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	return operation, plan
}

func blockIDs(plan reconciliation.Plan) []string {
	ids := make([]string, 0, len(plan.Blocks))
	for _, block := range plan.Blocks {
		ids = append(ids, block.ID)
	}
	return ids
}

func TestOneDestroyTakesTheWholeLineageBackTheAddedBlockFirst(t *testing.T) {
	h, f := completedLineage(t)
	f.h.binder.lost = []string{f.head.Bindings[0]}
	result, err := destroy(h)
	if err != nil || result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("destroy = %+v (%v)", result, diagnostics.Of(err))
	}
	if !slices.Equal(h.capability.destroys, []string{"consumer", "producer-a"}) {
		t.Fatalf("destroyed %v, want the addition before the root's block", h.capability.destroys)
	}
	removal, plan := currentRemoval(t, h)
	if removal.Verb != reconciliation.Destroy || removal.Source != f.head.ID || removal.Predecessor != "" {
		t.Fatalf("the removal records verb %s, source %q, predecessor %q, want a destroy of the head", removal.Verb, removal.Source, removal.Predecessor)
	}
	if got := blockIDs(plan); !slices.Equal(got, []string{"consumer", "producer-a"}) {
		t.Fatalf("the removal plan is %v", got)
	}
	if consumer, _ := plan.Block("consumer"); len(consumer.Dependencies) != 0 {
		t.Fatalf("the addition waits for %v", consumer.Dependencies)
	}
	if root, _ := plan.Block("producer-a"); !slices.Equal(root.Dependencies, []string{"consumer"}) {
		t.Fatalf("the root's block waits for %v, want its dependent", root.Dependencies)
	}
	if !slices.Equal(removal.Bindings, f.root.Bindings) {
		t.Fatalf("the removal reopened %v, want the root's %v and never the head's", removal.Bindings, f.root.Bindings)
	}
	released := slices.Clone(h.binder.released)
	slices.Sort(released)
	want := slices.Concat(f.root.Bindings, f.head.Bindings)
	slices.Sort(want)
	if !slices.Equal(released, want) {
		t.Fatalf("released %v, want every binding of the lineage %v", released, want)
	}
}

func TestADestroyOfALineageOfOneIsTheDestroyItWas(t *testing.T) {
	h := newHarness(t, "alpha", "beta")
	completeApply(t, h)
	root, _ := durableOperation(t, h)
	if _, err := destroy(h); err != nil {
		t.Fatal(err)
	}
	removal, plan := currentRemoval(t, h)
	if removal.Source != root.ID || len(plan.Blocks) != 2 || !slices.Equal(removal.Bindings, root.Bindings) {
		t.Fatalf("removal = %+v over %v", removal, blockIDs(plan))
	}
	if !slices.Equal(h.binder.released, root.Bindings) {
		t.Fatalf("released %v, want %v", h.binder.released, root.Bindings)
	}
}

func TestAFailedLineageRemovalIsReplacedOverWhatItHasNotRemoved(t *testing.T) {
	h, f := completedLineage(t)
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeFailed}}
	if _, err := destroy(h); err == nil {
		t.Fatal("the seeded removal failure did not fire")
	}
	failed, _ := durableOperation(t, h)
	if failed.Verb != reconciliation.Destroy || failed.State != reconciliation.OperationFailed {
		t.Fatalf("the first removal is %s %s", failed.Verb, failed.State)
	}
	if len(h.binder.released) != 0 {
		t.Fatalf("a failed removal released %v", h.binder.released)
	}
	result, err := destroy(h)
	if err != nil || result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("replacement = %+v (%v)", result, diagnostics.Of(err))
	}
	replacement, plan := currentRemoval(t, h)
	if replacement.Replaces != failed.ID || replacement.Source != f.head.ID || !slices.Equal(replacement.Bindings, f.root.Bindings) {
		t.Fatalf("replacement = %+v", replacement)
	}
	if got := blockIDs(plan); !slices.Equal(got, []string{"consumer", "producer-a"}) {
		t.Fatalf("the replacement plan is %v", got)
	}
	released := slices.Clone(h.binder.released)
	slices.Sort(released)
	want := slices.Concat(f.root.Bindings, f.head.Bindings)
	slices.Sort(want)
	if !slices.Equal(released, want) {
		t.Fatalf("released %v, want the root's and the addition's bindings", released)
	}
}

func TestAReplacementOnlyRemovesWhatTheFailedLineageRemovalLeft(t *testing.T) {
	h, f := completedLineage(t)
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeChanged}, {Outcome: reconciliation.OutcomeFailed}}
	if _, err := destroy(h); err == nil {
		t.Fatal("the seeded removal failure did not fire")
	}
	h.capability.destroys = nil
	if _, err := destroy(h); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	replacement, plan := currentRemoval(t, h)
	if got := blockIDs(plan); !slices.Equal(got, []string{"producer-a"}) || replacement.Source != f.head.ID {
		t.Fatalf("the replacement removes %v of %q", got, replacement.Source)
	}
	if !slices.Equal(h.capability.destroys, []string{"producer-a"}) {
		t.Fatalf("destroyed %v again", h.capability.destroys)
	}
	released := slices.Clone(h.binder.released)
	slices.Sort(released)
	want := slices.Concat(f.root.Bindings, f.head.Bindings)
	slices.Sort(want)
	if !slices.Equal(released, want) {
		t.Fatalf("released %v, want every binding of the lineage", released)
	}
}

func TestAnInterruptedLineageRemovalIsContinuedAndReleasesEveryBinding(t *testing.T) {
	h, f := completedLineage(t)
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	if _, err := destroy(h); err == nil {
		t.Fatal("the seeded unknown removal did not fire")
	}
	interrupted, _ := durableOperation(t, h)
	if interrupted.Verb != reconciliation.Destroy || interrupted.State != reconciliation.OperationUnknown {
		t.Fatalf("the interrupted removal is %s %s", interrupted.Verb, interrupted.State)
	}
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
	if _, err := destroy(h); err != nil {
		t.Fatalf("continuation: %v", diagnostics.Of(err))
	}
	continued, _ := durableOperation(t, h)
	if continued.ID != interrupted.ID || continued.State != reconciliation.OperationDone {
		t.Fatalf("the continuation is %s %s, want the same removal done", continued.ID, continued.State)
	}
	released := slices.Clone(h.binder.released)
	slices.Sort(released)
	want := slices.Concat(f.root.Bindings, f.head.Bindings)
	slices.Sort(want)
	if !slices.Equal(released, want) {
		t.Fatalf("released %v, want every binding of the lineage", released)
	}
}

func TestARemovalResolvesAPredecessorsUnprovedBlockIntoThePredecessorsRecords(t *testing.T) {
	h, f := completedLineage(t)
	rewriteState(t, h, path.Join(f.root.ID, "blocks", "producer-a", "state.json"), string(reconciliation.BlockRunning))
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
	if _, err := destroy(h); err != nil {
		t.Fatalf("destroy: %v", diagnostics.Of(err))
	}
	var resolved []Execution
	for _, execution := range h.capability.executions {
		if execution.Resolution != 0 {
			resolved = append(resolved, execution)
		}
	}
	if len(resolved) != 1 || resolved[0].Block.ID != "producer-a" || resolved[0].Operation != f.root.ID {
		t.Fatalf("resolutions = %+v, want one of producer-a under the root", resolved)
	}
	states, err := f.store.BlockStates(context.Background(), f.root.ID, f.rootPlan)
	if err != nil || states["producer-a"] != reconciliation.BlockDone {
		t.Fatalf("the root's block reads %v (%v), want the resolution recorded there", states, err)
	}
	headStates, err := f.store.BlockStates(context.Background(), f.head.ID, f.headPlan)
	if err != nil || headStates["consumer"] != reconciliation.BlockDone {
		t.Fatalf("the head's block reads %v (%v)", headStates, err)
	}
}

func TestAnUnprovedPredecessorBlockThatCannotBeResolvedRefusesTheWholeRemoval(t *testing.T) {
	h, f := completedLineage(t)
	rewriteState(t, h, path.Join(f.root.ID, "blocks", "producer-a", "state.json"), string(reconciliation.BlockUnknown))
	h.capability.observations = []Observation{{Effect: reconciliation.EffectUnknown}}
	_, err := destroy(h)
	reported := diagnostics.Of(err)
	if len(reported) == 0 || reported[0].Code != "lifecycle.unknown" {
		t.Fatalf("destroy = %+v (%v)", reported, err)
	}
	if len(h.capability.destroys) != 0 || len(h.binder.released) != 0 {
		t.Fatalf("a refused removal destroyed %v and released %v", h.capability.destroys, h.binder.released)
	}
	if current, _ := durableOperation(t, h); current.ID != f.head.ID {
		t.Fatalf("the context holds %s, want the head untouched", current.ID)
	}
}

func TestALostPredecessorRecordRefusesTheRemovalBeforeAnythingRuns(t *testing.T) {
	h, f := completedLineage(t)
	h.workspace.area.mutex.Lock()
	delete(h.workspace.area.files, path.Join(f.root.ID, "operation.json"))
	h.workspace.area.mutex.Unlock()
	_, err := destroy(h)
	if len(diagnostics.Of(err)) == 0 {
		t.Fatalf("destroy = %v, want a refusal", err)
	}
	if len(h.capability.destroys) != 0 || len(h.binder.released) != 0 || len(h.capability.removals) != 0 {
		t.Fatalf("a refused removal destroyed %v, planned %v and released %v", h.capability.destroys, h.capability.removals, h.binder.released)
	}
}

func TestARemovalReopensTheRootsBindingNotTheHeads(t *testing.T) {
	ctx := context.Background()
	_, f := completedLineage(t)
	scope, err := removalOver(ctx, f.store, f.head, f.headPlan, completedOwnership(f.headPlan))
	if err != nil {
		t.Fatal(err)
	}
	if scope.reopen != f.root.Bindings[0] || scope.reopen == f.head.Bindings[0] {
		t.Fatalf("reopen = %q, want the root's %q", scope.reopen, f.root.Bindings[0])
	}
	if !slices.Equal(scope.release, slices.Concat(f.root.Bindings, f.head.Bindings)) {
		t.Fatalf("release = %v", scope.release)
	}
	removal := operationstore.Operation{ID: "op-" + "d2d2d2d2d2d2d2d2d2d2d2d2d2d2d2d2", Verb: reconciliation.Destroy, Source: f.head.ID}
	replacement, err := replacementOf(ctx, f.store, removal, f.rootPlan)
	if err != nil || replacement.reopen != f.root.Bindings[0] || len(replacement.release) != 2 {
		t.Fatalf("a removal that names no binding reopens %q and releases %v (%v)", replacement.reopen, replacement.release, err)
	}
	removal.Bindings = []string{"rebound-1"}
	replacement, err = replacementOf(ctx, f.store, removal, f.rootPlan)
	if err != nil || replacement.reopen != "rebound-1" || len(replacement.release) != 3 {
		t.Fatalf("a rebound removal reopens %q and releases %v (%v)", replacement.reopen, replacement.release, err)
	}
	binding, err := removalBinding(ctx, f.store, f.head)
	if err != nil || binding != f.root.Bindings[0] {
		t.Fatalf("diff reopens %q (%v), want the root's", binding, err)
	}
	reopened, err := frozenReopenOf(ctx, f.store)
	if err != nil || reopened.binding != f.root.Bindings[0] {
		t.Fatalf("status reads the head's removal reopening %q (%v), want the root's", reopened.binding, err)
	}
}

func TestARebindDuringALineageRemovalNamesOnlyWhatTheRootBindingHolds(t *testing.T) {
	ctx := context.Background()
	h, f := completedLineage(t)
	h.binder.versions = map[string]string{"shared": "v2", "head-only": "v2"}
	h.binder.pins = map[string]map[string]string{
		f.root.Bindings[0]: {"shared": "v1"},
		f.head.Bindings[0]: {"shared": "v1", "head-only": "v1"},
	}
	var decided transition
	err := h.workspace.ReadLifecycle(ctx, testContextName, func(view View) (decideErr error) {
		decided, decideErr = h.service.decide(ctx, view, reconciliation.Destroy, reconciliation.Selection{})
		return decideErr
	})
	if err != nil || decided.reopen != f.root.Bindings[0] {
		t.Fatalf("the removal reopens %q (%v)", decided.reopen, err)
	}
	_, err = h.service.admitRebinds(ctx, testContextName, decided, []string{"head-only"})
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.authorization" {
		t.Fatalf("a rebind of a Secret only the head's binding holds = %+v (%v)", reported, err)
	}
}

func TestAnAdditiveAttemptIsAdmittedBeneathTheReserveOfItsWholeLineageRemoval(t *testing.T) {
	ctx := context.Background()
	_, f := completedLineage(t)
	release, err := f.store.AdmitAttempt(ctx, f.head, f.headPlan, "consumer", false)
	if err != nil {
		t.Fatalf("an attempt of the additive apply was refused: %+v", diagnostics.Of(err))
	}
	release()
	release, err = f.store.AdmitAttempt(ctx, f.root, f.rootPlan, "producer-a", true)
	if err != nil {
		t.Fatalf("a resolution of the root's block was refused: %+v", diagnostics.Of(err))
	}
	release()
}

func TestEvidenceUnderAnAdditiveHeadIsReadFromEachBlocksOwner(t *testing.T) {
	ctx := context.Background()
	h, _ := completedLineage(t)
	rootProof, err := h.service.Evidence(ctx, testContextName, "ArtifactServer", "producer-a")
	if err != nil || len(rootProof) != 1 || rootProof[0].State != reconciliation.BlockDone || rootProof[0].Verb != reconciliation.Apply {
		t.Fatalf("the root's evidence under the additive head = %+v (%v)", rootProof, err)
	}
	headProof, err := h.service.Evidence(ctx, testContextName, "ArtifactServer", "consumer")
	if err != nil || len(headProof) != 1 || string(headProof[0].Evidence) != `{"head":true}` {
		t.Fatalf("the head's evidence = %+v (%v)", headProof, err)
	}
	if none, err := h.service.Evidence(ctx, testContextName, "ArtifactServer", "absent"); err != nil || len(none) != 0 {
		t.Fatalf("an absent object read %+v (%v)", none, err)
	}
}

func TestADestroyNamingAnythingButAnApplyAsItsSourceRefuses(t *testing.T) {
	ctx := context.Background()
	_, f := completedLineage(t)
	other := f.root
	other.ID, other.Verb, other.Source, other.Predecessor = "op-"+strings.Repeat("e1", 16), reconciliation.Destroy, f.root.ID, ""
	removal := other
	removal.ID, removal.Source = "op-"+strings.Repeat("e2", 16), other.ID
	store := &sourceStore{recordReader: f.store, operations: map[string]operationstore.Operation{other.ID: other}}
	_, appliedErr := appliedLineage(ctx, store, removal)
	_, sourceErr := sourceLineage(ctx, store, other.ID)
	_, ownerErr := ownerID(ctx, store, removal, "producer-a")
	_, headErr := lineageOf(ctx, f.store, lineageMember{operation: other})
	for _, err := range []error{appliedErr, sourceErr, ownerErr, headErr} {
		requireStateRefusal(t, err, "")
	}
}

type sourceStore struct {
	recordReader
	operations map[string]operationstore.Operation
}

func (s *sourceStore) ReadOperation(ctx context.Context, id string) (operationstore.Operation, error) {
	if operation, held := s.operations[id]; held {
		return operation, nil
	}
	return s.recordReader.ReadOperation(ctx, id)
}

func TestAPlanOfAnAdditiveApplyWarnsOverTheRemovalOfItsWholeLineage(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "producer-a")
	f := newLineageFixture(t, h, consumerDefinition())
	var decided transition
	err := h.workspace.ReadLifecycle(ctx, testContextName, func(view View) (decideErr error) {
		decided, decideErr = h.service.decide(ctx, view, reconciliation.Apply, reconciliation.Selection{})
		return decideErr
	})
	if err != nil || decided.fresh || decided.operation.ID != f.head.ID {
		t.Fatalf("the continuation is %q fresh=%v (%v)", decided.operation.ID, decided.fresh, diagnostics.Of(err))
	}
	if _, err := decided.plan.Inverse(); err == nil {
		t.Fatal("the head's own plan inverts, so the warning would read only the addition")
	}
	inverse, err := decided.removed().Inverse()
	if err != nil || !slices.Equal(blockIDs(inverse), []string{"consumer", "producer-a"}) {
		t.Fatalf("the removal the plan warns over is %v (%v)", blockIDs(inverse), err)
	}
}

func TestTheFrozenStorageBlocksListEveryPlanOfTheLineage(t *testing.T) {
	r := newStorageRowsHarness(t)
	added := storageBlock("pool-meta", api.CephPool, "meta")
	added.Dependencies = []string{"cluster-ceph"}
	newLineageFixture(t, r.harness, added)
	found, err := r.service.StorageResources(context.Background(), testContextName)
	if err != nil {
		t.Fatal(err)
	}
	frozen := make([]string, 0, len(found.Frozen))
	for _, block := range found.Frozen {
		frozen = append(frozen, block.ID)
	}
	slices.Sort(frozen)
	if !slices.Equal(frozen, []string{"client-app", "cluster-ceph", "pool-meta", "pool-rbd"}) {
		t.Fatalf("frozen = %v, want the root's storage blocks and the addition's", frozen)
	}
}
