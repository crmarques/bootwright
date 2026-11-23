package lifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// relaxingTestCapability relaxes the removal of the blocks it names, as the
// physical erase relaxes the removal of the Machine its block names, and
// refuses the methods refused names.
type relaxingTestCapability struct {
	*testCapability
	relaxable map[string]bool
	refused   map[string]error
	relaxed   []string
}

func (c *relaxingTestCapability) RelaxesMachine(block reconciliation.Block) bool {
	return c.relaxable[block.ID]
}

func (c *relaxingTestCapability) Relax(_ context.Context, block reconciliation.Block, method string) (Removal, error) {
	c.record(&c.relaxed, block.ID+":"+method+":"+block.Relaxation)
	if err := c.refused[method]; err != nil {
		return Removal{}, err
	}
	return Removal{
		Description: "remove " + block.Object + " relaxed by --authorize erase=" + block.Object + ":" + method,
		Impacts:     []string{method + "-" + block.Object},
		Groups:      []reconciliation.Group{{ID: "relaxed", Description: "remove it relaxed", Machines: []string{block.Object}}},
	}, nil
}

func relaxingHarness(t *testing.T) (*harness, *relaxingTestCapability) {
	t.Helper()
	h := newPlannedHarness(t, chainedDefinitions())
	relaxer := &relaxingTestCapability{testCapability: h.capability, relaxable: map[string]bool{"bravo": true}}
	h.service.capabilities = testResolver{capability: relaxer}
	h.capability.consumes = map[string][]string{"bravo": {reconciliation.AuthorizationDataLoss}}
	return h, relaxer
}

func destroyRelaxed(h *harness, relaxations map[string]string, authorizations ...string) (*OperationResult, error) {
	return h.service.Destroy(context.Background(), DestroyRequest{
		ContextName: testContextName, SkipConfirmation: true, Relaxations: relaxations, Authorizations: authorizations,
	})
}

func currentPlan(t *testing.T, h *harness) reconciliation.Plan {
	t.Helper()
	ctx := context.Background()
	var plan reconciliation.Plan
	err := h.workspace.ReadLifecycle(ctx, testContextName, func(view View) error {
		store := h.service.store(view)
		index, err := store.Index(ctx)
		if err != nil {
			return err
		}
		plan, err = store.ReadPlan(ctx, index.Current)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func readOperation(t *testing.T, h *harness, id string) operationstore.Operation {
	t.Helper()
	ctx := context.Background()
	var operation operationstore.Operation
	err := h.workspace.ReadLifecycle(ctx, testContextName, func(view View) error {
		var err error
		operation, err = h.service.store(view).ReadOperation(ctx, id)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return operation
}

func relaxationOf(t *testing.T, plan reconciliation.Plan, id string) string {
	t.Helper()
	block, found := plan.Block(id)
	if !found {
		t.Fatalf("the plan holds no block %s", id)
	}
	return block.Relaxation
}

func destroyedRelaxations(h *harness) map[string]string {
	h.capability.mutex.Lock()
	defer h.capability.mutex.Unlock()
	seen := map[string]string{}
	for _, execution := range h.capability.executions {
		seen[execution.Block.ID] = execution.Block.Relaxation
	}
	return seen
}

func previewDigest(t *testing.T, result *PlanResult) string {
	t.Helper()
	for _, token := range result.Authorizations {
		if digest, found := strings.CutPrefix(token, reconciliation.AuthorizationDataLoss+"="); found {
			return digest
		}
	}
	t.Fatalf("the preview names no data-loss digest: %v", result.Authorizations)
	return ""
}

func TestAnErasePreviewShowsTheRelaxedRemovalAndTheDigestThatBindsIt(t *testing.T) {
	t.Parallel()
	h, _ := relaxingHarness(t)
	completeApply(t, h)
	ctx := context.Background()
	declared, err := h.service.Plan(ctx, PlanRequest{ContextName: testContextName})
	if err != nil {
		t.Fatal(err)
	}
	relaxed, err := h.service.Plan(ctx, PlanRequest{ContextName: testContextName, Relaxations: map[string]string{"bravo": EraseRetain}})
	if err != nil {
		t.Fatal(err)
	}
	if relaxed.Verb != string(reconciliation.Destroy) || relaxed.Receipt.Next != string(reconciliation.Destroy) {
		t.Fatalf("the relaxed preview is of the %s, next %q", relaxed.Verb, relaxed.Receipt.Next)
	}
	index := slices.IndexFunc(relaxed.Steps, func(step PlanStep) bool { return step.ID == "bravo" })
	step := relaxed.Steps[index]
	if step.Description != "remove bravo relaxed by --authorize erase=bravo:retain" || !slices.Equal(step.Impacts, []string{"retain-bravo"}) ||
		!slices.Equal(step.Consumes, []string{reconciliation.AuthorizationDataLoss}) {
		t.Fatalf("the relaxed step is %+v", step)
	}
	if previewDigest(t, relaxed) == previewDigest(t, declared) {
		t.Fatal("the data-loss digest does not cover the relaxation")
	}
	result, err := destroyRelaxed(h, map[string]string{"bravo": EraseRetain}, reconciliation.AuthorizationDataLoss+"="+previewDigest(t, declared))
	if !authorizationRefusal(firstCode(err)) || result != nil {
		t.Fatalf("a destroy bound to the declared removal's digest ran relaxed: %v", err)
	}
	if _, err := destroyRelaxed(h, map[string]string{"bravo": EraseRetain}, reconciliation.AuthorizationDataLoss+"="+previewDigest(t, relaxed)); err != nil {
		t.Fatalf("the destroy the preview bound refused: %v", err)
	}
	if seen := destroyedRelaxations(h); seen["bravo"] != EraseRetain || seen["alpha"] != "" || seen["charlie"] != "" {
		t.Fatalf("the removal executed relaxations %v", seen)
	}
}

func TestTheDigestCoversARelaxationAndLeavesAnUnrelaxedOneUnchanged(t *testing.T) {
	t.Parallel()
	block := reconciliation.Block{
		BlockDefinition: reconciliation.BlockDefinition{
			ID: "erase-node", Kind: "ManagedOSInstallation", Object: "node", Implementation: "physical-erase-v1",
			ContentDigest: strings.Repeat("c", 64), Consumes: []string{reconciliation.AuthorizationDataLoss},
		},
		RequestDigest: strings.Repeat("d", 64),
	}
	plan := reconciliation.Plan{Verb: reconciliation.Destroy, Blocks: []reconciliation.Block{block}}
	sum := sha256.Sum256([]byte("bootwright.authorization-v1\x00data-loss\x00erase-node\x00ManagedOSInstallation\x00node\x00physical-erase-v1\x00" +
		strings.Repeat("c", 64) + "\x00" + strings.Repeat("d", 64)))
	unrelaxed := AuthorizationDigest(plan, reconciliation.AuthorizationDataLoss)
	if unrelaxed != hex.EncodeToString(sum[:])[:authorizationDigestLength] {
		t.Fatalf("an unrelaxed digest moved to %s", unrelaxed)
	}
	digests := map[string]bool{unrelaxed: true}
	for _, method := range []string{EraseMetadataClear, EraseRetain} {
		plan.Blocks[0].Relaxation = method
		digests[AuthorizationDigest(plan, reconciliation.AuthorizationDataLoss)] = true
	}
	if len(digests) != 3 {
		t.Fatalf("the digest does not tell the relaxations apart: %v", digests)
	}
}

func TestAnInapplicableRelaxationRefusesBeforeAnythingRuns(t *testing.T) {
	t.Parallel()
	refusal := failure("erase.relaxation-inapplicable", "Machine/bravo already clears only metadata on every target", "repeat the command without --authorize erase=bravo:metadataClear")
	for name, test := range map[string]struct {
		relaxations     map[string]string
		message, remedy string
	}{
		"a Machine the plan does not hold": {
			relaxations: map[string]string{"bravo": EraseRetain, "zulu": EraseRetain},
			message:     "--authorize erase=zulu:retain names Machine/zulu, which declares no spec.hardware.rootDisk.onRemoval or spec.hardware.dataDisks[].onRemoval this destroy performs, so the relaxation changes nothing",
			remedy:      "repeat bootwright destroy --context lab --authorize erase=bravo:retain without --authorize erase=zulu:retain",
		},
		"a block whose capability relaxes nothing": {
			relaxations: map[string]string{"alpha": EraseMetadataClear},
			message:     "--authorize erase=alpha:metadataClear names Machine/alpha, which declares no spec.hardware.rootDisk.onRemoval or spec.hardware.dataDisks[].onRemoval this destroy performs, so the relaxation changes nothing",
			remedy:      "repeat bootwright destroy --context lab without --authorize erase=alpha:metadataClear",
		},
		"a method the capability refuses": {
			relaxations: map[string]string{"bravo": EraseMetadataClear},
			message:     "Machine/bravo already clears only metadata on every target",
			remedy:      "repeat the command without --authorize erase=bravo:metadataClear",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h, relaxer := relaxingHarness(t)
			relaxer.refused = map[string]error{EraseMetadataClear: refusal}
			completeApply(t, h)
			before := untouchedOf(h)
			_, err := destroyRelaxed(h, test.relaxations, reconciliation.AuthorizationDataLoss)
			reported := diagnostics.Of(err)
			if len(reported) != 1 || !(authorizationRefusal(reported[0].Code) || reported[0].Code == "erase.relaxation-inapplicable") || reported[0].Message != test.message || reported[0].Remediation != test.remedy {
				t.Fatalf("the destroy refused %+v (%v)", reported, err)
			}
			_, err = h.service.Plan(context.Background(), PlanRequest{ContextName: testContextName, Relaxations: test.relaxations})
			if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Message != test.message {
				t.Fatalf("the preview refused %+v (%v)", reported, err)
			}
			before.require(t, h)
			if len(h.capability.destroys) != 0 {
				t.Fatalf("the refused destroy removed %v", h.capability.destroys)
			}
		})
	}
}

func TestARelaxationOverASettledContextRefuses(t *testing.T) {
	t.Parallel()
	h, _ := relaxingHarness(t)
	if _, err := destroyRelaxed(h, map[string]string{"bravo": EraseRetain}); !authorizationRefusal(firstCode(err)) {
		t.Fatalf("a destroy that settles accepted a relaxation: %v", err)
	}
}

// unknownRelaxedRemoval leaves a removal whose first block, charlie, lost its
// outcome, under the relaxations it froze.
func unknownRelaxedRemoval(t *testing.T, h *harness, relaxations map[string]string) string {
	t.Helper()
	completeApply(t, h)
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	if _, err := destroyRelaxed(h, relaxations, reconciliation.AuthorizationDataLoss); err == nil {
		t.Fatal("a removal whose block lost its outcome reported success")
	}
	h.capability.outcomes = nil
	return currentOperation(t, h)
}

func decisionUnder(t *testing.T, h *harness, command string, relaxations map[string]string) (string, error) {
	t.Helper()
	ctx := context.Background()
	var decided operationPlan
	service := h.service.relaxingFor(command, relaxations)
	err := h.workspace.ReadLifecycle(ctx, testContextName, func(view View) error {
		var err error
		decided, _, err = service.decider(nil).decideAs(ctx, view, reconciliation.Destroy, reconciliation.Selection{})
		return err
	})
	if err != nil {
		return "refuse", err
	}
	return decisionNamed(decided), nil
}

func TestAContinuationKeepsExactlyTheRelaxationsItsRemovalFroze(t *testing.T) {
	t.Parallel()
	frozen := map[string]string{"bravo": EraseRetain}
	h, _ := relaxingHarness(t)
	removal := unknownRelaxedRemoval(t, h, frozen)
	if got := relaxationOf(t, currentPlan(t, h), "bravo"); got != EraseRetain {
		t.Fatalf("the removal froze the relaxation %q", got)
	}
	for name, test := range map[string]struct {
		command     string
		relaxations map[string]string
		want        string
	}{
		"the identical set":              {relaxingDestroy, frozen, "continue"},
		"a preview naming none":          {relaxingPlan, nil, "continue"},
		"a destroy naming none":          {relaxingDestroy, nil, "refuse"},
		"a different method":             {relaxingDestroy, map[string]string{"bravo": EraseMetadataClear}, "fresh destroy"},
		"another Machine besides":        {relaxingDestroy, map[string]string{"bravo": EraseRetain, "alpha": EraseRetain}, "fresh destroy"},
		"a preview of the identical set": {relaxingPlan, frozen, "continue"},
	} {
		got, err := decisionUnder(t, h, test.command, test.relaxations)
		if got != test.want || got == "refuse" && !authorizationRefusal(firstCode(err)) {
			t.Errorf("%s: the %s decides %q (%v), want %q", name, test.command, got, err, test.want)
		}
	}
	_, err := destroyRelaxed(h, nil, reconciliation.AuthorizationDataLoss)
	reported := diagnostics.Of(err)
	if len(reported) != 1 || !authorizationRefusal(reported[0].Code) ||
		!strings.Contains(reported[0].Message, "continues only under the relaxations its plan froze (erase=bravo:retain)") ||
		!strings.Contains(reported[0].Remediation, "--authorize erase=bravo:retain") {
		t.Fatalf("a continuation without its relaxation refused %+v (%v)", reported, err)
	}
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
	if _, err := destroyRelaxed(h, frozen, reconciliation.AuthorizationDataLoss); err != nil {
		t.Fatalf("the exact continuation refused: %v", err)
	}
	if current := currentOperation(t, h); current != removal {
		t.Fatalf("the exact continuation registered %s in place of %s", current, removal)
	}
	if seen := destroyedRelaxations(h); seen["bravo"] != EraseRetain {
		t.Fatalf("the continuation executed relaxations %v", seen)
	}
}

func TestANewRelaxationReplacesAnUnprovedRemovalAfterResolvingIt(t *testing.T) {
	t.Parallel()
	h, relaxer := relaxingHarness(t)
	replaced := unknownRelaxedRemoval(t, h, nil)
	h.capability.mutex.Lock()
	h.capability.calls, h.capability.executions = nil, nil
	h.capability.mutex.Unlock()
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
	if _, err := destroyRelaxed(h, map[string]string{"bravo": EraseMetadataClear}, reconciliation.AuthorizationDataLoss); err != nil {
		t.Fatalf("the relaxed replacement refused: %v", err)
	}
	current := currentOperation(t, h)
	if current == replaced {
		t.Fatal("the relaxation continued the removal its plan does not hold")
	}
	if !slices.Contains(relaxer.relaxed, "bravo:metadataClear:") {
		t.Fatalf("the replacement relaxed %v", relaxer.relaxed)
	}
	h.capability.mutex.Lock()
	calls := slices.Clone(h.capability.calls)
	h.capability.mutex.Unlock()
	if len(calls) == 0 || calls[0] != "observe-removal:charlie" {
		t.Fatalf("the replacement did not resolve the unproved block first: %v", calls)
	}
	if seen := destroyedRelaxations(h); seen["bravo"] != EraseMetadataClear || seen["alpha"] != "" {
		t.Fatalf("the replacement executed relaxations %v", seen)
	}
	operation := readOperation(t, h, current)
	if operation.Replaces != replaced {
		t.Fatalf("the replacement names %q as what it replaces, want %s", operation.Replaces, replaced)
	}
}

func TestAFreshRemovalInheritsNoRelaxationButNamesTheOneItReplaces(t *testing.T) {
	t.Parallel()
	h, _ := relaxingHarness(t)
	completeApply(t, h)
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeChanged}, {Outcome: reconciliation.OutcomeFailed}}
	failed, err := destroyRelaxed(h, map[string]string{"bravo": EraseRetain}, reconciliation.AuthorizationDataLoss)
	if err == nil {
		t.Fatal("a removal whose block failed reported success")
	}
	h.capability.outcomes = nil
	if want := "bootwright destroy --context lab --authorize data-loss=" + DigestPlaceholder + " --authorize erase=bravo:retain"; failed == nil || failed.NextCommand != want {
		t.Fatalf("the failed removal names %+v, want %q", failed, want)
	}
	if got := currentNextCommand(t, h); !strings.Contains(got, "--authorize erase=bravo:retain") {
		t.Fatalf("status names %q for the failed relaxed removal", got)
	}
	h.capability.mutex.Lock()
	h.capability.executions = nil
	h.capability.mutex.Unlock()
	if _, err := destroyRelaxed(h, nil, reconciliation.AuthorizationDataLoss); err != nil {
		t.Fatalf("the fresh removal refused: %v", err)
	}
	if seen := destroyedRelaxations(h); seen["bravo"] != "" {
		t.Fatalf("the fresh removal inherited the relaxation %q", seen["bravo"])
	}
	if got := relaxationOf(t, currentPlan(t, h), "bravo"); got != "" {
		t.Fatalf("the fresh removal froze the relaxation %q", got)
	}
}

func currentNextCommand(t *testing.T, h *harness) string {
	t.Helper()
	status, err := h.service.Status(context.Background(), StatusRequest{ContextName: testContextName})
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(status.NextSteps, "\n")
}

func TestOnlyARemovalRelaxationRefusalPassesThroughUnchanged(t *testing.T) {
	t.Parallel()
	h, relaxer := relaxingHarness(t)
	cause := errors.New("the request cannot be relaxed")
	relaxer.refused = map[string]error{EraseRetain: cause}
	completeApply(t, h)
	if _, err := destroyRelaxed(h, map[string]string{"bravo": EraseRetain}, reconciliation.AuthorizationDataLoss); !errors.Is(err, cause) {
		t.Fatalf("the capability's refusal was replaced by %v", err)
	}
}
