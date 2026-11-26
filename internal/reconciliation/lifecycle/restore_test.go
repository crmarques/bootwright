package lifecycle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func restoreWith(t *testing.T, h *harness) *OperationResult {
	t.Helper()
	result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
	if err != nil {
		t.Fatalf("the restoring apply: %+v", diagnostics.Of(err))
	}
	return result
}

// failedRestore trims a completed apply of the chained plan at the clusters
// stage, then restores it with its one added step failing.
func failedRestore(t *testing.T, h *harness) {
	t.Helper()
	completeApply(t, h)
	stagedDestroy(t, h, reconciliation.StageClusters)
	applyChained(t, h, map[string]Result{"charlie": {Outcome: reconciliation.OutcomeFailed}})
}

// runningRestore is a failed restore whose executor died with its block
// running, as the restore's own record then reads.
func runningRestore(t *testing.T, h *harness) {
	t.Helper()
	failedRestore(t, h)
	leaveExecutorDead(t, h, "charlie")
}

// pausedRestore restores two taken-back steps under a stage selection that
// holds only the first, so the restore pauses at the boundary.
func pausedRestore(t *testing.T, h *harness) {
	t.Helper()
	h.capability.definitions = append(chainedDefinitions(), stagedDefinition("delta", reconciliation.StageAddOns, "charlie"))
	completeApply(t, h)
	stagedDestroy(t, h, reconciliation.StageClusters)
	applyChained(t, h, nil, string(reconciliation.StageClusters))
	if current, _ := currentRecord(t, h); current.Restores == "" || current.State != reconciliation.OperationPaused {
		t.Fatalf("the restore under a stage selection left %+v", current)
	}
}

// unknownRestore is a restore whose one added step lost its outcome.
func unknownRestore(t *testing.T, h *harness) {
	t.Helper()
	completeApply(t, h)
	stagedDestroy(t, h, reconciliation.StageClusters)
	applyChained(t, h, map[string]Result{"charlie": {Outcome: reconciliation.OutcomeUnknown}})
}

func ownerOfBlock(t *testing.T, h *harness, operation, block string) string {
	t.Helper()
	store := h.service.store(h.workspace.view())
	record, err := store.ReadOperation(context.Background(), operation)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := ownerIn(context.Background(), store, record, block)
	if err != nil {
		t.Fatal(err)
	}
	return owner
}

// B1858: an apply over a trimmed context registers a restoring apply that
// names the head apply as its predecessor and the staged destroy it restores,
// holds only the step the staged destroy took back, with its edge into the
// kept step frozen as a prerequisite, and runs only that step. The completed
// restore settles a repeated apply, and one whole destroy then takes every
// step back exactly once and ends pristine.
func TestARestoringApplyCreatesOnlyWhatAStagedDestroyTookBack(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newPlannedHarness(t, chainedDefinitions())
	applied := completeApply(t, h)
	if receipt := stagedDestroy(t, h, reconciliation.StageClusters).Receipt; receipt.Next != "apply" {
		t.Fatalf("the completed staged destroy's receipt names next %q, want apply", receipt.Next)
	}
	removal, _ := currentRecord(t, h)
	issued := h.binder.issued
	h.capability.applies = nil
	result := restoreWith(t, h)
	restoring, plan := currentRecord(t, h)
	if restoring.Predecessor != applied || restoring.Restores != removal.ID || !slices.Equal(planIDs(plan), []string{"charlie"}) {
		t.Fatalf("the restore recorded %+v over %v", restoring, planIDs(plan))
	}
	if charlie, _ := plan.Block("charlie"); !slices.Equal(charlie.Prerequisites, []string{"bravo"}) || len(charlie.Dependencies) != 0 {
		t.Fatalf("the restored step froze %+v", charlie.BlockDefinition)
	}
	if !slices.Equal(h.capability.applies, []string{"charlie"}) || result.Receipt.State != "done" {
		t.Fatalf("the restore ran %v and returned %+v", h.capability.applies, result.Receipt)
	}
	if len(restoring.Bindings) != 0 || h.binder.issued != issued || len(removal.Bindings) != 1 {
		t.Fatalf("the restore recorded %v beside %v, issuing %d, want no binding for an added step that names no Secret", restoring.Bindings, removal.Bindings, h.binder.issued-issued)
	}
	if settled := restoreWith(t, h); !settled.Settled {
		t.Fatalf("an apply over the completed restore returned %+v", settled)
	}
	if owned, err := h.service.Ownership(ctx, testContextName); err != nil || owned[blockIdentity(t, h, "charlie")].Verb != "apply" || owned[blockIdentity(t, h, "alpha")].State != "done" {
		t.Fatalf("the restored context owns %v (%v)", owned, err)
	}
	h.capability.destroys = nil
	if err := destroyWith(h); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(h.capability.destroys, []string{"charlie", "bravo", "alpha"}) {
		t.Fatalf("the whole destroy over the restored lineage removed %v", h.capability.destroys)
	}
	pristine, err := projection(reconciliation.Destroy, reconciliation.OperationDone)
	if err != nil || !bytes.Equal(h.workspace.evidence, pristine) {
		t.Fatalf("the whole destroy left %s (%v)", h.workspace.evidence, err)
	}
}

// Decision 4: a re-created step takes the restoring apply's identity, so its
// execution, its owner and its attempt records are the restore's, while a
// kept step stays its creating apply's; its removal presents each identity.
func TestARecreatedStepCarriesTheRestoringApplysIdentity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newPlannedHarness(t, chainedDefinitions())
	applied := completeApply(t, h)
	stagedDestroy(t, h, reconciliation.StageClusters)
	h.capability.executions = nil
	restoreWith(t, h)
	restoring, _ := currentRecord(t, h)
	if len(h.capability.executions) != 1 || h.capability.executions[0].ApplyOperation != restoring.ID || h.capability.executions[0].Operation != restoring.ID {
		t.Fatalf("the re-created step ran as %+v", h.capability.executions)
	}
	if owner := ownerOfBlock(t, h, restoring.ID, "charlie"); owner != restoring.ID {
		t.Fatalf("the re-created step routes to %s", owner)
	}
	if owner := ownerOfBlock(t, h, restoring.ID, "bravo"); owner != applied {
		t.Fatalf("the kept step routes to %s", owner)
	}
	store := h.service.store(h.workspace.view())
	for operation, want := range map[string]int{applied: 1, restoring.ID: 1} {
		if record, err := store.Block(ctx, operation, "charlie"); err != nil || record.Attempts != want || record.State != reconciliation.BlockDone {
			t.Fatalf("the records of charlie under %s read %+v (%v)", operation, record, err)
		}
	}
	h.capability.executions = nil
	if err := destroyWith(h); err != nil {
		t.Fatal(err)
	}
	identities := map[string]string{}
	for _, execution := range h.capability.executions {
		identities[execution.Block.ID] = execution.ApplyOperation
	}
	if identities["charlie"] != restoring.ID || identities["bravo"] != applied || identities["alpha"] != applied {
		t.Fatalf("the removal presented the identities %v", identities)
	}
}

// A restore over a source apply that failed before an add-on started settles
// the next apply: the failed and never-started records of the source are
// history once the restore re-holds them, and nothing reads them unfinished.
func TestARestoreOverAFailedSourceCompletesTheLineage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	definitions := append(chainedDefinitions(), stagedDefinition("delta", reconciliation.StageAddOns, "charlie"))
	h := newPlannedHarness(t, definitions)
	applyChained(t, h, map[string]Result{"charlie": {Outcome: reconciliation.OutcomeFailed}})
	stagedDestroy(t, h, reconciliation.StageClusters)
	removal, _ := currentRecord(t, h)
	if !slices.Equal(removal.Stage.TakenBack, []string{"charlie", "delta"}) {
		t.Fatalf("the staged destroy over the failed apply took back %v", removal.Stage.TakenBack)
	}
	h.capability.outcomeFor = nil
	restoreWith(t, h)
	_, plan := currentRecord(t, h)
	if !slices.Equal(planIDs(plan), []string{"charlie", "delta"}) {
		t.Fatalf("the restore holds %v", planIDs(plan))
	}
	if settled := restoreWith(t, h); !settled.Settled {
		t.Fatalf("an apply over the restored lineage returned %+v", settled)
	}
	status, err := h.service.Status(ctx, StatusRequest{ContextName: testContextName})
	if err != nil || len(status.Contradictions) != 0 || status.Lifecycle.Next != "none" {
		t.Fatalf("status over the restored lineage reads %+v (%v)", status, err)
	}
}

// B1859: a restore admits a changed input only while every step the context
// keeps compares the same. An input that would freeze a kept step differently
// refuses apply.kept-block-changed, naming the floor that takes that step back,
// or the whole destroy when no floor does, as a build that would.
func TestARestoreRefusesWhatItCannotKeep(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newPlannedHarness(t, chainedDefinitions())
	completeApply(t, h)
	stagedDestroy(t, h, reconciliation.StageClusters)
	h.update("rev-edited", desiredstate.Sources{Roots: []string{"/synthetic"}, Files: []desiredstate.SourceFile{
		desiredstate.NewSourceFile("/synthetic/environment.yaml", []byte("kind: Environment\n# edited\n")),
	}})
	h.capability.definitions[1].Request = json.RawMessage(`{"moved":true,"name":"bravo"}`)
	_, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != applyKeptBlockChanged.Code ||
		!strings.Contains(reported[0].Message, "the step bravo") || !strings.Contains(reported[0].Remediation, "take back the whole context") {
		t.Fatalf("a restore that would change a kept service returned %v", diagnostics.Of(err))
	}

	h = stagedHarness(t)
	completeApply(t, h)
	stagedDestroy(t, h, reconciliation.StageMachines)
	h.capability.definitions[1].Request = json.RawMessage(`{"moved":true,"name":"host"}`)
	_, err = h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != applyKeptBlockChanged.Code ||
		reported[0].Remediation != "take it back with bootwright destroy --dry-run --context lab --stage substrates, then apply again" {
		t.Fatalf("a restore that would change a kept host returned %v", err)
	}
	if current, _ := currentRecord(t, h); !trimmed(current) {
		t.Fatalf("a refused restore left %+v current", current)
	}
}

type restorePowerMachine struct {
	*testCapability
	on      map[string]bool
	read    *[]Probe
	deletes map[string]bool
}

func (m *restorePowerMachine) StartPower(_ context.Context, probe Probe) (Quiescence, error) {
	*m.read = append(*m.read, probe)
	if m.on[probe.Block.Object] {
		return Quiescence{State: Live, Reason: "powered on"}, nil
	}
	return Quiescence{State: Quiescent}, nil
}

func (m *restorePowerMachine) DeletesMachine(block reconciliation.Block) bool {
	return m.deletes[block.ID]
}

func (m *restorePowerMachine) RemovesHost(block reconciliation.Block) bool {
	return m.deletes[block.ID]
}

// Decision 3: a restore proves powered off only the Machines it realizes. A
// kept Machine reads in either state, as a continuation reads it, so a
// running kept host admits the restore; a re-created Machine that is still on
// refuses it, and the re-created Machine's probe carries no earlier owner.
func TestARestoreReadsThePowerOfOnlyTheMachinesItRealizes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newHarness(t)
	bindings := claimResolver{}.Bindings()
	machines := claimingCapability("machine-first-v1", "kept", "made")
	machines.definitions[0].Stage, machines.definitions[1].Stage = reconciliation.StageSubstrates, reconciliation.StageMachines
	var read []Probe
	machine := &restorePowerMachine{testCapability: machines, on: map[string]bool{}, read: &read, deletes: map[string]bool{"machine-first-v1-made": true}}
	h.service.capabilities = claimResolver{bindings[0]: machine, bindings[1]: claimingCapability("machine-second-v1"), bindings[2]: claimingCapability("os-install-v1")}
	completeApply(t, h)
	if len(read) != 2 {
		t.Fatalf("the fresh apply read %d Machines", len(read))
	}
	stagedDestroy(t, h, reconciliation.StageMachines)
	if preview, err := h.service.Plan(ctx, PlanRequest{ContextName: testContextName}); err != nil || !slices.Equal(preview.Stops, []string{"made"}) {
		t.Fatalf("the restore's preview names the Machines %v to stop first (%v)", preview, err)
	}
	read, machine.on["kept"], machine.on["made"] = nil, true, true
	if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); firstCode(err) != startPowerRefused.Code {
		t.Fatalf("a restore over a running re-created Machine returned %v", err)
	}
	if len(read) != 1 || read[0].Block.Object != "made" || read[0].ApplyOperation != "" {
		t.Fatalf("the restore read %+v", read)
	}
	read, machine.on["made"] = nil, false
	restoreWith(t, h)
	if len(read) != 1 || read[0].Block.Object != "made" {
		t.Fatalf("the admitted restore read %+v", read)
	}
}

// A restore reserves the whole planned set again, which clears the releasing
// mark the deleted object's claim kept while the context rested trimmed.
func TestARestoreReservesTheWholePlannedSet(t *testing.T) {
	t.Parallel()
	h := newPlannedHarness(t, chainedDefinitions())
	server := func(object string) infrastructureservices.HostReservation {
		return infrastructureservices.HostReservation{Context: testContextName, Kind: "artifact-server", Service: object, Keys: []string{"bridge:br-" + object}}
	}
	h.capability.reservations = []infrastructureservices.HostReservation{server("alpha"), server("charlie")}
	completeApply(t, h)
	stagedDestroy(t, h, reconciliation.StageClusters)
	if !slices.ContainsFunc(h.workspace.reservations, func(reservation infrastructureservices.HostReservation) bool { return reservation.Releasing }) {
		t.Fatalf("the trimmed context holds %+v, want the deleted object's claim marked", h.workspace.reservations)
	}
	restoreWith(t, h)
	want := []infrastructureservices.HostReservation{server("alpha"), server("charlie")}
	if !slices.EqualFunc(h.workspace.reservations, want, func(x, y infrastructureservices.HostReservation) bool { return reflect.DeepEqual(x, y) }) {
		t.Fatalf("the restore holds %+v, want %+v", h.workspace.reservations, want)
	}
}

// A restoring apply that did not complete, failed, running, paused or
// unknown, is continued as an apply; a staged destroy over it takes back what
// it started at its floor with what it never started and keeps the rest, and
// a restore after that completes; and a whole destroy over it removes every
// step the context owns exactly once, never the paused restore's never-started
// delta, which the staged destroy already took back.
func TestAFailedRestoreIsContinuedOrTakenBack(t *testing.T) {
	t.Parallel()
	for name, arrange := range map[string]func(*testing.T, *harness){
		"failed": failedRestore, "running": runningRestore, "paused": pausedRestore, "unknown": unknownRestore,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			removed := []string{"charlie", "bravo", "alpha"}
			if name == "paused" {
				removed = append([]string{"delta"}, removed...)
			}
			h := newPlannedHarness(t, chainedDefinitions())
			arrange(t, h)
			h.capability.observations = slices.Repeat([]Observation{{Effect: reconciliation.EffectNoEffect}}, 4)
			restoring, _ := currentRecord(t, h)
			restoreWith(t, h)
			if current, _ := currentRecord(t, h); current.ID != restoring.ID || current.State != reconciliation.OperationDone {
				t.Fatalf("the continuation left %+v", current)
			}

			h = newPlannedHarness(t, chainedDefinitions())
			arrange(t, h)
			h.capability.observations = slices.Repeat([]Observation{{Effect: reconciliation.EffectNoEffect}}, 4)
			restoring, _ = currentRecord(t, h)
			stagedDestroy(t, h, reconciliation.StageClusters)
			again, _ := currentRecord(t, h)
			if again.Source != restoring.ID || !trimmed(again) || !slices.Equal(again.Stage.TakenBack, slices.Sorted(slices.Values(removed[:len(removed)-2]))) {
				t.Fatalf("the staged destroy over the restore recorded %+v", again)
			}
			restoreWith(t, h)
			h.capability.destroys = nil
			if err := destroyWith(h); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(h.capability.destroys, removed) {
				t.Fatalf("the whole destroy after two trims removed %v", h.capability.destroys)
			}

			h = newPlannedHarness(t, chainedDefinitions())
			arrange(t, h)
			h.capability.observations = slices.Repeat([]Observation{{Effect: reconciliation.EffectNoEffect}}, 4)
			h.capability.destroys = nil
			if err := destroyWith(h); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(h.capability.destroys, []string{"charlie", "bravo", "alpha"}) {
				t.Fatalf("the whole destroy over the restore removed %v", h.capability.destroys)
			}
		})
	}
}

// A preparation over a trimmed context refuses and names the restore, and
// status over the trimmed context offers the restore first and counts the
// bindings the context keeps.
func TestATrimmedContextOffersTheRestore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newPlannedHarness(t, chainedDefinitions())
	completeApply(t, h)
	stagedDestroy(t, h, reconciliation.StageClusters)
	status, err := h.service.Status(ctx, StatusRequest{ContextName: testContextName})
	if err != nil || status.Lifecycle.Next != "apply" || status.Secrets.Bindings != 1 ||
		!slices.Equal(status.NextSteps, []string{"bootwright apply --dry-run --context lab", "bootwright apply --context lab", "bootwright destroy --context lab"}) {
		t.Fatalf("status over the trimmed context reads %+v %+v %d (%v)", status.Lifecycle, status.NextSteps, status.Secrets.Bindings, err)
	}
	preview, err := h.service.Plan(ctx, PlanRequest{ContextName: testContextName})
	if err != nil || preview.Verb != "apply" || preview.Restores == "" || preview.AddsTo != "" || len(preview.Steps) != 1 {
		t.Fatalf("the preview over the trimmed context reads %+v (%v)", preview, err)
	}
}

// A continuation of a restore re-proves each kept prerequisite in its
// creating apply's records, and refuses apply.prerequisite-lost once one no
// longer reads done.
func TestARestoreContinuationRefusesALostPrerequisite(t *testing.T) {
	t.Parallel()
	h := newPlannedHarness(t, chainedDefinitions())
	failedRestore(t, h)
	restoring, _ := currentRecord(t, h)
	h.capability.outcomeFor = nil
	lose(h, restoring.Predecessor+"/blocks/bravo")
	h.capability.applies = nil
	_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
	if reported := diagnostics.Of(err); len(reported) == 0 || reported[0].Code != applyPrerequisiteLost.Code || !strings.Contains(reported[0].Message, "prerequisite bravo") {
		t.Fatalf("the continuation reported %v", err)
	}
	if len(h.capability.applies) != 0 {
		t.Fatalf("a refused continuation ran %v", h.capability.applies)
	}
}

// A restore runs no automatic preparation: it consults each automatic
// preparation as a fresh apply does, proceeds when none needs to run, and
// refuses prepare.refused, naming the Machines and the whole destroy, when one
// would, so no restore installs a physical Machine without its preparation.
func TestARestoreRunsNoAutomaticPreparation(t *testing.T) {
	t.Parallel()
	for name, targets := range map[string][]string{"none is needed": nil, "one is needed": {"node-01"}} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := newPlannedHarness(t, chainedDefinitions())
			needed := false
			h.service.options.Preparations = []PreparationDefinition{{
				Kind: "example-inventory", Command: "machine discover", Bindings: h.service.capabilities.Bindings(),
				Bearing:  func(api.Object) (Bearing, bool) { return Bearing{}, false },
				Validate: func(context.Context, PlanInput, reconciliation.PreparationScope) error { return nil },
				Advance:  func(context.Context, PreparationAdvance) error { return nil },
				Automatic: func(context.Context, PlanInput, reconciliation.Selection) ([]string, error) {
					if needed {
						return targets, nil
					}
					return nil, nil
				},
			}}
			completeApply(t, h)
			stagedDestroy(t, h, reconciliation.StageClusters)
			needed = true
			h.capability.applies = nil
			_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
			current, _ := currentRecord(t, h)
			if len(targets) == 0 {
				if err != nil || current.Restores == "" {
					t.Fatalf("a restore no preparation needs returned %v over %+v", err, current)
				}
				return
			}
			reported := diagnostics.Of(err)
			if firstCode(err) != prepareRefused.Code || !strings.Contains(reported[0].Message, "node-01") || !strings.Contains(reported[0].Remediation, "bootwright destroy --context lab") {
				t.Fatalf("a restore a preparation needs returned %v", reported)
			}
			if !trimmed(current) || len(h.capability.applies) != 0 {
				t.Fatalf("a refused restore left %+v current and ran %v", current, h.capability.applies)
			}
		})
	}
}

// A build that freezes another execution contract refuses the restore at
// once: for a kept step through the lineage's re-proof, and for an added step
// whose implementation only taken-back steps used, because a later removal of
// the lineage could not hold both contracts.
func TestARestoreRefusesAnotherExecutionContract(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct{ moved, message string }{
		"a kept step's":   {"artifact-server-nginx-v1", "does not support the recorded execution contract of block alpha"},
		"an added step's": {"artifact-server-other-v1", "would freeze another execution contract for artifact-server-other-v1"},
	} {
		moved := test.moved
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			definitions := chainedDefinitions()
			definitions[2].Implementation = "artifact-server-other-v1"
			h := newPlannedHarness(t, definitions)
			h.service.capabilities = anyKindResolver{testResolver{capability: h.capability}}
			completeApply(t, h)
			stagedDestroy(t, h, reconciliation.StageClusters)
			h.service.options.Contracts = func(block reconciliation.Block) (ExecutionContract, bool) {
				fingerprint := strings.Repeat("4", 64)
				if block.Implementation == moved {
					fingerprint = strings.Repeat("5", 64)
				}
				return ExecutionContract{ContentDigest: block.ContentDigest, Fingerprint: fingerprint}, true
			}
			_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
			if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != engineOtherBuild.Code || !strings.Contains(reported[0].Message, test.message) {
				t.Fatalf("the restore under another contract returned %v", reported)
			}
			if current, _ := currentRecord(t, h); !trimmed(current) {
				t.Fatalf("a refused restore left %+v current", current)
			}
		})
	}
}

// H1: a staged destroy takes back every step the lineage never started, so a
// never-started step bound to a Machine its scope keeps refuses it as a
// scoped step does, naming the same remedy: the restore would otherwise
// install on a Machine it never proved off. A physical Machine, whose own
// step never deletes it, is always kept.
func TestAStagedDestroyRefusesANeverStartedStepOnAKeptMachine(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		deletes        map[string]bool
		floor          reconciliation.Stage
		reason, remedy string
	}{
		"a virtual Machine": {deletes: map[string]bool{"machine": true}, floor: reconciliation.StageClusters,
			reason: "stay below the clusters stage", remedy: "--stage machines"},
		"a physical Machine": {deletes: map[string]bool{}, floor: reconciliation.StageMachines,
			reason: "a physical Machine is always kept", remedy: "take back the whole context with bootwright destroy --context lab"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			machine := stagedDefinition("machine", reconciliation.StageMachines, "host")
			machine.Kind = "Machine"
			h := newPlannedHarness(t, []reconciliation.BlockDefinition{
				stagedDefinition("alpha", reconciliation.StageInfrastructureServices),
				stagedDefinition("host", reconciliation.StageSubstrates, "alpha"),
				machine,
				stagedDefinition("gate", reconciliation.StageClusters),
				stagedDefinition("service", reconciliation.StageClusters, "gate", "machine"),
			})
			h.service.capabilities = anyKindResolver{testResolver{capability: &deletingTestCapability{testCapability: h.capability, deletes: test.deletes}}}
			applyChained(t, h, nil, string(reconciliation.StageInfrastructureServices), string(reconciliation.StageSubstrates), string(reconciliation.StageMachines))
			applyChained(t, h, map[string]Result{"gate": {Outcome: reconciliation.OutcomeFailed}})
			if failed, _ := currentRecord(t, h); failed.State != reconciliation.OperationFailed {
				t.Fatalf("the apply a staged destroy starts from reads %s", failed.State)
			}
			before := untouchedOf(h)
			_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, Floor: string(test.floor), SkipConfirmation: true})
			reported := diagnostics.Of(err)
			if firstCode(err) != destroyStageKeepsHost.Code || !strings.Contains(reported[0].Message, "the step service runs on Machine/machine") ||
				!strings.Contains(reported[0].Message, test.reason) || !strings.Contains(reported[0].Remediation, test.remedy) {
				t.Fatalf("destroy --stage %s over a never-started step on a kept Machine returned %v", test.floor, reported)
			}
			before.require(t, h)
		})
	}
}

// B1859: a restore re-creates only what a staged destroy took back or the
// lineage never started. A step new to the lineage, here an installation on a
// kept Machine, refuses apply.restore-adds-new naming it before any power read,
// and the preview, status and context update read the same refusal; without
// that step the restore proceeds.
func TestARestoreRefusesAStepNoStagedDestroyTookBack(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newHarness(t)
	bindings := claimResolver{}.Bindings()
	machines := claimingCapability("machine-first-v1", "kept", "made")
	machines.definitions[0].Stage, machines.definitions[1].Stage = reconciliation.StageSubstrates, reconciliation.StageMachines
	var read []Probe
	machine := &restorePowerMachine{testCapability: machines, on: map[string]bool{}, read: &read, deletes: map[string]bool{"machine-first-v1-made": true}}
	installs := claimingCapability("os-install-v1")
	h.service.capabilities = claimResolver{bindings[0]: machine, bindings[1]: claimingCapability("machine-second-v1"), bindings[2]: installs}
	completeApply(t, h)
	stagedDestroy(t, h, reconciliation.StageMachines)
	removal, _ := currentRecord(t, h)
	install := definition("os-install-v1-kept")
	install.Kind, install.Object, install.Implementation, install.Stage = "Machine", "kept", "os-install-v1", reconciliation.StageMachines
	install.Dependencies = []string{"machine-first-v1-kept"}
	installs.definitions = []reconciliation.BlockDefinition{install}
	if preview, err := h.service.Plan(ctx, PlanRequest{ContextName: testContextName}); err != nil || preview.Verb != "destroy" || preview.Restores != "" {
		t.Fatalf("the preview over a new step reads %+v (%v)", preview, err)
	}
	if status, err := h.service.Status(ctx, StatusRequest{ContextName: testContextName}); err != nil || status.Lifecycle.Next != "destroy" {
		t.Fatalf("status over a new step reads %+v (%v)", status, err)
	}
	if kept, err := h.service.KeptChanges(ctx, testContextName, h.workspace.inputs); err != nil || kept.Removal != removal.ID || !slices.Equal(kept.Added, []string{install.ID}) || len(kept.Steps) != 0 {
		t.Fatalf("the kept changes over a new step read %+v (%v)", kept, err)
	}
	read, machine.on["kept"] = nil, true
	_, err := h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != applyRestoreAddsNew.Code || !strings.Contains(reported[0].Message, "the step os-install-v1-kept, which no staged destroy") ||
		!strings.Contains(reported[0].Remediation, "additive apply") || !strings.Contains(reported[0].Remediation, "bootwright destroy --dry-run --context lab") {
		t.Fatalf("a restore adding a new step returned %v", reported)
	}
	if current, _ := currentRecord(t, h); !trimmed(current) || len(read) != 0 {
		t.Fatalf("a refused restore left %+v current and read %+v", current, read)
	}
	installs.definitions = nil
	restoreWith(t, h)
}

// M2: an added consumer of a kept producer borrows from that producer's
// custody entry, which the restore proves before it registers. The consumer
// never started, so the staged destroy took it back without removing it.
func TestARestoreRefusesAKeptProducersMissingCustody(t *testing.T) {
	t.Parallel()
	for name, lost := range map[string]bool{"the entry stands": false, "the entry is lost": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h, c := lendingHarness(t)
			gate := stagedDefinition("gate", reconciliation.StageClusters)
			consumer := h.capability.definitions[2]
			consumer.Stage, consumer.Dependencies = reconciliation.StageClusters, []string{"gate", "producer-b"}
			h.capability.definitions = []reconciliation.BlockDefinition{h.capability.definitions[1], gate, consumer}
			c.references["consumer"] = c.references["consumer"][1:]
			outcomes := h.capability.outcomeFor
			if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true, Stages: []string{string(reconciliation.StageInfrastructureServices)}}); err != nil {
				t.Fatal(err)
			}
			h.capability.outcomeFor["gate"] = Result{Outcome: reconciliation.OutcomeFailed}
			if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
				t.Fatal("the apply whose gate failed reported success")
			}
			outcomes["gate"] = Result{Outcome: reconciliation.OutcomeChanged, Evidence: []byte(`{"ok":true}`)}
			stagedDestroy(t, h, reconciliation.StageClusters)
			if removal, _ := currentRecord(t, h); !slices.Equal(removal.Stage.TakenBack, []string{"consumer", "gate"}) {
				t.Fatalf("the staged destroy took back %v", removal.Stage.TakenBack)
			}
			if _, held := h.binder.produced["producer-b/access"]; !held {
				t.Fatalf("the staged destroy discarded the kept producer's custody: %v", h.binder.produced)
			}
			if lost {
				delete(h.binder.produced, "producer-b/access")
			}
			h.capability.applies = nil
			_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
			current, _ := currentRecord(t, h)
			if !lost {
				if err != nil || current.Restores == "" || !slices.Contains(h.capability.applies, "consumer") {
					t.Fatalf("a restore over the kept producer's custody returned %v over %+v, running %v", diagnostics.Of(err), current, h.capability.applies)
				}
				return
			}
			if reported := diagnostics.Of(err); firstCode(err) != applyProducedInputMissing.Code || !strings.Contains(reported[0].Message, "producer-b/access") {
				t.Fatalf("a restore over a lost custody entry returned %v", reported)
			}
			if !trimmed(current) || len(h.capability.applies) != 0 {
				t.Fatalf("a refused restore left %+v current and ran %v", current, h.capability.applies)
			}
		})
	}
}

// B1859: a changed input that changes only a taken-back step is restored with
// the new request under the restore's identity, which freezes the new input,
// so a repeated apply settles over it; the receipt, status and the preview
// name the restore under any input, since only its comparison decides.
func TestARestoreCarriesAChangedInputInsideItsScope(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newPlannedHarness(t, chainedDefinitions())
	applied := completeApply(t, h)
	stagedDestroy(t, h, reconciliation.StageClusters)
	before, err := h.service.store(h.workspace.view()).ReadPlan(ctx, applied)
	if err != nil {
		t.Fatal(err)
	}
	h.update("rev-edited", desiredstate.Sources{Roots: []string{"/synthetic"}, Files: []desiredstate.SourceFile{
		desiredstate.NewSourceFile("/synthetic/environment.yaml", []byte("kind: Environment\n# edited\n")),
	}})
	h.capability.definitions[2].Request = json.RawMessage(`{"moved":true,"name":"charlie"}`)
	status, err := h.service.Status(ctx, StatusRequest{ContextName: testContextName})
	if err != nil || status.Lifecycle.Next != "apply" ||
		!slices.Equal(status.NextSteps, []string{"bootwright apply --dry-run --context lab", "bootwright apply --context lab", "bootwright destroy --context lab"}) {
		t.Fatalf("status under a changed input reads %+v %+v (%v)", status.Lifecycle, status.NextSteps, err)
	}
	if preview, err := h.service.Plan(ctx, PlanRequest{ContextName: testContextName}); err != nil || preview.Verb != "apply" || preview.Restores == "" {
		t.Fatalf("the preview under a changed input reads %+v (%v)", preview, err)
	}
	h.capability.executions = nil
	restoreWith(t, h)
	restoring, plan := currentRecord(t, h)
	charlie, _ := plan.Block("charlie")
	if restoring.Revision != "rev-edited" || restoring.InputDigest != digestOf(h.workspace.inputs) || string(charlie.Request) != `{"moved":true,"name":"charlie"}` {
		t.Fatalf("the restore froze %+v and %s", restoring, charlie.Request)
	}
	if len(h.capability.executions) != 1 || h.capability.executions[0].ApplyOperation != restoring.ID || ownerOfBlock(t, h, restoring.ID, "bravo") != applied {
		t.Fatalf("the restore ran %+v", h.capability.executions)
	}
	if kept, _ := before.Block("charlie"); kept.RequestDigest == charlie.RequestDigest {
		t.Fatalf("the restore froze the request charlie froze before")
	}
	if settled := restoreWith(t, h); !settled.Settled {
		t.Fatalf("an apply over the changed restore returned %+v", settled)
	}
	h.capability.executions = nil
	if err := destroyWith(h); err != nil {
		t.Fatal(err)
	}
	presented := map[string]string{}
	for _, execution := range h.capability.executions {
		presented[execution.Block.ID] = execution.ApplyOperation + " " + string(execution.Block.Request)
	}
	if presented["charlie"] != restoring.ID+` {"moved":true,"name":"charlie"}` || presented["bravo"] != applied+` {"name":"bravo"}` {
		t.Fatalf("the whole destroy over the changed restore presented %v", presented)
	}
}

// H1 of B1859: an input that drops exactly what the staged destroy took back
// leaves the restore nothing to add, so it refuses apply.restore-empty naming
// the whole destroy and the taken-back steps, never engine.records-damaged;
// status, the preview and context update read the same refusal.
func TestARestoreOfNothingRefusesNamingTheWholeDestroy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newPlannedHarness(t, chainedDefinitions())
	completeApply(t, h)
	stagedDestroy(t, h, reconciliation.StageClusters)
	removal, _ := currentRecord(t, h)
	h.update("rev-edited", desiredstate.Sources{Roots: []string{"/synthetic"}, Files: []desiredstate.SourceFile{
		desiredstate.NewSourceFile("/synthetic/environment.yaml", []byte("kind: Environment\n# dropped charlie\n")),
	}})
	h.capability.definitions = h.capability.definitions[:2]
	status, err := h.service.Status(ctx, StatusRequest{ContextName: testContextName})
	if err != nil || status.Lifecycle.Next != "destroy" ||
		!slices.Equal(status.NextSteps, []string{"bootwright destroy --dry-run --context lab", "bootwright destroy --context lab"}) {
		t.Fatalf("status over an input that drops what was taken back reads %+v %+v (%v)", status.Lifecycle, status.NextSteps, err)
	}
	if preview, err := h.service.Plan(ctx, PlanRequest{ContextName: testContextName}); err != nil || preview.Verb != "destroy" || preview.Restores != "" {
		t.Fatalf("the preview over an input that drops what was taken back reads %+v (%v)", preview, err)
	}
	if kept, err := h.service.KeptChanges(ctx, testContextName, h.workspace.inputs); err != nil || !kept.Empty || kept.Removal != removal.ID || !strings.Contains(kept.Remedy, "bootwright destroy --context lab") {
		t.Fatalf("the kept changes over an input that drops what was taken back read %+v (%v)", kept, err)
	}
	h.capability.applies = nil
	_, err = h.service.Apply(ctx, ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != applyRestoreEmpty.Code ||
		reported[0].Message != "the selected input declares nothing the staged destroy "+removal.ID+" took back, so there is nothing to restore" ||
		!strings.Contains(reported[0].Remediation, "bootwright destroy --dry-run --context lab") || !strings.Contains(reported[0].Remediation, "(charlie)") {
		t.Fatalf("a restore of nothing returned %v", reported)
	}
	if current, _ := currentRecord(t, h); !trimmed(current) || len(h.capability.applies) != 0 {
		t.Fatalf("a refused restore left %+v current and ran %v", current, h.capability.applies)
	}
}

type requestReservingCapability struct{ *testCapability }

func (c requestReservingCapability) Plan(ctx context.Context, input PlanInput) (CapabilityPlan, error) {
	planned, err := c.testCapability.Plan(ctx, input)
	for _, definition := range planned.Definitions {
		digest := sha256.Sum256(definition.Request)
		planned.Reservations = append(planned.Reservations, infrastructureservices.HostReservation{
			Context: testContextName, Kind: "artifact-server", Service: definition.ID, Keys: []string{"bridge:br-" + hex.EncodeToString(digest[:6])},
		})
	}
	return planned, err
}

// L4 of B1859: the restore replaces the held reservations with the new plan's,
// so a kept step that compares the same keeps its keys under a changed input,
// and only the re-created step's claim moves with its new request.
func TestARestoreUnderAChangedInputKeepsTheKeptStepsReservations(t *testing.T) {
	t.Parallel()
	h := newPlannedHarness(t, chainedDefinitions())
	h.service.capabilities = testResolver{capability: requestReservingCapability{h.capability}}
	completeApply(t, h)
	keys := func() map[string][]string {
		held := map[string][]string{}
		for _, reservation := range h.workspace.reservations {
			held[reservation.Service] = reservation.Keys
		}
		return held
	}
	before := keys()
	stagedDestroy(t, h, reconciliation.StageClusters)
	h.update("rev-edited", desiredstate.Sources{Roots: []string{"/synthetic"}, Files: []desiredstate.SourceFile{
		desiredstate.NewSourceFile("/synthetic/environment.yaml", []byte("kind: Environment\n# edited\n")),
	}})
	h.capability.definitions[2].Request = json.RawMessage(`{"moved":true,"name":"charlie"}`)
	restoreWith(t, h)
	after := keys()
	if len(after) != 3 || !slices.Equal(after["alpha"], before["alpha"]) || !slices.Equal(after["bravo"], before["bravo"]) || slices.Equal(after["charlie"], before["charlie"]) {
		t.Fatalf("the restore holds %v, want the kept steps' keys of %v and charlie's moved", after, before)
	}
}

// B1859: context update over a trimmed context names, by the restore's own
// comparison, each kept step a candidate input changes and the destroy that
// takes them back, and nothing for a change only taken-back steps carry.
func TestKeptChangesNamesWhatARestoreWouldRefuse(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := stagedHarness(t)
	completeApply(t, h)
	stagedDestroy(t, h, reconciliation.StageMachines)
	candidate := desiredstate.Sources{Roots: []string{"/synthetic"}, Files: []desiredstate.SourceFile{
		desiredstate.NewSourceFile("/synthetic/environment.yaml", []byte("kind: Environment\n# edited\n")),
	}}
	if kept, err := h.service.KeptChanges(ctx, testContextName, candidate); err != nil || len(kept.Steps) != 0 {
		t.Fatalf("an unchanged plan named %+v (%v)", kept, err)
	}
	h.capability.definitions[1].Request = json.RawMessage(`{"moved":true,"name":"host"}`)
	kept, err := h.service.KeptChanges(ctx, testContextName, candidate)
	removal, _ := currentRecord(t, h)
	if err != nil || kept.Removal != removal.ID || !slices.Equal(kept.Steps, []string{h.capability.definitions[1].ID}) ||
		kept.Remedy != "take it back with bootwright destroy --dry-run --context lab --stage substrates, then apply again" {
		t.Fatalf("a changed kept host named %+v (%v)", kept, err)
	}
	h.capability.planErr = planCapabilityDefect.Fail("the host plan broke", "")
	if kept, err := h.service.KeptChanges(ctx, testContextName, candidate); err != nil || kept.Removal != removal.ID || kept.Unchecked != "the host plan broke" || len(kept.Steps) != 0 {
		t.Fatalf("a comparison that could not plan named %+v (%v)", kept, err)
	}
}

type retainingCapability struct{ *testCapability }

func (c retainingCapability) Removal(ctx context.Context, block reconciliation.Block) (Removal, error) {
	removal, err := c.testCapability.Removal(ctx, block)
	if block.ID == "charlie" {
		removal.Retains = []string{"the data charlie served"}
	}
	return removal, err
}

// L3: the restored member holds every step its staged destroy took back done
// and absent, even one whose removal retains residue, which the staged
// destroy's own fold reads retained. Only the fold's take-back and its
// lingering lives read a step's life, and no removal acts on residue a
// frozen inverse retains, so the residue needs no holder; what the restore
// needs is the take-back, which admits it re-creating the step under its own
// identity, after which one whole destroy removes that step once.
func TestARestoreRecreatesAStepWhoseRemovalRetainedResidue(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newPlannedHarness(t, chainedDefinitions())
	h.service.capabilities = testResolver{capability: retainingCapability{h.capability}}
	completeApply(t, h)
	stagedDestroy(t, h, reconciliation.StageClusters)
	if _, plan := currentRecord(t, h); len(plan.Blocks) != 1 || plan.Blocks[0].Inverse == nil || len(plan.Blocks[0].Inverse.Retains) == 0 {
		t.Fatalf("the staged destroy froze %+v", plan.Blocks)
	}
	h.capability.applies = nil
	restoreWith(t, h)
	restoring, _ := currentRecord(t, h)
	if !slices.Equal(h.capability.applies, []string{"charlie"}) || ownerOfBlock(t, h, restoring.ID, "charlie") != restoring.ID {
		t.Fatalf("the restore ran %v and routes charlie to %s", h.capability.applies, ownerOfBlock(t, h, restoring.ID, "charlie"))
	}
	if owned, err := h.service.Ownership(ctx, testContextName); err != nil || owned[blockIdentity(t, h, "charlie")].Verb != "apply" {
		t.Fatalf("the restored context owns %v (%v)", owned, err)
	}
	h.capability.destroys = nil
	if err := destroyWith(h); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(h.capability.destroys, []string{"charlie", "bravo", "alpha"}) {
		t.Fatalf("the whole destroy over the restored lineage removed %v", h.capability.destroys)
	}
}
