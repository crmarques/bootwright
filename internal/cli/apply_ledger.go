package cli

import (
	"fmt"
	"io"
	"time"

	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/workflow"
)

func reconcileCurrentApplyBeforeMutation(stdout io.Writer, stateDir string) error {
	ledger, found, err := workflow.LoadRunLedger(stateDir)
	if err != nil {
		return err
	}
	if !found || !ledger.Active() {
		return nil
	}
	now := time.Now()
	activity, err := workflow.AssessRunActivity(stateDir, ledger, now)
	if err != nil {
		return err
	}
	switch activity.State {
	case workflow.RunActivityActive:
		return fmt.Errorf("apply run %s is still running; inspect it with bootwright status --watch", ledger.RunID)
	case workflow.RunActivityStale:
		cancelled, err := workflow.CancelRunLedger(stateDir, ledger, activity.Detail, now)
		if err != nil {
			return err
		}
		cliout.NewContinuation(stdout).Warning("stale apply", fmt.Sprintf("marked %s cancelled: %s", cancelled.RunID, activity.Detail))
	}
	return nil
}
