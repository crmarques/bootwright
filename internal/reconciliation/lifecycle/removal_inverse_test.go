package lifecycle

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func leavesWith(plan reconciliation.Plan) map[string][]string {
	frozen := map[string][]string{}
	for _, block := range plan.Blocks {
		if block.Inverse != nil && len(block.Inverse.LeavesWith) != 0 {
			frozen[block.ID] = block.Inverse.LeavesWith
		}
	}
	return frozen
}

func flaggedDestroys(c *testCapability, from int) map[string]bool {
	flagged := map[string]bool{}
	for _, execution := range c.executions[from:] {
		flagged[execution.Block.ID] = flagged[execution.Block.ID] || execution.RetiredWithHosts
	}
	return flagged
}

func TestARemovalFreezesWhatEachStepLeavesWithAndHandsThatToItsCapability(t *testing.T) {
	t.Parallel()
	h, _ := retiringHarness(t, hostedDefinitions(), []string{"rhel-01"})
	completeApply(t, h)
	applied := len(h.capability.executions)
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	requireFired(t, func() error { _, err := destroy(h); return err }())
	removal, _ := durableOperation(t, h)
	frozen := leavesWith(frozenPlan(t, h, removal.ID))
	if len(frozen) != 1 || !slices.Equal(frozen["storage"], []string{"machine"}) {
		t.Fatalf("the removal froze leaves-with %v, want storage with machine alone", frozen)
	}
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
	if result, err := destroy(h); err != nil || result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("continuation = %+v (%v)", result, err)
	}
	if flagged := flaggedDestroys(h.capability, applied); !flagged["storage"] || flagged["installation"] || flagged["machine"] || flagged["host"] {
		t.Fatalf("the frozen answer reached %v, want storage alone", flagged)
	}
}

func TestARecomputedLeavesWithThatDiffersRefusesBeforeAnyEffect(t *testing.T) {
	t.Parallel()
	for name, arrange := range map[string]func(*retiringTestCapability){
		"a step this build no longer retires":                          func(c *retiringTestCapability) { c.removes["machine"] = false },
		"a step an earlier build registered without the frozen member": nil,
	} {
		t.Run(name, func(t *testing.T) {
			hosts := []string{"rhel-01"}
			if arrange == nil {
				hosts = nil
			}
			h, capability := retiringHarness(t, hostedDefinitions(), hosts)
			completeApply(t, h)
			h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
			requireFired(t, func() error { _, err := destroy(h); return err }())
			registered, _ := durableOperation(t, h)
			if arrange == nil {
				capability.hosts["storage"] = []string{"rhel-01"}
			} else {
				arrange(capability)
			}
			calls := len(h.capability.calls)
			_, err := destroy(h)
			if firstCode(err) != "destroy.inverse-moved" || len(h.capability.calls) != calls {
				t.Fatalf("a moved answer = %v after calls %v (%v)", diagnostics.Of(err), h.capability.calls[calls:], err)
			}
			reported := diagnostics.Of(err)[0]
			if !strings.Contains(reported.Message, "storage") || !strings.Contains(reported.Remediation, registered.ID) {
				t.Fatalf("refusal = %+v, want the step and the removal named", reported)
			}
			if _, err := h.service.Plan(context.Background(), PlanRequest{ContextName: testContextName}); firstCode(err) != "destroy.inverse-moved" {
				t.Fatalf("the preview of a moved answer = %v", diagnostics.Of(err))
			}
			if after, _ := durableOperation(t, h); after.ID != registered.ID || after.State != registered.State {
				t.Fatalf("the refusal changed the removal from %+v to %+v", registered, after)
			}
		})
	}
}

func TestAFailedRemovalWithoutTheFrozenMemberIsReplacedByOneThatFreezesIt(t *testing.T) {
	t.Parallel()
	h, capability := retiringHarness(t, hostedDefinitions(), nil)
	completeApply(t, h)
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeFailed}}
	requireFired(t, func() error { _, err := destroy(h); return err }())
	failed, _ := durableOperation(t, h)
	if failed.State != reconciliation.OperationFailed || len(leavesWith(frozenPlan(t, h, failed.ID))) != 0 {
		t.Fatalf("seeded removal = %+v", failed)
	}
	capability.hosts["storage"] = []string{"rhel-01"}
	result, err := destroy(h)
	if err != nil || result.Receipt.State != string(reconciliation.OperationDone) || result.Receipt.Operation == failed.ID {
		t.Fatalf("replacement = %+v (%v)", result, err)
	}
	if frozen := leavesWith(frozenPlan(t, h, result.Receipt.Operation)); !slices.Equal(frozen["storage"], []string{"machine"}) {
		t.Fatalf("the replacement froze leaves-with %v", frozen)
	}
}

type unstatedCapability struct{ *testCapability }

func (c unstatedCapability) Removal(ctx context.Context, block reconciliation.Block) (Removal, error) {
	removal, err := c.testCapability.Removal(ctx, block)
	if block.ID == "service" {
		removal.Impacts = nil
	}
	return removal, err
}

func TestARemovalStepThatStatesNoInverseRefusesBeforeRegistration(t *testing.T) {
	t.Parallel()
	h := newHarness(t, "service")
	completeApply(t, h)
	applied, _ := durableOperation(t, h)
	h.service.capabilities = boundResolver{{Kind: "ArtifactServer", Implementation: "artifact-server-nginx-v1"}: unstatedCapability{h.capability}}
	_, err := destroy(h)
	if firstCode(err) != "destroy.inverse-unstated" || !strings.Contains(diagnostics.Of(err)[0].Message, "service") || len(h.capability.destroys) != 0 {
		t.Fatalf("an unstated inverse = %v, removed %v", diagnostics.Of(err), h.capability.destroys)
	}
	if current, _ := durableOperation(t, h); current.ID != applied.ID {
		t.Fatalf("an unstated inverse registered %s", current.ID)
	}
}

type nothingRetiring struct{ *retiringTestCapability }

func (c nothingRetiring) Removal(ctx context.Context, block reconciliation.Block) (Removal, error) {
	removal, err := c.retiringTestCapability.Removal(ctx, block)
	if block.ID == "storage" {
		removal.Impacts, removal.Nothing = nil, "its apply changed nothing"
	}
	return removal, err
}

func TestAStepWithNothingToTakeBackThatLeavesWithItsHostsRefusesBeforeRegistration(t *testing.T) {
	t.Parallel()
	h, c := retiringHarness(t, hostedDefinitions(), []string{"rhel-01"})
	completeApply(t, h)
	applied, _ := durableOperation(t, h)
	h.service.capabilities = boundResolver{
		{Kind: "ArtifactServer", Implementation: "artifact-server-nginx-v1"}: nothingRetiring{c},
		{Kind: "Machine", Implementation: "artifact-server-nginx-v1"}:        machineBinding{c},
	}
	_, err := destroy(h)
	if firstCode(err) != "destroy.inverse-unstated" || !strings.Contains(diagnostics.Of(err)[0].Message, "storage") || len(h.capability.destroys) != 0 {
		t.Fatalf("nothing beside leaves-with = %v, removed %v", diagnostics.Of(err), h.capability.destroys)
	}
	if current, _ := durableOperation(t, h); current.ID != applied.ID {
		t.Fatalf("the refusal registered %s", current.ID)
	}
}

type recordedCapability struct {
	*testCapability
	nothing      map[string]bool
	decidedAtRun bool
	read         []RemovalRecords
}

func (c *recordedCapability) NothingRecorded(block reconciliation.Block, records RemovalRecords) bool {
	c.read = append(c.read, records)
	return c.nothing[block.ID] && records.State == reconciliation.BlockDone
}

func (c *recordedCapability) NothingRemoval(_ context.Context, block reconciliation.Block) (Removal, error) {
	return Removal{Description: "remove nothing of " + block.Object, Nothing: "its apply recorded nothing on " + block.Object}, nil
}

func (c *recordedCapability) DecidedAtRun() bool { return c.decidedAtRun }

func recordedHarness(t *testing.T, nothing map[string]bool) (*harness, *recordedCapability) {
	t.Helper()
	h := newHarness(t, "service", "web")
	h.capability.consumes = map[string][]string{"service": {reconciliation.AuthorizationDataLoss}}
	completeApply(t, h)
	recorded := &recordedCapability{testCapability: h.capability, nothing: nothing}
	h.service.capabilities = boundResolver{{Kind: "ArtifactServer", Implementation: "artifact-server-nginx-v1"}: recorded}
	return h, recorded
}

// pausedRemoval registers a removal whose first step's effect is unknown, so the next destroy continues it.
func pausedRemoval(t *testing.T, h *harness, authorizations ...string) reconciliation.Plan {
	t.Helper()
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true, Authorizations: authorizations})
	requireFired(t, err)
	removal, _ := durableOperation(t, h)
	if removal.Verb != reconciliation.Destroy {
		t.Fatalf("the destroy registered no removal: %+v", removal)
	}
	return frozenPlan(t, h, removal.ID)
}

func TestARemovalFreezesWhatTheApplyRecordsProveAndEveryContinuationProvesIt(t *testing.T) {
	t.Parallel()
	h, recorded := recordedHarness(t, map[string]bool{"service": true})
	preview, err := h.service.Plan(context.Background(), PlanRequest{ContextName: testContextName, Verb: reconciliation.Destroy})
	if err != nil {
		t.Fatal(err)
	}
	index := slices.IndexFunc(preview.Steps, func(step PlanStep) bool { return step.ID == "service" })
	if index < 0 || preview.Steps[index].Nothing != "its apply recorded nothing on service" || len(preview.Steps[index].Consumes) != 0 || len(preview.Steps[index].Impacts) != 0 {
		t.Fatalf("the preview states %+v, want nothing to take back and no data-loss", preview.Steps)
	}
	frozen := pausedRemoval(t, h)
	service, _ := frozen.Block("service")
	web, _ := frozen.Block("web")
	if !TakesNothingBack(service) || len(service.Consumes) != 0 || TakesNothingBack(web) || len(web.Impacts) == 0 {
		t.Fatalf("the removal froze service %+v and web %+v", service.BlockDefinition, web.BlockDefinition)
	}
	read := len(recorded.read)
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
	result, err := destroy(h)
	if err != nil || result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("the continuation = %+v, %v (%v)", result, diagnostics.Of(err), err)
	}
	if len(recorded.read) == read || !slices.Contains(h.capability.destroys, "service") {
		t.Fatalf("the continuation read the records %d times and destroyed %v; a step with nothing to take back still calls its capability", len(recorded.read)-read, h.capability.destroys)
	}
}

func TestARecordedAnswerThatMovedRefusesBeforeAnyEffect(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		planned, now map[string]bool
		decidedAtRun bool
		refuses      bool
	}{
		"records no longer prove nothing":                   {planned: map[string]bool{"service": true}, now: map[string]bool{}, refuses: true},
		"records now prove nothing":                         {planned: map[string]bool{}, now: map[string]bool{"service": true}, refuses: true},
		"records settle to nothing for a run-time decision": {planned: map[string]bool{}, now: map[string]bool{"service": true}, decidedAtRun: true},
	} {
		t.Run(name, func(t *testing.T) {
			h, recorded := recordedHarness(t, tc.planned)
			recorded.decidedAtRun = tc.decidedAtRun
			var authorizations []string
			if !tc.planned["service"] {
				authorizations = []string{reconciliation.AuthorizationDataLoss}
			}
			pausedRemoval(t, h, authorizations...)
			registered, _ := durableOperation(t, h)
			recorded.nothing = tc.now
			calls := len(h.capability.calls)
			h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
			result, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true, Authorizations: authorizations})
			if !tc.refuses {
				if err != nil || result.Receipt.State != string(reconciliation.OperationDone) {
					t.Fatalf("the continuation = %+v, %v (%v)", result, diagnostics.Of(err), err)
				}
				return
			}
			if firstCode(err) != "destroy.inverse-moved" || len(h.capability.calls) != calls {
				t.Fatalf("a moved answer = %v after calls %v (%v)", diagnostics.Of(err), h.capability.calls[calls:], err)
			}
			if reported := diagnostics.Of(err)[0]; !strings.Contains(reported.Message, "service") || !strings.Contains(reported.Remediation, registered.ID) {
				t.Fatalf("refusal = %+v, want the step and the removal named", reported)
			}
			if _, err := h.service.Plan(context.Background(), PlanRequest{ContextName: testContextName}); firstCode(err) != "destroy.inverse-moved" {
				t.Fatalf("the preview of a moved answer = %v", diagnostics.Of(err))
			}
			if after, _ := durableOperation(t, h); after.ID != registered.ID || after.State != registered.State {
				t.Fatalf("the refusal changed the removal from %+v to %+v", registered, after)
			}
		})
	}
}

func TestARelaxationOfAStepWithNothingToTakeBackIsUnneeded(t *testing.T) {
	t.Parallel()
	h, _ := recordedHarness(t, map[string]bool{"service": true})
	_, err := h.service.Plan(context.Background(), PlanRequest{ContextName: testContextName, Verb: reconciliation.Destroy, Relaxations: map[string]string{"service": EraseRetain}})
	if reported := diagnostics.Of(err); len(reported) == 0 || reported[0].Code != "authorize.unneeded" || !strings.Contains(reported[0].Message, "records prove it has nothing to take back") {
		t.Fatalf("a relaxation of a step with nothing to take back = %+v (%v)", reported, err)
	}
}
