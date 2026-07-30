package converge

import (
	"fmt"
	"time"

	"github.com/crmarques/bootwright/internal/converge/workflow"
)

type StaleApplyCancellation struct {
	RunID    string
	Detail   string
	InFlight []string
}

func inFlightTaskLabels(ledger workflow.RunLedger) []string {
	var labels []string
	for _, task := range ledger.Tasks {
		if task.Status != workflow.TaskStatusRunning {
			continue
		}
		label := task.Label
		if label == "" {
			label = task.ID
		}
		labels = append(labels, label)
	}
	return labels
}

func CheckRunRecordsReadable(runsDir string) error {
	if _, _, err := workflow.LoadRunLedger(runsDir); err != nil {
		return unreadableRunRecord(err, workflow.LedgerPath(runsDir))
	}
	if _, _, err := workflow.LoadRunLease(runsDir); err != nil {
		return unreadableRunRecord(err, workflow.LeasePath(runsDir))
	}
	return nil
}

func unreadableRunRecord(err error, path string) error {
	return fmt.Errorf("%w; this file only tracks the progress of the current run — no ownership, convergence, or install evidence lives in it, and finished runs are archived under runs/history/ — so once you have confirmed no bootwright run is active, remove it with `sudo rm -f %s` and re-run", err, path)
}

func CheckCurrentApplyActive(runsDir string) error {
	if err := CheckRunRecordsReadable(runsDir); err != nil {
		return err
	}
	ledger, found, err := workflow.LoadRunLedger(runsDir)
	if err != nil {
		return err
	}
	if !found || !ledger.Active() {
		return nil
	}
	activity, err := workflow.AssessRunActivity(runsDir, ledger, time.Now())
	if err != nil {
		return err
	}
	if activity.State == workflow.RunActivityActive {
		return fmt.Errorf("apply run %s is still running; inspect it with bootwright status --watch", ledger.RunID)
	}
	return nil
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
		inflight := inFlightTaskLabels(ledger)
		cancelled, err := workflow.CancelRunLedger(runsDir, ledger, activity.Detail, now)
		if err != nil {
			return nil, err
		}
		return &StaleApplyCancellation{RunID: cancelled.RunID, Detail: activity.Detail, InFlight: inflight}, nil
	}
	return nil, nil
}
