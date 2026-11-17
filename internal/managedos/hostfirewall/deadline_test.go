package hostfirewall

import (
	"context"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestEveryCapabilityDeadlineCoversItsFrozenBudgets(t *testing.T) {
	block := firewallBlock(t)
	request, err := DecodeFirewallRequest(block.Request)
	if err != nil {
		t.Fatal(err)
	}
	complete := firewallServiceEvidence(t, block, "complete")
	complete.Postcondition = true
	runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: mustJSON(t, complete)}}
	if _, err := New(runner).Apply(context.Background(), firewallExecution(t, block)); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	deadline := runner.requests[0].Deadline
	if deadline <= time.Duration(request.Budgets.Session)*time.Second || deadline > lifecycle.MaxDeadline {
		t.Fatalf("the run states %s for a %ds session, within %s", deadline, request.Budgets.Session, lifecycle.MaxDeadline)
	}
}
