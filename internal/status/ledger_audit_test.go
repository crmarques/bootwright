package status

import (
	"testing"

	"github.com/crmarques/bootwright/internal/converge/workflow"
)

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

func TestLedgerNextStepsFullDestroyRetriesAsDestroy(t *testing.T) {
	ledger := workflow.RunLedger{Target: "all destroy", Status: workflow.RunStatusFailed}
	steps := LedgerNextSteps(ledger, workflow.RunActivity{}, nil)
	if len(steps) == 0 || steps[0] != "bootwright destroy --yes" {
		t.Fatalf("full destroy must retry as `bootwright destroy --yes`, got %v", steps)
	}
}

func TestLedgerNextStepsSubPhaseApplyKeepsStage(t *testing.T) {
	ledger := workflow.RunLedger{Target: "machines", Status: workflow.RunStatusFailed}
	steps := LedgerNextSteps(ledger, workflow.RunActivity{}, nil)
	if len(steps) == 0 || steps[0] != "bootwright apply --stage machines --yes" {
		t.Fatalf("sub-phase apply must keep its --stage, got %v", steps)
	}
}

func TestLedgerNextStepsThroughApplyKeepsThrough(t *testing.T) {
	ledger := workflow.RunLedger{Target: "through-base", Status: workflow.RunStatusFailed}
	steps := LedgerNextSteps(ledger, workflow.RunActivity{}, nil)
	if len(steps) == 0 || steps[0] != "bootwright apply --through base --yes" {
		t.Fatalf("--through apply must retry with --through, got %v", steps)
	}
}

func TestLedgerNextStepsFamilyApplyKeepsStageAndScope(t *testing.T) {
	ledger := workflow.RunLedger{Target: "clusters", Scope: "dc1", Status: workflow.RunStatusFailed}
	steps := LedgerNextSteps(ledger, workflow.RunActivity{}, nil)
	if len(steps) == 0 || steps[0] != "bootwright apply --stage clusters --clusters dc1 --yes" {
		t.Fatalf("family apply must keep its --stage and scope, got %v", steps)
	}
}
