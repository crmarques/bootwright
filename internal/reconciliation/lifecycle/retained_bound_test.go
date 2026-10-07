package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/contextguard"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// The exits a refusal at the retained-operation bound names: the destroy that
// frees what the context still holds, and otherwise the deletion the guard
// admits followed by a fresh init.
const (
	destroyOwnedExit = "remove what the context owns with bootwright destroy --context lab, then repeat the apply"
	reclaimClaimExit = "reclaim the operation directories interrupted applies left with bootwright destroy --context lab, then repeat the apply"
	createdAgain     = ", then create it again with bootwright context init --name lab from its original configuration and input"
)

const interruptedClaim = "op-0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e0e"

// retainedDirectories gives the context count retained operation directories
// that no reclaim removes.
func retainedDirectories(h *harness, count int) {
	for index := range count {
		h.workspace.area.directories[fmt.Sprintf("retained-%d", index)] = true
	}
}

// boundStates arranges each state a refusal at the retained-operation bound is
// raised over, by the verb that is refused there.
func boundStates() map[string]struct {
	verb    reconciliation.Verb
	arrange func(*testing.T) *harness
} {
	return map[string]struct {
		verb    reconciliation.Verb
		arrange func(*testing.T) *harness
	}{
		"an apply beside pristine evidence": {reconciliation.Apply, func(t *testing.T) *harness {
			h := newHarness(t, "alpha")
			retainedDirectories(h, operationstore.MaxOperations-1)
			return h
		}},
		"an apply whose operation area holds more than 48 MiB": {reconciliation.Apply, func(t *testing.T) *harness {
			h := newHarness(t, "alpha")
			h.workspace.area.files["filled/output"] = make([]byte, operationstore.MaxBytes-operationstore.ReservedBytes+1)
			return h
		}},
		"an apply beside the running evidence an interrupted registration left": {reconciliation.Apply, func(t *testing.T) *harness {
			h := newHarness(t, "alpha")
			h.workspace.evidence = evidenceBytes(t, reconciliation.Apply, reconciliation.OperationRunning)
			h.workspace.reservations = reservationOf("alpha")
			retainedDirectories(h, operationstore.MaxOperations-1)
			return h
		}},
		"an apply beside a claim an interrupted apply left": {reconciliation.Apply, func(t *testing.T) *harness {
			h := newHarness(t, "alpha")
			retainedDirectories(h, operationstore.MaxOperations-2)
			if err := operationstore.New(h.workspace.area, killClock).Claim(context.Background(), interruptedClaim, reconciliation.Plan{}); err != nil {
				t.Fatal(err)
			}
			return h
		}},
		"a removal the operation area cannot hold": {reconciliation.Destroy, func(t *testing.T) *harness {
			h := newHarness(t, "alpha")
			if err := killInvoke(context.Background(), h.service, reconciliation.Apply); err != nil {
				t.Fatal(err)
			}
			plan, err := reconciliation.NewPlan(reconciliation.Apply, []reconciliation.BlockDefinition{definition("alpha")})
			if err != nil {
				t.Fatal(err)
			}
			removal, err := plan.Inverse()
			if err != nil {
				t.Fatal(err)
			}
			for index := range operationstore.MaxEntries - areaEntries(h.workspace.area, "") - operationstore.AdmissionEntries(removal) {
				h.workspace.area.files[fmt.Sprintf("filled/record-%d", index)] = []byte("{}\n")
			}
			return h
		}},
	}
}

// refusedAtTheBound runs verb over what the harness holds and requires it to
// refuse at the retained-operation bound with exactly remedy, leaving the
// records, directories, evidence, reservations, bindings and effects as they
// were. A removal probes its targets' quiescence before it registers, which
// only reads.
func refusedAtTheBound(t *testing.T, h *harness, verb reconciliation.Verb, remedy string) {
	t.Helper()
	held := func() string {
		_, directories := operations(t, h.workspace)
		return fmt.Sprint("directories ", directories, ", evidence ", string(h.workspace.evidence), ", reservations ", h.workspace.reservations,
			", issued ", h.binder.issued, ", released ", h.binder.released, ", applies ", h.capability.applies, ", destroys ", h.capability.destroys)
	}
	records, before := h.workspace.area.clone(), held()
	err := killInvoke(context.Background(), h.service, verb)
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.state" ||
		!strings.Contains(reported[0].Message, "the context has retained the maximum number of lifecycle operations") || reported[0].Remediation != remedy {
		t.Fatalf("the %s = %+v (%v), want the retained-operation refusal naming %q", verb, reported, err, remedy)
	}
	if after := held(); !sameFiles(h.workspace.area, records) || after != before {
		t.Fatalf("the refused %s left %s, was %s", verb, after, before)
	}
}

// The refusal at the retained-operation bound names the exits that exist,
// which no command adds to by pruning: beside evidence the guard does not read
// as pristine, the destroy that removes what that evidence protects; beside a
// claim that holds nothing, the destroy that reclaims it; and otherwise, a
// removal's included, the deletion the guard admits followed by a fresh init.
func TestTheRetainedOperationRefusalsMatchTheirGolden(t *testing.T) {
	reports := map[string]verbReport{}
	for name, refused := range boundStates() {
		h := refused.arrange(t)
		reports[name] = refusalReport(t, killInvoke(context.Background(), h.service, refused.verb))
	}
	data, err := json.Marshal(reports)
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "lifecycle-retained-maximum", withoutIdentities(data), false)
}

// Over evidence the guard cannot read only the orphan acknowledgement deletes
// the context, so the exit of a refusal at the bound over it names, for
// either verb, that deletion, which cannot list what it abandons, and the
// init after it, never a destroy.
func TestTheRetainedOperationExitOverUnreadableEvidenceNamesTheAcknowledgedDeletion(t *testing.T) {
	h := newHarness(t, "alpha")
	h.workspace.evidence = []byte(`{`)
	view := h.workspace.view()
	want := unlistedDeletion + ", then create it again with bootwright context init --name lab from its original configuration and input"
	for _, verb := range []reconciliation.Verb{reconciliation.Apply, reconciliation.Destroy} {
		if exit := retainedExit(context.Background(), view, h.service.store(view), verb); exit != want {
			t.Fatalf("the %s exit over unreadable evidence is %q, want %q", verb, exit, want)
		}
	}
}

// recreate is what bootwright context delete --purge, with or without
// --allow-orphans, and the context init after it leave for this context: its
// operation and run areas go with it, the deletion releases its reservations
// and drops its keyring, and the new context starts from pristine evidence.
// The contexts package owns both commands; this holds what the lifecycle then
// finds.
func recreate(t *testing.T, h *harness) {
	t.Helper()
	h.workspace.area, h.workspace.runArea = newArea(), newArea()
	h.workspace.evidence, h.workspace.reservations = killPristine(t), nil
	for issued := 1; issued <= h.binder.issued; issued++ {
		h.binder.lost = append(h.binder.lost, fmt.Sprintf("bind-%d", issued))
	}
}

// requireDeletionAdmitted holds the deletion a refusal named to the one the
// context guard admits over the evidence the refusal left: a plain deletion
// only over evidence it reads as pristine.
func requireDeletionAdmitted(t *testing.T, h *harness, remedy string) {
	t.Helper()
	disposition, err := (contextguard.Guard{}).Check(context.Background(), h.workspace.evidence)
	if err != nil {
		t.Fatal(err)
	}
	if plain := strings.HasPrefix(remedy, plainDeletion+","); plain != disposition.Dispose || !plain && !strings.HasPrefix(remedy, orphanedDeletion+",") {
		t.Fatalf("the remedy %q names a deletion the guard, granting %+v, does not admit", remedy, disposition)
	}
}

// requireApplied runs a fresh apply and requires it to complete.
func requireApplied(t *testing.T, h *harness) {
	t.Helper()
	result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
	if err != nil || result.Receipt.State != string(reconciliation.OperationDone) || result.Settled {
		t.Fatalf("the apply after the exit = %+v (%v: %+v)", result, err, diagnostics.Of(err))
	}
}

// Each exit the refusal at the retained-operation bound names leads out of it
// when followed exactly. Beside the running evidence an interrupted
// registration left, the destroy releases it and publishes pristine evidence;
// the repeated apply still refuses at the bound, now naming the plain
// deletion the guard admits, and the context created again applies. Beside a
// claim an interrupted apply left, the destroy reclaims it and the repeated
// apply completes in the room it freed. A removal the area cannot hold names
// the deletion that abandons what the context owns, which the guard admits
// only with that acknowledgement, and the context created again applies.
func TestEachRetainedOperationExitLeadsOutOfTheBound(t *testing.T) {
	ctx := context.Background()
	cases := boundStates()
	t.Run("the destroy beside running evidence, then the deletion", func(t *testing.T) {
		h := cases["an apply beside the running evidence an interrupted registration left"].arrange(t)
		refusedAtTheBound(t, h, reconciliation.Apply, destroyOwnedExit)
		result, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
		if err != nil || !result.Settled || result.Recovered != RecoveredRelease || len(h.workspace.reservations) != 0 {
			t.Fatalf("the destroy the refusal named = %+v (%v), leaving %v", result, err, h.workspace.reservations)
		}
		remedy := plainDeletion + createdAgain
		refusedAtTheBound(t, h, reconciliation.Apply, remedy)
		requireDeletionAdmitted(t, h, remedy)
		recreate(t, h)
		requireApplied(t, h)
	})
	t.Run("the destroy that reclaims an interrupted claim", func(t *testing.T) {
		h := cases["an apply beside a claim an interrupted apply left"].arrange(t)
		refusedAtTheBound(t, h, reconciliation.Apply, reclaimClaimExit)
		result, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
		if err != nil || !result.Settled {
			t.Fatalf("the destroy the refusal named = %+v (%v)", result, err)
		}
		if _, directories := operations(t, h.workspace); slices.Contains(directories, interruptedClaim) || len(directories) != operationstore.MaxOperations-2 {
			t.Fatalf("the destroy left %d directories, the claim among them: %t", len(directories), slices.Contains(directories, interruptedClaim))
		}
		requireApplied(t, h)
	})
	t.Run("the deletion that abandons what a removal could not take back", func(t *testing.T) {
		h := cases["a removal the operation area cannot hold"].arrange(t)
		remedy := orphanedDeletion + createdAgain
		refusedAtTheBound(t, h, reconciliation.Destroy, remedy)
		requireDeletionAdmitted(t, h, remedy)
		recreate(t, h)
		requireApplied(t, h)
	})
}
