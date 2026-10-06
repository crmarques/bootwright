package lifecycle

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/contextguard"
)

const reviewWithStatus = "review its durable state with bootwright status --context lab"

// The exits of the record states neither verb acts on: deleting the context,
// acknowledging orphans unless its evidence is pristine.
const (
	plainDeletion    = "delete the context with bootwright context delete --name lab --purge"
	orphanedDeletion = plainDeletion + " --allow-orphans, which abandons what it may still own"
)

// requireRefused runs verb over what the harness holds and requires it to
// refuse with exactly message and remedy before it takes the exclusive lock,
// writes, binds, releases or reaches the host.
func requireRefused(t *testing.T, h *harness, verb reconciliation.Verb, message, remedy string) {
	t.Helper()
	ctx := context.Background()
	before, mutations := untouchedOf(h), h.workspace.mutations
	var err error
	if verb == reconciliation.Apply {
		_, err = h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
	} else {
		_, err = h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
	}
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Message != message || reported[0].Remediation != remedy {
		t.Fatalf("the %s = %+v (%v), want the refusal %q", verb, reported, err, message)
	}
	before.require(t, h)
	if h.workspace.mutations != mutations {
		t.Fatalf("the refused %s took the exclusive lock %d times", verb, h.workspace.mutations-mutations)
	}
}

// requirePreviewRefused holds a plan preview to the refusal of the verb it
// decides as.
func requirePreviewRefused(t *testing.T, h *harness, message, remedy string) {
	t.Helper()
	before := untouchedOf(h)
	_, err := h.service.Plan(context.Background(), PlanRequest{ContextName: testContextName})
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Message != message || reported[0].Remediation != remedy {
		t.Fatalf("the preview = %+v (%v), want the refusal %q", reported, err, message)
	}
	before.require(t, h)
}

// requireStatusNames holds status to the contradictions a refusal named, in
// the order it named them.
func requireStatusNames(t *testing.T, h *harness, entries ...string) {
	t.Helper()
	status, err := h.service.Status(context.Background(), StatusRequest{ContextName: testContextName})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(status.Contradictions, entries) {
		t.Fatalf("status names %q, want %q", status.Contradictions, entries)
	}
}

func unindexedDirectory(id string) string {
	return "the operation directory " + id + " lists block records"
}

// A lost index reads as no operation. The records beside it still name what
// the context's operations started, and the evidence what they left, so a
// fresh apply refuses rather than register beside them, a destroy refuses
// rather than settle over them whatever the evidence reads, and status names
// each of them as both refusals do. Both refusals name deleting the context as
// the exit, acknowledging orphans only beside evidence that is not pristine.
func TestAVerbOverALostIndexRefusesAndStatusNamesWhatNoIndexAccountsFor(t *testing.T) {
	ctx := context.Background()
	for name, test := range map[string]struct {
		arrange func(*testing.T, *harness) []string
		remedy  string
	}{
		"after a completed removal, beside pristine evidence": {remedy: plainDeletion, arrange: func(t *testing.T, h *harness) []string {
			applied := completeApply(t, h)
			if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(h.workspace.evidence, killPristine(t)) {
				t.Fatalf("the completed removal left evidence %q", h.workspace.evidence)
			}
			directories := slices.Sorted(slices.Values([]string{applied, currentOperation(t, h)}))
			return []string{unindexedDirectory(directories[0]), unindexedDirectory(directories[1])}
		}},
		"after a completed apply": {remedy: orphanedDeletion, arrange: func(t *testing.T, h *harness) []string {
			applied := completeApply(t, h)
			return []string{"the mutation evidence reads applied and retained", unindexedDirectory(applied)}
		}},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "alpha")
			entries := test.arrange(t, h)
			lose(h, "index.json")
			if currentOperation(t, h) != "" {
				t.Fatal("the lost index still names an operation")
			}
			h.capability.applies, h.capability.destroys = nil, nil
			requireStatusNames(t, h, entries...)
			message := "the context holds operation records or evidence that no index names: " + strings.Join(entries, ", ")
			requirePreviewRefused(t, h, message, test.remedy)
			requireRefused(t, h, reconciliation.Apply, message, test.remedy)
			requireRefused(t, h, reconciliation.Destroy, message, test.remedy)
		})
	}
}

// A destroy completes only once every block of its plan is done, so a
// completed one holding a block that is not done, as a lost record reads,
// proves nothing removed that block's effect. Neither verb settles or plans
// over it: each refuses naming the block and, since the removal's completion
// left pristine evidence, a plain deletion of the context as the exit, and
// status names the block too.
func TestACompletedDestroyHoldingABlockNotDoneRefusesEitherVerb(t *testing.T) {
	h := newHarness(t, "alpha", "bravo")
	completeApply(t, h)
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	removal := currentOperation(t, h)
	lose(h, path.Join(removal, "blocks", "bravo")+"/")
	requireStatusNames(t, h, "bravo (pending)")
	message := "the completed destroy " + removal + " records no block completion for these blocks, so nothing proves their effects removed: bravo (pending)"
	requirePreviewRefused(t, h, message, plainDeletion)
	requireRefused(t, h, reconciliation.Apply, message, plainDeletion)
	requireRefused(t, h, reconciliation.Destroy, message, plainDeletion)
}

// The deletion a refusal names is the one the context guard admits, decided
// from the guard's own reading of the evidence, however the record is
// spelled: a plain deletion over evidence it reads as pristine, an
// acknowledged one over any other evidence it reads, and none over evidence
// it cannot read, which no deletion admits.
func TestTheDeletionARefusalNamesIsTheOneTheGuardAdmits(t *testing.T) {
	ctx := context.Background()
	for name, test := range map[string]struct{ evidence, remedy string }{
		"respelled pristine evidence":  {`{"version":1,"operation":"none","ownership":"none"}`, plainDeletion},
		"respelled protected evidence": {`{"ownership":"retained","operation":"applied","version":1}`, orphanedDeletion},
		"corrupt evidence":             {`{`, unreadableEvidenceExit},
		"unsupported evidence":         {`{"version":2,"operation":"none","ownership":"none"}` + "\n", unreadableEvidenceExit},
	} {
		disposition, err := (contextguard.Guard{}).Check(ctx, []byte(test.evidence))
		admitted := orphanedDeletion
		switch {
		case err != nil:
			admitted = unreadableEvidenceExit
		case disposition.Dispose:
			admitted = plainDeletion
		}
		if admitted != test.remedy {
			t.Fatalf("%s: the guard grants %+v (%v), so the remedy is %q, not %q", name, disposition, err, admitted, test.remedy)
		}
		t.Run(name+" beside no operation", func(t *testing.T) {
			h := newHarness(t, "alpha")
			h.workspace.evidence = []byte(test.evidence)
			entry := "the mutation evidence reads an unrecognized record"
			requireStatusNames(t, h, entry)
			message := "the context holds operation records or evidence that no index names: " + entry
			requirePreviewRefused(t, h, message, test.remedy)
			requireRefused(t, h, reconciliation.Apply, message, test.remedy)
			requireRefused(t, h, reconciliation.Destroy, message, test.remedy)
		})
		t.Run(name+" beside a completed destroy holding a block that is not done", func(t *testing.T) {
			h := newHarness(t, "alpha", "bravo")
			completeApply(t, h)
			if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
				t.Fatal(err)
			}
			removal := currentOperation(t, h)
			lose(h, path.Join(removal, "blocks", "bravo")+"/")
			h.workspace.evidence = []byte(test.evidence)
			message := "the completed destroy " + removal + " records no block completion for these blocks, so nothing proves their effects removed: bravo (pending)"
			requireRefused(t, h, reconciliation.Apply, message, test.remedy)
			requireRefused(t, h, reconciliation.Destroy, message, test.remedy)
		})
	}
}

// failedRemovalWithEveryBlockDone leaves a failed removal whose resolution
// proved alpha done before a kill.
func failedRemovalWithEveryBlockDone(t *testing.T) (*harness, string) {
	t.Helper()
	return failedRemovalResolvedAs(t, reconciliation.EffectCompleted, reconciliation.BlockDone)
}

// failedRemovalResolvedAs removes alpha with a removal that fails and is then
// left running, as a kill before it recorded the failure leaves it; continues
// it with a retry whose start lands and then reports a failure, which records
// the removal failed beside alpha's running record; and then supersedes it
// with a removal whose resolution observes effect and lands alpha's record
// reading alpha, killed there, before it recorded what that proved.
func failedRemovalResolvedAs(t *testing.T, effect reconciliation.EffectState, alpha reconciliation.BlockState) (*harness, string) {
	t.Helper()
	ctx := context.Background()
	h := newHarness(t, "alpha")
	completeApply(t, h)
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeFailed}}
	if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
		t.Fatal("a removal whose block failed reported success")
	}
	removal := currentOperation(t, h)
	rewriteState(t, h, path.Join(removal, "operation.json"), string(reconciliation.OperationRunning))
	record := path.Join(removal, "blocks", "alpha", "state.json")
	h.workspace.area.failAfter["replace "+record] = errors.New("interrupted")
	if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
		t.Fatal("a continuation whose retry start reported a failure reported success")
	}
	if landed := string(h.workspace.area.files[record]); !strings.Contains(landed, `"state":"running","attempts":2`) {
		t.Fatalf("the retry start did not land before it failed: %s", landed)
	}
	requireRemovalRecords(t, h, removal, reconciliation.OperationFailed, reconciliation.BlockRunning)
	h.capability.observations = []Observation{{Effect: effect}}
	killed := false
	h.workspace.area.landing = func(_, _ string, files map[string][]byte) error {
		if strings.Contains(string(files[record]), `"state":"`+string(alpha)+`"`) {
			killed = true
			return errors.New("killed")
		}
		return nil
	}
	if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
		t.Fatal("a removal killed as its resolution landed reported success")
	}
	h.workspace.area.landing = nil
	if !killed {
		t.Fatal("the superseding removal never resolved alpha")
	}
	requireRemovalRecords(t, h, removal, reconciliation.OperationFailed, alpha)
	return h, removal
}

func requireRemovalRecords(t *testing.T, h *harness, removal string, state reconciliation.OperationState, alpha reconciliation.BlockState) {
	t.Helper()
	operation, states := durableOperation(t, h)
	if operation.ID != removal || operation.Verb != reconciliation.Destroy || operation.State != state ||
		!maps.Equal(states, map[string]reconciliation.BlockState{"alpha": alpha}) {
		t.Fatalf("the records read %s %s %s %v, want the removal %s %s with alpha %s", operation.ID, operation.Verb, operation.State, states, removal, state, alpha)
	}
}

// A failed removal whose blocks are all done has a record that lags them, as
// an unknown one does. The destroy finalizes it without a confirmation, a
// presented plan or another effect, releasing its bindings and publishing
// pristine evidence, and then settles; an apply still refuses the incomplete
// destroy.
func TestAFailedRemovalWhoseBlocksAreAllDoneIsFinalizedByTheDestroy(t *testing.T) {
	ctx := context.Background()
	h, removal := failedRemovalWithEveryBlockDone(t)
	requireStatusNames(t, h)
	before := untouchedOf(h)
	_, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Message != "an incomplete destroy must be continued before another operation" {
		t.Fatalf("the apply = %+v (%v)", reported, err)
	}
	before.require(t, h)
	h.service.options.Confirmer = nil
	destroys, observes, presented := len(h.capability.destroys), len(h.capability.observes), len(h.presenter.presented)
	result, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName})
	if err != nil || !result.Settled || result.Recovered != RecoveredFinalization || result.Receipt.Operation != removal || result.Receipt.State != "done" {
		t.Fatalf("the destroy = %+v, %+v (%v)", result, diagnostics.Of(err), err)
	}
	requireRemovalRecords(t, h, removal, reconciliation.OperationDone, reconciliation.BlockDone)
	if !bytes.Equal(h.workspace.evidence, killPristine(t)) || !slices.Equal(h.binder.released, []string{"bind-1"}) {
		t.Fatalf("the destroy left evidence %q and released %v", h.workspace.evidence, h.binder.released)
	}
	if len(h.capability.destroys) != destroys || len(h.capability.observes) != observes || len(h.presenter.presented) != presented {
		t.Fatal("the destroy ran or observed a block, or presented a plan")
	}
}

// A continuation refuses records that contradict what its operation started
// before it restores, raises or marks anything, and status names what it
// refuses: a block that lost its record beside an attempt of it, which a start
// would refuse only after the operation was marked running, including the
// first start an executable before 83dcbebe left as an attempt beside no block
// record.
func TestAContinuationOverContradictedRecordsRefusesBeforeItMarksItsOperation(t *testing.T) {
	ctx := context.Background()
	for name, test := range map[string]struct {
		arrange func(*testing.T, *harness) string
		verb    reconciliation.Verb
		entry   string
		// named is what status names where it is more than the entry: what a
		// removal of the same records refuses too.
		named []string
	}{
		"a failed apply whose failed block lost its record": {
			arrange: func(t *testing.T, h *harness) string {
				applyChained(t, h, map[string]Result{"charlie": {Outcome: reconciliation.OutcomeFailed}})
				lose(h, path.Join(currentOperation(t, h), "blocks", "charlie", "state.json"))
				return currentOperation(t, h)
			},
			verb: reconciliation.Apply, entry: lostRecord("charlie"), named: []string{lostRecord("charlie"), failedWithoutFailure},
		},
		"a running apply whose first start an earlier executable left beside no block record": {
			arrange: func(t *testing.T, h *harness) string {
				interrupted := false
				h.workspace.area.landing = func(operation, target string, files map[string][]byte) error {
					if operation != "replace" || path.Base(target) != "state.json" || path.Base(path.Dir(target)) != "alpha" {
						return nil
					}
					if _, exists := files[path.Join(path.Dir(target), "attempt-000001.json")]; exists {
						interrupted = true
						return errors.New("interrupted")
					}
					return nil
				}
				if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
					t.Fatal("an interrupted start reported success")
				}
				h.workspace.area.landing = nil
				current := currentOperation(t, h)
				lose(h, path.Join(current, "blocks", "alpha", "state.json"))
				if !interrupted || !strings.Contains(string(h.workspace.area.files[path.Join(current, "blocks", "alpha", "attempt-000001.json")]), `"phase":"running"`) {
					t.Fatal("the interruption left no running attempt beside the lost block record")
				}
				return current
			},
			verb: reconciliation.Apply, entry: lostRecord("alpha"),
		},
		"a running destroy whose done block lost its record": {
			arrange: func(t *testing.T, h *harness) string {
				applyChained(t, h, nil)
				h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeChanged}, {Outcome: reconciliation.OutcomeFailed}}
				if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
					t.Fatal("a removal whose block failed reported success")
				}
				h.capability.outcomes = nil
				removal := currentOperation(t, h)
				rewriteState(t, h, path.Join(removal, "operation.json"), string(reconciliation.OperationRunning))
				lose(h, path.Join(removal, "blocks", "charlie", "state.json"))
				return removal
			},
			verb: reconciliation.Destroy, entry: lostRecord("charlie"),
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newPlannedHarness(t, chainedDefinitions())
			current := test.arrange(t, h)
			h.capability.applies, h.capability.destroys, h.capability.observes = nil, nil, nil
			named := test.named
			if named == nil {
				named = []string{test.entry}
			}
			requireStatusNames(t, h, named...)
			message := "the incomplete " + string(test.verb) + " " + current + " holds records that contradict what it started, so it cannot be continued: " + test.entry
			requireRefused(t, h, test.verb, message, reviewWithStatus)
			requirePreviewRefused(t, h, message, reviewWithStatus)
		})
	}
}

// A block starts only once every dependency is done, and a done block never
// changes, so a dependency that reads anything but done beside a dependent
// that started contradicts the records, whatever it reads. A running block
// accounts for a failed apply only as the retry whose start landed and then
// failed, which counts the attempt it retried too; one that never retried
// cannot mask a failed block whose whole directory was lost. A removal refuses
// each, and status names it.
func TestARemovalRefusesADependencyNotDoneAndARunningBlockThatMasksALostFailure(t *testing.T) {
	failed := map[string]Result{"charlie": {Outcome: reconciliation.OutcomeFailed}}
	rewritten := func(state reconciliation.BlockState) func(*testing.T, *harness) {
		return func(t *testing.T, h *harness) {
			applyChained(t, h, failed)
			rewriteState(t, h, path.Join(currentOperation(t, h), "blocks", "bravo", "state.json"), string(state))
		}
	}
	for name, test := range map[string]struct {
		definitions []reconciliation.BlockDefinition
		arrange     func(*testing.T, *harness)
		entry       string
	}{
		"a dependency rewritten failed while its dependent started": {
			definitions: chainedDefinitions(), arrange: rewritten(reconciliation.BlockFailed),
			entry: "bravo (failed, yet charlie, which depends on it, started)",
		},
		"a dependency rewritten running while its dependent started": {
			definitions: chainedDefinitions(), arrange: rewritten(reconciliation.BlockRunning),
			entry: "bravo (running, yet charlie, which depends on it, started)",
		},
		"a dependency rewritten unknown while its dependent started": {
			definitions: chainedDefinitions(), arrange: rewritten(reconciliation.BlockUnknown),
			entry: "bravo (unknown, yet charlie, which depends on it, started)",
		},
		"a running block beside a failed block whose directory was lost": {
			definitions: []reconciliation.BlockDefinition{definition("alpha"), definition("bravo")},
			arrange: func(t *testing.T, h *harness) {
				applyChained(t, h, map[string]Result{"bravo": {Outcome: reconciliation.OutcomeFailed}})
				leaveRunning(t, h, "alpha")
				lose(h, path.Join(currentOperation(t, h), "blocks", "bravo")+"/")
				if operation, states := durableOperation(t, h); operation.State != reconciliation.OperationFailed ||
					!maps.Equal(states, map[string]reconciliation.BlockState{"alpha": "running", "bravo": "pending"}) {
					t.Fatalf("the records read %s with %v", operation.State, states)
				}
			},
			entry: failedWithoutFailure,
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newPlannedHarness(t, test.definitions)
			test.arrange(t, h)
			current := currentOperation(t, h)
			h.capability.applies, h.capability.destroys, h.capability.observes = nil, nil, nil
			requireRefused(t, h, reconciliation.Destroy, "the incomplete apply "+current+
				" holds records that contradict what it started, and a removal that skipped such a block would leave its effect in place: "+test.entry, reviewWithStatus)
			requireStatusNames(t, h, test.entry)
		})
	}
}

// An incomplete apply that records it started a block while no block record
// says so owns nothing its removal could take back. The removal refuses, and
// status names the operation it refuses over beside what else it contradicts.
func TestStatusNamesAnIncompleteApplyThatRecordsNoStartedBlock(t *testing.T) {
	for name, test := range map[string]struct {
		outcome reconciliation.Outcome
		named   []string
	}{
		"an unknown apply": {outcome: reconciliation.OutcomeUnknown, named: []string{"the apply records unknown, yet no block of it started"}},
		"a failed apply":   {outcome: reconciliation.OutcomeFailed, named: []string{failedWithoutFailure, "the apply records failed, yet no block of it started"}},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "alpha")
			applyChained(t, h, map[string]Result{"alpha": {Outcome: test.outcome}})
			lose(h, path.Join(currentOperation(t, h), "blocks", "alpha")+"/")
			requireRefused(t, h, reconciliation.Destroy, "the operation this removal supersedes records no block it still owns", reviewWithStatus)
			requireStatusNames(t, h, test.named...)
		})
	}
}

// A failed removal holding a block not done is replaced by a fresh removal of
// what it has not proved gone, which reads no block record of it, so a record
// it lost contradicts nothing status offers: status names no contradiction
// beside the replacing destroy, and that destroy decides. A running removal
// is continued, which reads every record, so its lost record is still named
// and nothing is offered.
func TestAFailedDestroysLostRecordIsNoContradiction(t *testing.T) {
	ctx := context.Background()
	for _, test := range []struct {
		state          reconciliation.OperationState
		contradictions []string
		steps          []string
	}{
		{reconciliation.OperationFailed, []string{}, []string{"bootwright destroy --context lab"}},
		{reconciliation.OperationRunning, []string{lostRecord("charlie")}, []string{}},
	} {
		t.Run(string(test.state), func(t *testing.T) {
			h := newPlannedHarness(t, chainedDefinitions())
			failedChainedRemoval(t, h)
			removal := currentOperation(t, h)
			if test.state != reconciliation.OperationFailed {
				rewriteState(t, h, path.Join(removal, "operation.json"), string(test.state))
			}
			lose(h, path.Join(removal, "blocks", "charlie", "state.json"))
			status, err := h.service.Status(ctx, StatusRequest{ContextName: testContextName})
			if err != nil || !slices.Equal(status.Contradictions, test.contradictions) || !slices.Equal(status.NextSteps, test.steps) {
				t.Fatalf("status = %+v (%v), want contradictions %q and steps %q", status, err, test.contradictions, test.steps)
			}
			requireDecidesIfOffered(t, h, reconciliation.Destroy, len(test.steps) != 0)
		})
	}
}
