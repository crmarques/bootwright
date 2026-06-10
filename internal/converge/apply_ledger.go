package converge

import (
	"fmt"
	"time"

	"github.com/crmarques/bootwright/internal/converge/workflow"
)

// StaleApplyCancellation reports that a stale in-flight apply ledger was
// marked cancelled before a new mutation; the CLI turns it into the
// user-facing warning.
type StaleApplyCancellation struct {
	RunID  string
	Detail string
}

func ReconcileCurrentApplyBeforeMutation(runsDir string) (*StaleApplyCancellation, error) {
	ledger, found, err := workflow.LoadRunLedger(runsDir)
	if err != nil {
		return nil, err
	}
	if !found || !ledger.Active() {
		return nil, nil
	}
	now := time.Now()
	activity, err := workflow.AssessRunActivity(runsDir, ledger, now)
	if err != nil {
		return nil, err
	}
	switch activity.State {
	case workflow.RunActivityActive:
		return nil, fmt.Errorf("apply run %s is still running; inspect it with bootwright status --watch", ledger.RunID)
	case workflow.RunActivityStale:
		cancelled, err := workflow.CancelRunLedger(runsDir, ledger, activity.Detail, now)
		if err != nil {
			return nil, err
		}
		return &StaleApplyCancellation{RunID: cancelled.RunID, Detail: activity.Detail}, nil
	}
	return nil, nil
}
