package status

import (
	"strings"

	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func LedgerNextSteps(ledger workflow.RunLedger, activity workflow.RunActivity, existing []string) []string {
	switch ledger.Status {
	case workflow.RunStatusRunning:
		if activity.State == workflow.RunActivityStale {
			return append([]string{ledgerRetryCommand(ledger)}, existing...)
		}
		return append([]string{"bootwright status --watch"}, existing...)
	case workflow.RunStatusFailed:
		hints := []string{ledgerRetryCommand(ledger)}
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

func ledgerRetryCommand(ledger workflow.RunLedger) string {
	command := "bootwright apply"
	if stage, ok := ledgerDestroyStage(ledger.Target); ok {
		command = "bootwright destroy"
		if stage != "" {
			command += " --stage " + stage
		}
	} else if through, ok := strings.CutPrefix(ledger.Target, "through-"); ok {
		command += " --through " + through
	} else if stage, through, ok := ledgerApplyRange(ledger.Target); ok {
		command += " --stage " + stage + " --through " + through
	} else if ledgerTargetIsApplyStage(ledger.Target) {
		command += " --stage " + ledger.Target
	}
	if ledger.Scope != "" {
		command += " --clusters " + ledger.Scope
	}
	return command + " --yes"
}

func ledgerDestroyStage(target string) (string, bool) {
	fields := strings.Fields(target)
	destroy := false
	for _, f := range fields {
		if f == "destroy" {
			destroy = true
			break
		}
	}
	if !destroy {
		return "", false
	}
	if len(fields) > 0 {
		switch fields[0] {
		case "infra", "clusters":
			return fields[0], true
		}
	}
	return "", true
}

func ledgerApplyRange(target string) (stage, through string, ok bool) {
	rem, found := strings.CutPrefix(target, "range-")
	if !found {
		return "", "", false
	}
	i := strings.Index(rem, "-")
	if i <= 0 || i >= len(rem)-1 {
		return "", "", false
	}
	return rem[:i], rem[i+1:], true
}

func ledgerTargetIsApplyStage(target string) bool {
	switch target {
	case "infra", "clusters", "fabric", "machines", "deps", "base", "add-ons":
		return true
	}
	return false
}
