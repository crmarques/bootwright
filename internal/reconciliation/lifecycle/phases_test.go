package lifecycle

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
)

func (s Service) schedulerOver(tx Transaction, store OperationStore, approved approved, logging *logBoundary, operation operationstore.Operation, plan reconciliation.Plan, states map[string]reconciliation.BlockState, material map[string]secrets.Material, selection reconciliation.Selection) *scheduler {
	return s.newScheduler(tx, store, approved, logging, operation, plan, states, material, selection, s.computePhases(plan), removalPhases{})
}

func decidedDestroy(t *testing.T, h *harness) operationPlan {
	t.Helper()
	ctx := context.Background()
	var decided operationPlan
	err := h.workspace.ReadLifecycle(ctx, testContextName, func(view View) (err error) {
		decided, err = h.service.decide(ctx, view, reconciliation.Destroy, reconciliation.Selection{})
		return err
	})
	if err != nil || decided.verb != reconciliation.Destroy {
		t.Fatalf("destroy decision = %+v (%v)", decided, err)
	}
	return decided
}

func frozenPlan(t *testing.T, h *harness, id string) reconciliation.Plan {
	t.Helper()
	plan, err := lineageReader(h).ReadPlan(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func requireFired(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("the seeded failure did not fire")
	}
}

func TestPhasesMatchRecomputation(t *testing.T) {
	for name, tc := range map[string]struct {
		arrange    func(*testing.T) *harness
		fresh      bool
		replaces   bool
		nontrivial func(removalPhases) bool
		executed   func(*testing.T, *harness)
	}{
		"online removal over a completed apply": {
			arrange: func(t *testing.T) *harness {
				h, _ := onlineHarness(t, true)
				completeApply(t, h)
				return h
			},
			fresh:      true,
			nontrivial: func(p removalPhases) bool { return p.effects["storage"] && p.machines["machine"] },
		},
		"pre-stop removal over a completed apply": {
			arrange: func(t *testing.T) *harness {
				h, _ := preStopHarness(t, false)
				completeApply(t, h)
				return h
			},
			fresh:      true,
			nontrivial: func(p removalPhases) bool { return p.preStop["installation"] && p.preStopMachines["machine"] },
		},
		"pre-stop removal over an incomplete apply": {
			arrange: func(t *testing.T) *harness {
				h, _ := preStopHarness(t, false)
				h.capability.outcomeFor = map[string]Result{"storage": {Outcome: reconciliation.OutcomeFailed}}
				_, err := apply(h)
				requireFired(t, err)
				return h
			},
			fresh:      true,
			nontrivial: func(p removalPhases) bool { return p.preStop["installation"] && p.preStopMachines["machine"] },
		},
		"host-bound retirement": {
			arrange: func(t *testing.T) *harness {
				h, _ := retiringHarness(t, hostedDefinitions(), []string{"rhel-01"})
				completeApply(t, h)
				return h
			},
			fresh:      true,
			nontrivial: func(p removalPhases) bool { return p.retired["storage"] && !p.effects["storage"] },
			executed:   requireRetiredAsFrozen,
		},
		"lineage removal": {
			arrange: func(t *testing.T) *harness {
				h, _ := completedLineage(t)
				return h
			},
			fresh: true,
		},
		"online removal continuation": {
			arrange: func(t *testing.T) *harness {
				h, _ := onlineHarness(t, false)
				completeApply(t, h)
				h.capability.quiescence = map[string]Quiescence{"machine": {State: Live, Reason: "running"}}
				_, err := destroy(h)
				requireFired(t, err)
				return h
			},
			nontrivial: func(p removalPhases) bool { return p.effects["storage"] && p.machines["machine"] },
		},
		"replacement of a failed online removal": {
			arrange: func(t *testing.T) *harness {
				h, _ := onlineHarness(t, false)
				completeApply(t, h)
				h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeChanged}, {Outcome: reconciliation.OutcomeFailed}}
				_, err := destroy(h)
				requireFired(t, err)
				return h
			},
			fresh:      true,
			replaces:   true,
			nontrivial: func(p removalPhases) bool { return len(p.effects) == 0 && len(p.machines) == 0 },
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := tc.arrange(t)
			decided := decidedDestroy(t, h)
			if decided.fresh != tc.fresh {
				t.Fatalf("decision fresh = %v, want %v", decided.fresh, tc.fresh)
			}
			runs := decided.plan
			if !decided.fresh {
				runs = frozenPlan(t, h, decided.operation.ID)
			}
			if want := h.service.computePhases(runs); !reflect.DeepEqual(decided.phases, want) {
				t.Fatalf("decided phases = %+v, want those of the plan the executor runs %+v", decided.phases, want)
			}
			if tc.nontrivial != nil && !tc.nontrivial(decided.phases) {
				t.Fatalf("the fixture's phases are not the ones it exercises: %+v", decided.phases)
			}
			requireReplacedPhases(t, h, decided, tc.replaces)
			if tc.executed != nil {
				tc.executed(t, h)
			}
		})
	}
}

func requireReplacedPhases(t *testing.T, h *harness, decided operationPlan, replaces bool) {
	t.Helper()
	if !replaces {
		if !reflect.DeepEqual(decided.replacedPhases, removalPhases{}) {
			t.Fatalf("a decision that replaces no removal carries replaced phases %+v", decided.replacedPhases)
		}
		return
	}
	if decided.basis.record.Verb != reconciliation.Destroy || decided.basis.operation == "" {
		t.Fatalf("the replacement was planned from %+v, want the failed removal", decided.basis.record)
	}
	want := h.service.computePhases(frozenPlan(t, h, decided.basis.operation))
	if !reflect.DeepEqual(decided.replacedPhases, want) {
		t.Fatalf("replaced phases = %+v, want those of the replaced removal %+v", decided.replacedPhases, want)
	}
	if !want.effects["storage"] || !want.machines["machine"] || reflect.DeepEqual(want, decided.phases) {
		t.Fatalf("the replaced removal's phases %+v do not tell it from the replacement's %+v", want, decided.phases)
	}
}

func requireRetiredAsFrozen(t *testing.T, h *harness) {
	t.Helper()
	before := len(h.capability.executions)
	if result, err := destroy(h); err != nil || result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("retiring removal = %+v (%v)", result, err)
	}
	removal, _ := durableOperation(t, h)
	retired := h.service.computePhases(frozenPlan(t, h, removal.ID)).retired
	flagged := false
	for _, execution := range h.capability.executions[before:] {
		if execution.RetiredWithHosts != retired[execution.Block.ID] {
			t.Fatalf("%s ran with RetiredWithHosts %v, want %v", execution.Block.ID, execution.RetiredWithHosts, retired[execution.Block.ID])
		}
		flagged = flagged || execution.RetiredWithHosts
	}
	if !flagged {
		t.Fatal("no removal execution carried the retirement its frozen plan decides")
	}
}

func TestPhasesAfterAFinalization(t *testing.T) {
	ctx := context.Background()
	h, _ := onlineHarness(t, false)
	completeApply(t, h)
	var decided operationPlan
	err := h.workspace.ReadLifecycle(ctx, testContextName, func(view View) error {
		store := h.service.store(view)
		operation, _ := durableOperation(t, h)
		frozen := frozenPlan(t, h, operation.ID)
		states, attempts, err := blockRecords(ctx, store, operation.ID, frozen)
		if err != nil {
			return err
		}
		marked := operationPlan{finalize: true, verb: reconciliation.Destroy, operation: operation, plan: frozen, states: states}
		marked.basis.attempts = attempts
		decided, err = h.service.afterFinalization(ctx, view, reconciliation.Destroy, reconciliation.Selection{}, marked)
		return err
	})
	if err != nil || !decided.fresh || decided.verb != reconciliation.Destroy {
		t.Fatalf("decision after a finalization = %+v (%v)", decided, err)
	}
	if want := h.service.computePhases(decided.plan); !reflect.DeepEqual(decided.phases, want) || !want.machines["machine"] {
		t.Fatalf("phases after a finalization = %+v, want %+v", decided.phases, want)
	}
}

func TestReplacementObservesTheReplacedRemovalWithItsOwnPhases(t *testing.T) {
	h, c := retiringHarness(t, hostedDefinitions(stagedDefinition("other", reconciliation.StageClusters, "installation")), []string{"rhel-01"})
	completeApply(t, h)
	h.service.options.Concurrency = 1
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeChanged}, {Outcome: reconciliation.OutcomeFailed}}
	_, err := destroy(h)
	requireFired(t, err)
	removal, _ := durableOperation(t, h)
	leaveRunning(t, h, "storage")
	c.online["other"] = true
	want := h.service.computePhases(frozenPlan(t, h, removal.ID)).retired["storage"]
	if own := decidedDestroy(t, h).phases.retired["storage"]; own == want {
		t.Fatalf("the replacement retires storage as the replaced removal does (%v): the fixture cannot tell them apart", want)
	}
	h.capability.outcomes = nil
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
	before := len(h.capability.executions)
	if result, err := destroy(h); err != nil || result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("replacement = %+v (%v)", result, err)
	}
	if !slices.Contains(h.capability.calls, "observe-removal:storage") {
		t.Fatalf("the replaced removal's storage was never observed: %v", h.capability.calls)
	}
	for _, execution := range h.capability.executions[before:] {
		if execution.Block.ID != "storage" {
			continue
		}
		if execution.RetiredWithHosts != want {
			t.Fatalf("the replaced removal's storage was observed with RetiredWithHosts %v, want %v", execution.RetiredWithHosts, want)
		}
		return
	}
	t.Fatal("no storage execution followed the replacement")
}
