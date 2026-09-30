package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

// lostBindingStates are the operations whose continuation or fresh removal
// reopens bind-1, the binding their apply froze, each holding the host
// reservation that apply took.
func lostBindingStates() map[string]func(*testing.T) *harness {
	ctx := context.Background()
	reserved := func(t *testing.T) *harness {
		h := newPlannedHarness(t, chainedDefinitions())
		h.capability.reservations = reservationOf("alpha")
		return h
	}
	return map[string]func(*testing.T) *harness{
		"a failed apply": func(t *testing.T) *harness {
			h := reserved(t)
			applyChained(t, h, map[string]Result{"charlie": {Outcome: reconciliation.OutcomeFailed}})
			return h
		},
		"a completed apply": func(t *testing.T) *harness {
			h := reserved(t)
			completeApply(t, h)
			return h
		},
		"a failed apply whose blocks are all done": func(t *testing.T) *harness {
			h := reserved(t)
			completeApply(t, h)
			leaveFailedWithEveryBlockDone(t, h)
			return h
		},
		"an unknown destroy": func(t *testing.T) *harness {
			h := reserved(t)
			completeApply(t, h)
			h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
			if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
				t.Fatal("a removal whose block lost its outcome reported success")
			}
			h.capability.outcomes = nil
			return h
		},
		"a failed destroy": func(t *testing.T) *harness {
			h := reserved(t)
			failedChainedRemoval(t, h)
			return h
		},
	}
}

// finalizedRemovalStates are removals whose blocks are all done while their
// record lags, each naming bind-1 and holding the reservation an apply of alpha
// takes, which the failed one's harness is given since it declares none. Only a
// destroy acts on one, and it finalizes it, which reopens no binding.
func finalizedRemovalStates() map[string]func(*testing.T) *harness {
	return map[string]func(*testing.T) *harness{
		"a running destroy whose blocks are all done":  runningDestroyWithEveryBlockDone,
		"an unknown destroy whose blocks are all done": unknownDestroyWithEveryBlockDone,
		"a failed destroy whose blocks are all done": func(t *testing.T) *harness {
			h, _ := failedRemovalWithEveryBlockDone(t)
			h.workspace.reservations = reservationOf("alpha")
			return h
		},
	}
}

// runningDestroyWithEveryBlockDone applies alpha and removes it with a removal
// killed as it records itself done, after alpha's record of its removal
// landed, which is what a kill between the last outcome and the last write
// leaves.
func runningDestroyWithEveryBlockDone(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{destructive("alpha")})
	h.capability.consumes = map[string][]string{"alpha": dataLoss()}
	h.capability.reservations = reservationOf("alpha")
	if err := authorized(ctx, h.service, reconciliation.Apply); err != nil {
		t.Fatal(err)
	}
	applied, killed := currentOperation(t, h), false
	h.workspace.area.landing = func(operation, target string, files map[string][]byte) error {
		if operation != "replace" || path.Base(target) != "operation.json" || strings.HasPrefix(target, applied+"/") {
			return nil
		}
		if strings.Contains(string(files[path.Join(path.Dir(target), "blocks", "alpha", "state.json")]), `"state":"done"`) {
			killed = true
			return errors.New("killed")
		}
		return nil
	}
	if err := authorized(ctx, h.service, reconciliation.Destroy); err == nil {
		t.Fatal("a removal killed as it recorded itself done reported success")
	}
	h.workspace.area.landing = nil
	if !killed {
		t.Fatal("the removal never recorded itself done")
	}
	requireRecordedWithEveryBlockDone(t, h, reconciliation.Destroy, reconciliation.OperationRunning)
	return h
}

// deleteBinding leaves the keyring without bind-1, as an earlier defect or an
// operator's edit of it does, while the operation still names it.
func deleteBinding(t *testing.T, h *harness) {
	t.Helper()
	operation, _ := durableOperation(t, h)
	if !slices.Contains(operation.Bindings, "bind-1") || len(h.workspace.reservations) == 0 {
		t.Fatalf("the %s %s names %v beside reservations %v", operation.Verb, operation.State, operation.Bindings, h.workspace.reservations)
	}
	h.binder.lost = []string{"bind-1"}
}

// losePart keeps bind-1 listed while the version it binds lost its part file,
// which the local keyring reports as its artifact check does
// (internal/secrets/localkeyring/artifacts.go), as the command-level test over
// a real keyring observes.
func losePart(t *testing.T, h *harness) {
	t.Helper()
	deleteBinding(t, h)
	h.binder.lost = nil
	h.binder.unreadable = map[string]error{"bind-1": secretstore.Failure("store.corrupt", "referenced secret artifact is missing")}
}

// A continuation or fresh removal whose frozen binding the keyring no longer
// lists refuses before it is presented or confirmed, and one whose binding is
// listed but whose material cannot be read refuses the same way once it
// reopens it. Neither registers, runs, probes, releases a reservation or a
// binding, or writes a record, and a retry refuses identically. Restoring the
// keyring is one exit: the same verb then goes on.
func TestALostFrozenBindingRefusesEveryContinuationAndRemovalBeforeAnyEffect(t *testing.T) {
	ctx := context.Background()
	reopens := map[string][]reconciliation.Verb{
		"a failed apply":                           {reconciliation.Apply, reconciliation.Destroy},
		"a completed apply":                        {reconciliation.Destroy},
		"a failed apply whose blocks are all done": {reconciliation.Destroy},
		"an unknown destroy":                       {reconciliation.Destroy},
		"a failed destroy":                         {reconciliation.Destroy},
	}
	for state, arrange := range lostBindingStates() {
		for _, loss := range []struct {
			name     string
			lose     func(*testing.T, *harness)
			why      string
			reopened bool
		}{
			{name: "a deleted binding", lose: deleteBinding, why: "which the context's keyring no longer lists"},
			{name: "a bound version whose part is missing", lose: losePart, reopened: true,
				why: "whose material the context's keyring cannot read: referenced secret artifact is missing"},
		} {
			for _, verb := range reopens[state] {
				t.Run(state+", "+loss.name+", "+string(verb), func(t *testing.T) {
					h := arrange(t)
					loss.lose(t, h)
					operation, _ := durableOperation(t, h)
					before := untouchedOf(h)
					var first diagnostics.Diagnostic
					for attempt := 1; attempt <= 3; attempt++ {
						presented, asked := len(h.presenter.presented), h.confirmer.asked
						var err error
						if verb == reconciliation.Apply {
							_, err = h.service.Apply(ctx, ApplyRequest{ContextName: testContextName})
						} else {
							_, err = h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName})
						}
						reported := diagnostics.Of(err)
						if len(reported) != 1 {
							t.Fatalf("attempt %d: want one diagnostic, got %+v (%v)", attempt, reported, err)
						}
						before.require(t, h)
						if attempt == 1 {
							first = reported[0]
						} else if reported[0] != first {
							t.Fatalf("attempt %d refused %+v, the first %+v", attempt, reported[0], first)
						}
						reached := 0
						if loss.reopened {
							reached = 1
						}
						if len(h.presenter.presented)-presented != reached || h.confirmer.asked-asked != reached {
							t.Fatalf("attempt %d presented %d plans and asked %d times", attempt, len(h.presenter.presented)-presented, h.confirmer.asked-asked)
						}
					}
					if first.Code != "lifecycle.state" ||
						!strings.HasPrefix(first.Message, "the "+string(operation.Verb)+" "+operation.ID+" ("+string(operation.State)+") froze the Secret binding bind-1, "+loss.why+"; ") ||
						!strings.Contains(first.Message, "ArtifactServer/alpha") ||
						first.Remediation != "restore the context's keyring from a complete backup and repeat bootwright "+string(verb)+
							", or run bootwright context delete --name lab --purge --allow-orphans and then remove the objects named here by hand" {
						t.Fatalf("the refusal = %+v", first)
					}
					h.binder.lost, h.binder.unreadable = nil, nil
					h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
					result, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
					if err != nil || result.Receipt.State != "done" || len(h.workspace.reservations) != 0 || !slices.Contains(h.binder.released, "bind-1") {
						t.Fatalf("once the keyring is restored the destroy = %+v (%v), leaving %v and releasing %v", result, err, h.workspace.reservations, h.binder.released)
					}
				})
			}
		}
	}
}

// A binding the keyring listed when the verb decided and lost before the verb
// reopened it refuses at that reopen, the same way and still before any
// effect.
func TestABindingLostAfterItWasListedRefusesAtItsReopen(t *testing.T) {
	ctx := context.Background()
	h := lostBindingStates()["a failed apply"](t)
	h.service.binder = &listedThen{testBinder: h.binder, then: func() { h.binder.lost = []string{"bind-1"} }}
	before, presented := untouchedOf(h), len(h.presenter.presented)
	_, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.state" ||
		!strings.Contains(reported[0].Message, "froze the Secret binding bind-1, which the context's keyring no longer lists; ") {
		t.Fatalf("the apply = %+v (%v)", reported, err)
	}
	before.require(t, h)
	if len(h.presenter.presented) != presented+1 {
		t.Fatalf("the apply presented %d plans before its reopen", len(h.presenter.presented)-presented)
	}
}

// Only an invocation that moved the context on releases a binding its
// operation names, so a removal decided before another one completed and
// released that binding reports the change rather than a lost binding.
func TestABindingAnotherRemovalReleasedSinceTheDecisionReportsTheChange(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "alpha")
	completeApply(t, h)
	h.service.workspace = &readThen{testWorkspace: h.workspace, then: func() {
		if result, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil || result.Receipt.State != "done" {
			t.Fatalf("the other destroy = %+v (%v)", result, err)
		}
	}}
	_, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.state" ||
		!strings.HasPrefix(reported[0].Message, "the context changed after this command read it") ||
		strings.Contains(reported[0].Remediation, "--allow-orphans") {
		t.Fatalf("the destroy = %+v (%v)", reported, err)
	}
}

// A removal whose blocks are all done is finalized by its own destroy, which
// reopens no binding, so a binding the keyring no longer lists refuses nothing
// there: status names no contradiction and offers that destroy, which records
// the removal done, releases its reservation and publishes pristine evidence
// without a confirmation or a presented plan, and a repeated destroy settles.
func TestALostBindingLeavesARemovalWhoseBlocksAreAllDoneToItsDestroy(t *testing.T) {
	ctx := context.Background()
	for state, arrange := range finalizedRemovalStates() {
		t.Run(state, func(t *testing.T) {
			h := arrange(t)
			deleteBinding(t, h)
			removal := currentOperation(t, h)
			status, err := h.service.Status(ctx, StatusRequest{ContextName: testContextName})
			if err != nil || len(status.Contradictions) != 0 || !slices.Equal(status.NextSteps, []string{"bootwright destroy"}) {
				t.Fatalf("status = %+v (%v)", status, err)
			}
			h.service.options.Confirmer = nil
			destroys, observes, presented := len(h.capability.destroys), len(h.capability.observes), len(h.presenter.presented)
			result, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName})
			if err != nil || !result.Settled || result.Recovered != RecoveredFinalization || result.Receipt.Operation != removal || result.Receipt.State != "done" {
				t.Fatalf("the destroy = %+v, %+v (%v)", result, diagnostics.Of(err), err)
			}
			requireRecordedWithEveryBlockDone(t, h, reconciliation.Destroy, reconciliation.OperationDone)
			if !bytes.Equal(h.workspace.evidence, killPristine(t)) || len(h.workspace.reservations) != 0 {
				t.Fatalf("the destroy left evidence %q and reservations %v", h.workspace.evidence, h.workspace.reservations)
			}
			if len(h.capability.destroys) != destroys || len(h.capability.observes) != observes || len(h.presenter.presented) != presented {
				t.Fatal("the destroy ran or observed a block, or presented a plan")
			}
			if status, err := h.service.Status(ctx, StatusRequest{ContextName: testContextName}); err != nil ||
				len(status.Contradictions) != 0 || len(status.NextSteps) != 0 {
				t.Fatalf("status after the destroy = %+v (%v)", status, err)
			}
			if repeated, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName}); err != nil || !repeated.Settled || repeated.Recovered != "" {
				t.Fatalf("the repeated destroy = %+v (%v)", repeated, err)
			}
		})
	}
}

// A lost frozen binding has a golden of what status, a plan preview and both
// verbs report over each operation that names it. Status names the operation
// and the binding, and offers only the deletion that abandons what it owns; a
// plan previewing the verb refuses as that verb does. A binding still listed
// whose material cannot be read is found only when a verb reopens it. A
// removal whose blocks are all done is left to the destroy that finalizes it.
func TestALostFrozenBindingMatchesItsGolden(t *testing.T) {
	variants := map[string]func(*testing.T) *harness{}
	for _, states := range []map[string]func(*testing.T) *harness{lostBindingStates(), finalizedRemovalStates()} {
		for state, arrange := range states {
			variants[state+" whose binding is deleted"] = func(t *testing.T) *harness {
				h := arrange(t)
				deleteBinding(t, h)
				return h
			}
		}
	}
	variants["a failed apply whose bound version lost a part"] = func(t *testing.T) *harness {
		h := lostBindingStates()["a failed apply"](t)
		losePart(t, h)
		return h
	}
	reports := map[string]recordStateReport{}
	for variant, arrange := range variants {
		reports[variant] = reportOver(t, arrange)
	}
	data, err := json.Marshal(reports)
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "lifecycle-lost-binding", withoutIdentities(data), false)
}
