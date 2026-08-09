package status

import (
	"strings"

	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/host/shellquote"
)

const unavailableLedgerRetryGuidance = "review the failed run and repeat the original mutating invocation from operator history; its exact command was not recorded with a validated context"

func LedgerNextSteps(ledger workflow.RunLedger, activity workflow.RunActivity, existing []string) []string {
	switch ledger.Status {
	case workflow.RunStatusRunning:
		if activity.State == workflow.RunActivityStale {
			return append([]string{ledgerRetryHint(ledger)}, existing...)
		}
		return append([]string{ledgerWatchHint(ledger)}, existing...)
	case workflow.RunStatusFailed:
		hints := []string{ledgerRetryHint(ledger)}
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

func ledgerRetryHint(ledger workflow.RunLedger) string {
	if _, ok := recordedMutatingInvocationContext(ledger.InvocationArgs); !ok {
		return unavailableLedgerRetryGuidance
	}
	return shellquote.QuoteWords(ledger.InvocationArgs)
}

func ledgerWatchHint(ledger workflow.RunLedger) string {
	contextName, ok := recordedMutatingInvocationContext(ledger.InvocationArgs)
	if !ok {
		return "continue monitoring the active run from the explicitly selected context; its exact context was not recorded, so no runnable status command is suggested"
	}
	return shellquote.QuoteWords([]string{"bootwright", "status", "--watch", "--context", contextName})
}

func recordedMutatingInvocationContext(args []string) (string, bool) {
	if len(args) < 4 || args[0] != "bootwright" || (args[1] != "apply" && args[1] != "destroy") {
		return "", false
	}
	contextName := ""
	for i, arg := range args[2:] {
		if arg == "--dry-run" || strings.HasPrefix(arg, "--dry-run=") {
			return "", false
		}
		if arg == "--context" {
			index := i + 3
			if contextName != "" || index >= len(args) || strings.TrimSpace(args[index]) == "" || strings.HasPrefix(args[index], "-") {
				return "", false
			}
			contextName = args[index]
			continue
		}
		if value, ok := strings.CutPrefix(arg, "--context="); ok {
			if contextName != "" || strings.TrimSpace(value) == "" || strings.HasPrefix(value, "-") {
				return "", false
			}
			contextName = value
		}
	}
	return contextName, contextName != ""
}
