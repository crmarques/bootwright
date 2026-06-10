package status

import (
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func LedgerNextSteps(ledger workflow.RunLedger, activity workflow.RunActivity, existing []string) []string {
	switch ledger.Status {
	case workflow.RunStatusRunning:
		if activity.State == workflow.RunActivityStale {
			return append([]string{ledgerRetryApplyCommand(ledger)}, existing...)
		}
		return append([]string{"bootwright status --watch"}, existing...)
	case workflow.RunStatusFailed:
		hints := []string{ledgerRetryApplyCommand(ledger)}
		for _, task := range ledger.FailedTasks() {
			if task.LogPath != "" {
				hints = append(hints, "inspect "+task.LogPath)
			}
		}
		return append(hints, existing...)
	default:
		return existing
	}
}

func ledgerRetryApplyCommand(ledger workflow.RunLedger) string {
	command := "bootwright apply"
	switch ledger.Target {
	case "infra", "clusters":
		command += " --stage " + ledger.Target
	}
	if ledger.Scope != "" {
		command += " --clusters " + ledger.Scope
	}
	return command + " --yes"
}
