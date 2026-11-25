package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
)

// unfinishedStagedDestroys leave a staged destroy from the clusters stage in
// each state a widening replaces.
func unfinishedStagedDestroys() map[string]func(*testing.T, *harness) {
	rewritten := func(state reconciliation.OperationState) func(*testing.T, *harness) {
		return func(t *testing.T, h *harness) {
			failedStagedRemoval(t, h)
			rewriteState(t, h, path.Join(currentOperation(t, h), "operation.json"), string(state))
		}
	}
	return map[string]func(*testing.T, *harness){
		"failed":  failedStagedRemoval,
		"running": rewritten(reconciliation.OperationRunning),
		"paused":  rewritten(reconciliation.OperationPaused),
		"unknown": func(t *testing.T, h *harness) {
			completeApply(t, h)
			h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
			if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true}); err == nil {
				t.Fatal("a staged destroy whose block lost its outcome reported success")
			}
			h.capability.outcomes = nil
		},
	}
}

// A destroy without --stage over an unfinished staged destroy, whatever its
// state, replaces it with a whole removal of what the context still owns,
// naming it by replaces, resolving its unproved block first under a remedy
// that repeats the widening, and ends pristine with every binding released.
func TestADestroyWithoutStageWidensAnUnfinishedStagedDestroy(t *testing.T) {
	t.Parallel()
	for name, arrange := range unfinishedStagedDestroys() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			h := newPlannedHarness(t, chainedDefinitions())
			arrange(t, h)
			staged, _ := currentRecord(t, h)
			if name == "unknown" {
				_, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
				if reported := diagnostics.Of(err); firstCode(err) != engineOutcomeUnknown.Code || !strings.HasSuffix(reported[0].Remediation, "repeat bootwright destroy --context "+testContextName+" to observe it again") {
					t.Fatalf("a widening whose resolution proved nothing returned %+v", reported)
				}
				h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
			}
			if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
				t.Fatalf("the widening destroy returned %+v", diagnostics.Of(err))
			}
			whole, plan := currentRecord(t, h)
			if whole.Stage != nil || whole.Replaces != staged.ID || whole.Source != staged.Source || whole.State != reconciliation.OperationDone ||
				!slices.Equal(planIDs(plan), []string{"charlie", "bravo", "alpha"}) {
				t.Fatalf("the widening destroy recorded %+v over %v", whole, planIDs(plan))
			}
			pristine, err := projection(reconciliation.Destroy, reconciliation.OperationDone)
			if err != nil || !bytes.Equal(h.workspace.evidence, pristine) || !slices.Equal(h.binder.released, []string{"bind-1"}) {
				t.Fatalf("the widening destroy left the evidence %s and released %v (%v)", h.workspace.evidence, h.binder.released, err)
			}
			if resolved := slices.Contains(h.capability.observes, "charlie"); resolved != (name == "unknown") {
				t.Fatalf("the widening destroy observed %v", h.capability.observes)
			}
		})
	}
}

// A widening takes back only what the staged destroy has not proved gone: a
// step it removed is in no later plan, and the lower floor's staged destroy
// freezes the cumulative taken-back set and rests trimmed.
func TestALowerFloorWidensAnUnfinishedStagedDestroyCumulatively(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := stagedHarness(t)
	completeApply(t, h)
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeChanged}, {Outcome: reconciliation.OutcomeFailed}}
	if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageMachines), SkipConfirmation: true}); err == nil {
		t.Fatal("a staged destroy whose block failed reported success")
	}
	h.capability.outcomes = nil
	failed, _ := currentRecord(t, h)
	stagedDestroy(t, h, reconciliation.StageSubstrates)
	lower, plan := currentRecord(t, h)
	if !trimmed(lower) || lower.Replaces != failed.ID || lower.Stage.Floor != reconciliation.StageSubstrates ||
		!slices.Equal(lower.Stage.TakenBack, []string{"addon", "host", "installation", "machine"}) || !slices.Equal(planIDs(plan), []string{"installation", "machine", "host"}) {
		t.Fatalf("the lower floor recorded %+v over %v", lower, planIDs(plan))
	}
	if len(h.binder.released) != 0 || !bytes.Equal(h.workspace.evidence, trimmedEvidence(t)) {
		t.Fatalf("the lower floor released %v and left the evidence %s", h.binder.released, h.workspace.evidence)
	}
}

// A later floor cannot narrow an unfinished staged destroy: it refuses before
// anything registers and names the continuation and the whole destroy.
func TestALaterFloorOverAnUnfinishedStagedDestroyRefuses(t *testing.T) {
	t.Parallel()
	h := newPlannedHarness(t, chainedDefinitions())
	failedStagedRemoval(t, h)
	before := untouchedOf(h)
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageAddOns), SkipConfirmation: true})
	reported := diagnostics.Of(err)
	if firstCode(err) != destroyStageChanged.Code || !strings.Contains(reported[0].Remediation, "destroy --context "+testContextName+" --stage clusters") ||
		!strings.HasSuffix(reported[0].Remediation, "review taking back the whole context with bootwright destroy --dry-run --context "+testContextName+" and run bootwright destroy --context "+testContextName) {
		t.Fatalf("a later floor returned %v %+v", err, reported)
	}
	before.require(t, h)
}

type recordingConfirmer struct{ actions []string }

func (c *recordingConfirmer) Confirm(_ context.Context, action, _ string) error {
	c.actions = append(c.actions, action)
	return nil
}

// The widening is named where the operator reviews it: the preview carries the
// staged destroy and its floor, the confirmation states it, and the data-loss
// digest is the widened plan's own, so the staged destroy's digest is stale and
// an unattended run without the widened digest refuses.
func TestAWideningIsNamedAndAuthorizedAgainstItsOwnPlan(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newPlannedHarness(t, chainedDefinitions())
	h.capability.consumes = map[string][]string{"charlie": {reconciliation.AuthorizationDataLoss}, "alpha": {reconciliation.AuthorizationDataLoss}}
	completeApply(t, h)
	stagedPreview, err := h.service.Plan(ctx, PlanRequest{ContextName: testContextName, Verb: reconciliation.Destroy, Floor: string(reconciliation.StageClusters)})
	if err != nil {
		t.Fatal(err)
	}
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeFailed}}
	if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), Authorizations: stagedPreview.Authorizations, SkipConfirmation: true, Unattended: true}); err == nil {
		t.Fatal("a staged destroy whose block failed reported success")
	}
	h.capability.outcomes = nil
	staged, _ := currentRecord(t, h)
	preview, err := h.service.Plan(ctx, PlanRequest{ContextName: testContextName, Verb: reconciliation.Destroy})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Widens == nil || *preview.Widens != (Widening{Operation: staged.ID, From: string(reconciliation.StageClusters)}) || preview.Scope != nil {
		t.Fatalf("the widening preview reads %+v beside the scope %+v", preview.Widens, preview.Scope)
	}
	if len(preview.Authorizations) != 1 || slices.Equal(preview.Authorizations, stagedPreview.Authorizations) {
		t.Fatalf("the widening requires %v, the staged destroy %v", preview.Authorizations, stagedPreview.Authorizations)
	}
	for name, request := range map[string]DestroyRequest{
		"the staged destroy's digest": {Authorizations: stagedPreview.Authorizations, SkipConfirmation: true, Unattended: true},
		"no digest under --yes":       {Authorizations: []string{reconciliation.AuthorizationDataLoss}, SkipConfirmation: true, Unattended: true},
	} {
		request.ContextName = testContextName
		if _, err := h.service.Destroy(ctx, request); firstCode(err) != authorizeStale.Code {
			t.Fatalf("a widening authorized by %s returned %v", name, err)
		}
	}
	if current, _ := currentRecord(t, h); current.ID != staged.ID {
		t.Fatalf("a refused widening registered %s", current.ID)
	}
	confirmer := &recordingConfirmer{}
	h.service.options.Confirmer = confirmer
	if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, Authorizations: preview.Authorizations}); err != nil {
		t.Fatalf("the widening under its own digest returned %+v", diagnostics.Of(err))
	}
	want := "destroy, which widens the unfinished staged destroy " + staged.ID + " from the clusters stage to the whole context,"
	if !slices.Equal(confirmer.actions, []string{want}) {
		t.Fatalf("the widening confirmed %q, want %q", confirmer.actions, want)
	}
}

// A lower floor's confirmation states its own scope and the widening.
func TestALowerFloorConfirmationNamesTheWidening(t *testing.T) {
	t.Parallel()
	decided := operationPlan{
		verb: reconciliation.Destroy, plan: reconciliation.Plan{Blocks: make([]reconciliation.Block, 2)}, kept: []string{"alpha"},
		stage: &operationstore.Stage{Floor: reconciliation.StageSubstrates}, widens: &widening{operation: "op-1", from: reconciliation.StageMachines},
	}
	want := "destroy --stage substrates, taking back 2 steps and keeping 1, which widens the unfinished staged destroy op-1 from the machines stage to the substrates stage,"
	if got := confirmedAction(decided); got != want {
		t.Fatalf("the lower floor confirms %q, want %q", got, want)
	}
}

// failedStagedRetiringRemoval leaves a failed staged destroy from the machines
// stage whose retired storage block is unproved, so a widening resolves that
// block first.
func failedStagedRetiringRemoval(t *testing.T) (*harness, *retiringTestCapability, operationstore.Operation) {
	t.Helper()
	h, c := retiringHarness(t, hostedDefinitions(), []string{"rhel-01"})
	h.service.capabilities = boundResolver{
		{Kind: "ArtifactServer", Implementation: "artifact-server-nginx-v1"}: c,
		{Kind: "Machine", Implementation: "artifact-server-nginx-v1"}:        deletingMachine{machineBinding{c}, map[string]bool{"machine": true}},
	}
	completeApply(t, h)
	h.service.options.Concurrency = 1
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeChanged}, {Outcome: reconciliation.OutcomeFailed}}
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageMachines), SkipConfirmation: true})
	requireFired(t, err)
	staged, _ := currentRecord(t, h)
	leaveRunning(t, h, "storage")
	if frozen := leavesWith(frozenPlan(t, h, staged.ID)); !slices.Equal(frozen["storage"], []string{"machine"}) {
		t.Fatalf("the staged destroy froze leaves-with %v", frozen)
	}
	h.capability.outcomes = nil
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
	return h, c, staged
}

// A widening resolves the staged destroy's unproved block with the phases that
// destroy froze, not the widened plan's.
func TestAWideningObservesTheStagedDestroyWithItsOwnPhases(t *testing.T) {
	t.Parallel()
	h, _, _ := failedStagedRetiringRemoval(t)
	before := len(h.capability.executions)
	if result, err := destroy(h); err != nil || result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("widening = %+v (%v)", result, diagnostics.Of(err))
	}
	if !slices.Contains(h.capability.calls, "observe-removal:storage") {
		t.Fatalf("the staged destroy's storage was never observed: %v", h.capability.calls)
	}
	for _, execution := range h.capability.executions[before:] {
		if execution.Block.ID != "storage" {
			continue
		}
		if !execution.RetiredWithHosts {
			t.Fatal("the staged destroy's storage was observed without the leaves-with answer its plan froze")
		}
		return
	}
	t.Fatal("no storage execution followed the widening")
}

// A widening refuses before resolving anything when the rule its staged
// destroy froze has moved, whichever way it widens, and leaves that staged
// destroy current, named by the remedy.
func TestAWideningRefusesBeforeResolvingAStagedDestroyWhoseRuleMoved(t *testing.T) {
	t.Parallel()
	for name, floor := range map[string]reconciliation.Stage{"a destroy without --stage": "", "a lower floor": reconciliation.StageSubstrates} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h, c, staged := failedStagedRetiringRemoval(t)
			c.removes["machine"] = false
			calls := len(h.capability.calls)
			_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(floor), SkipConfirmation: true})
			if firstCode(err) != "destroy.inverse-moved" || len(h.capability.calls) != calls {
				t.Fatalf("a widening over a moved rule = %v after calls %v", diagnostics.Of(err), h.capability.calls[calls:])
			}
			if !strings.Contains(diagnostics.Of(err)[0].Remediation, staged.ID) {
				t.Fatalf("refusal = %+v, want the staged destroy named", diagnostics.Of(err)[0])
			}
			if current, _ := currentRecord(t, h); current.ID != staged.ID {
				t.Fatalf("the refusal registered %s", current.ID)
			}
		})
	}
}

// A widening refuses before resolving anything when the records that proved a
// step has nothing to take back no longer prove it.
func TestAWideningRefusesBeforeResolvingAStagedDestroyWhoseRecordsMoved(t *testing.T) {
	t.Parallel()
	h := newPlannedHarness(t, chainedDefinitions())
	completeApply(t, h)
	recorded := &recordedCapability{testCapability: h.capability, nothing: map[string]bool{"charlie": true}}
	h.service.capabilities = boundResolver{{Kind: "ArtifactServer", Implementation: "artifact-server-nginx-v1"}: recorded}
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true})
	requireFired(t, err)
	h.capability.outcomes = nil
	staged, plan := currentRecord(t, h)
	if charlie, _ := plan.Block("charlie"); !TakesNothingBack(charlie) {
		t.Fatalf("the staged destroy froze %+v", charlie.BlockDefinition)
	}
	recorded.nothing = map[string]bool{}
	calls := len(h.capability.calls)
	_, err = destroy(h)
	if firstCode(err) != "destroy.inverse-moved" || len(h.capability.calls) != calls {
		t.Fatalf("a widening over moved records = %v after calls %v", diagnostics.Of(err), h.capability.calls[calls:])
	}
	if !strings.Contains(diagnostics.Of(err)[0].Remediation, staged.ID) {
		t.Fatalf("refusal = %+v, want the staged destroy named", diagnostics.Of(err)[0])
	}
	if current, _ := currentRecord(t, h); current.ID != staged.ID {
		t.Fatalf("the refusal registered %s", current.ID)
	}
}

// A widening removes each step once: what the staged destroy proved gone is in
// neither the widened plan nor any later removal.
func TestAWideningDoesNotRemoveAStepTheStagedDestroyRemoved(t *testing.T) {
	t.Parallel()
	h := stagedHarness(t)
	completeApply(t, h)
	h.service.options.Concurrency = 1
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeChanged}, {Outcome: reconciliation.OutcomeFailed}}
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageMachines), SkipConfirmation: true}); err == nil {
		t.Fatal("a staged destroy whose second block failed reported success")
	}
	h.capability.outcomes = nil
	if !slices.Equal(h.capability.destroys, []string{"addon", "installation"}) {
		t.Fatalf("the staged destroy removed %v", h.capability.destroys)
	}
	h.capability.destroys = nil
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatalf("the widening destroy returned %+v", diagnostics.Of(err))
	}
	_, plan := currentRecord(t, h)
	if want := []string{"installation", "machine", "host", "alpha"}; !slices.Equal(planIDs(plan), want) || !slices.Equal(h.capability.destroys, want) {
		t.Fatalf("the widening planned %v and removed %v, want %v without the add-on the staged destroy removed", planIDs(plan), h.capability.destroys, want)
	}
}

// A widening over a staged destroy that rebound the credential its steps
// present releases that destroy's binding beside the lineage's, and its host
// reservations.
func TestAWideningReleasesTheStagedDestroysBindingBesideTheLineages(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	base, cluster := stagedDefinition("base", reconciliation.StageInfrastructureServices), stagedDefinition("cluster", reconciliation.StageClusters, "base")
	base.Request = json.RawMessage(`{"credentialsRef":"bmc","name":"base"}`)
	cluster.Request = json.RawMessage(`{"credentialsRef":"bmc","name":"cluster"}`)
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{base, cluster})
	h.capability.reservations = reservationOf("base")
	h.capability.secrets = []string{"bmc"}
	h.capability.access = map[string][]string{"base": {"bmc"}, "cluster": {"bmc"}}
	h.binder.material = map[string]secrets.Material{"bmc": password("first")}
	h.binder.versions = map[string]string{"bmc": "ver-1"}
	h.binder.versionMaterial = map[string]secrets.Material{"ver-1": password("first"), "ver-2": password("second")}
	completeApply(t, h)
	applied, _ := durableOperation(t, h)
	h.binder.versions["bmc"] = "ver-2"
	presentedTo(h)
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeFailed}}
	if _, err := h.service.Destroy(ctx, DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), Authorizations: []string{RebindAuthorization + "bmc"}, SkipConfirmation: true}); err == nil {
		t.Fatal("a staged destroy whose block failed reported success")
	}
	h.capability.outcomes = nil
	staged, _ := currentRecord(t, h)
	if len(staged.Bindings) != 1 || len(applied.Bindings) != 1 || staged.Bindings[0] == applied.Bindings[0] || len(h.workspace.reservations) == 0 || len(h.binder.released) != 0 {
		t.Fatalf("the staged destroy holds %v beside the lineage's %v, with reservations %v and released %v", staged.Bindings, applied.Bindings, h.workspace.reservations, h.binder.released)
	}
	if err := destroyWith(h); err != nil {
		t.Fatalf("the widening destroy returned %+v", diagnostics.Of(err))
	}
	if released := slices.Sorted(slices.Values(h.binder.released)); !slices.Equal(released, slices.Sorted(slices.Values(slices.Concat(applied.Bindings, staged.Bindings)))) || len(h.workspace.reservations) != 0 {
		t.Fatalf("the widening released %v and left reservations %v, want %v and %v", h.binder.released, h.workspace.reservations, applied.Bindings, staged.Bindings)
	}
}

// A widening over a restore lineage, whatever state its restoring apply
// stopped in, removes every live step exactly once, leaving out a step an
// earlier staged destroy took back and the restore never re-created or the
// staged destroy over the restore removed, and ends pristine.
func TestAWideningOverARestoreRemovesEachStepOnce(t *testing.T) {
	t.Parallel()
	for name, arrange := range map[string]func(*testing.T, *harness){
		"failed": failedRestore, "running": runningRestore, "paused": pausedRestore, "unknown": unknownRestore,
		"partly removed": func(t *testing.T, h *harness) {
			h.capability.definitions = append(chainedDefinitions(), stagedDefinition("delta", reconciliation.StageAddOns, "charlie"))
			completeApply(t, h)
			stagedDestroy(t, h, reconciliation.StageClusters)
			applyChained(t, h, map[string]Result{"delta": {Outcome: reconciliation.OutcomeFailed}})
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			removed := []string{"charlie", "bravo", "alpha"}
			h := newPlannedHarness(t, chainedDefinitions())
			arrange(t, h)
			h.capability.observations = slices.Repeat([]Observation{{Effect: reconciliation.EffectNoEffect}}, 4)
			h.service.options.Concurrency = 1
			h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeFailed}}
			if name == "partly removed" {
				h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeChanged}, {Outcome: reconciliation.OutcomeFailed}}
			}
			if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageClusters), SkipConfirmation: true}); err == nil {
				t.Fatal("a staged destroy whose block failed reported success")
			}
			h.capability.outcomes = nil
			staged, _ := currentRecord(t, h)
			h.capability.destroys = nil
			if err := destroyWith(h); err != nil {
				t.Fatalf("the widening destroy returned %+v", diagnostics.Of(err))
			}
			whole, _ := currentRecord(t, h)
			pristine, err := projection(reconciliation.Destroy, reconciliation.OperationDone)
			if whole.Replaces != staged.ID || !slices.Equal(h.capability.destroys, removed) || err != nil || !bytes.Equal(h.workspace.evidence, pristine) {
				t.Fatalf("the widening replaced %q, removed %v and left the evidence %s (%v), want %v", whole.Replaces, h.capability.destroys, h.workspace.evidence, err, removed)
			}
		})
	}
}

// A lower floor's widening, when its resolution proves nothing, repeats itself
// at that floor.
func TestALowerFloorWideningRepeatsItsFloorWhenResolutionProvesNothing(t *testing.T) {
	t.Parallel()
	h := newPlannedHarness(t, chainedDefinitions())
	unfinishedStagedDestroys()["unknown"](t, h)
	staged, _ := currentRecord(t, h)
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(reconciliation.StageMachines), SkipConfirmation: true})
	reported := diagnostics.Of(err)
	if firstCode(err) != engineOutcomeUnknown.Code || !strings.HasSuffix(reported[0].Remediation, "repeat bootwright destroy --context "+testContextName+" --stage machines to observe it again") {
		t.Fatalf("a lower floor's widening whose resolution proved nothing returned %+v", reported)
	}
	if current, _ := currentRecord(t, h); current.ID != staged.ID {
		t.Fatalf("the unresolved widening registered %s", current.ID)
	}
}
