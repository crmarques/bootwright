package converge

import (
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func TestReconcileNamesInFlightTasksOfAbandonedRun(t *testing.T) {
	runsDir := t.TempDir()
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	ledger := workflow.NewRunLedger("apply-dead", "clusters", "", workflow.ConcurrencyLimits{}, []workflow.TaskLedgerEntry{
		{ID: "addon.fusion-data-foundation.ocp-prd-01", Kind: "clusterAddon", Label: "install add-on fusion-data-foundation", Status: workflow.TaskStatusRunning},
		{ID: "addon.nmstate.ocp-prd-01", Kind: "clusterAddon", Label: "install add-on nmstate", Status: workflow.TaskStatusPending},
	}, now)
	if err := workflow.SaveRunLedger(runsDir, ledger); err != nil {
		t.Fatalf("SaveRunLedger: %v", err)
	}

	cancelled, err := ReconcileCurrentApplyBeforeMutation(runsDir)
	if err != nil {
		t.Fatalf("ReconcileCurrentApplyBeforeMutation: %v", err)
	}
	if cancelled == nil {
		t.Fatal("expected the abandoned run to be cancelled")
	}
	if len(cancelled.InFlight) != 1 || cancelled.InFlight[0] != "install add-on fusion-data-foundation" {
		t.Fatalf("InFlight = %v, want only the running add-on label", cancelled.InFlight)
	}
}
