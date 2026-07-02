package status

import (
	"testing"

	"github.com/crmarques/bootwright/internal/converge/workflow"
)

// TestLedgerNextStepsDestroyTargetRetriesAsDestroy pins L4: a failed run whose
// ledger target was stamped by destroy ("<stage> destroy") must retry as
// `bootwright destroy` — never a re-apply of what a teardown just failed to
// remove — and keep its stage and cluster scope.
func TestLedgerNextStepsDestroyTargetRetriesAsDestroy(t *testing.T) {
	ledger := workflow.RunLedger{Target: "clusters destroy", Scope: "dc1", Status: workflow.RunStatusFailed}
	steps := LedgerNextSteps(ledger, workflow.RunActivity{}, nil)
	if len(steps) == 0 {
		t.Fatalf("a failed run must emit a retry hint")
	}
	if steps[0] != "bootwright destroy --stage clusters --clusters dc1 --yes" {
		t.Fatalf("destroy target must retry as destroy with its stage/scope, got %q", steps[0])
	}
}

// TestLedgerNextStepsFullDestroyRetriesAsDestroy covers the full-destroy label
// ("all destroy"), which carries no --stage.
func TestLedgerNextStepsFullDestroyRetriesAsDestroy(t *testing.T) {
	ledger := workflow.RunLedger{Target: "all destroy", Status: workflow.RunStatusFailed}
	steps := LedgerNextSteps(ledger, workflow.RunActivity{}, nil)
	if len(steps) == 0 || steps[0] != "bootwright destroy --yes" {
		t.Fatalf("full destroy must retry as `bootwright destroy --yes`, got %v", steps)
	}
}

// TestLedgerNextStepsSubPhaseApplyKeepsStage pins L4's second half: a sub-phase
// apply target threads back through as --stage instead of widening to a full
// apply.
func TestLedgerNextStepsSubPhaseApplyKeepsStage(t *testing.T) {
	ledger := workflow.RunLedger{Target: "machines", Status: workflow.RunStatusFailed}
	steps := LedgerNextSteps(ledger, workflow.RunActivity{}, nil)
	if len(steps) == 0 || steps[0] != "bootwright apply --stage machines --yes" {
		t.Fatalf("sub-phase apply must keep its --stage, got %v", steps)
	}
}

// TestLedgerNextStepsThroughApplyKeepsThrough covers a --through prefix scope,
// which threads back through as --through rather than an invalid --stage.
func TestLedgerNextStepsThroughApplyKeepsThrough(t *testing.T) {
	ledger := workflow.RunLedger{Target: "through-base", Status: workflow.RunStatusFailed}
	steps := LedgerNextSteps(ledger, workflow.RunActivity{}, nil)
	if len(steps) == 0 || steps[0] != "bootwright apply --through base --yes" {
		t.Fatalf("--through apply must retry with --through, got %v", steps)
	}
}

// TestLedgerNextStepsFamilyApplyKeepsStageAndScope guards the pre-existing
// family-stage mapping (infra|clusters) is retained.
func TestLedgerNextStepsFamilyApplyKeepsStageAndScope(t *testing.T) {
	ledger := workflow.RunLedger{Target: "clusters", Scope: "dc1", Status: workflow.RunStatusFailed}
	steps := LedgerNextSteps(ledger, workflow.RunActivity{}, nil)
	if len(steps) == 0 || steps[0] != "bootwright apply --stage clusters --clusters dc1 --yes" {
		t.Fatalf("family apply must keep its --stage and scope, got %v", steps)
	}
}
